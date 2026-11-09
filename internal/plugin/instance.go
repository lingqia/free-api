// Package plugin 实现站点插件的 stdio JSON-RPC 宿主。
//
// 插件是独立可执行文件，网关通过子进程 stdin/stdout 与其通信，
// 每行一个 JSON 对象。选 stdio 而非本地 HTTP 端口，是为了避免端口冲突、
// 简化生命周期管理，并让插件可用任意语言实现。
package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// 插件方法名。
const (
	MethodModels = "models"
	MethodChat   = "chat"
	MethodPing   = "ping"
)

// 事件方法名：插件通过这个 method 回调网关推送流式事件。
const MethodEvent = "event"

// rpcRequest 是发给插件的 JSON-RPC 请求。
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// rpcResponse 是插件的应答。插件可能以两种形态回应：
// 一是「请求-响应」（带 id，result 为最终结果），
// 二是「流式事件」（method=event，携带 params 事件体）。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcError 是 JSON-RPC 错误体。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ModelInfo 是插件上报的单个模型信息。
type ModelInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	ContextLen   int      `json:"context_length,omitempty"`
}

// EventPayload 是插件推送的统一事件。
type EventPayload struct {
	Type      string     `json:"type"`
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     *Usage     `json:"usage,omitempty"`
	Error     string     `json:"error,omitempty"`
	Finish    string     `json:"finish,omitempty"`
}

// ToolCall 是插件侧的工具调用结构。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Usage 是插件侧的 token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Instance 是一个运行中的插件进程。
type Instance struct {
	Name    string
	Command string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	writeMu  sync.Mutex
	pendMu   sync.Mutex
	pending  map[string]chan *rpcResponse
	seq      int64
	models   []ModelInfo
	healthy  atomic.Bool
	closed   atomic.Bool
	lastErr  atomic.Value
	started  time.Time
	stopOnce sync.Once
}

// Start 启动插件进程并完成首次模型能力探测。
func Start(ctx context.Context, name, command string, args, env []string, dir string) (*Instance, error) {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(cmd.Env, env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	// 插件 stderr 直接转给网关日志，便于排查。
	cmd.Stderr = newLogWriter(name)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动插件进程失败: %w", err)
	}

	inst := &Instance{
		Name:    name,
		Command: command,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewScanner(stdout),
		pending: map[string]chan *rpcResponse{},
		started: time.Now(),
	}
	inst.stdout.Buffer(make([]byte, 0, 64*1024), 16<<20)
	inst.healthy.Store(true)

	go inst.readLoop()
	go func() {
		err := cmd.Wait()
		inst.healthy.Store(false)
		if err != nil && !inst.closed.Load() {
			inst.lastErr.Store(err.Error())
			slog.Warn("插件进程退出", "plugin", name, "err", err)
		}
		// 进程退出时唤醒所有等待者，避免调用方永久阻塞。
		inst.failPending("插件进程已退出")
	}()

	// 首次握手：确认插件可响应并拿到模型清单。
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := inst.call(probeCtx, MethodModels, map[string]any{})
	if err != nil {
		inst.Stop()
		return nil, fmt.Errorf("插件 %s 模型探测失败: %w", name, err)
	}
	if len(res) > 0 {
		var payload struct {
			Models []ModelInfo `json:"models"`
		}
		if err := json.Unmarshal(res, &payload); err == nil {
			inst.models = payload.Models
		}
	}

	return inst, nil
}

// Models 返回插件上报的模型清单。
func (i *Instance) Models() []ModelInfo { return i.models }

// Healthy 返回插件当前是否可用。
func (i *Instance) Healthy() bool { return i.healthy.Load() && !i.closed.Load() }

// LastError 返回最近一次失败原因。
func (i *Instance) LastError() string {
	if v, ok := i.lastErr.Load().(string); ok {
		return v
	}
	return ""
}

// Uptime 返回进程运行时长。
func (i *Instance) Uptime() time.Duration { return time.Since(i.started) }

// Chat 发起一次对话请求，返回事件通道。调用方需持续读取直到通道关闭。
// 事件既可能来自插件的流式推送，也可能来自插件最终应答里的汇总结果。
func (i *Instance) Chat(ctx context.Context, params any) (<-chan EventPayload, error) {
	if !i.Healthy() {
		return nil, fmt.Errorf("插件 %s 不可用: %s", i.Name, i.LastError())
	}
	id := i.nextID()
	ch := make(chan EventPayload, 64)

	// 注册流式事件路由
	i.pendMu.Lock()
	streamCh := make(chan *rpcResponse, 256)
	i.pending["stream:"+id] = streamCh
	i.pendMu.Unlock()

	go func() {
		defer close(ch)
		defer func() {
			i.pendMu.Lock()
			delete(i.pending, "stream:"+id)
			i.pendMu.Unlock()
		}()

		req := rpcRequest{JSONRPC: "2.0", ID: id, Method: MethodChat, Params: params}
		if err := i.write(req); err != nil {
			ch <- EventPayload{Type: "error", Error: fmt.Sprintf("向插件写入失败: %v", err)}
			return
		}

		// 同时等待最终应答与流式事件。
		finalCh := make(chan *rpcResponse, 1)
		i.pendMu.Lock()
		i.pending[id] = finalCh
		i.pendMu.Unlock()
		defer func() {
			i.pendMu.Lock()
			delete(i.pending, id)
			i.pendMu.Unlock()
		}()

		sawToolCall := false
		sawDone := false
		for {
			select {
			case <-ctx.Done():
				ch <- EventPayload{Type: "error", Error: "请求超时或被取消"}
				return

			case resp, ok := <-streamCh:
				if !ok {
					return
				}
				var ev EventPayload
				if err := json.Unmarshal(resp.Params, &ev); err != nil {
					continue
				}
				if ev.Type == "tool_call" {
					sawToolCall = true
				}
				if ev.Type == "done" && sawToolCall && ev.Finish == "" {
					ev.Finish = "tool_calls"
				}
				ch <- ev
				// done 与 error 是流结束标记：插件推完即结束本次调用，
				// 不再等待最终应答，否则会一直阻塞到超时。
				if ev.Type == "done" || ev.Type == "error" {
					sawDone = true
					return
				}

			case resp, ok := <-finalCh:
				if !ok {
					return
				}
				if resp.Error != nil {
					ch <- EventPayload{Type: "error", Error: resp.Error.Message}
					return
				}
				// 插件可以用最终应答直接返回完整结果（非流式），
				// 但如果已经推过 done，就不再重复追加，避免重复 done 帧。
				if evs := decodeFinalResult(resp.Result); len(evs) > 0 && !sawDone {
					for _, ev := range evs {
						if ev.Type == "tool_call" {
							sawToolCall = true
						}
						if ev.Type == "done" && sawToolCall && ev.Finish == "" {
							ev.Finish = "tool_calls"
						}
						ch <- ev
					}
				}
				return
			}
		}
	}()

	return ch, nil
}

// decodeFinalResult 解析插件最终应答。支持两种形态：
// 一是事件数组；二是 {content, tool_calls, usage} 汇总对象。
func decodeFinalResult(raw json.RawMessage) []EventPayload {
	if len(raw) == 0 {
		return nil
	}
	var events []EventPayload
	if err := json.Unmarshal(raw, &events); err == nil && len(events) > 0 {
		return events
	}
	var summary struct {
		Content  string     `json:"content"`
		ToolCalls []ToolCall `json:"tool_calls"`
		Usage    *Usage     `json:"usage"`
		Error    string     `json:"error"`
		Finish   string     `json:"finish"`
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		return nil
	}
	out := []EventPayload{}
	if summary.Content != "" {
		out = append(out, EventPayload{Type: "delta", Text: summary.Content})
	}
	if len(summary.ToolCalls) > 0 {
		out = append(out, EventPayload{Type: "tool_call", ToolCalls: summary.ToolCalls})
	}
	if summary.Usage != nil {
		out = append(out, EventPayload{Type: "usage", Usage: summary.Usage})
	}
	if summary.Error != "" {
		out = append(out, EventPayload{Type: "error", Error: summary.Error})
		return out
	}
	out = append(out, EventPayload{Type: "done", Finish: summary.Finish})
	return out
}

// call 发送请求并等待最终应答。
func (i *Instance) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if !i.Healthy() {
		return nil, fmt.Errorf("插件 %s 不可用", i.Name)
	}
	id := i.nextID()
	ch := make(chan *rpcResponse, 1)
	i.pendMu.Lock()
	i.pending[id] = ch
	i.pendMu.Unlock()
	defer func() {
		i.pendMu.Lock()
		delete(i.pending, id)
		i.pendMu.Unlock()
	}()

	if err := i.write(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("插件无应答")
		}
		if resp.Error != nil {
			return nil, errors.New(resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// write 向插件 stdin 写入一行 JSON。
func (i *Instance) write(req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	if _, err := i.stdin.Write(append(data, '\n')); err != nil {
		i.healthy.Store(false)
		return fmt.Errorf("写入插件 stdin 失败: %w", err)
	}
	return nil
}

// readLoop 持续读取插件 stdout 并按 id 分发应答。
func (i *Instance) readLoop() {
	for i.stdout.Scan() {
		line := i.stdout.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			slog.Debug("插件输出非 JSON，已忽略", "plugin", i.Name, "raw", string(line))
			continue
		}

		// 流式事件：method=event，按请求 id 路由到 stream 通道。
		if resp.Method == MethodEvent {
			i.pendMu.Lock()
			target := i.pending["stream:"+resp.ID]
			i.pendMu.Unlock()
			if target != nil {
				select {
				case target <- &resp:
				default:
					// 消费端跟不上时丢弃事件，避免插件被杀。
					slog.Debug("事件缓冲已满，丢弃一帧", "plugin", i.Name)
				}
			}
			continue
		}

		i.pendMu.Lock()
		target := i.pending[resp.ID]
		i.pendMu.Unlock()
		if target != nil {
			select {
			case target <- &resp:
			default:
			}
		}
	}
}

// failPending 在进程退出时唤醒所有等待者。
func (i *Instance) failPending(reason string) {
	i.pendMu.Lock()
	defer i.pendMu.Unlock()
	for key, ch := range i.pending {
		select {
		case ch <- &rpcResponse{Error: &rpcError{Code: -32000, Message: reason}}:
		default:
		}
		delete(i.pending, key)
	}
}

// nextID 生成请求 id。
func (i *Instance) nextID() string {
	n := atomic.AddInt64(&i.seq, 1)
	return fmt.Sprintf("%s-%d", i.Name, n)
}

// Stop 关闭插件进程。
func (i *Instance) Stop() {
	i.stopOnce.Do(func() {
		i.closed.Store(true)
		i.healthy.Store(false)
		_ = i.stdin.Close()
		if i.cmd.Process != nil {
			_ = i.cmd.Process.Kill()
		}
	})
}

// logWriter 把插件 stderr 接入网关日志。
type logWriter struct{ name string }

func newLogWriter(name string) *logWriter { return &logWriter{name: name} }

// Write 实现 io.Writer。
func (l *logWriter) Write(p []byte) (int, error) {
	slog.Debug("插件 stderr", "plugin", l.name, "msg", string(p))
	return len(p), nil
}

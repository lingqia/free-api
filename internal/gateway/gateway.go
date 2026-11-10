// Package gateway 负责请求路由、能力过滤、失败回退与对外 HTTP 端点。
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"free-api/internal/credential"
	"free-api/internal/event"
	"free-api/internal/plugin"
	"free-api/internal/protocol"
)

// ErrNoCandidate 表示没有满足条件的插件可路由。
var ErrNoCandidate = errors.New("没有可用插件")

// Gateway 是网关运行时。
type Gateway struct {
	Registry *plugin.Registry
	Creds    *credential.Store

	// 每插件并发信号量
	semMu sync.Mutex
	sems  map[string]chan struct{}

	// 每插件令牌桶
	bucketMu sync.Mutex
	buckets  map[string]*bucket

	timeout time.Duration
	retries int
	// semSize 是每插件并发上限。
	semSize int
	// defaultRPS 是插件未单独声明时的限流速率。
	defaultRPS float64
	// settings 保存控制台密码与网关 API Key。
	settings *Settings
	// pluginRPS 是插件级限流覆盖。
	pluginRPS map[string]float64

	// 运行统计（内存态，进程重启后归零）。
	statsMu sync.Mutex
	stats   map[string]*PluginStats

	// 网关 API Key 使用统计（内存态）。
	keyMu  sync.Mutex
	keyUse map[string]KeyUsage
}

// New 构造网关运行时。
func New(reg *plugin.Registry, creds *credential.Store, cfg Config) *Gateway {
	settings := cfg.Settings
	if settings == nil {
		settings = newMemorySettings(cfg.APIKeys)
	}
	g := &Gateway{
		Registry:  reg,
		Creds:     creds,
		sems:      map[string]chan struct{}{},
		buckets:   map[string]*bucket{},
		timeout:   cfg.Timeout,
		retries:   cfg.Retries,
		settings:  settings,
		pluginRPS: cfg.PluginRPS,
		stats:     map[string]*PluginStats{},
		keyUse:    map[string]KeyUsage{},
	}
	if g.timeout <= 0 {
		g.timeout = 180 * time.Second
	}
	g.semSize = cfg.PerPluginConcurrency
	if g.semSize <= 0 {
		g.semSize = 2
	}
	g.defaultRPS = cfg.PerPluginRPS
	if g.defaultRPS <= 0 {
		g.defaultRPS = 1
	}
	return g
}

// Config 是网关运行参数。
type Config struct {
	Timeout              time.Duration
	Retries              int
	PerPluginConcurrency int
	PerPluginRPS         float64
	APIKeys              []string
	// Settings 是控制台密码与 API Key 的持久化设置；为空时使用 APIKeys 构造内存设置。
	Settings *Settings
	// PluginRPS 是插件级限流覆盖：插件名 → 每秒请求数。
	// 用于对齐各站点的真实免费配额（例如「每 45 秒 1 次」）。
	PluginRPS map[string]float64
}

// Route 描述一次路由决策的结果。
type Route struct {
	Plugin    *plugin.Instance
	Upstream  string // 上游模型 ID（插件内部使用的模型名）
	ToolLevel string // native / builtin / none
}

// Resolve 选择目标插件。模型 ID 支持两种形式：
//   - `插件/模型`：直接锁定到该插件，内部只把斜杠后的模型名传给插件；
//   - 纯模型名：在所有提供该模型的插件里按优先级故障转移。
//
// 先按完整模型名匹配，避免把 meta-llama/xxx 这类本身带斜杠的模型误拆。
// 带函数工具的请求只考虑 tools=native 的插件。
func (g *Gateway) Resolve(model string, needTools bool) ([]Route, error) {
	if candidates := g.Registry.Candidates(model, needTools); len(candidates) > 0 {
		routes := make([]Route, 0, len(candidates))
		for _, inst := range candidates {
			routes = append(routes, Route{
				Plugin:    inst,
				Upstream:  model,
				ToolLevel: g.Registry.ToolsLevel(inst.Name),
			})
		}
		return routes, nil
	}

	if pluginName, upstream, ok := g.Registry.SplitQualified(model); ok {
		inst, exists := g.Registry.Get(pluginName)
		if !exists || !inst.Healthy() {
			return nil, fmt.Errorf("%w: 插件 %s 未运行", ErrNoCandidate, pluginName)
		}
		if !g.Registry.ServesModel(pluginName, upstream) {
			return nil, fmt.Errorf("%w: 插件 %s 不提供模型 %s", ErrNoCandidate, pluginName, upstream)
		}
		level := g.Registry.ToolsLevel(pluginName)
		if needTools && level != "native" {
			return nil, fmt.Errorf("%w: 插件 %s 不支持函数工具", ErrNoCandidate, pluginName)
		}
		if !g.Registry.AuthReady(pluginName) {
			return nil, fmt.Errorf("%w: 插件 %s 缺少必需凭证", ErrNoCandidate, pluginName)
		}
		return []Route{{Plugin: inst, Upstream: upstream, ToolLevel: level}}, nil
	}

	if needTools {
		return nil, fmt.Errorf("%w: 模型 %s 在支持工具调用的站点上没有可用来源（模型 ID 可用 插件/模型 形式）", ErrNoCandidate, model)
	}
	return nil, fmt.Errorf("%w: 模型 %s 没有已配置且健康的来源（模型 ID 可用 插件/模型 形式）", ErrNoCandidate, model)
}

// Dispatch 向候选路由依次尝试，返回第一个成功的统一事件通道。
// 每次尝试都有自己的超时与重试配额；任一候选失败会自动落到下一个。
func (g *Gateway) Dispatch(ctx context.Context, routes []Route, req *protocol.Request) (<-chan event.Event, error) {
	if len(routes) == 0 {
		return nil, ErrNoCandidate
	}

	var lastErr error
	for _, route := range routes {
		events, err := g.attempt(ctx, route, req)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", route.Plugin.Name, err)
			slog.Warn("插件尝试失败，切换下一候选", "plugin", route.Plugin.Name, "err", err)
			continue
		}
		return events, nil
	}
	if lastErr == nil {
		lastErr = ErrNoCandidate
	}
	return nil, lastErr
}

// attempt 对单个插件发起调用，把插件事件翻译成网关统一事件。
func (g *Gateway) attempt(ctx context.Context, route Route, req *protocol.Request) (<-chan event.Event, error) {
	inst := route.Plugin

	// 限流：等待令牌桶
	if err := g.acquireRate(ctx, inst.Name); err != nil {
		return nil, err
	}
	// 并发信号量
	release, err := g.acquireSem(ctx, inst.Name)
	if err != nil {
		return nil, err
	}

	// 组装插件请求参数：模型、消息、工具、凭证。
	// 插件只看到斜杠后的上游模型名，不感知插件前缀。
	params := map[string]any{
		"model":       route.Upstream,
		"system":      req.System,
		"messages":    req.Messages,
		"tools":       req.Tools,
		"tool_choice": req.ToolChoice,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
		"top_p":       req.TopP,
		"stop":        req.Stop,
	}
	if len(req.BuiltinTools) > 0 {
		params["builtin_tools"] = req.BuiltinTools
	}

	// 按插件声明的字段注入凭证（插件看不到未声明的字段）。
	// 所有站点都额外接受可选的 proxy：站点独立代理优先，否则从全局代理池轮换。
	auth := g.Registry.AuthOf(inst.Name)
	fields := append([]string{}, auth.RequiredFields...)
	fields = append(fields, auth.OptionalFields...)
	fields = appendUniqueString(fields, "proxy")
	if len(fields) > 0 && g.Creds != nil {
		creds := g.Creds.CollectOne(inst.Name, fields)
		if _, ok := creds["proxy"]; !ok {
			if proxy, ok := g.Creds.Pick(proxyPoolPlugin, "proxy"); ok {
				creds["proxy"] = proxy
			}
		}
		if len(creds) > 0 {
			params["credential"] = creds
		}
	}

	g.statsStart(inst.Name)
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
	raw, err := inst.Chat(callCtx, params)
	if err != nil {
		cancel()
		release()
		g.statsEnd(inst.Name, false, nil)
		return nil, err
	}

	out := make(chan event.Event, 64)
	go func() {
		defer close(out)
		defer cancel()
		defer release()

		ok := false
		sawError := false
		var usage *event.Usage
		defer func() { g.statsEnd(inst.Name, ok, usage) }()

		for payload := range raw {
			ev := translate(payload)
			if ev.Usage != nil {
				usage = ev.Usage
			}
			if ev.Type == event.TypeError {
				sawError = true
				ok = false
			}
			if ev.Type == event.TypeDone && !sawError {
				ok = true
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

// translate 把插件事件转换为网关统一事件。
func translate(p plugin.EventPayload) event.Event {
	ev := event.Event{
		Type:   event.Type(p.Type),
		Text:   p.Text,
		Error:  p.Error,
		Finish: p.Finish,
	}
	for _, tc := range p.ToolCalls {
		ev.ToolCalls = append(ev.ToolCalls, event.ToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}
	if p.Usage != nil {
		ev.Usage = &event.Usage{
			PromptTokens:     p.Usage.PromptTokens,
			CompletionTokens: p.Usage.CompletionTokens,
			TotalTokens:      p.Usage.PromptTokens + p.Usage.CompletionTokens,
		}
	}
	return ev
}

// acquireSem 获取插件并发槽位，返回释放函数。
func (g *Gateway) acquireSem(ctx context.Context, name string) (func(), error) {
	g.semMu.Lock()
	sem, ok := g.sems[name]
	if !ok {
		sem = make(chan struct{}, g.semSize)
		g.sems[name] = sem
	}
	g.semMu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// bucket 是简单令牌桶。
type bucket struct {
	tokens   float64
	capacity float64
	rate     float64
	last     time.Time
}

// allow 判断当前是否可消费一个令牌。
func (b *bucket) allow(now time.Time) bool {
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = minF(b.capacity, b.tokens+elapsed*b.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// acquireRate 按插件 RPS 限流。插件声明的速率优先于全局默认值。
func (g *Gateway) acquireRate(ctx context.Context, name string) error {
	rate := g.defaultRPS
	if v, ok := g.pluginRPS[name]; ok && v > 0 {
		rate = v
	}

	g.bucketMu.Lock()
	b, ok := g.buckets[name]
	if !ok {
		b = &bucket{tokens: 1, capacity: 1, rate: rate, last: time.Now()}
		g.buckets[name] = b
	}
	b.rate = rate
	allowed := b.allow(time.Now())
	g.bucketMu.Unlock()
	if allowed {
		return nil
	}
	// 简单等待一个最小间隔（令牌以 1/rate 秒补充）。
	wait := time.Duration(float64(time.Second) / b.rate)
	if wait > 90*time.Second {
		wait = 90 * time.Second
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// minF 返回较小值。
func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

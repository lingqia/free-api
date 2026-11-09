// Package protocol 实现三套对外 API 的解析与渲染。
//
// 所有入口先解析成统一的 Request，所有出口从统一事件流渲染，
// 因此新增协议只需实现「解析 + 渲染」两个方向，不必两两互转。
package protocol

import "encoding/json"

// 角色常量。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ToolCall 是工具调用的统一表示。Arguments 始终为 JSON 字符串。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message 是统一消息表示。
type Message struct {
	Role string `json:"role"`
	// Content 是展平后的文本内容。
	Content string `json:"content"`
	// ToolCalls 仅 assistant 消息携带。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID 仅 tool 消息携带，指向对应的助手工具调用。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Name 是工具名（tool 消息）或发送者名。
	Name string `json:"name,omitempty"`
}

// Tool 是统一工具定义。
type Tool struct {
	// Type 固定为 function（平台内置工具在网关侧标为 builtin）。
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Request 是统一的请求表示。
type Request struct {
	Model       string          `json:"model"`
	System      string          `json:"system,omitempty"`
	Messages    []Message       `json:"messages"`
	Tools       []Tool          `json:"tools,omitempty"`
	ToolChoice  json.RawMessage `json:"tool_choice,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	Stream      bool            `json:"stream"`
	Stop        []string        `json:"stop,omitempty"`
	// IncludeUsage 对应 OpenAI 的 stream_options.include_usage。
	IncludeUsage bool `json:"include_usage,omitempty"`
	// BuiltinTools 收集平台内置工具（如 web_search），仅 builtin 档位站点可用。
	BuiltinTools []string `json:"builtin_tools,omitempty"`
}

// HasTools 判断请求是否携带工具定义。
func (r *Request) HasTools() bool { return len(r.Tools) > 0 }

// HasFunctionTools 判断请求是否携带函数型工具定义。
func (r *Request) HasFunctionTools() bool {
	for _, t := range r.Tools {
		if t.Type == "function" || t.Type == "" {
			return true
		}
	}
	return false
}

// ParseError 表示请求体解析或校验失败。
type ParseError struct {
	Msg string
}

func (e *ParseError) Error() string { return e.Msg }

// newParseError 构造解析错误。
func newParseError(format string, args ...any) error {
	return &ParseError{Msg: sprintf(format, args...)}
}

// Package event 定义网关内部统一的流式事件模型。
// 所有协议（OpenAI Chat / OpenAI Responses / Anthropic Messages）的解析与渲染，
// 都以这一套事件为中间表示，避免三套协议之间两两互转产生 6 份转换代码。
package event

// Type 是统一事件类型。
type Type string

const (
	// TypeDelta 增量文本。
	TypeDelta Type = "delta"
	// TypeToolCall 模型请求调用工具。
	TypeToolCall Type = "tool_call"
	// TypeUsage token 用量统计。
	TypeUsage Type = "usage"
	// TypeDone 生成结束。
	TypeDone Type = "done"
	// TypeError 出错。
	TypeError Type = "error"
)

// ToolCall 是工具调用请求。Arguments 始终以 JSON 字符串承载，
// 与 OpenAI 的 function.arguments 保持一致；其它协议在渲染时再转换。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Usage 是 token 计数。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Event 是网关内部的统一事件。
type Event struct {
	Type      Type       `json:"type"`
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     *Usage     `json:"usage,omitempty"`
	Error     string     `json:"error,omitempty"`
	// Finish 是结束原因：stop / tool_calls / length / error。
	Finish string `json:"finish,omitempty"`
}

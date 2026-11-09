// Package protocol 的 Anthropic Messages 协议实现。
package protocol

import (
	"encoding/json"
	"net/http"
	"strings"

	"free-api/internal/event"
)

// ParseAnthropicRequest 解析 POST /v1/messages 请求体。
// Anthropic 与 OpenAI 的差异：system 是顶层字段、max_tokens 必填、
// content 是块数组、工具定义用 input_schema。
func ParseAnthropicRequest(body []byte) (*Request, error) {
	raw, err := decodeMap(body)
	if err != nil {
		return nil, err
	}
	model := jsonString(raw["model"])
	if model == "" {
		return nil, newParseError("缺少 model 参数")
	}
	maxTokens := intPtr(raw, "max_tokens")
	if maxTokens == nil || *maxTokens <= 0 {
		return nil, newParseError("max_tokens 为必填参数且必须大于 0")
	}

	req := &Request{
		Model:       model,
		MaxTokens:   *maxTokens,
		Temperature: 1.0,
		TopP:        1.0,
	}

	// system 可以是字符串，也可以是文本块数组。
	if sysRaw, ok := raw["system"]; ok {
		var sysStr string
		if err := json.Unmarshal(sysRaw, &sysStr); err == nil {
			req.System = sysStr
		} else {
			var blocks []map[string]json.RawMessage
			if err := json.Unmarshal(sysRaw, &blocks); err == nil {
				texts := []string{}
				for _, b := range blocks {
					if jsonString(b["type"]) == "text" {
						texts = append(texts, jsonString(b["text"]))
					}
				}
				req.System = strings.Join(texts, "\n")
			}
		}
	}

	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(raw["messages"], &messages); err != nil {
		return nil, newParseError("messages 必须是数组")
	}
	if len(messages) == 0 {
		return nil, newParseError("messages 不能为空")
	}

	for i, msgMap := range messages {
		role := jsonString(msgMap["role"])
		if role == "" {
			return nil, newParseError("messages[%d] 缺少 role", i)
		}
		msg := Message{Role: role}

		// content 可能是字符串或块数组。
		var str string
		if err := json.Unmarshal(msgMap["content"], &str); err == nil {
			msg.Content = str
		} else {
			var blocks []map[string]json.RawMessage
			if err := json.Unmarshal(msgMap["content"], &blocks); err != nil {
				return nil, newParseError("messages[%d].content 格式非法", i)
			}
			texts := []string{}
			for _, b := range blocks {
				switch jsonString(b["type"]) {
				case "text":
					texts = append(texts, jsonString(b["text"]))
				case "tool_use":
					// assistant 发起的工具调用
					argsRaw := b["input"]
					msg.ToolCalls = append(msg.ToolCalls, ToolCall{
						ID:        jsonString(b["id"]),
						Name:      jsonString(b["name"]),
						Arguments: string(rawOrNil(argsRaw)),
					})
				case "tool_result":
					// tool 结果回传：Anthropic 把它放在 user 消息的块里，
					// 这里拆成独立的 tool 消息，保证中间结构统一。
					resultText := ""
					var rs string
					if err := json.Unmarshal(b["content"], &rs); err == nil {
						resultText = rs
					} else {
						var inner []map[string]json.RawMessage
						if err := json.Unmarshal(b["content"], &inner); err == nil {
							parts := []string{}
							for _, ib := range inner {
								if jsonString(ib["type"]) == "text" {
									parts = append(parts, jsonString(ib["text"]))
								}
							}
							resultText = strings.Join(parts, "\n")
						}
					}
					req.Messages = append(req.Messages, Message{
						Role:       RoleTool,
						Content:    resultText,
						ToolCallID: jsonString(b["tool_use_id"]),
					})
				}
			}
			content := strings.Join(texts, "")
			if content != "" || len(msg.ToolCalls) == 0 {
				msg.Content = content
				req.Messages = append(req.Messages, msg)
				continue
			}
		}
		req.Messages = append(req.Messages, msg)
	}

	// tools：Anthropic 用扁平结构 {name, description, input_schema}。
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(raw["tools"], &tools); err == nil {
		for _, t := range tools {
			name := jsonString(t["name"])
			typ := jsonString(t["type"])
			if name == "" {
				// 服务端内置工具，如 {"type":"web_search_20250305"}。
				if typ != "" {
					req.BuiltinTools = append(req.BuiltinTools, typ)
				}
				continue
			}
			req.Tools = append(req.Tools, Tool{
				Type:        "function",
				Name:        name,
				Description: jsonString(t["description"]),
				Parameters:  rawOrNil(t["input_schema"]),
			})
		}
	}

	req.ToolChoice = rawOrNil(raw["tool_choice"])

	if temp := numberPtr(raw, "temperature"); temp != nil {
		req.Temperature = *temp
	}
	if top := numberPtr(raw, "top_p"); top != nil {
		req.TopP = *top
	}
	if stream := boolPtr(raw, "stream"); stream != nil {
		req.Stream = *stream
	}
	req.Stop = stringList(raw, "stop_sequences")

	return req, nil
}

// RenderAnthropicStream 把统一事件流渲染成 Anthropic Messages SSE。
func RenderAnthropicStream(w http.ResponseWriter, model string, events <-chan event.Event) error {
	sw := NewSSEWriter(w)
	msgID := "msg_" + randomID()

	// message_start
	if err := sw.Event("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            msgID,
			"type":          "message",
			"role":          RoleAssistant,
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	}); err != nil {
		return err
	}

	textBlockOpen := false
	toolBlockIndex := 0
	inputTokens, outputTokens := 0, 0
	finishReason := "end_turn"

	closeTextBlock := func() error {
		if !textBlockOpen {
			return nil
		}
		textBlockOpen = false
		return sw.Event("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
	}

	for evt := range events {
		switch evt.Type {
		case event.TypeDelta:
			if !textBlockOpen {
				textBlockOpen = true
				if err := sw.Event("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": 0,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				}); err != nil {
					return err
				}
			}
			if err := sw.Event("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{
					"type": "text_delta",
					"text": evt.Text,
				},
			}); err != nil {
				return err
			}

		case event.TypeToolCall:
			if err := closeTextBlock(); err != nil {
				return err
			}
			for _, tc := range evt.ToolCalls {
				toolBlockIndex++
				args := tc.Arguments
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				if err := sw.Event("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": toolBlockIndex,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": map[string]any{},
					},
				}); err != nil {
					return err
				}
				if err := sw.Event("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": toolBlockIndex,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": args,
					},
				}); err != nil {
					return err
				}
				if err := sw.Event("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": toolBlockIndex,
				}); err != nil {
					return err
				}
			}
			finishReason = "tool_use"

		case event.TypeUsage:
			if evt.Usage != nil {
				inputTokens = evt.Usage.PromptTokens
				outputTokens = evt.Usage.CompletionTokens
			}

		case event.TypeDone:
			if evt.Finish == "tool_calls" {
				finishReason = "tool_use"
			} else if evt.Finish == "length" {
				finishReason = "max_tokens"
			}
			if err := closeTextBlock(); err != nil {
				return err
			}
			if err := sw.Event("message_delta", map[string]any{
				"type": "message_delta",
				"delta": map[string]any{
					"stop_reason":   finishReason,
					"stop_sequence": nil,
				},
				"usage": map[string]any{
					"output_tokens": outputTokens,
				},
			}); err != nil {
				return err
			}
			if err := sw.Event("message_stop", map[string]any{"type": "message_stop"}); err != nil {
				return err
			}

		case event.TypeError:
			if err := sw.Event("error", map[string]any{
				"type": "error",
				"error": map[string]any{
					"type":    "api_error",
					"message": evt.Error,
				},
			}); err != nil {
				return err
			}
			return nil
		}
	}

	_ = inputTokens
	return sw.Done()
}

// RenderAnthropicNonStream 渲染 Anthropic 非流式响应。
func RenderAnthropicNonStream(w http.ResponseWriter, model string, events <-chan event.Event) error {
	var texts []string
	var toolCalls []event.ToolCall
	usage := event.Usage{}
	finishReason := "end_turn"

	for evt := range events {
		switch evt.Type {
		case event.TypeDelta:
			texts = append(texts, evt.Text)
		case event.TypeToolCall:
			toolCalls = append(toolCalls, evt.ToolCalls...)
			finishReason = "tool_use"
		case event.TypeUsage:
			if evt.Usage != nil {
				usage = *evt.Usage
			}
		case event.TypeDone:
			if evt.Finish == "tool_calls" {
				finishReason = "tool_use"
			} else if evt.Finish == "length" {
				finishReason = "max_tokens"
			}
		case event.TypeError:
			writeError(w, http.StatusInternalServerError, "anthropic", "api_error", evt.Error)
			return nil
		}
	}

	content := []map[string]any{}
	if text := strings.Join(texts, ""); text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	for _, tc := range toolCalls {
		input := map[string]any{}
		if strings.TrimSpace(tc.Arguments) != "" {
			if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
				input = map[string]any{}
			}
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Name,
			"input": input,
		})
	}

	resp := map[string]any{
		"id":            "msg_" + randomID(),
		"type":          "message",
		"role":          RoleAssistant,
		"model":         model,
		"content":       content,
		"stop_reason":   finishReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(resp)
}

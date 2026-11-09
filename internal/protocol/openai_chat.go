// Package protocol 的 OpenAI Chat Completions 协议实现。
package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"free-api/internal/event"
)

// ParseChatRequest 解析 OpenAI /v1/chat/completions 请求体。
func ParseChatRequest(body []byte) (*Request, error) {
	raw, err := decodeMap(body)
	if err != nil {
		return nil, err
	}
	model := jsonString(raw["model"])
	if model == "" {
		return nil, newParseError("缺少 model 参数")
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(raw["messages"], &messages); err != nil {
		return nil, newParseError("messages 必须是数组")
	}
	if len(messages) == 0 {
		return nil, newParseError("messages 不能为空")
	}

	req := &Request{
		Model:       model,
		Stream:      false,
		MaxTokens:   0,
		Temperature: 1.0,
		TopP:        1.0,
	}

	// 解析 messages
	for i, msgRaw := range messages {
		var msgMap map[string]json.RawMessage
		if err := json.Unmarshal(msgRaw, &msgMap); err != nil {
			return nil, newParseError("messages[%d] 不是对象: %v", i, err)
		}
		role := jsonString(msgMap["role"])
		if role == "" {
			return nil, newParseError("messages[%d] 缺少 role", i)
		}

		msg := Message{Role: role, Name: jsonString(msgMap["name"])}

		// content 可能是 string 或 array
		contentRaw := msgMap["content"]
		if len(contentRaw) > 0 {
			// 尝试字符串
			var str string
			if err := json.Unmarshal(contentRaw, &str); err == nil {
				msg.Content = str
			} else {
				// 尝试数组（多模态）
				var parts []map[string]json.RawMessage
				if err := json.Unmarshal(contentRaw, &parts); err == nil {
					texts := []string{}
					for _, part := range parts {
						if jsonString(part["type"]) == "text" {
							texts = append(texts, jsonString(part["text"]))
						}
					}
					msg.Content = strings.Join(texts, "\n")
				}
			}
		}

		// tool_calls (仅 assistant)
		if role == RoleAssistant {
			var toolCalls []map[string]json.RawMessage
			if err := json.Unmarshal(msgMap["tool_calls"], &toolCalls); err == nil {
				for _, tc := range toolCalls {
					call := ToolCall{ID: jsonString(tc["id"])}
					if fnRaw, ok := tc["function"]; ok {
						var fn map[string]json.RawMessage
						if err := json.Unmarshal(fnRaw, &fn); err == nil {
							call.Name = jsonString(fn["name"])
							call.Arguments = string(fn["arguments"])
						}
					}
					msg.ToolCalls = append(msg.ToolCalls, call)
				}
			}
		}

		// tool_call_id (仅 tool)
		if role == RoleTool {
			msg.ToolCallID = jsonString(msgMap["tool_call_id"])
		}

		req.Messages = append(req.Messages, msg)
	}

	// 提取 system（OpenAI 把它放 messages 里第一条）
	if len(req.Messages) > 0 && req.Messages[0].Role == RoleSystem {
		req.System = req.Messages[0].Content
		req.Messages = req.Messages[1:]
	}

	// tools
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(raw["tools"], &tools); err == nil {
		for _, t := range tools {
			tool := Tool{Type: jsonString(t["type"])}
			if fnRaw, ok := t["function"]; ok {
				var fn map[string]json.RawMessage
				if err := json.Unmarshal(fnRaw, &fn); err == nil {
					tool.Name = jsonString(fn["name"])
					tool.Description = jsonString(fn["description"])
					tool.Parameters = fn["parameters"]
				}
			}
			// 平台内置工具（如 {"type":"web_search"}）没有 function 包裹。
			if tool.Type == "" {
				tool.Type = "function"
			}
			if tool.Type != "function" {
				req.BuiltinTools = append(req.BuiltinTools, tool.Type)
				continue
			}
			req.Tools = append(req.Tools, tool)
		}
	}

	req.ToolChoice = rawOrNil(raw["tool_choice"])

	// 其他参数
	if mt := intPtr(raw, "max_tokens"); mt != nil {
		req.MaxTokens = *mt
	}
	if temp := numberPtr(raw, "temperature"); temp != nil {
		req.Temperature = *temp
	}
	if top := numberPtr(raw, "top_p"); top != nil {
		req.TopP = *top
	}
	if stream := boolPtr(raw, "stream"); stream != nil {
		req.Stream = *stream
	}
	req.Stop = stringList(raw, "stop")

	// stream_options.include_usage
	if soRaw, ok := raw["stream_options"]; ok {
		var so map[string]json.RawMessage
		if err := json.Unmarshal(soRaw, &so); err == nil {
			if iu := boolPtr(so, "include_usage"); iu != nil {
				req.IncludeUsage = *iu
			}
		}
	}

	return req, nil
}

// RenderChatStream 把统一事件流渲染成 OpenAI Chat Completions SSE。
func RenderChatStream(w http.ResponseWriter, model string, events <-chan event.Event) error {
	sw := NewSSEWriter(w)
	chunkID := fmt.Sprintf("chatcmpl-%s", randomID())
	createdAt := nowUnix()

	for evt := range events {
		switch evt.Type {
		case event.TypeDelta:
			chunk := map[string]any{
				"id":      chunkID,
				"object":  "chat.completion.chunk",
				"created": createdAt,
				"model":   model,
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{"content": evt.Text},
				}},
			}
			if err := sw.Data(chunk); err != nil {
				return err
			}

		case event.TypeToolCall:
			for _, tc := range evt.ToolCalls {
				chunk := map[string]any{
					"id":      chunkID,
					"object":  "chat.completion.chunk",
					"created": createdAt,
					"model":   model,
					"choices": []map[string]any{{
						"index": 0,
						"delta": map[string]any{
							"tool_calls": []map[string]any{{
								"index": 0,
								"id":    tc.ID,
								"type":  "function",
								"function": map[string]any{
									"name":      tc.Name,
									"arguments": tc.Arguments,
								},
							}},
						},
					}},
				}
				if err := sw.Data(chunk); err != nil {
					return err
				}
			}

		case event.TypeUsage:
			if evt.Usage != nil {
				chunk := map[string]any{
					"id":      chunkID,
					"object":  "chat.completion.chunk",
					"created": createdAt,
					"model":   model,
					"usage": map[string]any{
						"prompt_tokens":     evt.Usage.PromptTokens,
						"completion_tokens": evt.Usage.CompletionTokens,
						"total_tokens":      evt.Usage.TotalTokens,
					},
				}
				if err := sw.Data(chunk); err != nil {
					return err
				}
			}

		case event.TypeDone:
			finishReason := "stop"
			if evt.Finish != "" {
				finishReason = evt.Finish
			}
			chunk := map[string]any{
				"id":      chunkID,
				"object":  "chat.completion.chunk",
				"created": createdAt,
				"model":   model,
				"choices": []map[string]any{{
					"index":        0,
					"delta":        map[string]any{},
					"finish_reason": finishReason,
				}},
			}
			if err := sw.Data(chunk); err != nil {
				return err
			}
			if err := sw.Data("[DONE]"); err != nil {
				return err
			}

		case event.TypeError:
			// 流式错误：继续用 SSE 格式传 error chunk
			chunk := map[string]any{
				"id":      chunkID,
				"object":  "chat.completion.chunk",
				"created": createdAt,
				"model":   model,
				"error": map[string]any{
					"message": evt.Error,
					"type":    "server_error",
				},
			}
			_ = sw.Data(chunk)
			return fmt.Errorf("流式错误: %s", evt.Error)
		}
	}

	return sw.Done()
}

// RenderChatNonStream 把统一事件流渲染成 OpenAI Chat Completions 非流式 JSON 响应。
func RenderChatNonStream(w http.ResponseWriter, model string, events <-chan event.Event) error {
	var contentParts []string
	var toolCalls []event.ToolCall
	var usage *event.Usage
	var finishReason = "stop"

	for evt := range events {
		switch evt.Type {
		case event.TypeDelta:
			contentParts = append(contentParts, evt.Text)
		case event.TypeToolCall:
			toolCalls = append(toolCalls, evt.ToolCalls...)
			finishReason = "tool_calls"
		case event.TypeUsage:
			usage = evt.Usage
		case event.TypeDone:
			if evt.Finish != "" {
				finishReason = evt.Finish
			}
		case event.TypeError:
			writeError(w, http.StatusInternalServerError, "openai", "server_error", evt.Error)
			return fmt.Errorf("生成失败: %s", evt.Error)
		}
	}

	content := strings.Join(contentParts, "")
	respID := fmt.Sprintf("chatcmpl-%s", randomID())

	choice := map[string]any{
		"index": 0,
		"message": map[string]any{
			"role":    RoleAssistant,
			"content": content,
		},
		"finish_reason": finishReason,
	}

	if len(toolCalls) > 0 {
		calls := []map[string]any{}
		for _, tc := range toolCalls {
			calls = append(calls, map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			})
		}
		choice["message"].(map[string]any)["tool_calls"] = calls
	}

	resp := map[string]any{
		"id":      respID,
		"object":  "chat.completion",
		"created": nowUnix(),
		"model":   model,
		"choices": []map[string]any{choice},
	}

	if usage != nil {
		resp["usage"] = map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(resp)
}

// Package protocol 的 OpenAI Responses 协议实现。
package protocol

import (
	"encoding/json"
	"net/http"
	"strings"

	"free-api/internal/event"
)

// ParseResponsesRequest 解析 POST /v1/responses 请求体。
// 与 Chat Completions 的主要差异：input 可以是字符串或消息数组、
// 系统提示放 instructions、tools 是扁平结构、工具调用走 function_call 类型。
func ParseResponsesRequest(body []byte) (*Request, error) {
	raw, err := decodeMap(body)
	if err != nil {
		return nil, err
	}
	model := jsonString(raw["model"])
	if model == "" {
		return nil, newParseError("缺少 model 参数")
	}

	req := &Request{
		Model:       model,
		System:      jsonString(raw["instructions"]),
		Temperature: 1.0,
		TopP:        1.0,
	}

	// input：字符串或数组
	if inputRaw, ok := raw["input"]; ok {
		var inputStr string
		if err := json.Unmarshal(inputRaw, &inputStr); err == nil {
			req.Messages = append(req.Messages, Message{Role: RoleUser, Content: inputStr})
		} else {
			var items []map[string]json.RawMessage
			if err := json.Unmarshal(inputRaw, &items); err != nil {
				return nil, newParseError("input 必须是字符串或数组")
			}
			for i, item := range items {
				typ := jsonString(item["type"])
				role := jsonString(item["role"])

				switch typ {
				case "function_call":
					// 历史里的工具调用：归到 assistant 消息
					req.Messages = append(req.Messages, Message{
						Role: RoleAssistant,
						ToolCalls: []ToolCall{{
							ID:        jsonString(item["call_id"]),
							Name:      jsonString(item["name"]),
							Arguments: jsonString(item["arguments"]),
						}},
					})
					continue
				case "function_call_output":
					req.Messages = append(req.Messages, Message{
						Role:       RoleTool,
						Content:    jsonString(item["output"]),
						ToolCallID: jsonString(item["call_id"]),
					})
					continue
				case "message", "":
					if role == "" {
						return nil, newParseError("input[%d] 缺少 role", i)
					}
				default:
					// reasoning、item_reference 等类型网关忽略
					continue
				}

				msg := Message{Role: role}
				if contentRaw, ok := item["content"]; ok {
					var s string
					if err := json.Unmarshal(contentRaw, &s); err == nil {
						msg.Content = s
					} else {
						var parts []map[string]json.RawMessage
						if err := json.Unmarshal(contentRaw, &parts); err == nil {
							texts := []string{}
							for _, p := range parts {
								if t := jsonString(p["type"]); t == "input_text" || t == "output_text" || t == "text" {
									texts = append(texts, jsonString(p["text"]))
								}
							}
							msg.Content = strings.Join(texts, "")
						}
					}
				}
				req.Messages = append(req.Messages, msg)
			}
		}
	}

	if len(req.Messages) == 0 {
		return nil, newParseError("input 不能为空")
	}

	// tools：扁平结构 {type:"function", name, description, parameters}
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(raw["tools"], &tools); err == nil {
		for _, t := range tools {
			typ := jsonString(t["type"])
			if typ != "function" {
				// web_search、file_search、code_interpreter 等内置工具
				if typ != "" {
					req.BuiltinTools = append(req.BuiltinTools, typ)
				}
				continue
			}
			req.Tools = append(req.Tools, Tool{
				Type:        "function",
				Name:        jsonString(t["name"]),
				Description: jsonString(t["description"]),
				Parameters:  rawOrNil(t["parameters"]),
			})
		}
	}

	req.ToolChoice = rawOrNil(raw["tool_choice"])

	if mt := intPtr(raw, "max_output_tokens"); mt != nil {
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

	return req, nil
}

// RenderResponsesStream 把统一事件流渲染成 OpenAI Responses SSE。
func RenderResponsesStream(w http.ResponseWriter, model string, events <-chan event.Event) error {
	sw := NewSSEWriter(w)
	respID := "resp_" + randomID()
	itemID := "msg_" + randomID()
	createdAt := nowUnix()

	emit := func(typ string, payload map[string]any) error {
		payload["type"] = typ
		payload["sequence_number"] = sequenceIncr()
		return sw.Event(typ, payload)
	}

	// response.created
	if err := emit("response.created", map[string]any{
		"response": map[string]any{
			"id":         respID,
			"object":     "response",
			"created_at": createdAt,
			"model":      model,
			"status":     "in_progress",
			"output":     []any{},
		},
	}); err != nil {
		return err
	}

	textStarted := false
	var fullText strings.Builder
	var toolCalls []event.ToolCall
	usage := event.Usage{}
	finishReason := "completed"

	startTextItem := func() error {
		if textStarted {
			return nil
		}
		textStarted = true
		if err := emit("response.output_item.added", map[string]any{
			"output_index": 0,
			"item": map[string]any{
				"id":      itemID,
				"type":    "message",
				"role":    RoleAssistant,
				"status":  "in_progress",
				"content": []any{},
			},
		}); err != nil {
			return err
		}
		return emit("response.content_part.added", map[string]any{
			"item_id":       itemID,
			"output_index":  0,
			"content_index": 0,
			"part": map[string]any{
				"type":        "output_text",
				"text":        "",
				"annotations": []any{},
			},
		})
	}

	for evt := range events {
		switch evt.Type {
		case event.TypeDelta:
			if err := startTextItem(); err != nil {
				return err
			}
			fullText.WriteString(evt.Text)
			if err := emit("response.output_text.delta", map[string]any{
				"item_id":       itemID,
				"output_index":  0,
				"content_index": 0,
				"delta":         evt.Text,
			}); err != nil {
				return err
			}

		case event.TypeToolCall:
			for _, tc := range evt.ToolCalls {
				toolCalls = append(toolCalls, tc)
				fcID := "fc_" + randomID()
				if err := emit("response.output_item.added", map[string]any{
					"output_index": len(toolCalls),
					"item": map[string]any{
						"id":        fcID,
						"type":      "function_call",
						"call_id":   tc.ID,
						"name":      tc.Name,
						"arguments": "",
						"status":    "in_progress",
					},
				}); err != nil {
					return err
				}
				if err := emit("response.function_call_arguments.delta", map[string]any{
					"item_id":      fcID,
					"output_index": len(toolCalls),
					"delta":        tc.Arguments,
				}); err != nil {
					return err
				}
				if err := emit("response.function_call_arguments.done", map[string]any{
					"item_id":      fcID,
					"output_index": len(toolCalls),
					"arguments":    tc.Arguments,
				}); err != nil {
					return err
				}
			}
			finishReason = "completed"

		case event.TypeUsage:
			if evt.Usage != nil {
				usage = *evt.Usage
			}

		case event.TypeDone:
			if evt.Finish == "length" {
				finishReason = "incomplete"
			}
			text := fullText.String()
			if textStarted {
				if err := emit("response.output_text.done", map[string]any{
					"item_id":       itemID,
					"output_index":  0,
					"content_index": 0,
					"text":          text,
				}); err != nil {
					return err
				}
				if err := emit("response.content_part.done", map[string]any{
					"item_id":       itemID,
					"output_index":  0,
					"content_index": 0,
					"part": map[string]any{
						"type":        "output_text",
						"text":        text,
						"annotations": []any{},
					},
				}); err != nil {
					return err
				}
				if err := emit("response.output_item.done", map[string]any{
					"output_index": 0,
					"item": map[string]any{
						"id":     itemID,
						"type":   "message",
						"role":   RoleAssistant,
						"status": "completed",
						"content": []map[string]any{{
							"type":        "output_text",
							"text":        text,
							"annotations": []any{},
						}},
					},
				}); err != nil {
					return err
				}
			}
			output := buildResponsesOutput(itemID, text, toolCalls)
			if err := emit("response.completed", map[string]any{
				"response": map[string]any{
					"id":         respID,
					"object":     "response",
					"created_at": createdAt,
					"model":      model,
					"status":     finishReason,
					"output":     output,
					"usage": map[string]any{
						"input_tokens":  usage.PromptTokens,
						"output_tokens": usage.CompletionTokens,
						"total_tokens":  usage.PromptTokens + usage.CompletionTokens,
					},
				},
			}); err != nil {
				return err
			}

		case event.TypeError:
			if err := emit("response.failed", map[string]any{
				"response": map[string]any{
					"id":     respID,
					"object": "response",
					"status": "failed",
					"error": map[string]any{
						"code":    "server_error",
						"message": evt.Error,
					},
				},
			}); err != nil {
				return err
			}
			return nil
		}
	}

	return sw.Done()
}

// RenderResponsesNonStream 渲染 OpenAI Responses 非流式响应。
func RenderResponsesNonStream(w http.ResponseWriter, model string, events <-chan event.Event) error {
	var textParts []string
	var toolCalls []event.ToolCall
	usage := event.Usage{}
	status := "completed"

	for evt := range events {
		switch evt.Type {
		case event.TypeDelta:
			textParts = append(textParts, evt.Text)
		case event.TypeToolCall:
			toolCalls = append(toolCalls, evt.ToolCalls...)
		case event.TypeUsage:
			if evt.Usage != nil {
				usage = *evt.Usage
			}
		case event.TypeDone:
			if evt.Finish == "length" {
				status = "incomplete"
			}
		case event.TypeError:
			writeError(w, http.StatusInternalServerError, "openai", "server_error", evt.Error)
			return nil
		}
	}

	respID := "resp_" + randomID()
	itemID := "msg_" + randomID()

	resp := map[string]any{
		"id":         respID,
		"object":     "response",
		"created_at": nowUnix(),
		"model":      model,
		"status":     status,
		"output":     buildResponsesOutput(itemID, strings.Join(textParts, ""), toolCalls),
		"usage": map[string]any{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
			"total_tokens":  usage.PromptTokens + usage.CompletionTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(resp)
}

// buildResponsesOutput 构造 Responses 的 output 数组。
func buildResponsesOutput(itemID, text string, toolCalls []event.ToolCall) []map[string]any {
	output := []map[string]any{}
	if text != "" {
		output = append(output, map[string]any{
			"id":     itemID,
			"type":   "message",
			"role":   RoleAssistant,
			"status": "completed",
			"content": []map[string]any{{
				"type":        "output_text",
				"text":        text,
				"annotations": []any{},
			}},
		})
	}
	for _, tc := range toolCalls {
		output = append(output, map[string]any{
			"id":        "fc_" + randomID(),
			"type":      "function_call",
			"call_id":   tc.ID,
			"name":      tc.Name,
			"arguments": tc.Arguments,
			"status":    "completed",
		})
	}
	return output
}

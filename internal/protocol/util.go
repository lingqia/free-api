package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// marshalJSON 序列化负载，错误直接透出。
func marshalJSON(v any) ([]byte, error) { return json.Marshal(v) }

// newBytesReader 构造字节读取器，供 SSE 行扫描使用。
func newBytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// rawOrNil 把空 RawMessage 归一化为 nil，便于后续按需判断。
func rawOrNil(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// jsonString 尝试把 RawMessage 读成字符串，失败返回空串。
func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// numberPtr 读取 JSON 数值字段，字段缺失时返回 nil。
func numberPtr(raw map[string]json.RawMessage, key string) *float64 {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	var f float64
	if err := json.Unmarshal(v, &f); err != nil {
		return nil
	}
	return &f
}

// intPtr 读取 JSON 整数字段，字段缺失时返回 nil。
func intPtr(raw map[string]json.RawMessage, key string) *int {
	f := numberPtr(raw, key)
	if f == nil {
		return nil
	}
	i := int(*f)
	return &i
}

// boolPtr 读取 JSON 布尔字段，字段缺失时返回 nil。
func boolPtr(raw map[string]json.RawMessage, key string) *bool {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return nil
	}
	return &b
}

// stringList 读取 JSON 字符串数组字段。
func stringList(raw map[string]json.RawMessage, key string) []string {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	var list []string
	if err := json.Unmarshal(v, &list); err != nil {
		return nil
	}
	return list
}

// decodeMap 把请求体解成 map，失败返回解析错误。
func decodeMap(body []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, newParseError("请求体为空")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, newParseError("请求体不是合法 JSON: %v", err)
	}
	return raw, nil
}

// errEnvelope 是网关统一的错误响应信封。
type errEnvelope struct {
	Error errBody `json:"error"`
}

// errBody 是错误体。Type 区分 openai 与 anthropic 两种客户端习惯。
type errBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Param   string `json:"param,omitempty"`
}

// writeError 输出统一错误响应。OpenAI 客户端期望顶层 error 对象，
// Anthropic 客户端期望 {"type":"error","error":{...}}，这里按请求来源分别输出。
func writeError(w http.ResponseWriter, status int, flavor, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	switch flavor {
	case "anthropic":
		payload := map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    anthropicErrorType(code),
				"message": message,
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	default:
		payload := errEnvelope{Error: errBody{
			Message: message,
			Type:    openaiErrorType(code),
			Code:    code,
		}}
		_ = json.NewEncoder(w).Encode(payload)
	}
}

// anthropicErrorType 把错误码映射为 Anthropic 的错误类型。
func anthropicErrorType(code string) string {
	switch code {
	case "invalid_request_error":
		return "invalid_request_error"
	case "authentication_error":
		return "authentication_error"
	case "rate_limit_error":
		return "rate_limit_error"
	default:
		return "api_error"
	}
}

// openaiErrorType 把错误码映射为 OpenAI 的错误类型。
func openaiErrorType(code string) string {
	switch code {
	case "invalid_request_error":
		return "invalid_request_error"
	case "authentication_error":
		return "authentication_error"
	case "rate_limit_error":
		return "rate_limit_error"
	case "not_found_error":
		return "not_found_error"
	default:
		return "server_error"
	}
}

// fmtInt 格式化整数计数。
func fmtInt(n int) string { return fmt.Sprintf("%d", n) }

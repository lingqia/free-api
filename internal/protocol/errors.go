package protocol

import "net/http"

// WriteErrorJSON 输出 OpenAI 风格的错误响应。
func WriteErrorJSON(w http.ResponseWriter, status int, code, message string) {
	writeError(w, status, "openai", code, message)
}

// WriteErrorFlavor 按协议风味输出错误响应：openai 或 anthropic。
func WriteErrorFlavor(w http.ResponseWriter, status int, flavor, code, message string) {
	writeError(w, status, flavor, code, message)
}

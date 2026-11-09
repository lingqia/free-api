package gateway

import "net/http"

// serveConsole 返回内置控制台页面。
func (g *Gateway) serveConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(consoleHTML))
}

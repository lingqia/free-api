package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"free-api/internal/plugin"
	"free-api/internal/protocol"
)

// proxyPoolPlugin 是全局代理池使用的保留凭证名。
const proxyPoolPlugin = "__proxy_pool__"

// Handler 构造对外 HTTP 路由。
func (g *Gateway) Handler(maxBody int64) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/chat/completions", g.wrap(maxBody, g.handleChat))
	mux.HandleFunc("POST /v1/responses", g.wrap(maxBody, g.handleResponses))
	mux.HandleFunc("POST /v1/messages", g.wrap(maxBody, g.handleMessages))
	mux.HandleFunc("GET /v1/models", g.handleModels)
	mux.HandleFunc("GET /v1/models/{id...}", g.handleModel)
	mux.HandleFunc("GET /healthz", g.handleHealth)
	mux.HandleFunc("GET /api/models", g.handleModels)
	mux.HandleFunc("GET /api/plugins", g.handlePlugins)
	mux.HandleFunc("GET /api/credentials", g.handleCredentialList)
	mux.HandleFunc("POST /api/credentials", g.handleCredentialSet)
	mux.HandleFunc("DELETE /api/credentials", g.handleCredentialDelete)
	mux.HandleFunc("GET /api/session", g.handleSession)
	mux.HandleFunc("POST /api/login", g.handleLogin)
	mux.HandleFunc("POST /api/logout", g.handleLogout)
	mux.HandleFunc("GET /api/settings", g.handleSettingsGet)
	mux.HandleFunc("POST /api/settings", g.handleSettingsSet)
	mux.HandleFunc("GET /api/proxy", g.handleProxyGet)
	mux.HandleFunc("POST /api/proxy", g.handleProxySet)
	mux.HandleFunc("DELETE /api/proxy", g.handleProxyDelete)
	mux.HandleFunc("GET /api/stats", g.handleStats)
	mux.Handle("/", g.staticHandler())

	return withCORS(g.withConsoleAuth(g.withAPIAuth(mux)))
}

// withAPIAuth 校验网关 API Key，仅作用于 /v1 接口。
// api_keys 为空时不启用鉴权。
func (g *Gateway) withAPIAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") && r.URL.Path != "/v1" {
			next.ServeHTTP(w, r)
			return
		}
		if len(g.settings.APIKeys()) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		token := g.requestToken(r)
		if g.settings.ValidAPIKey(token) {
			g.recordKeyUse(token)
			next.ServeHTTP(w, r)
			return
		}
		flavor := "openai"
		if strings.HasPrefix(r.URL.Path, "/v1/messages") {
			flavor = "anthropic"
		}
		protocol.WriteErrorFlavor(w, http.StatusUnauthorized, flavor, "authentication_error", "API Key 无效或缺失")
	})
}

// withConsoleAuth 保护控制台数据接口。
// 未设置登录密码时保持免登录；设置后要求会话 Cookie 或有效 API Key。
func (g *Gateway) withConsoleAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api") {
			next.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/session", "/api/login", "/api/logout":
			next.ServeHTTP(w, r)
			return
		}
		if !g.settings.PasswordSet() || g.settings.ValidSession(r) || g.validAPIKey(g.requestToken(r)) {
			next.ServeHTTP(w, r)
			return
		}
		protocol.WriteErrorJSON(w, http.StatusUnauthorized, "authentication_error", "请先登录控制台")
	})
}

// requestToken 读取请求里的 API Key（Bearer 或 x-api-key）。
func (g *Gateway) requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if v := strings.TrimSpace(r.Header.Get("x-api-key")); v != "" {
		return v
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// validAPIKey 判断 API Key 是否有效且未被停用。
func (g *Gateway) validAPIKey(token string) bool {
	return g.settings.ValidAPIKey(token)
}

// wrap 统一处理请求体大小限制与解析错误。
func (g *Gateway) wrap(maxBody int64, fn func(http.ResponseWriter, *http.Request, []byte)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			protocol.WriteErrorJSON(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "请求体过大或读取失败")
			return
		}
		fn(w, r, body)
	}
}

// handleChat 处理 /v1/chat/completions。
func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := protocol.ParseChatRequest(body)
	if err != nil {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	g.serve(w, r, req, "openai-chat")
}

// handleResponses 处理 /v1/responses。
func (g *Gateway) handleResponses(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := protocol.ParseResponsesRequest(body)
	if err != nil {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	g.serve(w, r, req, "openai-responses")
}

// handleMessages 处理 /v1/messages。
func (g *Gateway) handleMessages(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := protocol.ParseAnthropicRequest(body)
	if err != nil {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	g.serve(w, r, req, "anthropic")
}

// serve 是三条协议共用的执行路径。
func (g *Gateway) serve(w http.ResponseWriter, r *http.Request, req *protocol.Request, flavor string) {
	start := time.Now()

	routes, err := g.Resolve(req.Model, req.HasFunctionTools())
	if err != nil {
		flavorName := "openai"
		if flavor == "anthropic" {
			flavorName = "anthropic"
		}
		protocol.WriteErrorFlavor(w, http.StatusNotFound, flavorName, "not_found_error", err.Error())
		return
	}

	events, err := g.Dispatch(r.Context(), routes, req)
	if err != nil {
		flavorName := "openai"
		if flavor == "anthropic" {
			flavorName = "anthropic"
		}
		protocol.WriteErrorFlavor(w, http.StatusBadGateway, flavorName, "server_error", err.Error())
		return
	}

	slog.Info("请求路由",
		"model", req.Model,
		"protocol", flavor,
		"stream", req.Stream,
		"tools", len(req.Tools),
		"candidates", len(routes),
	)

	var renderErr error
	switch flavor {
	case "openai-chat":
		if req.Stream {
			renderErr = protocol.RenderChatStream(w, req.Model, events)
		} else {
			renderErr = protocol.RenderChatNonStream(w, req.Model, events)
		}
	case "openai-responses":
		if req.Stream {
			renderErr = protocol.RenderResponsesStream(w, req.Model, events)
		} else {
			renderErr = protocol.RenderResponsesNonStream(w, req.Model, events)
		}
	case "anthropic":
		if req.Stream {
			renderErr = protocol.RenderAnthropicStream(w, req.Model, events)
		} else {
			renderErr = protocol.RenderAnthropicNonStream(w, req.Model, events)
		}
	}

	if renderErr != nil {
		slog.Warn("渲染失败", "protocol", flavor, "err", renderErr)
	}
	slog.Info("请求完成", "model", req.Model, "protocol", flavor, "elapsed_ms", time.Since(start).Milliseconds())
}

// modelEntry 是 /v1/models 的单项。
type modelEntry struct {
	ID           string         `json:"id"`
	Object       string         `json:"object"`
	Created      int64          `json:"created"`
	OwnedBy      string         `json:"owned_by"`
	Capabilities map[string]any `json:"capabilities"`
	Auth         map[string]any `json:"auth"`
	Sources      []sourceEntry  `json:"sources"`
}

// sourceEntry 描述某模型在一个站点上的可用性。
type sourceEntry struct {
	Plugin string `json:"plugin"`
	Tools  string `json:"tools"`
	Ready  bool   `json:"ready"`
}

// handleModels 列出所有已知模型及其能力与认证状态。
func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request) {
	list := g.modelCatalog()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   list,
	})
}

// handleModel 返回单个模型详情。模型 ID 形如 插件/模型名，可包含斜杠。
func (g *Gateway) handleModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		g.handleModels(w, r)
		return
	}
	for _, m := range g.modelCatalog() {
		if m.ID == id {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(m)
			return
		}
	}
	protocol.WriteErrorJSON(w, http.StatusNotFound, "not_found_error", "模型不存在: "+id)
}

// modelCatalog 汇总所有插件的模型。对外模型 ID 统一为「插件/模型名」，
// 例如 aki/deepseek-v4.1-flash；同名模型由不同插件提供时会各自成为独立条目。
func (g *Gateway) modelCatalog() []modelEntry {
	byID := map[string]*modelEntry{}
	for _, snap := range g.Registry.Snapshot() {
		for _, mid := range snap.Models {
			id := snap.Name + "/" + mid
			entry, ok := byID[id]
			if !ok {
				entry = &modelEntry{
					ID:      id,
					Object:  "model",
					Created: 0,
					OwnedBy: "free-api",
					Capabilities: map[string]any{
						"streaming": true,
						"tools":     snap.Tools,
					},
					Auth: map[string]any{
						"required":   len(snap.AuthFields) > 0,
						"configured": snap.AuthReady,
						"fields":     snap.AuthFields,
					},
				}
				if len(snap.AuthFields) == 0 {
					entry.Auth["required"] = false
					entry.Auth["configured"] = true
				}
				byID[id] = entry
			}
			entry.Sources = append(entry.Sources, sourceEntry{
				Plugin: snap.Name,
				Tools:  snap.Tools,
				Ready:  snap.Healthy && snap.AuthReady,
			})
		}
	}
	out := make([]modelEntry, 0, len(byID))
	for _, e := range byID {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// handleHealth 健康检查。
func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	snaps := g.Registry.Snapshot()
	healthy := 0
	for _, s := range snaps {
		if s.Healthy {
			healthy++
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"plugins": len(snaps),
		"healthy": healthy,
	})
}

// pluginAccounts 是站点账号数视图。
// 匿名站点按 IP 代理数统计，需要 key 的站点按 key 数统计。
type pluginAccounts struct {
	Total     int    `json:"total"`
	Available int    `json:"available"`
	Kind      string `json:"kind"` // proxy / key
}

// pluginView 是 /api/plugins 的单项：插件快照 + 运行统计 + 账号数。
type pluginView struct {
	plugin.Snapshot
	Stats    PluginStats    `json:"stats"`
	Accounts pluginAccounts `json:"accounts"`
}

// handlePlugins 返回插件状态列表。
func (g *Gateway) handlePlugins(w http.ResponseWriter, r *http.Request) {
	snaps := g.Registry.Snapshot()
	views := make([]pluginView, 0, len(snaps))
	for _, snap := range snaps {
		views = append(views, pluginView{
			Snapshot: snap,
			Stats:    g.StatsOf(snap.Name),
			Accounts: g.accountsFor(snap),
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"plugins": views,
	})
}

// quotaBasis 返回站点的额度计量方式：ip 或 key。
// 配置 auth.quota 优先；未声明时沿用旧行为（匿名站按 IP，其余按 key）。
func (g *Gateway) quotaBasis(s plugin.Snapshot) string {
	quota := g.Registry.AuthOf(s.Name).Quota
	if quota == "ip" || quota == "key" {
		return quota
	}
	if s.AuthType == "" || s.AuthType == "none" {
		return "ip"
	}
	return "key"
}

// accountsFor 计算站点账号数。
//   - 按 IP 计量：账号数按出口 IP 统计，站点代理优先，其次全局代理池；
//     都没有时按直连 1 个 IP 计算；
//   - 按 key 计量：账号数按主凭证字段的非空行数统计（每行一个 key）。
func (g *Gateway) accountsFor(s plugin.Snapshot) pluginAccounts {
	acc := pluginAccounts{Kind: "key"}
	if g.quotaBasis(s) == "ip" {
		acc.Kind = "proxy"
		acc.Total = 1
		if g.Creds != nil {
			n := g.Creds.CountLines(s.Name, "proxy")
			if n == 0 {
				n = g.proxyPoolCount()
			}
			if n > 0 {
				acc.Total = n
			}
		}
	} else {
		field := "api_key"
		if len(s.AuthFields) > 0 {
			field = s.AuthFields[0]
		}
		if g.Creds != nil {
			acc.Total = g.Creds.CountLines(s.Name, field)
		}
		if acc.Total == 0 && s.AuthReady {
			acc.Total = 1
		}
	}
	if s.Healthy && (s.AuthType == "" || s.AuthType == "none" || s.AuthReady) {
		acc.Available = acc.Total
	}
	return acc
}

// credentialItem 是凭证列表项，只暴露字段名、数量与是否配置。
type credentialItem struct {
	Plugin     string         `json:"plugin"`
	AuthType   string         `json:"auth_type"`
	Quota      string         `json:"quota"`
	Fields     []string       `json:"fields"`
	Configured []string       `json:"configured"`
	Counts     map[string]int `json:"counts"`
	Ready      bool           `json:"ready"`
}

// handleCredentialList 列出各插件的凭证配置情况（永不返回明文）。
func (g *Gateway) handleCredentialList(w http.ResponseWriter, r *http.Request) {
	items := []credentialItem{}
	for _, snap := range g.Registry.Snapshot() {
		fields := append([]string{}, snap.AuthFields...)
		fields = appendUniqueString(fields, "proxy")
		item := credentialItem{
			Plugin:     snap.Name,
			AuthType:   snap.AuthType,
			Quota:      g.quotaBasis(snap),
			Fields:     fields,
			Configured: []string{},
			Counts:     map[string]int{},
			Ready:      snap.AuthReady,
		}
		if g.Creds != nil {
			item.Configured = g.Creds.Fields(snap.Name)
			for _, f := range fields {
				item.Counts[f] = g.Creds.CountLines(snap.Name, f)
			}
		}
		items = append(items, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"credentials": items})
}

// credentialSetRequest 是写入凭证的请求体。
type credentialSetRequest struct {
	Plugin string `json:"plugin"`
	Field  string `json:"field"`
	Value  string `json:"value"`
}

// handleCredentialSet 写入单个凭证字段。
func (g *Gateway) handleCredentialSet(w http.ResponseWriter, r *http.Request) {
	if g.Creds == nil {
		protocol.WriteErrorJSON(w, http.StatusServiceUnavailable, "server_error", "凭证存储不可用：请设置 FREE_API_CRED_KEY 环境变量")
		return
	}
	var req credentialSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "请求体格式错误")
		return
	}
	if req.Plugin == "" || req.Field == "" {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "plugin 与 field 均不能为空")
		return
	}
	if err := g.Creds.Set(req.Plugin, req.Field, req.Value); err != nil {
		protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handleCredentialDelete 删除某插件的某凭证字段。
func (g *Gateway) handleCredentialDelete(w http.ResponseWriter, r *http.Request) {
	if g.Creds == nil {
		protocol.WriteErrorJSON(w, http.StatusServiceUnavailable, "server_error", "凭证存储不可用")
		return
	}
	pluginName := r.URL.Query().Get("plugin")
	field := r.URL.Query().Get("field")
	if pluginName == "" || field == "" {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "plugin 与 field 查询参数必填")
		return
	}
	if err := g.Creds.Delete(pluginName, field); err != nil {
		protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handleSession 返回控制台登录状态。
func (g *Gateway) handleSession(w http.ResponseWriter, r *http.Request) {
	passwordSet := g.settings.PasswordSet()
	loggedIn := !passwordSet || g.settings.ValidSession(r)
	writeJSON(w, map[string]any{
		"password_set": passwordSet,
		"logged_in":    loggedIn,
		"api_key_set":  len(g.settings.APIKeys()) > 0,
	})
}

// handleLogin 校验控制台登录密码并写入会话 Cookie。
func (g *Gateway) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !g.settings.PasswordSet() {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "当前未设置登录密码")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "请求体格式错误")
		return
	}
	if !g.settings.VerifyPassword(req.Password) {
		time.Sleep(300 * time.Millisecond)
		protocol.WriteErrorJSON(w, http.StatusUnauthorized, "authentication_error", "密码错误")
		return
	}
	token, err := g.settings.CreateSession()
	if err != nil {
		protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	setSessionCookie(w, token)
	writeJSON(w, map[string]any{"ok": true})
}

// handleLogout 注销当前控制台会话。
func (g *Gateway) handleLogout(w http.ResponseWriter, r *http.Request) {
	g.settings.DropSession(sessionTokenFromRequest(r))
	clearSessionCookie(w)
	writeJSON(w, map[string]any{"ok": true})
}

// keyView 是设置接口返回的单个 API Key 视图。
type keyView struct {
	ID       string `json:"id"`
	Masked   string `json:"masked"`
	Enabled  bool   `json:"enabled"`
	Calls    int64  `json:"calls"`
	LastUsed string `json:"last_used,omitempty"`
}

// settingsView 是设置接口的返回结构。
func (g *Gateway) settingsView() map[string]any {
	infos := g.settings.KeyInfos()
	keys := make([]keyView, 0, len(infos))
	for _, info := range infos {
		usage := g.keyUsage(info.ID)
		keys = append(keys, keyView{
			ID:       info.ID,
			Masked:   info.Masked,
			Enabled:  info.Enabled,
			Calls:    usage.Calls,
			LastUsed: usage.LastUsed,
		})
	}
	return map[string]any{
		"password_set": g.settings.PasswordSet(),
		"api_key_set":  len(keys) > 0,
		"keys":         keys,
	}
}

// handleSettingsGet 返回控制台密码状态与网关 API Key。
func (g *Gateway) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, g.settingsView())
}

// handleSettingsSet 更新登录密码、网关 API Key 及其状态。
func (g *Gateway) handleSettingsSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password      *string   `json:"password"`
		ClearPassword bool      `json:"clear_password"`
		APIKeys       *[]string `json:"api_keys"`
		AddKeys       *[]string `json:"add_keys"`
		ToggleKey     string    `json:"toggle_key"`
		KeyEnabled    *bool     `json:"key_enabled"`
		DeleteKey     string    `json:"delete_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "请求体格式错误")
		return
	}

	switch {
	case req.ClearPassword:
		if err := g.settings.SetPassword(""); err != nil {
			protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
		clearSessionCookie(w)
	case req.Password != nil:
		password := *req.Password
		if err := g.settings.SetPassword(password); err != nil {
			protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
		if password == "" {
			clearSessionCookie(w)
		} else {
			token, err := g.settings.CreateSession()
			if err != nil {
				protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
				return
			}
			setSessionCookie(w, token)
		}
	}

	if req.APIKeys != nil {
		if err := g.settings.SetAPIKeys(*req.APIKeys); err != nil {
			protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
	}
	if req.AddKeys != nil {
		if err := g.settings.AddAPIKeys(*req.AddKeys); err != nil {
			protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
	}
	if req.ToggleKey != "" {
		enabled := true
		if req.KeyEnabled != nil {
			enabled = *req.KeyEnabled
		}
		if err := g.settings.ToggleAPIKey(req.ToggleKey, enabled); err != nil {
			protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	}
	if req.DeleteKey != "" {
		if err := g.settings.DeleteAPIKey(req.DeleteKey); err != nil {
			protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	}
	writeJSON(w, g.settingsView())
}

// proxyPoolCount 返回全局代理池的代理数量。
func (g *Gateway) proxyPoolCount() int {
	if g.Creds == nil {
		return 0
	}
	return g.Creds.CountLines(proxyPoolPlugin, "proxy")
}

// proxyPoolView 返回代理池状态（只展示脱敏后的地址）。
func (g *Gateway) proxyPoolView() map[string]any {
	proxies := []string{}
	if g.Creds != nil {
		if raw, ok := g.Creds.Get(proxyPoolPlugin, "proxy"); ok {
			for _, line := range splitNonEmpty(raw) {
				proxies = append(proxies, maskProxy(line))
			}
		}
	}
	return map[string]any{
		"count":   len(proxies),
		"proxies": proxies,
	}
}

// handleProxyGet 返回代理池概况。
func (g *Gateway) handleProxyGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, g.proxyPoolView())
}

// handleProxySet 覆盖或追加代理池。
func (g *Gateway) handleProxySet(w http.ResponseWriter, r *http.Request) {
	if g.Creds == nil {
		protocol.WriteErrorJSON(w, http.StatusServiceUnavailable, "server_error", "凭证存储不可用：请设置 FREE_API_CRED_KEY 环境变量")
		return
	}
	var req struct {
		Value  string `json:"value"`
		Append bool   `json:"append"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "请求体格式错误")
		return
	}
	proxies, err := normalizeProxyList(req.Value)
	if err != nil {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if len(proxies) == 0 {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "代理池不能为空")
		return
	}
	if req.Append {
		existing := []string{}
		if raw, ok := g.Creds.Get(proxyPoolPlugin, "proxy"); ok {
			existing = splitNonEmpty(raw)
		}
		proxies = mergeProxyLines(existing, proxies)
	}
	if err := g.Creds.Set(proxyPoolPlugin, "proxy", strings.Join(proxies, "\n")); err != nil {
		protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeJSON(w, g.proxyPoolView())
}

// handleProxyDelete 删除单个代理（index 查询参数），不带 index 时清空整个代理池。
func (g *Gateway) handleProxyDelete(w http.ResponseWriter, r *http.Request) {
	if g.Creds == nil {
		protocol.WriteErrorJSON(w, http.StatusServiceUnavailable, "server_error", "凭证存储不可用")
		return
	}
	indexStr := r.URL.Query().Get("index")
	if indexStr == "" {
		if err := g.Creds.Delete(proxyPoolPlugin, "proxy"); err != nil {
			protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
		writeJSON(w, g.proxyPoolView())
		return
	}
	idx, err := strconv.Atoi(indexStr)
	if err != nil || idx < 0 {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "index 必须是自然数")
		return
	}
	raw, ok := g.Creds.Get(proxyPoolPlugin, "proxy")
	if !ok {
		writeJSON(w, g.proxyPoolView())
		return
	}
	lines := splitNonEmpty(raw)
	if idx >= len(lines) {
		protocol.WriteErrorJSON(w, http.StatusBadRequest, "invalid_request_error", "代理不存在")
		return
	}
	lines = append(lines[:idx], lines[idx+1:]...)
	if len(lines) == 0 {
		err = g.Creds.Delete(proxyPoolPlugin, "proxy")
	} else {
		err = g.Creds.Set(proxyPoolPlugin, "proxy", strings.Join(lines, "\n"))
	}
	if err != nil {
		protocol.WriteErrorJSON(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeJSON(w, g.proxyPoolView())
}

// mergeProxyLines 合并两份代理列表并去重，保持先出现者在前。
func mergeProxyLines(existing, added []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(added))
	for _, line := range append(append([]string{}, existing...), added...) {
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

// normalizeProxyList 规范化多行代理配置：去空行、补协议、去重并逐行校验。
func normalizeProxyList(value string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for i, raw := range strings.Split(value, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		normalized, err := normalizeProxyLine(line)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", i+1, err)
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out, nil
}

// normalizeProxyLine 校验单个代理地址，未写协议时按 http 处理。
func normalizeProxyLine(line string) (string, error) {
	candidate := line
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("代理格式不正确: %s", line)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf("不支持的代理协议 %q", u.Scheme)
	}
	return u.String(), nil
}

// maskProxy 隐藏代理地址中的用户名与密码。
func maskProxy(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	return u.String()
}

// splitNonEmpty 按行拆分并去掉空行。
func splitNonEmpty(value string) []string {
	out := []string{}
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// appendUniqueString 仅在值不存在时追加。
func appendUniqueString(list []string, value string) []string {
	for _, v := range list {
		if v == value {
			return list
		}
	}
	return append(list, value)
}

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// handleStats 返回运行统计。
func (g *Gateway) handleStats(w http.ResponseWriter, r *http.Request) {
	snaps := g.Registry.Snapshot()
	byTools := map[string]int{"native": 0, "builtin": 0, "none": 0}
	models := 0
	for _, s := range snaps {
		byTools[s.Tools]++
		models += len(s.Models)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"plugins":    len(snaps),
		"by_tools":   byTools,
		"model_refs": models,
		"uptime":     time.Now().Format(time.RFC3339),
	})
}

// withCORS 允许浏览器控制台跨源调用。
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, anthropic-version")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ErrNotFound 用于静态资源缺失。
var ErrNotFound = errors.New("资源不存在")

// staticHandler 提供内置控制台页面。
func (g *Gateway) staticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/v1") {
			protocol.WriteErrorJSON(w, http.StatusNotFound, "not_found_error", "接口不存在")
			return
		}
		g.serveConsole(w, r)
	})
}

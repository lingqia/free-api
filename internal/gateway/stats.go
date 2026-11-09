package gateway

import (
	"time"

	"free-api/internal/event"
)

// PluginStats 是单个站点的运行统计。统计只保存在内存中，进程重启后归零。
type PluginStats struct {
	TotalCalls       int64   `json:"total_calls"`
	ActiveCalls      int64   `json:"active_calls"`
	SuccessCalls     int64   `json:"success_calls"`
	FailedCalls      int64   `json:"failed_calls"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	SuccessRate      float64 `json:"success_rate"`
}

// statsFor 返回插件统计对象，调用方需持有 statsMu。
func (g *Gateway) statsFor(name string) *PluginStats {
	if g.stats[name] == nil {
		g.stats[name] = &PluginStats{}
	}
	return g.stats[name]
}

// statsStart 记录一次插件调用开始。
func (g *Gateway) statsStart(name string) {
	g.statsMu.Lock()
	defer g.statsMu.Unlock()
	s := g.statsFor(name)
	s.TotalCalls++
	s.ActiveCalls++
}

// statsEnd 记录一次插件调用结束。
func (g *Gateway) statsEnd(name string, ok bool, usage *event.Usage) {
	g.statsMu.Lock()
	defer g.statsMu.Unlock()
	s := g.statsFor(name)
	if s.ActiveCalls > 0 {
		s.ActiveCalls--
	}
	if ok {
		s.SuccessCalls++
	} else {
		s.FailedCalls++
	}
	if usage != nil {
		s.PromptTokens += int64(usage.PromptTokens)
		s.CompletionTokens += int64(usage.CompletionTokens)
	}
}

// StatsOf 返回插件统计快照。
func (g *Gateway) StatsOf(name string) PluginStats {
	g.statsMu.Lock()
	defer g.statsMu.Unlock()
	s := g.statsFor(name)
	out := *s
	if done := s.SuccessCalls + s.FailedCalls; done > 0 {
		out.SuccessRate = float64(s.SuccessCalls) / float64(done) * 100
	}
	return out
}

// KeyUsage 是单个网关 API Key 的使用统计。
type KeyUsage struct {
	Calls    int64  `json:"calls"`
	LastUsed string `json:"last_used,omitempty"`
}

// recordKeyUse 记录一次携带该 Key 的 /v1 调用。
func (g *Gateway) recordKeyUse(token string) {
	id := keyID(token)
	g.keyMu.Lock()
	defer g.keyMu.Unlock()
	u := g.keyUse[id]
	u.Calls++
	u.LastUsed = time.Now().Format(time.RFC3339)
	g.keyUse[id] = u
}

// keyUsage 返回某个 Key 的使用统计快照。
func (g *Gateway) keyUsage(id string) KeyUsage {
	g.keyMu.Lock()
	defer g.keyMu.Unlock()
	return g.keyUse[id]
}

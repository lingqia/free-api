package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"free-api/internal/config"
)

// Registry 管理所有站点插件的生命周期与可用性。
type Registry struct {
	mu        sync.RWMutex
	instances map[string]*Instance
	// order 保存插件优先序（数字越小越优先），用于稳定排序。
	order map[string]int
	// tools 保存插件的工具档位：native / builtin / none。
	tools map[string]string
	// models 缓存「模型 ID → 插件名」索引。
	modelIndex map[string][]string
	// authFields 记录插件声明的认证字段。
	authFields map[string]config.Auth
	// configured 由凭证层注入的回调，用于判断插件认证是否齐备。
	configured func(plugin string, required []string) bool
}

// NewRegistry 构造空注册表。
func NewRegistry() *Registry {
	return &Registry{
		instances:  map[string]*Instance{},
		order:      map[string]int{},
		tools:      map[string]string{},
		modelIndex: map[string][]string{},
		authFields: map[string]config.Auth{},
	}
}

// SetCredentialCheck 注入认证完备性检查回调。
func (r *Registry) SetCredentialCheck(fn func(plugin string, required []string) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configured = fn
}

// StartAll 启动配置里所有启用的插件。单个插件失败不影响其它插件，
// 失败信息记录在注册表里供控制台展示。
func (r *Registry) StartAll(ctx context.Context, plugins []config.Plugin) {
	var wg sync.WaitGroup
	for _, p := range plugins {
		if !p.IsEnabled() {
			slog.Info("插件已禁用，跳过", "plugin", p.Name)
			continue
		}
		wg.Add(1)
		go func(p config.Plugin) {
			defer wg.Done()
			env := []string{}
			for k, v := range p.Env {
				env = append(env, fmt.Sprintf("%s=%s", k, v))
			}
			inst, err := Start(ctx, p.Name, p.Command, p.Args, env, p.Dir)
			if err != nil {
				slog.Error("插件启动失败", "plugin", p.Name, "err", err)
				r.mu.Lock()
				r.registerMeta(p, nil)
				r.mu.Unlock()
				return
			}
			slog.Info("插件已启动", "plugin", p.Name, "models", len(inst.Models()))
			r.mu.Lock()
			r.registerMeta(p, inst)
			r.mu.Unlock()
		}(p)
	}
	wg.Wait()
}

// registerMeta 登记插件元数据与模型索引（调用方需持锁）。
func (r *Registry) registerMeta(p config.Plugin, inst *Instance) {
	if inst != nil {
		r.instances[p.Name] = inst
	}
	r.order[p.Name] = p.Priority
	r.tools[p.Name] = p.Tools
	r.authFields[p.Name] = p.Auth
	for _, m := range p.Models {
		r.modelIndex[m] = append(r.modelIndex[m], p.Name)
	}
	if inst != nil {
		for _, m := range inst.Models() {
			found := false
			for _, existing := range r.modelIndex[m.ID] {
				if existing == p.Name {
					found = true
					break
				}
			}
			if !found {
				r.modelIndex[m.ID] = append(r.modelIndex[m.ID], p.Name)
			}
		}
	}
}

// Get 按名字取插件实例。
func (r *Registry) Get(name string) (*Instance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.instances[name]
	return inst, ok
}

// ToolsLevel 返回插件工具档位。
func (r *Registry) ToolsLevel(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if t, ok := r.tools[name]; ok {
		return t
	}
	return "none"
}

// AuthOf 返回插件认证声明。
func (r *Registry) AuthOf(name string) config.Auth {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.authFields[name]
}

// AuthReady 判断插件认证是否已配置齐全。
func (r *Registry) AuthReady(name string) bool {
	r.mu.RLock()
	p := r.authFields[name]
	fn := r.configured
	r.mu.RUnlock()
	if len(p.RequiredFields) == 0 {
		return true
	}
	if fn == nil {
		return false
	}
	return fn(name, p.RequiredFields)
}

// Candidates 返回某模型可用的插件列表，按优先级排序。
// requireTools 为 true 时只保留 native 档位，builtin 档位单独标记。
func (r *Registry) Candidates(model string, requireTools bool) []*Instance {
	r.mu.RLock()
	names := append([]string{}, r.modelIndex[model]...)
	r.mu.RUnlock()

	type scored struct {
		inst     *Instance
		priority int
	}
	list := []scored{}
	for _, name := range names {
		inst, ok := r.Get(name)
		if !ok || !inst.Healthy() {
			continue
		}
		level := r.ToolsLevel(name)
		if requireTools && level != "native" {
			continue
		}
		if !r.AuthReady(name) {
			continue
		}
		priority := 1000
		r.mu.RLock()
		if p, ok := r.order[name]; ok {
			priority = p
		}
		r.mu.RUnlock()
		list = append(list, scored{inst: inst, priority: priority})
	}

	sort.SliceStable(list, func(i, j int) bool { return list[i].priority < list[j].priority })

	out := make([]*Instance, 0, len(list))
	for _, s := range list {
		out = append(out, s.inst)
	}
	return out
}

// All 返回所有已启动的插件实例。
func (r *Registry) All() []*Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Instance, 0, len(r.instances))
	for _, inst := range r.instances {
		out = append(out, inst)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return r.order[out[i].Name] < r.order[out[j].Name]
	})
	return out
}

// Names 返回所有已登记插件名（含启动失败的）。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.order))
	for name := range r.order {
		out = append(out, name)
	}
	sort.SliceStable(out, func(i, j int) bool { return r.order[out[i]] < r.order[out[j]] })
	return out
}

// Restart 重启指定插件。
func (r *Registry) Restart(ctx context.Context, name string, p config.Plugin) error {
	if inst, ok := r.Get(name); ok {
		inst.Stop()
	}
	env := []string{}
	for k, v := range p.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	inst, err := Start(ctx, p.Name, p.Command, p.Args, env, p.Dir)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.instances[name] = inst
	r.mu.Unlock()
	return nil
}

// StopAll 关闭所有插件进程。
func (r *Registry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inst := range r.instances {
		inst.Stop()
	}
}

// Snapshot 是插件运行状态快照。
type Snapshot struct {
	Name        string   `json:"name"`
	Healthy     bool     `json:"healthy"`
	UptimeSec   int      `json:"uptime_seconds"`
	Tools       string   `json:"tools"`
	Models      []string `json:"models"`
	AuthType    string   `json:"auth_type"`
	AuthReady   bool     `json:"auth_ready"`
	AuthFields  []string `json:"auth_required_fields,omitempty"`
	LastError   string   `json:"last_error,omitempty"`
	Priority    int      `json:"priority"`
}

// Snapshot 生成当前插件状态快照，供控制台展示。
func (r *Registry) Snapshot() []Snapshot {
	r.mu.RLock()
	names := make([]string, 0, len(r.order))
	for name := range r.order {
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool { return r.order[names[i]] < r.order[names[j]] })

	type entry struct {
		inst     *Instance
		tools    string
		priority int
		auth     config.Auth
	}
	entries := make([]entry, 0, len(names))
	for _, name := range names {
		entries = append(entries, entry{
			inst:     r.instances[name],
			tools:    r.tools[name],
			priority: r.order[name],
			auth:     r.authFields[name],
		})
	}
	check := r.configured
	r.mu.RUnlock()

	out := make([]Snapshot, 0, len(entries))
	for i, name := range names {
		e := entries[i]
		snap := Snapshot{
			Name:       name,
			Tools:      e.tools,
			Priority:   e.priority,
			AuthType:   e.auth.Type,
			AuthFields: e.auth.RequiredFields,
		}
		if e.inst != nil {
			snap.Healthy = e.inst.Healthy()
			snap.UptimeSec = int(e.inst.Uptime().Seconds())
			snap.LastError = e.inst.LastError()
			for _, m := range e.inst.Models() {
				snap.Models = append(snap.Models, m.ID)
			}
		}
		switch {
		case len(e.auth.RequiredFields) == 0:
			snap.AuthReady = true
		case check != nil:
			snap.AuthReady = check(name, e.auth.RequiredFields)
		}
		out = append(out, snap)
	}
	return out
}

// WaitReady 等待所有插件完成首次探测，最多等待 d。
func (r *Registry) WaitReady(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		r.mu.RLock()
		allReady := true
		for _, inst := range r.instances {
			if !inst.Healthy() {
				allReady = false
				break
			}
		}
		r.mu.RUnlock()
		if allReady {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Package config 负责网关 YAML 配置的加载、默认值填充与校验。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Server 是 HTTP 服务相关配置。
type Server struct {
	Addr           string   `yaml:"addr"`
	APIKeys        []string `yaml:"api_keys"`
	RequestTimeout string   `yaml:"request_timeout"`
	MaxBodyBytes   int64    `yaml:"max_body_bytes"`
}

// Limits 是网关级限流与重试配置。
type Limits struct {
	PerPluginConcurrency int     `yaml:"per_plugin_concurrency"`
	PerPluginRPS         float64 `yaml:"per_plugin_rps"`
	MaxRetries           int     `yaml:"max_retries"`
}

// Auth 描述一个站点的认证要求。网关只负责按这里声明的字段名注入凭证，
// 不负责获取凭证；required_fields 未配置时该插件不参与路由。
type Auth struct {
	Type            string   `yaml:"type"`
	RequiredFields  []string `yaml:"required_fields"`
	OptionalFields  []string `yaml:"optional_fields"`
	RefreshEndpoint string   `yaml:"refresh_endpoint"`
}

// Plugin 是一个站点插件的声明。
type Plugin struct {
	Name        string            `yaml:"name"`
	Enabled     *bool             `yaml:"enabled"`
	Command     string            `yaml:"command"`
	Args        []string          `yaml:"args"`
	Dir         string            `yaml:"dir"`
	Env         map[string]string `yaml:"env"`
	Priority    int               `yaml:"priority"`
	Tools       string            `yaml:"tools"`
	Auth        Auth              `yaml:"auth"`
	Models      []string          `yaml:"models"`
	Timeout     string            `yaml:"timeout"`
	PerPluginRPS float64          `yaml:"per_plugin_rps"`
}

// IsEnabled 判断插件是否启用，默认启用。
func (p Plugin) IsEnabled() bool { return p.Enabled == nil || *p.Enabled }

// Config 是完整配置。
type Config struct {
	Server  Server   `yaml:"server"`
	Limits  Limits   `yaml:"limits"`
	Plugins []Plugin `yaml:"plugins"`
}

// Duration 解析插件级超时字符串，缺省回落到 fallback。
func (p Plugin) Duration(fallback time.Duration) time.Duration {
	if strings.TrimSpace(p.Timeout) == "" {
		return fallback
	}
	d, err := time.ParseDuration(p.Timeout)
	if err != nil {
		return fallback
	}
	return d
}

// Load 从路径读取 YAML 配置，应用默认值并校验。相对路径的插件命令
// 会被解析成相对于配置文件所在目录的绝对路径，保证网关从任意工作目录启动都能拉起插件。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		base = filepath.Dir(path)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	for i := range cfg.Plugins {
		p := &cfg.Plugins[i]
		if p.Command == "" {
			continue
		}
		// 插件路径优先按「网关启动工作目录」解析，其次才是配置文件所在目录。
		// 这样 ./plugins/xxx 这类相对路径符合直觉，而配置放在 configs/ 子目录也不会拼错。
		if !filepath.IsAbs(p.Command) && !fileExists(p.Command) {
			alt := filepath.Join(base, p.Command)
			if fileExists(alt) {
				p.Command = alt
			}
		}
		if p.Dir != "" && !filepath.IsAbs(p.Dir) && !dirExists(p.Dir) {
			alt := filepath.Join(base, p.Dir)
			if dirExists(alt) {
				p.Dir = alt
			}
		}
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Server.RequestTimeout == "" {
		c.Server.RequestTimeout = "180s"
	}
	if c.Server.MaxBodyBytes == 0 {
		c.Server.MaxBodyBytes = 8 << 20
	}
	if c.Limits.PerPluginConcurrency == 0 {
		c.Limits.PerPluginConcurrency = 2
	}
	if c.Limits.PerPluginRPS == 0 {
		c.Limits.PerPluginRPS = 1
	}
	if c.Limits.MaxRetries == 0 {
		c.Limits.MaxRetries = 2
	}
	for i := range c.Plugins {
		p := &c.Plugins[i]
		if p.Tools == "" {
			p.Tools = "none"
		}
		if p.Timeout == "" {
			p.Timeout = c.Server.RequestTimeout
		}
		if p.PerPluginRPS == 0 {
			p.PerPluginRPS = c.Limits.PerPluginRPS
		}
		if p.Auth.Type == "" {
			p.Auth.Type = "none"
		}
	}
}

// Validate 校验配置合法性。
func (c *Config) Validate() error {
	seen := map[string]bool{}
	for _, p := range c.Plugins {
		if p.Name == "" {
			return fmt.Errorf("插件缺少 name")
		}
		if seen[p.Name] {
			return fmt.Errorf("插件名重复: %s", p.Name)
		}
		seen[p.Name] = true
		switch p.Tools {
		case "native", "builtin", "none":
		default:
			return fmt.Errorf("插件 %s 的 tools 档位非法: %q (允许 native/builtin/none)", p.Name, p.Tools)
		}
		if p.Command == "" {
			return fmt.Errorf("插件 %s 缺少 command", p.Name)
		}
	}
	return nil
}

// RequestTimeout 返回服务级请求超时。
func (c *Config) RequestTimeout() time.Duration {
	d, err := time.ParseDuration(c.Server.RequestTimeout)
	if err != nil || d <= 0 {
		return 180 * time.Second
	}
	return d
}

// fileExists 判断路径是否存在且不是目录。
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// PluginRPSMap 返回插件级限流覆盖表。
func (c *Config) PluginRPSMap() map[string]float64 {
	out := map[string]float64{}
	for _, p := range c.Plugins {
		if p.PerPluginRPS > 0 {
			out[p.Name] = p.PerPluginRPS
		}
	}
	return out
}

// dirExists 判断路径是否存在且是目录。
func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

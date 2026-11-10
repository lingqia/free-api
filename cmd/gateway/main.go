// Command gateway 是通用 AI 网关入口。
//
// 它把多个站点插件统一暴露成三套兼容 API：
//   - POST /v1/chat/completions  (OpenAI Chat Completions)
//   - POST /v1/responses         (OpenAI Responses)
//   - POST /v1/messages          (Anthropic Messages)
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"free-api/internal/config"
	"free-api/internal/credential"
	"free-api/internal/gateway"
	"free-api/internal/plugin"
)

func main() {
	configPath := flag.String("config", "configs/gateway.yaml", "配置文件路径")
	credPath := flag.String("credentials", "configs/credentials.json", "凭证文件路径")
	settingsPath := flag.String("settings", "configs/settings.json", "控制台设置文件路径")
	debug := flag.Bool("debug", false, "输出调试日志")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("配置加载失败", "err", err)
		os.Exit(1)
	}

	// 凭证存储：缺少密钥时不阻断启动，只禁用凭证功能并给出提示。
	creds, err := credential.New(*credPath)
	if err != nil {
		if errors.Is(err, credential.ErrNoKey) {
			slog.Warn("未设置 FREE_API_CRED_KEY，凭证功能已禁用；需要登录的站点将无法参与路由")
		} else {
			slog.Error("凭证存储初始化失败", "err", err)
			os.Exit(1)
		}
	}

	reg := plugin.NewRegistry()
	if creds != nil {
		reg.SetCredentialCheck(func(name string, required []string) bool {
			for _, f := range required {
				if !creds.Has(name, f) {
					return false
				}
			}
			return true
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 控制台设置：登录密码与网关 API Key。文件不存在时沿用配置里的 API Key。
	settings, err := gateway.LoadSettings(*settingsPath, cfg.Server.APIKeys)
	if err != nil {
		slog.Error("控制台设置加载失败", "err", err)
		os.Exit(1)
	}

	// 统一代理入口：所有插件进程自动注入 HTTP_PROXY，按全局代理池轮换出口。
	// 插件不需要自己实现代理选择，池为空时直接放行。
	baseEnv := []string{}
	if creds != nil {
		hub := gateway.NewProxyHub(creds)
		if err := hub.Start(); err != nil {
			slog.Warn("本地代理入口启动失败，插件将直连", "err", err)
		} else {
			defer hub.Close()
			baseEnv = hub.Env()
			slog.Info("插件统一代理已启用", "addr", hub.Addr())
		}
	}

	slog.Info("正在启动插件…", "count", len(cfg.Plugins))
	reg.StartAll(ctx, cfg.Plugins, baseEnv)

	gw := gateway.New(reg, creds, gateway.Config{
		Timeout:              cfg.RequestTimeout(),
		Retries:              cfg.Limits.MaxRetries,
		PerPluginConcurrency: cfg.Limits.PerPluginConcurrency,
		PerPluginRPS:         cfg.Limits.PerPluginRPS,
		APIKeys:              cfg.Server.APIKeys,
		Settings:             settings,
		PluginRPS:            cfg.PluginRPSMap(),
	})

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           gw.Handler(cfg.Server.MaxBodyBytes),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 优雅退出：收到信号后关闭插件进程。
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("收到退出信号，正在关闭…")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)
		reg.StopAll()
		cancel()
	}()

	snaps := reg.Snapshot()
	for _, s := range snaps {
		slog.Info("插件状态",
			"name", s.Name,
			"healthy", s.Healthy,
			"tools", s.Tools,
			"models", len(s.Models),
			"auth_ready", s.AuthReady,
		)
	}

	slog.Info("网关已启动", "addr", cfg.Server.Addr, "config", mustAbs(*configPath))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP 服务异常退出", "err", err)
		os.Exit(1)
	}
	slog.Info("网关已停止")
}

// mustAbs 返回绝对路径，失败时原样返回。
func mustAbs(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

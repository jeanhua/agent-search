// agent-search：SearXNG 搜索 + trafilatura 网页阅读的轻量 AI 联网服务。
//
// 接口：
//
//	GET  /healthz         健康检查
//	POST /extract         URL -> 干净 Markdown/TXT
//	GET  /search          搜索互联网（代理 SearXNG JSON API）
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agent-search/internal/cache"
	"agent-search/internal/config"
	"agent-search/internal/fetch"
	"agent-search/internal/search"
	"agent-search/internal/server"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := config.FromEnv()

	fetcher := fetch.New(fetch.Config{
		UserAgent:      cfg.UserAgent,
		MaxRedirects:   cfg.MaxRedirects,
		MaxHTMLBytes:   cfg.MaxHTMLBytes,
		AllowPrivate:   cfg.AllowPrivate,
		ConnectTimeout: cfg.ConnectTimeout,
		ReadTimeout:    cfg.ReadTimeout,
		RobotsTimeout:  cfg.RobotsTimeout,
	}, nil)

	c := cache.New(cfg.CacheSize, cfg.CacheTTL)
	searxng := search.NewClient(cfg.SearxngURL, cfg.ReadTimeout)
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.New(cfg, fetcher, c, searxng).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("agent-search listening",
			"addr", cfg.ListenAddr,
			"searxng", cfg.SearxngURL,
			"cache_size", cfg.CacheSize,
			"cache_ttl", cfg.CacheTTL.String(),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("bye")
}

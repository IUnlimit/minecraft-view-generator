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

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/httpapi"
	"github.com/illtamer/minecraft-view-generator2/internal/renderer"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

// 由构建脚本通过 -ldflags -X 注入。
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	logger.Info("starting", "version", version, "commit", commit)
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	resourceManager, err := resources.NewManager(cfg, nil)
	if err != nil {
		logger.Error("initialize resource manager", "error", err)
		os.Exit(2)
	}
	adapter := renderer.ModelAdapter(renderer.UnavailableAdapter{Reason: "RENDERER_COMMAND is not configured"})
	if cfg.RendererCommand != "" {
		commandAdapter := renderer.CommandAdapter{Command: cfg.RendererCommand, MaxOutputBytes: cfg.RendererMaxOutputBytes, Version: "1.21.11"}
		if renderer.Info(commandAdapter).Status == "ready" {
			adapter = commandAdapter
		} else if cfg.AllowFallbackRenderer {
			adapter = renderer.PrimitiveAdapter{}
		} else {
			adapter = renderer.UnavailableAdapter{Reason: renderer.Info(commandAdapter).Error}
		}
	} else if cfg.AllowFallbackRenderer {
		adapter = renderer.PrimitiveAdapter{}
	}
	server := httpapi.NewServer(cfg, resourceManager, renderer.NewCompositor(adapter), logger)
	httpServer := &http.Server{Addr: cfg.ListenAddr, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.HTTPTimeout, WriteTimeout: cfg.HTTPTimeout, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		logger.Info("HTTP server listening", "addr", cfg.ListenAddr, "renderer", adapter.Backend())
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown failed", "error", err)
	}
}

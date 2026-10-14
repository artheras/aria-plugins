package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/artheras/aria-plugins/apps/commerce"
	"github.com/artheras/aria-plugins/packages/sdk"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.LevelKey {
			a.Key = "severity"
		}
		if a.Key == slog.MessageKey {
			a.Key = "message"
		}
		return a
	}}))
	slog.SetDefault(logger)
	config, err := commerce.FromEnvironment()
	if err != nil {
		logger.Error("server_failed", "error", err.Error())
		os.Exit(1)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "4180"
	}
	host := "127.0.0.1"
	if os.Getenv("VERCEL") != "" || os.Getenv("K_SERVICE") != "" || os.Getenv("ARIA_BIND_ALL") == "1" {
		host = ""
	}
	config.Logger = logger
	if config.MerchantToken != "" && config.MerchantID == "" {
		logger.Warn("legacy_merchant_identity", "message_detail", "Configure ARIA_MERCHANT_ID after reviewing the ownership migration guide; legacy credential-derived identity remains enabled for compatibility.")
	}
	if closer, ok := config.Store.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	server := &http.Server{Addr: host + ":" + port, Handler: commerce.New(config), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Info("server_started", "mode", config.Mode, "store", config.StorageKind, "version", sdk.Version)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server_failed", "error", err.Error())
		os.Exit(1)
	}
}

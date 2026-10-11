// commerce-worker is a bounded Cloud Run Job, not an HTTP background goroutine.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/artheras/aria-plugins/apps/commerce"
)

func main() {
	limit := flag.Int("batch", 100, "Maximum records per scan page (Redis COUNT is a hint)")
	maxPages := flag.Int("pages", 100, "Maximum scan pages per execution")
	timeout := flag.Duration("timeout", 8*time.Minute, "Maximum execution duration")
	flag.Parse()
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
	if *limit < 1 || *limit > 500 || *maxPages < 1 || *maxPages > 10000 || *timeout <= 0 {
		logger.Error("invalid_worker_configuration")
		os.Exit(1)
	}
	c, err := commerce.FromEnvironmentForWorker()
	if err != nil {
		logger.Error("worker_configuration_failed", "error", err.Error())
		os.Exit(1)
	}
	c.Logger = logger
	if closer, ok := c.Store.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	svc := commerce.NewService(c)
	parent, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, *timeout)
	defer cancel()
	cursor := ""
	recovered, pending := 0, 0
	for page := 0; page < *maxPages; page++ {
		next, n, p, err := svc.Recover(ctx, cursor, *limit)
		recovered += n
		pending += p
		if err != nil {
			logger.Error("recovery_failed", "recovered", recovered, "pending", pending)
			os.Exit(1)
		}
		if next == "" {
			logger.Info("recovery_completed", "recovered", recovered, "pending", pending)
			return
		}
		cursor = next
	}
	// Fail so the scheduler surfaces incomplete scans instead of silently starving later records.
	logger.Error("recovery_scan_limit", "recovered", recovered, "pending", pending)
	os.Exit(1)
}

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/artheras/aria-plugins/apps/commerce"
)

func main() {
	config, err := commerce.FromEnvironment()
	if err != nil {
		log.Fatal(err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "4180"
	}
	host := "127.0.0.1"
	if os.Getenv("VERCEL") != "" || os.Getenv("ARIA_BIND_ALL") == "1" {
		host = ""
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
	log.Printf("Aria Commerce: %s (%s)", config.PublicURL, config.Mode)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/server"
)

func main() {
	configPath := os.Getenv("INFERCORE_CONFIG")
	if configPath == "" {
		configPath = "configs/infercore.example.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config %q failed: %v", configPath, err)
	}

	srv := server.New(cfg)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	readTO, writeTO, idleTO := server.HTTPLayerTimeouts(cfg)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       readTO,
		WriteTimeout:      writeTO,
		IdleTimeout:       idleTO,
	}

	go func() {
		log.Printf("infercore listening on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = httpServer.Shutdown(context.Background())
}

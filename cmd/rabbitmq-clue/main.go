package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/saromanov/rabbitmq-clue/internal/api"
	"github.com/saromanov/rabbitmq-clue/internal/config"
	"github.com/saromanov/rabbitmq-clue/internal/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "err", err)
		os.Exit(1)
	}

	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		slog.Error("embedded UI is broken", "err", err)
		os.Exit(1)
	}
	handler := api.New(cfg).Handler(dist)

	slog.Info("rabbitmq-clue starting",
		"addr", cfg.Addr,
		"read_only", cfg.ReadOnly,
		"connections", len(cfg.Connections),
	)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

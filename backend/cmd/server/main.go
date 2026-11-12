// Befehl server startet das Go-Backend von „Was koch ich heute?“.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/raigen/was-koch-ich-heute/backend/internal/api"
	"github.com/raigen/was-koch-ich-heute/backend/internal/research"
)

func main() {
	researcher, err := research.New(research.Config{
		Provider: os.Getenv("AI_PROVIDER"),
		APIKey:   os.Getenv("ANTHROPIC_API_KEY"),
		Model:    os.Getenv("CLAUDE_MODEL"),
		Effort:   os.Getenv("CLAUDE_EFFORT"),
	})
	if err != nil {
		slog.Error("Konfiguration fehlerhaft", "err", err)
		os.Exit(1)
	}

	parallel, _ := strconv.Atoi(os.Getenv("PRICE_PARALLEL"))
	srv := api.New(researcher, api.Options{
		StaticDir: os.Getenv("STATIC_DIR"),
		Parallel:  parallel,
	})

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Kein WriteTimeout: Preisrecherchen werden über mehrere Minuten gestreamt.
	}

	go func() {
		slog.Info("Server läuft", "addr", addr, "ki", researcher.Name())
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server-Fehler", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

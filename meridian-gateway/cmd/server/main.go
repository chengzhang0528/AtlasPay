package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/config"
	"github.com/atlaspay/meridian-gateway/internal/gateway"
	"github.com/atlaspay/meridian-gateway/internal/httpapi"
	"github.com/atlaspay/meridian-gateway/internal/security"
	"github.com/atlaspay/meridian-gateway/internal/storage"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	enc, err := security.NewEncryptor(cfg.MasterKey)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := storage.Open(ctx, cfg.DatabasePath, enc)
	if err != nil {
		return err
	}
	defer store.Close()
	service := &gateway.Service{Store: store, BaseURL: cfg.BaseURL}
	worker := &gateway.Worker{Store: store, Service: service, Interval: cfg.WorkerInterval, HTTPClient: &http.Client{Timeout: cfg.WebhookTimeout}, Logger: logger}
	go worker.Run(ctx)
	api := &httpapi.Server{Gateway: service, AdminToken: cfg.AdminToken, BaseURL: cfg.BaseURL, Environment: cfg.Environment, AdminLocalOnly: cfg.AdminLocalOnly, Logger: logger, Assets: httpapi.WebAssets(), OpenAPI: httpapi.OpenAPISpec}
	httpServer := &http.Server{Addr: cfg.Address, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("Meridian Gateway started", "version", version, "config", cfg.String())
		err := httpServer.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return httpServer.Shutdown(shutdownCtx)
}

func healthcheck() error {
	base := os.Getenv("MERIDIAN_HEALTH_URL")
	if base == "" {
		base = "http://127.0.0.1:8080/healthz"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

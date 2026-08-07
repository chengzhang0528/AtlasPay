package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address        string
	BaseURL        string
	DatabasePath   string
	MasterKey      []byte
	AdminToken     string
	Environment    string
	WorkerInterval time.Duration
	WebhookTimeout time.Duration
	AdminLocalOnly bool
}

func Load() (Config, error) {
	cfg := Config{
		Address:        env("MERIDIAN_ADDRESS", ":8080"),
		BaseURL:        strings.TrimRight(env("MERIDIAN_BASE_URL", "http://127.0.0.1:8080"), "/"),
		DatabasePath:   env("MERIDIAN_DATABASE_PATH", "./data/meridian.db"),
		AdminToken:     strings.TrimSpace(os.Getenv("MERIDIAN_ADMIN_TOKEN")),
		Environment:    env("MERIDIAN_ENV", "production"),
		WorkerInterval: durationEnv("MERIDIAN_WORKER_INTERVAL", 2*time.Second),
		WebhookTimeout: durationEnv("MERIDIAN_WEBHOOK_TIMEOUT", 10*time.Second),
		AdminLocalOnly: boolEnv("MERIDIAN_ADMIN_LOCAL_ONLY", true),
	}

	rawKey := strings.TrimSpace(os.Getenv("MERIDIAN_MASTER_KEY"))
	if rawKey == "" {
		return Config{}, errors.New("MERIDIAN_MASTER_KEY is required (base64-encoded 32 bytes)")
	}
	key, err := base64.StdEncoding.DecodeString(rawKey)
	if err != nil || len(key) != 32 {
		return Config{}, errors.New("MERIDIAN_MASTER_KEY must be base64-encoded 32 bytes")
	}
	cfg.MasterKey = key
	if len(cfg.AdminToken) < 24 {
		return Config{}, errors.New("MERIDIAN_ADMIN_TOKEN must contain at least 24 characters")
	}
	if cfg.WorkerInterval < 250*time.Millisecond {
		return Config{}, errors.New("MERIDIAN_WORKER_INTERVAL must be at least 250ms")
	}
	if cfg.WebhookTimeout < time.Second || cfg.WebhookTimeout > time.Minute {
		return Config{}, errors.New("MERIDIAN_WEBHOOK_TIMEOUT must be between 1s and 1m")
	}
	return cfg, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolEnv(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func (c Config) String() string {
	return fmt.Sprintf("address=%s base_url=%s database=%s environment=%s", c.Address, c.BaseURL, c.DatabasePath, c.Environment)
}

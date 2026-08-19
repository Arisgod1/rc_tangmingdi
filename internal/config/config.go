package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"
)

type Config struct {
	GRPCAddr           string
	HTTPAddr           string
	DBPath             string
	WorkerPollInterval time.Duration
	LeaseDuration      time.Duration
	WebhookURL         string
	WebhookHeaders     map[string]string
	WebhookTimeout     time.Duration
}

func FromEnv() (Config, error) {
	poll, err := durationEnv("WORKER_POLL_INTERVAL", time.Second)
	if err != nil {
		return Config{}, err
	}
	lease, err := durationEnv("WORKER_LEASE_DURATION", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	timeout, err := durationEnv("WEBHOOK_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	headers := map[string]string{"Content-Type": "application/json"}
	if raw := strings.TrimSpace(os.Getenv("WEBHOOK_HEADERS_JSON")); raw != "" {
		var configured map[string]string
		if err := json.Unmarshal([]byte(raw), &configured); err != nil {
			return Config{}, fmt.Errorf("parse WEBHOOK_HEADERS_JSON: %w", err)
		}
		maps.Copy(headers, configured)
	}
	return Config{
		GRPCAddr: env("GRPC_ADDR", ":9000"), HTTPAddr: env("HTTP_ADDR", ":8080"),
		DBPath: env("DB_PATH", "notification.db"), WorkerPollInterval: poll,
		LeaseDuration: lease, WebhookURL: strings.TrimSpace(os.Getenv("WEBHOOK_URL")),
		WebhookHeaders: headers, WebhookTimeout: timeout,
	}, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return duration, nil
}

func Validate(c Config) error {
	if c.GRPCAddr == "" || c.HTTPAddr == "" || c.DBPath == "" {
		return fmt.Errorf("addresses and DB path are required")
	}
	if c.WorkerPollInterval <= 0 || c.LeaseDuration <= 0 || c.WebhookTimeout <= 0 {
		return fmt.Errorf("durations must be positive")
	}
	return nil
}

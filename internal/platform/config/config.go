// Package config loads runtime configuration from the environment. No secrets in code.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port            string
	DatabaseURL     string
	NATSURL         string
	JWTSecret       string
	WebhookSecret   string // shared HMAC secret with ScoreBoard
	SignatureScheme string // "hmac" (default) or "rsa"
	RSAPublicKey    string // PEM, required when SignatureScheme == "rsa"
	DevMode         bool   // enables the /dev/token minter
	WorkerPoolSize  int
	MigrationsDir   string
}

func Load() (Config, error) {
	c := Config{
		Port:            envOr("PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		NATSURL:         envOr("NATS_URL", "nats://127.0.0.1:4222"),
		JWTSecret:       envOr("JWT_SECRET", "dev-jwt-secret-change-me"),
		WebhookSecret:   envOr("WEBHOOK_HMAC_SECRET", "dev-webhook-secret-change-me"),
		SignatureScheme: envOr("SIGNATURE_SCHEME", "hmac"),
		RSAPublicKey:    os.Getenv("WEBHOOK_RSA_PUBLIC_KEY"),
		DevMode:         envOr("DEV_MODE", "false") == "true",
		WorkerPoolSize:  envInt("WORKER_POOL_SIZE", 4),
		MigrationsDir:   envOr("MIGRATIONS_DIR", ""),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

// MustSecret returns the JWT secret without requiring a full config (used by the
// offline `mint-token` subcommand, which needs no database).
func MustSecret() string {
	return envOr("JWT_SECRET", "dev-jwt-secret-change-me")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

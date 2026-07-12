// Package config loads runtime configuration from the environment.
//
// In development we lean on sensible defaults so a student can `go run` the app
// with zero setup beyond a running Postgres. In production every value is
// expected to come from the environment (Railway injects them).
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	// DatabaseURL is a standard libpq/pgx connection string.
	DatabaseURL string
	// Port is the TCP port the HTTP server listens on. Railway sets $PORT.
	Port string
	// Env is "dev" or "prod". It flips cookie Secure flags and the email sender.
	Env string
	// BaseURL is the public origin used to build absolute links in emails,
	// e.g. https://signflow.up.railway.app. In dev it is http://localhost:PORT.
	BaseURL string
	// SessionSecret signs/authenticates session cookies. Must be set in prod.
	SessionSecret string

	// Email configuration. EmailSender is "console" (default) or "resend".
	EmailSender  string
	ResendAPIKey string
	EmailFrom    string

	// UploadDir is where document files are stored on the local filesystem.
	// NOTE: Railway's container filesystem is ephemeral — see README. In prod,
	// point this at a mounted Railway Volume (or swap in object storage).
	UploadDir string
	// MaxUploadBytes caps a single upload.
	MaxUploadBytes int64
}

func (c Config) IsProd() bool { return c.Env == "prod" }

// Load reads configuration, applying dev-friendly defaults. It first tries to
// load a local .env file (ignored if absent) so secrets stay out of the shell.
func Load() (Config, error) {
	_ = godotenv.Load() // best-effort; fine if there is no .env

	cfg := Config{
		DatabaseURL:    getenv("DATABASE_URL", "postgres://postgres@localhost:5432/signflow?sslmode=disable"),
		Port:           getenv("PORT", "8080"),
		Env:            getenv("APP_ENV", "dev"),
		BaseURL:        os.Getenv("BASE_URL"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
		EmailSender:    getenv("EMAIL_SENDER", "console"),
		ResendAPIKey:   os.Getenv("RESEND_API_KEY"),
		EmailFrom:      os.Getenv("EMAIL_FROM"),
		UploadDir:      getenv("UPLOAD_DIR", "uploads"),
		MaxUploadBytes: 25 << 20, // 25 MiB
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:" + cfg.Port
	}

	if cfg.IsProd() {
		if cfg.SessionSecret == "" {
			return Config{}, fmt.Errorf("SESSION_SECRET is required in production")
		}
		if os.Getenv("BASE_URL") == "" {
			return Config{}, fmt.Errorf("BASE_URL is required in production")
		}
	}
	if cfg.SessionSecret == "" {
		// Dev-only fallback so sessions work out of the box. Never used in prod
		// because of the guard above.
		cfg.SessionSecret = "dev-insecure-session-secret-change-me"
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

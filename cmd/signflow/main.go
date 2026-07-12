// Command signflow is the HTTP server entrypoint.
//
// On startup it: loads config, runs embedded goose migrations, opens a pgx
// pool, builds the Chi router, and serves with graceful shutdown.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	migrations "github.com/programuoki/signflow/db"
	"github.com/programuoki/signflow/internal/config"
	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/email"
	"github.com/programuoki/signflow/internal/handlers"
	"github.com/programuoki/signflow/internal/session"
	"github.com/programuoki/signflow/internal/storage"
	"github.com/programuoki/signflow/static"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Info("starting signflow", "env", cfg.Env, "port", cfg.Port, "base_url", cfg.BaseURL)

	ctx := context.Background()

	// 1. Migrate. We use a throwaway database/sql handle (pgx stdlib driver)
	//    because goose speaks database/sql, then hand the app a pgx pool.
	if err := runMigrations(cfg.DatabaseURL, log); err != nil {
		return err
	}

	// 2. Connect the application pool used by sqlc.
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := db.New(pool)
	sessions := session.NewManager(queries, cfg.IsProd())
	mailer := email.New(cfg.EmailSender, cfg.ResendAPIKey, cfg.EmailFrom, log)
	store, err := storage.NewLocalStore(cfg.UploadDir)
	if err != nil {
		return err
	}
	log.Info("file storage: local disk", "dir", cfg.UploadDir)
	h := handlers.New(cfg, pool, queries, sessions, mailer, store, log)

	staticFS, err := fs.Sub(static.FS, "assets")
	if err != nil {
		return err
	}

	// Derive a stable 32-byte CSRF key from the session secret so there is only
	// one secret to manage.
	csrfKey := sha256.Sum256([]byte("csrf:" + cfg.SessionSecret))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           h.Router(staticFS, csrfKey[:]),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			stop()
		}
	}()

	<-shutdownCtx.Done()
	log.Info("shutting down")

	timeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(timeout)
}

// runMigrations applies all pending goose migrations from the embedded FS.
func runMigrations(databaseURL string, log *slog.Logger) error {
	sqlDB, err := goose.OpenDBWithDriver("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{log})
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		return err
	}
	return nil
}

// gooseLogger adapts slog to goose's logger interface.
type gooseLogger struct{ log *slog.Logger }

func (g gooseLogger) Fatalf(format string, v ...interface{}) {
	g.log.Error("goose", "msg", fmt.Sprintf(format, v...))
}
func (g gooseLogger) Printf(format string, v ...interface{}) {
	g.log.Info("goose", "msg", fmt.Sprintf(format, v...))
}

// ensure the pgx stdlib driver is registered for goose.OpenDBWithDriver("pgx", ...)
var _ = stdlib.GetDefaultDriver

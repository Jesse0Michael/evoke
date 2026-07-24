// Command evoke-registry is the hosted registry API: accounts, auth, and
// push/pull/list of immutable .evoke artifact versions backed by Postgres.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jesse0michael/evoke/internal/api"
	"github.com/jesse0michael/evoke/internal/ent"
	"github.com/jesse0michael/evoke/internal/store"
	"github.com/jesse0michael/pkg/auth"
	"github.com/jesse0michael/pkg/auth/oidc"
	"github.com/jesse0michael/pkg/boot"
	"github.com/jesse0michael/pkg/config"
	httpserver "github.com/jesse0michael/pkg/http/server"
	_ "github.com/lib/pq"
)

// Config is the top-level service configuration, composed of the shared pkg
// config structs plus this service's HTTP server config. boot.NewApp loads it
// (env + files + flags) and wires the logger; adding config.OpenTelemetryConfig
// here would have boot auto-initialize telemetry.
type Config struct {
	App      config.AppConfig
	Auth     auth.Config
	Google   oidc.GoogleAuthConfig
	Postgres config.PostgresConfig
	Server   httpserver.Config
}

func main() {
	// boot.NewApp handles signal-aware context, config loading, logger setup,
	// and (if configured) telemetry. app.Run supervises the runner, serves the
	// health/metrics endpoint, waits for shutdown, then calls Close.
	app := boot.NewApp[Config]()
	if err := app.Run(&registry{}); err != nil {
		os.Exit(1)
	}
}

// registry is the boot.Runner for the HTTP API. Run builds dependencies and
// starts serving without blocking; Close performs graceful shutdown.
type registry struct {
	httpServer *http.Server
	entClient  *ent.Client
}

func (r *registry) Run(ctx context.Context, cfg Config) error {
	sqlxDB, err := config.NewPostgresClient(cfg.Postgres)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	r.entClient = ent.NewClient(ent.Driver(entsql.OpenDB("postgres", sqlxDB.DB)))
	// Schema.Create is fine for the MVP; move to versioned migrations before prod.
	if err := r.entClient.Schema.Create(ctx); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	jwtAuth := auth.NewJWTAuth(cfg.Auth, jwt.SigningMethodHS256)

	// The verifier performs OIDC discovery against Google, so it needs network
	// at startup.
	verifier, err := oidc.NewGoogleAuth(ctx, cfg.Google)
	if err != nil {
		return fmt.Errorf("failed to build google verifier: %w", err)
	}

	// pkg/http/server builds the *http.Server (handler, addr, timeouts) from the
	// config and our Router (api.Server.Routes).
	srv := api.NewServer(store.New(r.entClient), jwtAuth, verifier)
	r.httpServer = httpserver.New(cfg.Server, srv).Server

	// Bind synchronously so a bind failure (e.g. port in use) surfaces as a Run
	// error boot can act on; then serve in the background.
	listener, err := net.Listen("tcp", r.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", r.httpServer.Addr, err)
	}

	slog.InfoContext(ctx, "registry server started", "port", cfg.Server.Port)
	go func() {
		if err := r.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(ctx, "http server stopped", "err", err)
		}
	}()
	return nil
}

func (r *registry) Close() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if r.httpServer != nil {
		_ = r.httpServer.Shutdown(shutdownCtx)
	}
	if r.entClient != nil {
		return r.entClient.Close()
	}
	return nil
}

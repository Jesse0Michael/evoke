// Package api is the hosted registry: a REST/HTTP service for publishing and
// retrieving immutable .evoke artifact versions, plus the accounts and auth
// that identify publishers. It is the "docker registry" side of the format —
// push/pull/list of content-addressed documents — and stays deliberately
// separate from the client-side internal/registry reference resolver.
package api

import (
	"net/http"

	"github.com/jesse0michael/evoke/internal/store"
	"github.com/jesse0michael/pkg/auth"
	"github.com/jesse0michael/pkg/auth/oidc"
	"github.com/jesse0michael/pkg/http/handlers"
	"github.com/jesse0michael/pkg/http/middleware"
)

// Server serves the registry HTTP API. It satisfies pkg/http/server.Router via
// Routes, so the *http.Server itself is constructed by that package.
type Server struct {
	store    store.Store
	auth     *auth.JWTAuth
	verifier oidc.Verifier
}

// NewServer constructs a registry API server. verifier validates the external
// ID tokens exchanged at login; jwtAuth issues and validates our own tokens.
func NewServer(st store.Store, jwtAuth *auth.JWTAuth, verifier oidc.Verifier) *Server {
	return &Server{store: st, auth: jwtAuth, verifier: verifier}
}

// Routes builds the HTTP routing table. Method+path patterns require Go 1.22+.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("GET /health", handlers.HandleHealth())

	// Auth: exchange a verified provider ID token for our token pair.
	mux.Handle("POST /v1/tokens/google", handlers.HandleOIDCLogin(s.verifier, s.auth, s.resolveOIDC))
	mux.Handle("POST /v1/tokens/refresh", handlers.HandleRefreshTokens(s.auth))

	// Account (self-service, authenticated as the token subject).
	mux.Handle("GET /v1/account", handlers.HandleWithMiddleware(s.getAccount(), middleware.Default, middleware.Auth(s.auth)))
	mux.Handle("PATCH /v1/account", handlers.HandleWithMiddleware(s.updateAccount(), middleware.Default, middleware.Auth(s.auth)))
	mux.Handle("DELETE /v1/account", handlers.HandleWithMiddleware(s.deleteAccount(), middleware.Default, middleware.Auth(s.auth)))

	// Artifact push requires an authenticated publisher; pull and list are open.
	mux.Handle("PUT /v1/{namespace}/{name}", handlers.HandleWithMiddleware(s.push(), middleware.Default, middleware.Auth(s.auth)))
	mux.HandleFunc("GET /v1/{namespace}/{name}", s.listVersions)
	mux.HandleFunc("GET /v1/{namespace}/{name}/{version}", s.pull)

	return mux
}

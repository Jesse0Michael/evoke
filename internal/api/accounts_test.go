package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jesse0michael/evoke/internal/ent/enttest"
	"github.com/jesse0michael/evoke/internal/store"
	"github.com/jesse0michael/pkg/auth"
	"github.com/jesse0michael/pkg/auth/oidc"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// fakeVerifier is a swappable oidc.Verifier for tests: set claims or err.
type fakeVerifier struct {
	claims *oidc.Claims
	err    error
}

func (f *fakeVerifier) Verify(context.Context, string) (*oidc.Claims, error) {
	return f.claims, f.err
}

func newTestServer(t *testing.T) (*Server, *fakeVerifier) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	jwtAuth := auth.NewJWTAuth(auth.Config{
		SecretKey:       []byte("test-secret"),
		Issuer:          "test",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: time.Hour,
	}, jwt.SigningMethodHS256)
	verifier := &fakeVerifier{}
	srv := NewServer(store.New(client), jwtAuth, verifier)
	return srv, verifier
}

func do(t *testing.T, srv *Server, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestLoginWithGoogle(t *testing.T) {
	verifiedClaims := &oidc.Claims{Provider: "google", Subject: "sub-1", Email: "alice@example.com", EmailVerified: true, Name: "Alice"}

	tests := []struct {
		name       string
		claims     *oidc.Claims
		verifyErr  error
		body       string
		wantStatus int
	}{
		{
			name:       "valid token creates account and issues tokens",
			claims:     verifiedClaims,
			body:       `{"id_token":"valid"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing id_token is a bad request",
			claims:     verifiedClaims,
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid id token is unauthorized",
			verifyErr:  errors.New("bad signature"),
			body:       `{"id_token":"bad"}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unverified email is unauthorized",
			claims:     &oidc.Claims{Provider: "google", Subject: "sub-2", Email: "eve@example.com", EmailVerified: false},
			body:       `{"id_token":"unverified"}`,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, verifier := newTestServer(t)
			verifier.claims = tt.claims
			verifier.err = tt.verifyErr

			rec := do(t, srv, http.MethodPost, "/v1/tokens/google", "", tt.body)
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())

			if tt.wantStatus == http.StatusOK {
				var resp authResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.NotEmpty(t, resp.AccessToken)
				require.NotEmpty(t, resp.RefreshToken)
				require.NotEmpty(t, resp.Subject)
			}
		})
	}
}

// authResponse mirrors the pkg handlers.AuthResponse for test deserialization.
type authResponse struct {
	Subject      string `json:"subject"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// login drives a google login and returns the issued access token.
func login(t *testing.T, srv *Server, verifier *fakeVerifier, sub, email string) string {
	t.Helper()
	verifier.claims = &oidc.Claims{Provider: "google", Subject: sub, Email: email, EmailVerified: true}
	verifier.err = nil
	rec := do(t, srv, http.MethodPost, "/v1/tokens/google", "", `{"id_token":"valid"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp authResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp.AccessToken
}

func TestAccountFlow(t *testing.T) {
	srv, verifier := newTestServer(t)
	token := login(t, srv, verifier, "sub-1", "alice@example.com")

	// Unauthenticated access is rejected by the auth middleware.
	require.Equal(t, http.StatusForbidden, do(t, srv, http.MethodGet, "/v1/account", "", "").Code)

	// GET /v1/account returns the authenticated account.
	rec := do(t, srv, http.MethodGet, "/v1/account", token, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got accountView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotEmpty(t, got.ID)
	require.Equal(t, "alice", got.Username)

	// PATCH updates the username.
	rec = do(t, srv, http.MethodPatch, "/v1/account", token, `{"username":"alice-cooper"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "alice-cooper", got.Username)

	// DELETE removes the account; the same token then resolves to nothing.
	require.Equal(t, http.StatusNoContent, do(t, srv, http.MethodDelete, "/v1/account", token, "").Code)
	require.Equal(t, http.StatusNotFound, do(t, srv, http.MethodGet, "/v1/account", token, "").Code)
}

func TestUpdateAccountUsernameConflict(t *testing.T) {
	srv, verifier := newTestServer(t)
	_ = login(t, srv, verifier, "sub-1", "bob@example.com") // owns username "bob"
	token := login(t, srv, verifier, "sub-2", "alice@example.com")

	rec := do(t, srv, http.MethodPatch, "/v1/account", token, `{"username":"bob"}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

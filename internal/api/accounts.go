package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/jesse0michael/evoke/internal/ent"
	"github.com/jesse0michael/evoke/internal/evoke/store"
	"github.com/jesse0michael/pkg/auth"
	httperrors "github.com/jesse0michael/pkg/http/errors"
)

// googleLoginRequest carries the Google ID token the client obtained through
// its own browser/PKCE (CLI) or Google Identity Services (web) flow.
type googleLoginRequest struct {
	IDToken string `json:"id_token"`
}

// tokenResponse is the login response carrying the account and our issued pair.
type tokenResponse struct {
	User         accountView `json:"user"`
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
}

type accountView struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
	Avatar   string `json:"avatar_url,omitempty"`
}

func toAccountView(u *ent.User) accountView {
	return accountView{ID: u.ID, Username: u.Username, Email: u.Email, Name: u.Name, Avatar: u.AvatarURL}
}

// loginWithGoogle verifies a Google ID token, create-or-links the account, and
// returns our own access/refresh token pair. Google is only involved here; every
// other endpoint authenticates with our tokens.
func (s *Server) loginWithGoogle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req googleLoginRequest
	if err := decodeJSON(r, &req); err != nil {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusBadRequest, "invalid request body", err.Error()))
		return
	}
	if req.IDToken == "" {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusBadRequest, "id_token is required", ""))
		return
	}

	claims, err := s.verifier.Verify(ctx, req.IDToken)
	if err != nil {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusUnauthorized, "invalid id token", ""))
		return
	}
	if !claims.EmailVerified {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusUnauthorized, "email not verified with provider", ""))
		return
	}

	u, err := s.store.FindOrCreateUserByIdentity(ctx, claims.Provider, claims.Subject, claims.Email, claims.Name, claims.Picture)
	if err != nil {
		httperrors.WriteError(ctx, w, err)
		return
	}

	s.issueTokens(ctx, w, u, http.StatusOK)
}

// issueTokens signs an access/refresh pair for the user and writes the response.
func (s *Server) issueTokens(ctx context.Context, w http.ResponseWriter, u *ent.User, status int) {
	access, refresh, err := s.auth.GenerateTokens(auth.WithSubject(u.ID))
	if err != nil {
		httperrors.WriteError(ctx, w, err)
		return
	}
	writeJSON(w, status, tokenResponse{
		User:         toAccountView(u),
		AccessToken:  access,
		RefreshToken: refresh,
	})
}

// getAccount returns the authenticated account ("me").
func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subject, ok := auth.Subject(ctx)
	if !ok {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusUnauthorized, "unauthenticated", ""))
		return
	}

	u, err := s.store.UserByID(ctx, subject)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusNotFound, "account not found", ""))
			return
		}
		httperrors.WriteError(ctx, w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAccountView(u))
}

type updateAccountRequest struct {
	Username string `json:"username"`
}

// updateAccount changes the authenticated account's username/handle.
func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subject, ok := auth.Subject(ctx)
	if !ok {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusUnauthorized, "unauthenticated", ""))
		return
	}

	var req updateAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusBadRequest, "invalid request body", err.Error()))
		return
	}
	if req.Username == "" {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusBadRequest, "username is required", ""))
		return
	}

	u, err := s.store.UpdateUsername(ctx, subject, req.Username)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusNotFound, "account not found", ""))
		case errors.Is(err, store.ErrConflict):
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusConflict, "username already taken", ""))
		default:
			httperrors.WriteError(ctx, w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, toAccountView(u))
}

// deleteAccount removes the authenticated account.
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subject, ok := auth.Subject(ctx)
	if !ok {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusUnauthorized, "unauthenticated", ""))
		return
	}

	if err := s.store.DeleteUser(ctx, subject); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusNotFound, "account not found", ""))
			return
		}
		httperrors.WriteError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

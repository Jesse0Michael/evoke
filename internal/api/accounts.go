package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/jesse0michael/evoke/internal/ent"
	"github.com/jesse0michael/evoke/internal/store"
	"github.com/jesse0michael/pkg/auth"
	"github.com/jesse0michael/pkg/auth/oidc"
	httperrors "github.com/jesse0michael/pkg/http/errors"
	server "github.com/jesse0michael/pkg/http/server"
)

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

// resolveOIDC is the OIDCResolver callback for HandleOIDCLogin. It checks email
// verification, create-or-links the account, and returns token options.
func (s *Server) resolveOIDC(ctx context.Context, claims *oidc.Claims) ([]auth.TokenOption, error) {
	if !claims.EmailVerified {
		return nil, httperrors.NewError(http.StatusUnauthorized, "email not verified with provider", "")
	}

	u, err := s.store.FindOrCreateUserByIdentity(ctx, claims.Provider, claims.Subject, claims.Email, claims.Name, claims.Picture)
	if err != nil {
		return nil, err
	}

	return []auth.TokenOption{auth.WithSubject(u.ID)}, nil
}

// getAccount returns the authenticated account ("me").
func (s *Server) getAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		_ = server.Encode(w, http.StatusOK, toAccountView(u))
	}
}

type updateAccountRequest struct {
	Username string `json:"username"`
}

// updateAccount changes the authenticated account's username/handle.
func (s *Server) updateAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		subject, ok := auth.Subject(ctx)
		if !ok {
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusUnauthorized, "unauthenticated", ""))
			return
		}

		var req updateAccountRequest
		if err := server.Decode(r.Body, &req); err != nil {
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
		_ = server.Encode(w, http.StatusOK, toAccountView(u))
	}
}

// deleteAccount removes the authenticated account.
func (s *Server) deleteAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
}

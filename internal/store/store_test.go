package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jesse0michael/evoke/internal/ent"
	"github.com/jesse0michael/evoke/internal/ent/enttest"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// newTestStore returns a Store backed by a fresh in-memory sqlite database.
func newTestStore(t *testing.T) (Store, *ent.Client) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	return New(client), client
}

func TestFindOrCreateUserByIdentity(t *testing.T) {
	tests := []struct {
		name         string
		seed         func(t *testing.T, s Store)
		provider     string
		subject      string
		email        string
		wantUsername string
		wantSameAs   string // email of a seeded user this call should resolve to
	}{
		{
			name:         "creates new account and derives username",
			seed:         func(*testing.T, Store) {},
			provider:     "google",
			subject:      "sub-1",
			email:        "alice@example.com",
			wantUsername: "alice",
		},
		{
			name: "same identity is idempotent",
			seed: func(t *testing.T, s Store) {
				_, err := s.FindOrCreateUserByIdentity(t.Context(), "google", "sub-1", "alice@example.com", "Alice", "")
				require.NoError(t, err)
			},
			provider:     "google",
			subject:      "sub-1",
			email:        "alice@example.com",
			wantUsername: "alice",
			wantSameAs:   "alice@example.com",
		},
		{
			name: "links a new identity to an existing account by email",
			seed: func(t *testing.T, s Store) {
				_, err := s.FindOrCreateUserByIdentity(t.Context(), "google", "sub-1", "alice@example.com", "Alice", "")
				require.NoError(t, err)
			},
			provider:     "github",
			subject:      "gh-9",
			email:        "alice@example.com",
			wantUsername: "alice",
			wantSameAs:   "alice@example.com",
		},
		{
			name: "derives a unique username when the handle is taken",
			seed: func(t *testing.T, s Store) {
				_, err := s.FindOrCreateUserByIdentity(t.Context(), "google", "sub-1", "alice@example.com", "Alice", "")
				require.NoError(t, err)
			},
			provider:     "google",
			subject:      "sub-2",
			email:        "alice@work.com",
			wantUsername: "alice-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestStore(t)
			tt.seed(t, s)

			u, err := s.FindOrCreateUserByIdentity(t.Context(), tt.provider, tt.subject, tt.email, "Name", "http://avatar")
			require.NoError(t, err)
			require.Equal(t, tt.wantUsername, u.Username)
			require.Equal(t, tt.email, u.Email)

			if tt.wantSameAs != "" {
				same, err := s.(*entStore).client.User.Query().All(t.Context())
				require.NoError(t, err)
				require.Len(t, same, 1, "expected the identity to resolve to the single seeded account")
				require.Equal(t, u.ID, same[0].ID)
			}
		})
	}
}

func TestFindOrCreateUserByIdentity_IdentityCount(t *testing.T) {
	s, client := newTestStore(t)
	ctx := context.Background()

	u, err := s.FindOrCreateUserByIdentity(ctx, "google", "sub-1", "alice@example.com", "Alice", "")
	require.NoError(t, err)

	// A second provider for the same email links to the same account.
	u2, err := s.FindOrCreateUserByIdentity(ctx, "github", "gh-9", "alice@example.com", "Alice", "")
	require.NoError(t, err)
	require.Equal(t, u.ID, u2.ID)

	identities, err := client.User.QueryIdentities(u).All(ctx)
	require.NoError(t, err)
	require.Len(t, identities, 2)
}

func TestUpdateUsername(t *testing.T) {
	tests := []struct {
		name      string
		seed      func(t *testing.T, s Store) string // returns target user id
		username  string
		wantErr   error
		wantValue string
	}{
		{
			name: "success",
			seed: func(t *testing.T, s Store) string {
				u, err := s.FindOrCreateUserByIdentity(t.Context(), "google", "sub-1", "alice@example.com", "Alice", "")
				require.NoError(t, err)
				return u.ID
			},
			username:  "alice-cooper",
			wantValue: "alice-cooper",
		},
		{
			name: "conflict when username taken",
			seed: func(t *testing.T, s Store) string {
				_, err := s.FindOrCreateUserByIdentity(t.Context(), "google", "sub-1", "bob@example.com", "Bob", "")
				require.NoError(t, err)
				u, err := s.FindOrCreateUserByIdentity(t.Context(), "google", "sub-2", "alice@example.com", "Alice", "")
				require.NoError(t, err)
				return u.ID
			},
			username: "bob",
			wantErr:  ErrConflict,
		},
		{
			name:     "not found",
			seed:     func(*testing.T, Store) string { return "missing-id" },
			username: "whoever",
			wantErr:  ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestStore(t)
			id := tt.seed(t, s)

			u, err := s.UpdateUsername(t.Context(), id, tt.username)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantValue, u.Username)
		})
	}
}

func TestDeleteUser(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	u, err := s.FindOrCreateUserByIdentity(ctx, "google", "sub-1", "alice@example.com", "Alice", "")
	require.NoError(t, err)

	require.NoError(t, s.DeleteUser(ctx, u.ID))

	_, err = s.UserByID(ctx, u.ID)
	require.True(t, errors.Is(err, ErrNotFound))

	// Deleting again reports not found.
	require.ErrorIs(t, s.DeleteUser(ctx, u.ID), ErrNotFound)
}

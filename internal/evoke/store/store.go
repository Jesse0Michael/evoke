// Package store is the persistence seam for the registry. The MVP
// implementation is ent-backed (Postgres) and keeps artifact content in a
// column on the version row. Because published versions are addressed by their
// sha256 digest, a future object-store backend can implement the same
// interface without changing the HTTP layer.
package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jesse0michael/evoke/internal/ent"
	"github.com/jesse0michael/evoke/internal/ent/artifact"
	"github.com/jesse0michael/evoke/internal/ent/identity"
	"github.com/jesse0michael/evoke/internal/ent/user"
	"github.com/jesse0michael/evoke/internal/ent/version"
)

// ErrNotFound is returned when a requested record does not exist. Callers map
// it to a 404 at the HTTP layer.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a uniqueness constraint would be violated (a
// taken username/email, or a duplicate namespace/name).
var ErrConflict = errors.New("conflict")

// Store persists registry accounts and immutable artifact versions.
type Store interface {
	// FindOrCreateUserByIdentity resolves the account for a verified external
	// identity (provider + subject). It returns the existing account if the
	// identity is already linked, links the identity to an account with a
	// matching email if one exists, or creates a fresh account (deriving a
	// unique username from the email) and links the identity to it.
	FindOrCreateUserByIdentity(ctx context.Context, provider, subject, email, name, avatarURL string) (*ent.User, error)
	// UserByID looks up an account by its id.
	UserByID(ctx context.Context, id string) (*ent.User, error)
	// UpdateUsername changes an account's handle.
	UpdateUsername(ctx context.Context, id, username string) (*ent.User, error)
	// DeleteUser removes an account and its linked identities and artifacts.
	DeleteUser(ctx context.Context, id string) error

	// PushVersion appends a new immutable version to namespace/name, creating
	// the artifact (owned by ownerID) if it does not yet exist. It assigns the
	// next monotonic version number and returns the created version.
	PushVersion(ctx context.Context, ownerID, namespace, name string, content []byte, sha256 string) (*ent.Version, error)
	// Versions lists every version of namespace/name, oldest first.
	Versions(ctx context.Context, namespace, name string) ([]*ent.Version, error)
	// Version returns a single version of namespace/name.
	Version(ctx context.Context, namespace, name string, ver int) (*ent.Version, error)
}

// entStore is the ent/Postgres implementation of Store.
type entStore struct {
	client *ent.Client
}

// New returns an ent-backed Store.
func New(client *ent.Client) Store {
	return &entStore{client: client}
}

// FindOrCreateUserByIdentity runs in a transaction so the identity lookup and
// the account create/link cannot race with a concurrent first login.
func (s *entStore) FindOrCreateUserByIdentity(ctx context.Context, provider, subject, email, name, avatarURL string) (*ent.User, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open transaction: %w", err)
	}

	// 1. Identity already linked → return its account.
	id, err := tx.Identity.Query().
		Where(identity.ProviderEQ(provider), identity.SubjectEQ(subject)).
		WithUser().
		Only(ctx)
	switch {
	case err == nil:
		u := id.Edges.User
		if u == nil {
			u, err = id.QueryUser().Only(ctx)
			if err != nil {
				return nil, rollback(tx, fmt.Errorf("failed to load identity user: %w", err))
			}
		}
		return u, commit(tx)
	case !ent.IsNotFound(err):
		return nil, rollback(tx, fmt.Errorf("failed to query identity: %w", err))
	}

	// 2. No identity yet. Link to an existing account with the same email, or
	//    create a fresh account with a unique derived username.
	u, err := tx.User.Query().Where(user.EmailEQ(email)).Only(ctx)
	switch {
	case ent.IsNotFound(err):
		username, uerr := uniqueUsername(ctx, tx, email)
		if uerr != nil {
			return nil, rollback(tx, uerr)
		}
		u, err = tx.User.Create().
			SetUsername(username).
			SetEmail(email).
			SetName(name).
			SetAvatarURL(avatarURL).
			Save(ctx)
		if err != nil {
			return nil, rollback(tx, fmt.Errorf("failed to create user: %w", err))
		}
	case err != nil:
		return nil, rollback(tx, fmt.Errorf("failed to query user by email: %w", err))
	}

	if _, err := tx.Identity.Create().
		SetProvider(provider).
		SetSubject(subject).
		SetUserID(u.ID).
		Save(ctx); err != nil {
		return nil, rollback(tx, fmt.Errorf("failed to link identity: %w", err))
	}

	return u, commit(tx)
}

func (s *entStore) UpdateUsername(ctx context.Context, id, username string) (*ent.User, error) {
	u, err := s.client.User.UpdateOneID(id).SetUsername(username).Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("%w: username already taken", ErrConflict)
		}
		return nil, fmt.Errorf("failed to update username: %w", err)
	}
	return u, nil
}

// DeleteUser removes an account and everything it owns. Children are deleted
// explicitly (rather than relying on FK cascade actions, which ent does not
// apply uniformly across dialects) inside a transaction.
func (s *entStore) DeleteUser(ctx context.Context, id string) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("failed to open transaction: %w", err)
	}

	exists, err := tx.User.Query().Where(user.IDEQ(id)).Exist(ctx)
	if err != nil {
		return rollback(tx, fmt.Errorf("failed to check user: %w", err))
	}
	if !exists {
		return rollback(tx, ErrNotFound)
	}

	artIDs, err := tx.Artifact.Query().Where(artifact.HasOwnerWith(user.IDEQ(id))).IDs(ctx)
	if err != nil {
		return rollback(tx, fmt.Errorf("failed to list artifacts: %w", err))
	}
	if len(artIDs) > 0 {
		if _, err := tx.Version.Delete().Where(version.HasArtifactWith(artifact.IDIn(artIDs...))).Exec(ctx); err != nil {
			return rollback(tx, fmt.Errorf("failed to delete versions: %w", err))
		}
		if _, err := tx.Artifact.Delete().Where(artifact.IDIn(artIDs...)).Exec(ctx); err != nil {
			return rollback(tx, fmt.Errorf("failed to delete artifacts: %w", err))
		}
	}
	if _, err := tx.Identity.Delete().Where(identity.HasUserWith(user.IDEQ(id))).Exec(ctx); err != nil {
		return rollback(tx, fmt.Errorf("failed to delete identities: %w", err))
	}
	if err := tx.User.DeleteOneID(id).Exec(ctx); err != nil {
		return rollback(tx, fmt.Errorf("failed to delete user: %w", err))
	}

	return commit(tx)
}

// usernameSanitize keeps lowercase alphanumerics and hyphens.
var usernameSanitize = regexp.MustCompile(`[^a-z0-9-]+`)

// uniqueUsername derives a handle from the email local-part and appends a
// numeric suffix until it is free. The check runs inside the caller's tx.
func uniqueUsername(ctx context.Context, tx *ent.Tx, email string) (string, error) {
	base := email
	if i := strings.IndexByte(base, '@'); i >= 0 {
		base = base[:i]
	}
	base = usernameSanitize.ReplaceAllString(strings.ToLower(base), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "user"
	}

	candidate := base
	for i := 2; ; i++ {
		exists, err := tx.User.Query().Where(user.UsernameEQ(candidate)).Exist(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to check username: %w", err)
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

func (s *entStore) UserByID(ctx context.Context, id string) (*ent.User, error) {
	u, err := s.client.User.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return u, nil
}

// PushVersion runs in a transaction so the find-or-create of the artifact and
// the version-number assignment cannot race with a concurrent push.
func (s *entStore) PushVersion(ctx context.Context, ownerID, namespace, name string, content []byte, sha256 string) (*ent.Version, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open transaction: %w", err)
	}

	art, err := tx.Artifact.Query().
		Where(artifact.NamespaceEQ(namespace), artifact.NameEQ(name)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		art, err = tx.Artifact.Create().
			SetNamespace(namespace).
			SetName(name).
			SetOwnerID(ownerID).
			Save(ctx)
		if err != nil {
			return nil, rollback(tx, fmt.Errorf("failed to create artifact: %w", err))
		}
	case err != nil:
		return nil, rollback(tx, fmt.Errorf("failed to query artifact: %w", err))
	}

	count, err := tx.Version.Query().
		Where(version.HasArtifactWith(artifact.IDEQ(art.ID))).
		Count(ctx)
	if err != nil {
		return nil, rollback(tx, fmt.Errorf("failed to count versions: %w", err))
	}

	v, err := tx.Version.Create().
		SetVersion(count + 1).
		SetContent(content).
		SetSha256(sha256).
		SetArtifactID(art.ID).
		Save(ctx)
	if err != nil {
		return nil, rollback(tx, fmt.Errorf("failed to create version: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit push: %w", err)
	}
	return v, nil
}

func (s *entStore) Versions(ctx context.Context, namespace, name string) ([]*ent.Version, error) {
	vs, err := s.client.Version.Query().
		Where(version.HasArtifactWith(artifact.NamespaceEQ(namespace), artifact.NameEQ(name))).
		Order(ent.Asc(version.FieldVersion)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list versions: %w", err)
	}
	return vs, nil
}

func (s *entStore) Version(ctx context.Context, namespace, name string, ver int) (*ent.Version, error) {
	v, err := s.client.Version.Query().
		Where(
			version.VersionEQ(ver),
			version.HasArtifactWith(artifact.NamespaceEQ(namespace), artifact.NameEQ(name)),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get version: %w", err)
	}
	return v, nil
}

// rollback wraps err with any rollback failure so neither is lost.
func rollback(tx *ent.Tx, err error) error {
	if rerr := tx.Rollback(); rerr != nil {
		return fmt.Errorf("%w: rollback failed: %w", err, rerr)
	}
	return err
}

// commit finalizes tx, wrapping any commit error.
func commit(tx *ent.Tx) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

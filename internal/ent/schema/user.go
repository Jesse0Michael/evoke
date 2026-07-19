package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// User is a registry account and the identified publisher of artifacts. The
// account carries no password: authentication happens through linked
// Identities (Google today, other providers later).
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			DefaultFunc(uuid.NewString).
			Unique().
			Immutable(),
		// username is the account handle and the basis for the publishing
		// namespace. Derived from the provider profile on first login; editable.
		field.String("username").
			Unique(),
		field.String("email").
			Unique(),
		field.String("name").
			Optional(),
		field.String("avatar_url").
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		// identities are the linked login methods (e.g. google:<sub>).
		edge.To("identities", Identity.Type),
		// artifacts a user has published.
		edge.To("artifacts", Artifact.Type),
	}
}

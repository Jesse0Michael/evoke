package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Identity is a linked login method for a User: an external provider plus the
// stable subject that provider assigns. Keeping this separate from User is what
// makes authentication pluggable — Google today, additional providers later,
// with no change to the account model.
type Identity struct {
	ent.Schema
}

func (Identity) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			DefaultFunc(uuid.NewString).
			Unique().
			Immutable(),
		// provider is the login method, e.g. "google".
		field.String("provider").
			Immutable(),
		// subject is the provider's stable, unique user identifier (Google's sub).
		field.String("subject").
			Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

func (Identity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("identities").
			Unique().
			Required().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Identity) Indexes() []ent.Index {
	return []ent.Index{
		// A (provider, subject) pair maps to exactly one account.
		index.Fields("provider", "subject").Unique(),
	}
}

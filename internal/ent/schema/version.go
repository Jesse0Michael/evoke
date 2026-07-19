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

// Version is an immutable published revision of an Artifact. Content is the raw
// .evoke bytes; sha256 is the hex digest clients verify against. Versions are
// monotonic per artifact (1, 2, 3, ...) and never mutated once written.
//
// Storage note: content lives in a Postgres column for the MVP. If artifacts
// ever grow large, swap this column for an object-store pointer behind the
// store.Store seam — the reference (sha256) already addresses the content.
type Version struct {
	ent.Schema
}

func (Version) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			DefaultFunc(uuid.NewString).
			Unique().
			Immutable(),
		field.Int("version").
			Immutable().
			Positive(),
		field.Bytes("content").
			Immutable(),
		field.String("sha256").
			Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

func (Version) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("artifact", Artifact.Type).
			Ref("versions").
			Unique().
			Required().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Version) Indexes() []ent.Index {
	return []ent.Index{
		// version numbers are unique within an artifact.
		index.Edges("artifact").Fields("version").Unique(),
	}
}

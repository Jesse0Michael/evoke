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

// Artifact is a named entry in a namespace (the registry-side analogue of a
// Docker repository). It carries no notion of what the .evoke content *is* —
// meaning emerges from the declarations inside each Version, never from here.
type Artifact struct {
	ent.Schema
}

func (Artifact) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			DefaultFunc(uuid.NewString).
			Unique().
			Immutable(),
		// namespace/name together form the reference path (namespace/name@version).
		// The hierarchy is organizational only; it does not define file semantics.
		field.String("namespace").
			Immutable(),
		field.String("name").
			Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (Artifact) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("artifacts").
			Unique().
			Required().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("versions", Version.Type),
	}
}

func (Artifact) Indexes() []ent.Index {
	return []ent.Index{
		// A namespace/name pair is globally unique.
		index.Fields("namespace", "name").Unique(),
	}
}

// Package schema defines the built-in Evoke declarations and their merge and
// channel semantics. It is the single source of truth for which declarations
// exist and which prefixes each one supports: "!" for the negative/exclusion
// channel and "?" for default contributions.
package schema

// MergeMode determines how multiple contributions to the same declaration
// combine during resolution.
type MergeMode string

const (
	// MergeSingular allows at most one explicit value; more than one explicit
	// value for the same channel is a conflict.
	MergeSingular MergeMode = "singular"
	// MergeAccumulating combines values in source order, deduplicating exact
	// matches.
	MergeAccumulating MergeMode = "accumulating"
)

// Definition describes one built-in declaration. Every declaration supports the
// plain (positive) form; Negative and Default report whether the "!" and "?"
// prefixes are additionally allowed.
type Definition struct {
	// Name is the canonical uppercase declaration name.
	Name string
	// Merge is how repeated contributions combine.
	Merge MergeMode
	// Negative reports whether the "!" exclusion channel is supported.
	Negative bool
	// Default reports whether the "?" default prefix is supported.
	Default bool
	// Order is the canonical render order, ascending. Renderers may override it,
	// but it provides a deterministic default.
	Order int
}

// builtins is the MVP declaration set, listed in canonical render order. Scene
// setting is carried entirely by ENVIRONMENT for the MVP; a dedicated LOCATION
// declaration is intentionally deferred.
var builtins = []Definition{
	{Name: "NAME", Merge: MergeSingular, Order: 10},
	{Name: "IDENTITY", Merge: MergeAccumulating, Order: 20},
	{Name: "PERSONALITY", Merge: MergeAccumulating, Negative: true, Default: true, Order: 30},
	{Name: "BACKSTORY", Merge: MergeAccumulating, Order: 40},
	{Name: "APPEARANCE", Merge: MergeAccumulating, Negative: true, Default: true, Order: 50},
	{Name: "APPAREL", Merge: MergeAccumulating, Negative: true, Default: true, Order: 60},
	{Name: "ENVIRONMENT", Merge: MergeAccumulating, Negative: true, Default: true, Order: 70},
	{Name: "SCENARIO", Merge: MergeSingular, Default: true, Order: 80},
	{Name: "PROMPT", Merge: MergeAccumulating, Negative: true, Default: true, Order: 90},
}

var byName = func() map[string]Definition {
	m := make(map[string]Definition, len(builtins))
	for _, d := range builtins {
		m[d.Name] = d
	}
	return m
}()

// Lookup returns the definition for a canonical (uppercase) declaration name and
// reports whether it is a known built-in.
func Lookup(name string) (Definition, bool) {
	d, ok := byName[name]
	return d, ok
}

// All returns a copy of the built-in declarations in canonical render order.
func All() []Definition {
	out := make([]Definition, len(builtins))
	copy(out, builtins)
	return out
}

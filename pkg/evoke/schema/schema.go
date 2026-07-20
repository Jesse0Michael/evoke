// Package schema defines the built-in Evoke declarations and their merge and
// channel semantics. It is the single source of truth for which declarations
// exist and which prefixes each one supports: "!" for the negative/exclusion
// channel and "?" for default contributions.
package schema

import "strings"

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
	// SelectorAliases are short CLI aliases for declaration-faceted selection.
	SelectorAliases []string
}

// builtins is the MVP declaration set, listed in canonical render order. Scene
// setting is carried entirely by ENVIRONMENT for the MVP; a dedicated LOCATION
// declaration is intentionally deferred.
var builtins = []Definition{
	{Name: "NAME", Merge: MergeSingular, Order: 10},
	{Name: "CHARACTER", Merge: MergeAccumulating, Order: 20, SelectorAliases: []string{"c"}},
	{Name: "PERSONALITY", Merge: MergeAccumulating, Negative: true, Default: true, Order: 30},
	{Name: "BACKSTORY", Merge: MergeAccumulating, Order: 40},
	{Name: "APPEARANCE", Merge: MergeAccumulating, Negative: true, Default: true, Order: 50, SelectorAliases: []string{"ap"}},
	{Name: "APPAREL", Merge: MergeAccumulating, Negative: true, Default: true, Order: 60, SelectorAliases: []string{"a"}},
	{Name: "ENVIRONMENT", Merge: MergeAccumulating, Negative: true, Default: true, Order: 70, SelectorAliases: []string{"e"}},
	{Name: "SCENARIO", Merge: MergeSingular, Default: true, Order: 80},
	{Name: "PROMPT", Merge: MergeAccumulating, Negative: true, Default: true, Order: 90, SelectorAliases: []string{"p"}},
}

var byName = func() map[string]Definition {
	m := make(map[string]Definition, len(builtins))
	for _, d := range builtins {
		m[d.Name] = d
	}
	return m
}()

// byAlias maps short selector aliases to their canonical declaration name.
var byAlias = func() map[string]string {
	m := make(map[string]string)
	for _, d := range builtins {
		for _, alias := range d.SelectorAliases {
			m[alias] = d.Name
		}
		// The full lowercase name is also a valid facet.
		m[strings.ToLower(d.Name)] = d.Name
	}
	return m
}()

// migrationAliases maps deprecated declaration names to their canonical
// replacement. The parser uses this for backward compatibility.
var migrationAliases = map[string]string{
	"IDENTITY": "CHARACTER",
}

// Lookup returns the definition for a canonical (uppercase) declaration name and
// reports whether it is a known built-in.
func Lookup(name string) (Definition, bool) {
	d, ok := byName[name]
	return d, ok
}

// MigrationAlias returns the canonical name for a deprecated declaration alias,
// or empty string if none exists.
func MigrationAlias(name string) string {
	return migrationAliases[name]
}

// ResolveFacet maps a selector facet (short alias or full name, case-insensitive)
// to the canonical declaration name. Returns the name and true if found.
func ResolveFacet(facet string) (string, bool) {
	name, ok := byAlias[strings.ToLower(facet)]
	return name, ok
}

// All returns a copy of the built-in declarations in canonical render order.
func All() []Definition {
	out := make([]Definition, len(builtins))
	copy(out, builtins)
	return out
}

// Package ast defines the parsed representation of an Evoke source file.
//
// The AST mirrors the source syntax: an ordered list of declaration blocks,
// each with its values as plain strings. Declarations retain the line they
// start on purely so diagnostics can point at the offending line; values carry
// no source location (tracing an output value back to its origin file is not an
// MVP goal).
package ast

// Declaration is one declaration block: a header line naming the declaration
// (optionally prefixed) followed by one or more indented value lines.
//
// The prefix is decomposed into two orthogonal properties rather than kept as a
// raw string: Negative selects the exclusion channel ("!") and Default marks a
// default contribution ("?"). Both may be true for the "?!" prefix. These map
// directly onto the resolver's channel and default selection.
type Declaration struct {
	// Name is the canonical (uppercased) declaration name.
	Name string
	// RawName is the name exactly as written in the source, preserved for
	// diagnostics.
	RawName string
	// Negative reports whether the "!" prefix was present, selecting the
	// negative/exclusion channel.
	Negative bool
	// Default reports whether the "?" prefix was present, marking these values
	// as defaults used only when no explicit contribution exists.
	Default bool
	// Values are the declaration's value lines in source order.
	Values []string
	// Line is the 1-based line the header appears on, used only for diagnostics.
	Line int
}

// Document is a parsed Evoke source file: an ordered list of declaration blocks.
// Order is preserved because accumulating declarations render their values in
// source order.
type Document struct {
	// Metadata holds document-level metadata parsed from source (e.g. TAGS).
	Metadata     Metadata
	Declarations []*Declaration
}

// Metadata holds document-level information that is not a mergeable declaration.
type Metadata struct {
	// Tags are normalized (lowercase, trimmed) discovery tags from the TAGS block.
	Tags []string
}

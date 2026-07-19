// Package validate performs declaration-level semantic checks on a parsed Evoke
// document: that each declaration is a known built-in and that any "!" or "?"
// prefix it carries is supported by that declaration. Syntactic correctness is
// already guaranteed by the parser, so validation operates on a well-formed AST.
//
// This is per-file validation. Cross-file composition checks (singular
// conflicts across multiple files, target requirements) belong to the resolve
// and render stages.
package validate

import (
	"errors"
	"fmt"

	"github.com/jesse0michael/evoke/internal/evoke/ast"
	"github.com/jesse0michael/evoke/internal/evoke/schema"
)

// Error is a semantic error on a specific declaration's line.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// Document checks every declaration in doc against the built-in schema and
// returns the joined set of semantic errors, or nil when the document is valid.
func Document(doc *ast.Document) error {
	var errs []error
	for _, decl := range doc.Declarations {
		def, ok := schema.Lookup(decl.Name)
		if !ok {
			errs = append(errs, &Error{Line: decl.Line, Msg: fmt.Sprintf("unknown declaration %q", decl.RawName)})
			continue
		}
		if decl.Negative && !def.Negative {
			errs = append(errs, &Error{Line: decl.Line, Msg: fmt.Sprintf("%s does not support the ! (negative) prefix", def.Name)})
		}
		if decl.Default && !def.Default {
			errs = append(errs, &Error{Line: decl.Line, Msg: fmt.Sprintf("%s does not support the ? (default) prefix", def.Name)})
		}
	}
	return errors.Join(errs...)
}

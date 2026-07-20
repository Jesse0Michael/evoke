// Package parser turns Evoke source bytes into an [ast.Document].
//
// The Evoke syntax is line-oriented and block-structured, so the parser scans
// the source line by line rather than using a separate token stream:
//
//   - A declaration header starts at column 1 (no indentation) and is an
//     optional prefix ("!", "?", or "?!") immediately followed by a name.
//   - Value lines are indented and belong to the most recent header.
//   - Blank lines and lines whose first non-space rune is '#' are ignored.
//
// The parser only enforces syntax. It does not know which declarations exist or
// which prefixes they support; those are declaration-level concerns handled by
// the schema and validate stages. Unknown names parse happily here.
package parser

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jesse0michael/evoke/pkg/evoke/ast"
	"github.com/jesse0michael/evoke/pkg/evoke/schema"
)

// Error is a syntax error on a specific source line.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// Parse parses Evoke source into a document. It accumulates every syntax error
// it encounters rather than stopping at the first, so callers can report them
// all at once; the returned error is the joined set (nil when the source is
// valid). The returned document reflects the successfully parsed declarations
// even when errors are present.
func Parse(src []byte) (*ast.Document, error) {
	p := &parser{doc: &ast.Document{}}
	p.run(src)
	return p.doc, errors.Join(p.errs...)
}

type parser struct {
	doc     *ast.Document
	errs    []error
	current *ast.Declaration // declaration currently accepting value lines
	isTags  bool             // true when current is the TAGS metadata block
}

func (p *parser) errorf(line int, format string, args ...any) {
	p.errs = append(p.errs, &Error{Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (p *parser) run(src []byte) {
	if !utf8.Valid(src) {
		p.errorf(1, "file is not valid UTF-8")
		return
	}

	// Splitting on "\n" and trimming a trailing "\r" handles both LF and CRLF
	// line endings without depending on the platform.
	for i, raw := range strings.Split(string(src), "\n") {
		p.line(i+1, strings.TrimSuffix(raw, "\r"))
	}
	p.finish()
}

func (p *parser) line(lineNo int, line string) {
	indent, rest := splitIndent(line)
	trimmed := strings.TrimRight(rest, " ")

	// Blank line: does not terminate the current block; a value block may
	// contain blank lines and continues until the next header.
	if trimmed == "" {
		return
	}

	// Comment: any line whose first non-space rune is '#', at any indentation.
	if strings.HasPrefix(trimmed, "#") {
		return
	}

	if strings.ContainsRune(indent, '\t') {
		p.errorf(lineNo, "tabs are not allowed for indentation; use spaces")
		return
	}

	if indent == "" {
		p.header(lineNo, trimmed)
		return
	}
	p.value(lineNo, trimmed)
}

// header parses a declaration header line and makes it the current block.
func (p *parser) header(lineNo int, text string) {
	p.finish() // close out the previous declaration before starting a new one

	isDefault, negative, name := splitPrefix(text)
	name = strings.TrimLeft(name, " \t")
	if name == "" {
		p.errorf(lineNo, "declaration is missing a name")
		return
	}
	if name[0] == '!' || name[0] == '?' {
		p.errorf(lineNo, "invalid prefix; only \"!\", \"?\", and \"?!\" are allowed before a declaration name")
		return
	}
	if fields := strings.Fields(name); len(fields) > 1 {
		p.errorf(lineNo, "unexpected text %q after declaration name; values belong on indented lines", strings.Join(fields[1:], " "))
		return
	}
	if !validName(name) {
		p.errorf(lineNo, "invalid declaration name %q", name)
		return
	}

	canonical := strings.ToUpper(name)

	// TAGS is document metadata, not a mergeable declaration.
	if canonical == "TAGS" {
		if isDefault || negative {
			p.errorf(lineNo, "TAGS does not support prefixes")
			return
		}
		p.current = &ast.Declaration{
			Name:    "TAGS",
			RawName: name,
			Line:    lineNo,
		}
		p.isTags = true
		return
	}

	// Migration alias: IDENTITY → CHARACTER.
	if alias := schema.MigrationAlias(canonical); alias != "" {
		canonical = alias
	}

	p.current = &ast.Declaration{
		Name:     canonical,
		RawName:  name,
		Negative: negative,
		Default:  isDefault,
		Line:     lineNo,
	}
	p.isTags = false
}

// value attaches an indented line to the current declaration.
func (p *parser) value(lineNo int, text string) {
	if p.current == nil {
		p.errorf(lineNo, "indented value has no preceding declaration")
		return
	}
	p.current.Values = append(p.current.Values, text)
}

// finish appends the current declaration to the document, rejecting empty
// blocks (a header with no value lines).
func (p *parser) finish() {
	if p.current == nil {
		return
	}
	if len(p.current.Values) == 0 {
		p.errorf(p.current.Line, "declaration %q has no values", p.current.RawName)
	} else if p.isTags {
		p.finishTags()
	} else {
		p.doc.Declarations = append(p.doc.Declarations, p.current)
	}
	p.current = nil
	p.isTags = false
}

// finishTags normalizes and validates tag values, storing them as document metadata.
func (p *parser) finishTags() {
	seen := make(map[string]bool)
	for _, raw := range p.current.Values {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if tag == "" {
			continue
		}
		if strings.ContainsAny(tag, " \t:+") {
			p.errorf(p.current.Line, "invalid tag %q: tags must not contain spaces, '+', or ':'", raw)
			continue
		}
		if !seen[tag] {
			seen[tag] = true
			p.doc.Metadata.Tags = append(p.doc.Metadata.Tags, tag)
		}
	}
}

// splitIndent separates leading whitespace (spaces and/or tabs) from the rest
// of the line.
func splitIndent(line string) (indent, rest string) {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return line[:n], line[n:]
}

// splitPrefix consumes a leading "?" and/or "!" prefix and returns the prefix
// flags along with the remaining text. Only the canonical orders "", "!", "?",
// and "?!" set both flags cleanly; a stray leading operator is left on name for
// the caller to reject.
func splitPrefix(text string) (isDefault, negative bool, name string) {
	name = text
	if strings.HasPrefix(name, "?") {
		isDefault = true
		name = name[1:]
	}
	if strings.HasPrefix(name, "!") {
		negative = true
		name = name[1:]
	}
	return isDefault, negative, name
}

// validName reports whether name contains only letters, digits, underscores, or
// hyphens. Namespaced extension declarations (dotted names) are intentionally
// out of scope for the MVP.
func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

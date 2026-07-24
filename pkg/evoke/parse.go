package evoke

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// Parse parses .evoke source into a Document.
func Parse(src []byte) (*Document, error) {
	p := &evokeParser{doc: &Document{}}
	p.run(src)
	return p.doc, errors.Join(p.errs...)
}

type evokeParser struct {
	doc     *Document
	errs    []error
	current *Declaration
	isTags  bool
}

func (p *evokeParser) errorf(line int, format string, args ...any) {
	p.errs = append(p.errs, &ParseError{Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (p *evokeParser) run(src []byte) {
	if !utf8.Valid(src) {
		p.errorf(1, "file is not valid UTF-8")
		return
	}
	for i, raw := range strings.Split(string(src), "\n") {
		p.line(i+1, strings.TrimSuffix(raw, "\r"))
	}
	p.finish()
}

func (p *evokeParser) line(lineNo int, line string) {
	indent, rest := splitIndent(line)
	trimmed := strings.TrimRight(rest, " ")

	if trimmed == "" {
		return
	}
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

func (p *evokeParser) header(lineNo int, text string) {
	p.finish()

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

	var argument string
	if fields := strings.Fields(name); len(fields) > 1 {
		name = fields[0]
		argument = fields[1]
		if len(fields) > 2 {
			p.errorf(lineNo, "unexpected text %q after declaration argument; values belong on indented lines", strings.Join(fields[2:], " "))
			return
		}
		if !validName(argument) {
			p.errorf(lineNo, "invalid declaration argument %q", argument)
			return
		}
	}
	if !validName(name) {
		p.errorf(lineNo, "invalid declaration name %q", name)
		return
	}

	canonical := strings.ToUpper(name)

	if canonical == "TAGS" {
		if isDefault || negative {
			p.errorf(lineNo, "TAGS does not support prefixes")
			return
		}
		p.current = &Declaration{Name: "TAGS", RawName: name, Line: lineNo}
		p.isTags = true
		return
	}

	if alias := MigrationAlias(canonical); alias != "" {
		canonical = alias
	}

	p.current = &Declaration{
		Name:     canonical,
		RawName:  name,
		Argument: argument,
		Negative: negative,
		Default:  isDefault,
		Line:     lineNo,
	}
	p.isTags = false
}

func (p *evokeParser) value(lineNo int, text string) {
	if p.current == nil {
		p.errorf(lineNo, "indented value has no preceding declaration")
		return
	}
	p.current.Values = append(p.current.Values, text)
}

func (p *evokeParser) finish() {
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

func (p *evokeParser) finishTags() {
	seen := make(map[string]bool)
	for _, raw := range p.current.Values {
		// Tags can be comma-separated or newline-separated.
		parts := strings.Split(raw, ",")
		for _, part := range parts {
			tag := strings.ToLower(strings.TrimSpace(part))
			if tag == "" {
				continue
			}
			if !seen[tag] {
				seen[tag] = true
				p.doc.Metadata.Tags = append(p.doc.Metadata.Tags, tag)
			}
		}
	}
}

func splitIndent(line string) (indent, rest string) {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return line[:n], line[n:]
}

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

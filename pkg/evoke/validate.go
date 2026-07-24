package evoke

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type ValidationError struct {
	Line int
	Msg  string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// Validate checks every declaration in doc against the built-in schema.
func Validate(doc *Document) error {
	var errs []error
	for _, decl := range doc.Declarations {
		def, ok := LookupDeclaration(decl.Name)
		if !ok {
			errs = append(errs, &ValidationError{Line: decl.Line, Msg: fmt.Sprintf("unknown declaration %q", decl.RawName)})
			continue
		}
		if decl.Negative && !def.Negative {
			errs = append(errs, &ValidationError{Line: decl.Line, Msg: fmt.Sprintf("%s does not support the ! (negative) prefix", def.Name)})
		}
		if decl.Default && !def.Default {
			errs = append(errs, &ValidationError{Line: decl.Line, Msg: fmt.Sprintf("%s does not support the ? (default) prefix", def.Name)})
		}
		if decl.Argument != "" && !def.AcceptsArgument {
			errs = append(errs, &ValidationError{Line: decl.Line, Msg: fmt.Sprintf("%s does not accept an argument", def.Name)})
		}
		if decl.Argument == "" && def.RequiresArgument {
			errs = append(errs, &ValidationError{Line: decl.Line, Msg: fmt.Sprintf("%s requires an argument", def.Name)})
		}
		if decl.Negative && def.Structured && hasSettings(decl.Values) {
			errs = append(errs, &ValidationError{Line: decl.Line, Msg: fmt.Sprintf("negative %s must not contain settings (key=value lines)", def.Name)})
		}
	}
	return errors.Join(errs...)
}

var settingPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\s*=`)

// IsSetting reports whether a value line is a key=value setting.
func IsSetting(line string) bool {
	return settingPattern.MatchString(line)
}

// ParseSetting splits a setting line into key and value.
// Returns empty strings if the line is not a setting.
func ParseSetting(line string) (key, value string) {
	if !IsSetting(line) {
		return "", ""
	}
	k, v, _ := strings.Cut(line, "=")
	return strings.TrimSpace(k), strings.TrimSpace(v)
}

func hasSettings(values []string) bool {
	for _, v := range values {
		if IsSetting(v) {
			return true
		}
	}
	return false
}

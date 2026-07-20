// Package selector parses and matches tag-based source selectors for the
// evoke generate command.
package selector

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/jesse0michael/evoke/pkg/evoke/ast"
	"github.com/jesse0michael/evoke/pkg/evoke/schema"
)

// Selector is a parsed source selector (e.g. "c:nurse+modern").
type Selector struct {
	// Facet is the canonical declaration name required, or empty for tag-only.
	Facet string
	// Tags are the required tags (all must match).
	Tags []string
	// Raw is the original selector string.
	Raw string
}

// Parse parses a selector string into a Selector.
// Grammar: [facet ":"] [tag { "+" tag }]
func Parse(raw string) (Selector, error) {
	s := Selector{Raw: raw}

	if raw == "" {
		return s, fmt.Errorf("empty selector")
	}

	facetPart, tagPart, hasFacet := strings.Cut(raw, ":")
	if hasFacet {
		if facetPart == "" {
			return s, fmt.Errorf("selector %q has empty facet", raw)
		}
		canonical, ok := schema.ResolveFacet(facetPart)
		if !ok {
			return s, fmt.Errorf("unknown facet %q in selector %q", facetPart, raw)
		}
		s.Facet = canonical
	} else {
		tagPart = raw
	}

	if tagPart == "" {
		return s, fmt.Errorf("selector %q has no tags", raw)
	}

	tags := strings.Split(tagPart, "+")
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			return s, fmt.Errorf("selector %q has empty tag", raw)
		}
		s.Tags = append(s.Tags, t)
	}

	return s, nil
}

// SourceDocument is a parsed document with its source file path.
type SourceDocument struct {
	Path     string
	Document *ast.Document
}

// Match reports whether a document satisfies the selector.
func Match(doc *ast.Document, sel Selector) bool {
	if sel.Facet != "" && !providesPositiveDeclaration(doc, sel.Facet) {
		return false
	}
	for _, tag := range sel.Tags {
		if !containsTag(doc.Metadata.Tags, tag) {
			return false
		}
	}
	return true
}

// providesPositiveDeclaration reports whether the document has a positive (explicit
// or default) contribution for the named declaration.
func providesPositiveDeclaration(doc *ast.Document, name string) bool {
	for _, decl := range doc.Declarations {
		if decl.Name == name && !decl.Negative {
			return true
		}
	}
	return false
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Selection is the result of resolving a selector to a source document.
type Selection struct {
	Selector Selector
	Source   SourceDocument
}

// SelectionError holds diagnostic information about a failed selector match.
type SelectionError struct {
	Selector       Selector
	IndividualHits map[string]int // tag → number of files matching that tag alone
	FacetHits      map[string]int // declaration → count of files matching tags but providing that declaration
	Suggestion     string         // "did you mean" suggestion, if any
}

func (e *SelectionError) Error() string {
	if len(e.Selector.Tags) == 1 && e.Selector.Facet == "" {
		msg := fmt.Sprintf("no Evoke file matches tag %q", e.Selector.Tags[0])
		if e.Suggestion != "" {
			msg += fmt.Sprintf("\n\ndid you mean %q?", e.Suggestion)
		}
		return msg
	}
	if e.Selector.Facet != "" {
		// Facet mismatch.
		total := 0
		for _, n := range e.IndividualHits {
			total += n
		}
		if total > 0 {
			msg := fmt.Sprintf("no Evoke file matches %s", e.Selector.Raw)
			tagCount := 0
			for _, tag := range e.Selector.Tags {
				tagCount += e.IndividualHits[tag]
			}
			if tagCount > 0 {
				msg += fmt.Sprintf("\n\n%d file(s) match the tag(s), but none provide %s", tagCount, e.Selector.Facet)
				if len(e.FacetHits) > 0 {
					msg += "\n\nmatching files provide:"
					for decl, count := range e.FacetHits {
						msg += fmt.Sprintf("\n  %s: %d", decl, count)
					}
				}
			}
			return msg
		}
		return fmt.Sprintf("no Evoke file matches %s", e.Selector.Raw)
	}
	// Multi-tag empty intersection.
	msg := "no Evoke file matches all requested tags:"
	for _, tag := range e.Selector.Tags {
		msg += fmt.Sprintf("\n  %s", tag)
	}
	if len(e.IndividualHits) > 0 {
		msg += "\n\nindividual matches:"
		for _, tag := range e.Selector.Tags {
			msg += fmt.Sprintf("\n  %s: %d files", tag, e.IndividualHits[tag])
		}
	}
	return msg
}

// Select resolves each selector against the candidate documents, choosing one
// distinct file per selector. The seed controls random selection when multiple
// candidates match.
func Select(candidates []SourceDocument, selectors []Selector, seed uint64) ([]Selection, error) {
	rng := rand.New(rand.NewPCG(seed, 0))
	used := make(map[string]bool) // paths already selected

	selections := make([]Selection, 0, len(selectors))
	for _, sel := range selectors {
		var matches []SourceDocument
		for _, c := range candidates {
			if used[c.Path] {
				continue
			}
			if Match(c.Document, sel) {
				matches = append(matches, c)
			}
		}

		if len(matches) == 0 {
			return nil, buildSelectionError(sel, candidates)
		}

		var chosen SourceDocument
		if len(matches) == 1 {
			chosen = matches[0]
		} else {
			chosen = matches[rng.IntN(len(matches))]
		}
		used[chosen.Path] = true
		selections = append(selections, Selection{Selector: sel, Source: chosen})
	}
	return selections, nil
}

// buildSelectionError produces a diagnostic error for a failed selector.
func buildSelectionError(sel Selector, candidates []SourceDocument) *SelectionError {
	e := &SelectionError{
		Selector:       sel,
		IndividualHits: make(map[string]int),
		FacetHits:      make(map[string]int),
	}

	// Count individual tag hits.
	for _, tag := range sel.Tags {
		for _, c := range candidates {
			if containsTag(c.Document.Metadata.Tags, tag) {
				e.IndividualHits[tag]++
			}
		}
	}

	// If faceted, find what declarations the tag-matching files provide.
	if sel.Facet != "" {
		for _, c := range candidates {
			allTags := true
			for _, tag := range sel.Tags {
				if !containsTag(c.Document.Metadata.Tags, tag) {
					allTags = false
					break
				}
			}
			if !allTags {
				continue
			}
			for _, decl := range c.Document.Declarations {
				if !decl.Negative {
					e.FacetHits[decl.Name]++
				}
			}
		}
	}

	// Simple "did you mean" for single-tag selectors: find closest tag by edit distance.
	if len(sel.Tags) == 1 && sel.Facet == "" {
		allTags := collectAllTags(candidates)
		if suggestion := closestMatch(sel.Tags[0], allTags); suggestion != "" {
			e.Suggestion = suggestion
		}
	}

	return e
}

func collectAllTags(candidates []SourceDocument) []string {
	seen := make(map[string]bool)
	var tags []string
	for _, c := range candidates {
		for _, t := range c.Document.Metadata.Tags {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}
	return tags
}

// closestMatch returns the closest string by Levenshtein distance, or empty if
// none is close enough (threshold: 3).
func closestMatch(target string, candidates []string) string {
	best := ""
	bestDist := 4 // threshold
	for _, c := range candidates {
		d := levenshtein(target, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

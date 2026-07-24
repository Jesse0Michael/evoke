package evoke

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

type Selector struct {
	Tags []string
	Raw  string
}

// ParseSelector parses a selector string (e.g. "nurse+modern").
func ParseSelector(raw string) (Selector, error) {
	s := Selector{Raw: raw}

	if raw == "" {
		return s, fmt.Errorf("empty selector")
	}

	tags := strings.Split(raw, "+")
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			return s, fmt.Errorf("selector %q has empty tag", raw)
		}
		s.Tags = append(s.Tags, t)
	}

	return s, nil
}

type SourceDocument struct {
	Path     string
	Document *Document
}

type Selection struct {
	Selector Selector
	Source   SourceDocument
}

// MatchSelector reports whether a document satisfies the selector.
func MatchSelector(doc *Document, sel Selector) bool {
	for _, tag := range sel.Tags {
		if !containsTag(doc.Metadata.Tags, tag) {
			return false
		}
	}
	return true
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Select resolves each selector against candidates, choosing one distinct file per selector.
func Select(candidates []SourceDocument, selectors []Selector, seed uint64) ([]Selection, error) {
	rng := rand.New(rand.NewPCG(seed, 0))
	used := make(map[string]bool)

	selections := make([]Selection, 0, len(selectors))
	for _, sel := range selectors {
		var matches []SourceDocument
		for _, c := range candidates {
			if used[c.Path] {
				continue
			}
			if MatchSelector(c.Document, sel) {
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

type SelectionError struct {
	Selector       Selector
	IndividualHits map[string]int
	Suggestion     string
}

func (e *SelectionError) Error() string {
	if len(e.Selector.Tags) == 1 {
		msg := fmt.Sprintf("no Evoke file matches tag %q", e.Selector.Tags[0])
		if e.Suggestion != "" {
			msg += fmt.Sprintf("\n\ndid you mean %q?", e.Suggestion)
		}
		return msg
	}
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

func buildSelectionError(sel Selector, candidates []SourceDocument) *SelectionError {
	e := &SelectionError{
		Selector:       sel,
		IndividualHits: make(map[string]int),
	}

	for _, tag := range sel.Tags {
		for _, c := range candidates {
			if containsTag(c.Document.Metadata.Tags, tag) {
				e.IndividualHits[tag]++
			}
		}
	}

	if len(sel.Tags) == 1 {
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

func closestMatch(target string, candidates []string) string {
	best := ""
	bestDist := 4
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

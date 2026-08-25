package cli

import (
	"sort"
	"testing"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/stretchr/testify/require"
)

func TestPickTopAffinity(t *testing.T) {
	tags := map[string][]string{
		"abagail-winter.evoke": {"stardew", "apparel", "abagail", "abagail-winter"},
		"abagail-beach.evoke":  {"stardew", "apparel", "abagail", "abagail-beach"},
		"haley-beach.evoke":    {"stardew", "apparel", "haley", "haley-beach"},
		"raincoat.evoke":       {"apparel", "outerwear"},
	}

	tests := []struct {
		name       string
		affinity   []string
		candidates []string
		// want is every path the pick may return; the whole set must be
		// reachable, so a candidate that should be excluded cannot hide.
		want []string
	}{
		{
			name:       "specific tag outranks the shared franchise tag",
			affinity:   []string{"abagail", "stardew", "female", "character"},
			candidates: []string{"abagail-winter.evoke", "haley-beach.evoke"},
			want:       []string{"abagail-winter.evoke"},
		},
		{
			name:       "ties at the top score all stay in the pool",
			affinity:   []string{"abagail", "stardew", "female", "character"},
			candidates: []string{"abagail-winter.evoke", "abagail-beach.evoke", "haley-beach.evoke"},
			want:       []string{"abagail-beach.evoke", "abagail-winter.evoke"},
		},
		{
			name:       "candidates sharing nothing are excluded",
			affinity:   []string{"stardew"},
			candidates: []string{"haley-beach.evoke", "raincoat.evoke"},
			want:       []string{"haley-beach.evoke"},
		},
		{
			name:       "no candidate shares anything falls back to all",
			affinity:   []string{"cyberpunk"},
			candidates: []string{"abagail-winter.evoke", "raincoat.evoke"},
			want:       []string{"abagail-winter.evoke", "raincoat.evoke"},
		},
		{
			name:       "single candidate wins regardless of overlap",
			affinity:   []string{"cyberpunk"},
			candidates: []string{"raincoat.evoke"},
			want:       []string{"raincoat.evoke"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			affinity := make(map[string]bool, len(tt.affinity))
			for _, a := range tt.affinity {
				affinity[a] = true
			}
			candidates := make([]indexCandidate, 0, len(tt.candidates))
			for _, p := range tt.candidates {
				candidates = append(candidates, indexCandidate{Path: p})
			}
			tagsFor := func(c indexCandidate) []string { return tags[c.Path] }

			// The pick rolls within the top-scoring group, so sample it enough
			// times that every reachable path appears.
			seen := make(map[string]bool)
			for range 200 {
				seen[pickTopAffinity(candidates, affinity, tagsFor).Path] = true
			}

			got := make([]string, 0, len(seen))
			for p := range seen {
				got = append(got, p)
			}
			sort.Strings(got)

			require.Equal(t, tt.want, got)
		})
	}
}

func TestPreferNameMatches(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		// candidates are name/path pairs: the index stores an extension-less
		// name, findInCwd keeps the extension, so both spellings must match.
		candidates []indexCandidate
		want       []indexCandidate
	}{
		{
			name:     "base name outranks files carrying the tag",
			selector: "sumi",
			candidates: []indexCandidate{
				{Name: "sumi", Path: "/src/sumi.evoke"},
				{Name: "sumi-winter", Path: "/src/sumi-winter.evoke"},
				{Name: "sumi-beach", Path: "/src/sumi-beach.evoke"},
			},
			want: []indexCandidate{{Name: "sumi", Path: "/src/sumi.evoke"}},
		},
		{
			name:     "cwd candidates keep the extension and still match",
			selector: "sumi",
			candidates: []indexCandidate{
				{Name: "sumi.evoke", Path: "/cwd/sumi.evoke"},
				{Name: "sumi-winter", Path: "/src/sumi-winter.evoke"},
			},
			want: []indexCandidate{{Name: "sumi.evoke", Path: "/cwd/sumi.evoke"}},
		},
		{
			name:     "name comparison ignores case",
			selector: "sumi",
			candidates: []indexCandidate{
				{Name: "Sumi", Path: "/src/Sumi.evoke"},
				{Name: "sumi-winter", Path: "/src/sumi-winter.evoke"},
			},
			want: []indexCandidate{{Name: "Sumi", Path: "/src/Sumi.evoke"}},
		},
		{
			name:     "same base name in two roots keeps both",
			selector: "sumi",
			candidates: []indexCandidate{
				{Name: "sumi", Path: "/a/sumi.evoke"},
				{Name: "sumi", Path: "/b/sumi.evoke"},
				{Name: "sumi-beach", Path: "/a/sumi-beach.evoke"},
			},
			want: []indexCandidate{
				{Name: "sumi", Path: "/a/sumi.evoke"},
				{Name: "sumi", Path: "/b/sumi.evoke"},
			},
		},
		{
			name:     "no base name match leaves the pool untouched",
			selector: "apparel",
			candidates: []indexCandidate{
				{Name: "sumi-winter", Path: "/src/sumi-winter.evoke"},
				{Name: "sumi-beach", Path: "/src/sumi-beach.evoke"},
			},
			want: []indexCandidate{
				{Name: "sumi-winter", Path: "/src/sumi-winter.evoke"},
				{Name: "sumi-beach", Path: "/src/sumi-beach.evoke"},
			},
		},
		{
			name:     "multi-tag selector is left alone",
			selector: "sumi+apparel",
			candidates: []indexCandidate{
				{Name: "sumi", Path: "/src/sumi.evoke"},
				{Name: "sumi-winter", Path: "/src/sumi-winter.evoke"},
			},
			want: []indexCandidate{
				{Name: "sumi", Path: "/src/sumi.evoke"},
				{Name: "sumi-winter", Path: "/src/sumi-winter.evoke"},
			},
		},
		{
			name:       "single candidate is returned unchanged",
			selector:   "sumi-winter",
			candidates: []indexCandidate{{Name: "sumi", Path: "/src/sumi.evoke"}},
			want:       []indexCandidate{{Name: "sumi", Path: "/src/sumi.evoke"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := evoke.ParseSelector(tt.selector)
			require.NoError(t, err)

			require.Equal(t, tt.want, preferNameMatches(tt.candidates, sel))
		})
	}
}

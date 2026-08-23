package cli

import (
	"sort"
	"testing"

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

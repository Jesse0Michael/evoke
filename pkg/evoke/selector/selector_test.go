package selector_test

import (
	"testing"

	"github.com/jesse0michael/evoke/pkg/evoke/ast"
	"github.com/jesse0michael/evoke/pkg/evoke/selector"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		expected  selector.Selector
		wantError bool
	}{
		{
			name: "tag only",
			raw:  "nurse",
			expected: selector.Selector{
				Tags: []string{"nurse"},
				Raw:  "nurse",
			},
		},
		{
			name: "multiple tags with +",
			raw:  "nurse+adult",
			expected: selector.Selector{
				Tags: []string{"nurse", "adult"},
				Raw:  "nurse+adult",
			},
		},
		{
			name: "facet with tag",
			raw:  "c:nurse",
			expected: selector.Selector{
				Facet: "CHARACTER",
				Tags:  []string{"nurse"},
				Raw:   "c:nurse",
			},
		},
		{
			name: "full facet name",
			raw:  "character:nurse",
			expected: selector.Selector{
				Facet: "CHARACTER",
				Tags:  []string{"nurse"},
				Raw:   "character:nurse",
			},
		},
		{
			name: "apparel alias",
			raw:  "a:nurse+modern",
			expected: selector.Selector{
				Facet: "APPAREL",
				Tags:  []string{"nurse", "modern"},
				Raw:   "a:nurse+modern",
			},
		},
		{
			name: "environment alias",
			raw:  "e:hospital",
			expected: selector.Selector{
				Facet: "ENVIRONMENT",
				Tags:  []string{"hospital"},
				Raw:   "e:hospital",
			},
		},
		{
			name: "appearance alias",
			raw:  "ap:cute",
			expected: selector.Selector{
				Facet: "APPEARANCE",
				Tags:  []string{"cute"},
				Raw:   "ap:cute",
			},
		},
		{
			name:      "empty selector",
			raw:       "",
			wantError: true,
		},
		{
			name:      "unknown facet",
			raw:       "x:nurse",
			wantError: true,
		},
		{
			name:      "empty tag after +",
			raw:       "nurse+",
			wantError: true,
		},
		{
			name:      "empty facet",
			raw:       ":nurse",
			wantError: true,
		},
		{
			name:      "facet with no tags",
			raw:       "c:",
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := selector.Parse(tt.raw)

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if !tt.wantError {
				require.Equal(t, tt.expected, sel)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name     string
		doc      *ast.Document
		selector selector.Selector
		want     bool
	}{
		{
			name: "tag match",
			doc: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse", "medical"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Values: []string{"scrubs"}},
				},
			},
			selector: selector.Selector{Tags: []string{"nurse"}},
			want:     true,
		},
		{
			name: "tag no match",
			doc: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse"}},
			},
			selector: selector.Selector{Tags: []string{"hospital"}},
			want:     false,
		},
		{
			name: "multi-tag intersection match",
			doc: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse", "modern"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Values: []string{"scrubs"}},
				},
			},
			selector: selector.Selector{Tags: []string{"nurse", "modern"}},
			want:     true,
		},
		{
			name: "multi-tag intersection miss",
			doc: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Values: []string{"scrubs"}},
				},
			},
			selector: selector.Selector{Tags: []string{"nurse", "modern"}},
			want:     false,
		},
		{
			name: "facet match with positive declaration",
			doc: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Values: []string{"scrubs"}},
				},
			},
			selector: selector.Selector{Facet: "APPAREL", Tags: []string{"nurse"}},
			want:     true,
		},
		{
			name: "facet match with default declaration",
			doc: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Default: true, Values: []string{"scrubs"}},
				},
			},
			selector: selector.Selector{Facet: "APPAREL", Tags: []string{"nurse"}},
			want:     true,
		},
		{
			name: "facet no match negative only",
			doc: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Negative: true, Values: []string{"scrubs"}},
				},
			},
			selector: selector.Selector{Facet: "APPAREL", Tags: []string{"nurse"}},
			want:     false,
		},
		{
			name: "facet no match wrong declaration",
			doc: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse"}},
				Declarations: []*ast.Declaration{
					{Name: "CHARACTER", Values: []string{"a nurse"}},
				},
			},
			selector: selector.Selector{Facet: "APPAREL", Tags: []string{"nurse"}},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selector.Match(tt.doc, tt.selector)

			require.Equal(t, tt.want, got)
		})
	}
}

func TestSelect(t *testing.T) {
	candidates := []selector.SourceDocument{
		{
			Path: "gwen.evoke",
			Document: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"character", "nurse", "adult"}},
				Declarations: []*ast.Declaration{
					{Name: "CHARACTER", Values: []string{"Gwen Kowalski"}},
					{Name: "APPEARANCE", Values: []string{"dark hair"}},
				},
			},
		},
		{
			Path: "scrubs.evoke",
			Document: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse", "medical", "modern"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Values: []string{"navy nurse scrubs"}},
				},
			},
		},
		{
			Path: "hospital.evoke",
			Document: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"hospital", "medical", "interior"}},
				Declarations: []*ast.Declaration{
					{Name: "ENVIRONMENT", Values: []string{"hospital examination room"}},
				},
			},
		},
	}

	tests := []struct {
		name      string
		selectors []selector.Selector
		wantPaths []string
		wantError bool
	}{
		{
			name: "basic three-file selection",
			selectors: []selector.Selector{
				{Facet: "CHARACTER", Tags: []string{"nurse"}},
				{Facet: "APPAREL", Tags: []string{"nurse"}},
				{Facet: "ENVIRONMENT", Tags: []string{"hospital"}},
			},
			wantPaths: []string{"gwen.evoke", "scrubs.evoke", "hospital.evoke"},
		},
		{
			name: "tag only selection",
			selectors: []selector.Selector{
				{Tags: []string{"character"}},
			},
			wantPaths: []string{"gwen.evoke"},
		},
		{
			name: "no match returns error",
			selectors: []selector.Selector{
				{Tags: []string{"nonexistent"}},
			},
			wantError: true,
		},
		{
			name: "same file not used twice",
			selectors: []selector.Selector{
				{Tags: []string{"nurse"}},       // could match gwen or scrubs
				{Tags: []string{"nurse"}},       // must pick the other one
				{Tags: []string{"nonexistent"}}, // should fail
			},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selections, err := selector.Select(candidates, tt.selectors, 42)

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if !tt.wantError {
				require.Len(t, selections, len(tt.wantPaths))
				for i, s := range selections {
					require.Equal(t, tt.wantPaths[i], s.Source.Path)
				}
			}
		})
	}
}

func TestSelect_Deterministic(t *testing.T) {
	candidates := []selector.SourceDocument{
		{
			Path: "a.evoke",
			Document: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Values: []string{"a"}},
				},
			},
		},
		{
			Path: "b.evoke",
			Document: &ast.Document{
				Metadata: ast.Metadata{Tags: []string{"nurse"}},
				Declarations: []*ast.Declaration{
					{Name: "APPAREL", Values: []string{"b"}},
				},
			},
		},
	}

	selectors := []selector.Selector{{Tags: []string{"nurse"}}}

	// Same seed should yield same result.
	s1, err := selector.Select(candidates, selectors, 99)
	require.NoError(t, err)
	s2, err := selector.Select(candidates, selectors, 99)
	require.NoError(t, err)
	require.Equal(t, s1[0].Source.Path, s2[0].Source.Path)
}

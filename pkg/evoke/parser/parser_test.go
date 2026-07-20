package parser_test

import (
	"testing"

	"github.com/jesse0michael/evoke/pkg/evoke/ast"
	"github.com/jesse0michael/evoke/pkg/evoke/parser"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		expected  []*ast.Declaration
		wantError bool
	}{
		{
			name: "single declaration with values",
			src: "NAME\n" +
				"    Sumi\n",
			expected: []*ast.Declaration{
				{Name: "NAME", RawName: "NAME", Line: 1, Values: []string{"Sumi"}},
			},
		},
		{
			name: "accumulating values preserve order",
			src: "APPEARANCE\n" +
				"    small\n" +
				"    round\n" +
				"    violet skin\n",
			expected: []*ast.Declaration{
				{Name: "APPEARANCE", RawName: "APPEARANCE", Line: 1, Values: []string{"small", "round", "violet skin"}},
			},
		},
		{
			name: "negative prefix",
			src: "!APPAREL\n" +
				"    sandals\n",
			expected: []*ast.Declaration{
				{Name: "APPAREL", RawName: "APPAREL", Negative: true, Line: 1, Values: []string{"sandals"}},
			},
		},
		{
			name: "default prefix",
			src: "?APPAREL\n" +
				"    green shirt\n",
			expected: []*ast.Declaration{
				{Name: "APPAREL", RawName: "APPAREL", Default: true, Line: 1, Values: []string{"green shirt"}},
			},
		},
		{
			name: "default negative prefix",
			src: "?!APPAREL\n" +
				"    formalwear\n",
			expected: []*ast.Declaration{
				{Name: "APPAREL", RawName: "APPAREL", Negative: true, Default: true, Line: 1, Values: []string{"formalwear"}},
			},
		},
		{
			name:      "invalid prefix order",
			src:       "!?APPAREL\n    sandals\n",
			wantError: true,
		},
		{
			name: "name is lowercased to canonical uppercase",
			src: "personality\n" +
				"    warm\n",
			expected: []*ast.Declaration{
				{Name: "PERSONALITY", RawName: "personality", Line: 1, Values: []string{"warm"}},
			},
		},
		{
			name:      "dotted name is invalid (namespaces out of scope for MVP)",
			src:       "COMFYUI.FACE_DETAILER\n    on\n",
			wantError: true,
		},
		{
			name: "hyphenated name",
			src: "FOO-BAR\n" +
				"    hello\n",
			expected: []*ast.Declaration{
				{Name: "FOO-BAR", RawName: "FOO-BAR", Line: 1, Values: []string{"hello"}},
			},
		},
		{
			name: "comments and blank lines are ignored",
			src: "# a heading comment\n" +
				"\n" +
				"NAME\n" +
				"    # indented comment inside a block\n" +
				"    Sumi\n" +
				"\n",
			expected: []*ast.Declaration{
				{Name: "NAME", RawName: "NAME", Line: 3, Values: []string{"Sumi"}},
			},
		},
		{
			name: "blank line does not terminate a value block",
			src: "PERSONALITY\n" +
				"    warm\n" +
				"\n" +
				"    playful\n",
			expected: []*ast.Declaration{
				{Name: "PERSONALITY", RawName: "PERSONALITY", Line: 1, Values: []string{"warm", "playful"}},
			},
		},
		{
			name: "multiple declarations",
			src: "NAME\n" +
				"    Sumi\n" +
				"\n" +
				"APPEARANCE\n" +
				"    violet skin\n",
			expected: []*ast.Declaration{
				{Name: "NAME", RawName: "NAME", Line: 1, Values: []string{"Sumi"}},
				{Name: "APPEARANCE", RawName: "APPEARANCE", Line: 4, Values: []string{"violet skin"}},
			},
		},
		{
			name: "value preserves internal punctuation",
			src: "SCENARIO\n" +
				"    Ashley is checking the user's temperature, gently.\n",
			expected: []*ast.Declaration{
				{Name: "SCENARIO", RawName: "SCENARIO", Line: 1, Values: []string{"Ashley is checking the user's temperature, gently."}},
			},
		},
		{
			name:      "empty declaration block is invalid",
			src:       "NAME\n\nAPPEARANCE\n    violet skin\n",
			wantError: true,
		},
		{
			name:      "value without declaration is invalid",
			src:       "    orphan value\n",
			wantError: true,
		},
		{
			name:      "tab indentation is invalid",
			src:       "NAME\n\tSumi\n",
			wantError: true,
		},
		{
			name:      "extra text after name is invalid",
			src:       "NAME Sumi\n",
			wantError: true,
		},
		{
			name:      "trailing empty declaration at EOF is invalid",
			src:       "NAME\n    Sumi\n\nAPPEARANCE\n",
			wantError: true,
		},
		{
			name:     "empty document is valid",
			src:      "",
			expected: nil,
		},
		{
			name: "IDENTITY is migrated to CHARACTER",
			src: "IDENTITY\n" +
				"    a nurse\n",
			expected: []*ast.Declaration{
				{Name: "CHARACTER", RawName: "IDENTITY", Line: 1, Values: []string{"a nurse"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := parser.Parse([]byte(tt.src))

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if !tt.wantError {
				require.Equal(t, tt.expected, doc.Declarations)
			}
		})
	}
}

func TestParse_Tags(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantTags  []string
		wantError bool
	}{
		{
			name: "basic tags",
			src: "TAGS\n" +
				"    nurse\n" +
				"    medical\n" +
				"    contemporary\n",
			wantTags: []string{"nurse", "medical", "contemporary"},
		},
		{
			name: "tags are normalized to lowercase",
			src: "TAGS\n" +
				"    Nurse\n" +
				"    MEDICAL\n",
			wantTags: []string{"nurse", "medical"},
		},
		{
			name: "duplicate tags are removed",
			src: "TAGS\n" +
				"    nurse\n" +
				"    nurse\n" +
				"    medical\n",
			wantTags: []string{"nurse", "medical"},
		},
		{
			name: "hyphenated tags are valid",
			src: "TAGS\n" +
				"    emergency-room\n" +
				"    warm-comedy\n",
			wantTags: []string{"emergency-room", "warm-comedy"},
		},
		{
			name: "tags with spaces are invalid",
			src: "TAGS\n" +
				"    nurse outfit\n",
			wantError: true,
		},
		{
			name: "tags with + are invalid",
			src: "TAGS\n" +
				"    nurse+medical\n",
			wantError: true,
		},
		{
			name: "tags with : are invalid",
			src: "TAGS\n" +
				"    apparel:nurse\n",
			wantError: true,
		},
		{
			name: "TAGS does not appear in declarations",
			src: "TAGS\n" +
				"    nurse\n" +
				"\n" +
				"NAME\n" +
				"    Gwen\n",
			wantTags: []string{"nurse"},
		},
		{
			name:      "TAGS does not support prefixes",
			src:       "?TAGS\n    nurse\n",
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := parser.Parse([]byte(tt.src))

			require.Equal(t, tt.wantError, err != nil, "error mismatch: %v", err)
			if !tt.wantError {
				require.Equal(t, tt.wantTags, doc.Metadata.Tags)
			}
		})
	}
}

package knowledge

import (
	"testing"

	"github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/stretchr/testify/require"
)

func TestRenderEvokeMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name: "only world-fact declarations survive",
			src: `NAME
    Sumi

CHARACTER
    an octopus humanoid, and the mascot of the Evoke project

PERSONALITY
    curious
    playful

BACKSTORY
    Washed up on the pier as a hatchling.

APPEARANCE
    (smooth violet skin:1.25)

APPAREL
    teal explorer vest

ENVIRONMENT
    seaside noodle cart

SCENARIO
    tending the cart at dusk

PROMPT
    low-angle shot

IMAGE
    steps = 30

LORA detail
    strength = 0.8

DETAILER face
    denoise = 0.4

CHAT
    model = qwen2.5.gguf

KNOWLEDGE lore
    top_k = 5
`,
			expected: "# Sumi\n" +
				"\n## Character\n\nan octopus humanoid, and the mascot of the Evoke project\n" +
				"\n## Personality\n\ncurious\nplayful\n" +
				"\n## Backstory\n\nWashed up on the pier as a hatchling.\n",
		},
		{
			name: "negative channels are dropped",
			src: `NAME
    Sumi

PERSONALITY
    warm

!PERSONALITY
    cruel
    emotionally detached
`,
			expected: "# Sumi\n\n## Personality\n\nwarm\n",
		},
		{
			name: "positive defaults are the effective value",
			src: `NAME
    Sumi

?PERSONALITY
    endlessly helpful
`,
			expected: "# Sumi\n\n## Personality\n\nendlessly helpful\n",
		},
		{
			name: "evoke comments never become headings",
			src: `# Sumi — our octopus humanoid mascot.
#
#   evoke chat sumi

NAME
    Sumi

CHARACTER
    an octopus humanoid
`,
			expected: "# Sumi\n\n## Character\n\nan octopus humanoid\n",
		},
		{
			name: "a fragment with no NAME renders nothing",
			src: `CHARACTER
    an octopus humanoid

PERSONALITY
    curious
`,
			expected: "",
		},
		{
			name: "a NAME with no world facts renders nothing",
			src: `NAME
    Winter Coat

APPEARANCE
    heavy green winter coat

IMAGE
    steps = 30
`,
			expected: "",
		},
		{
			name:     "an empty document renders nothing",
			src:      "# just a comment\n",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := evoke.Parse([]byte(tt.src))
			require.NoError(t, err)

			result := renderEvokeMarkdown(doc)

			require.Equal(t, tt.expected, result)
		})
	}
}

// TestRenderEvokeMarkdown_Chunks pins the whole point of rendering to markdown
// rather than embedding the raw file: one chunk per aspect, each carrying a
// heading path that says whose aspect it is.
func TestRenderEvokeMarkdown_Chunks(t *testing.T) {
	src := `# Sumi — our octopus humanoid mascot.

NAME
    Sumi

CHARACTER
    an octopus humanoid

PERSONALITY
    curious

!PERSONALITY
    cruel

APPEARANCE
    (smooth violet skin:1.25)

!APPEARANCE
    human skin
`
	doc, err := evoke.Parse([]byte(src))
	require.NoError(t, err)

	chunks := ChunkMarkdown("sumi.evoke", renderEvokeMarkdown(doc), DefaultMaxTokens, DefaultOverlap)

	require.Equal(t, []Chunk{
		{File: "sumi.evoke", Heading: "Sumi > Character", Content: "an octopus humanoid"},
		{File: "sumi.evoke", Heading: "Sumi > Personality", Content: "curious"},
	}, chunks)
}

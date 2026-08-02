package knowledge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChunkMarkdown(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		content   string
		maxTokens int
		overlap   int
		expected  []Chunk
	}{
		{
			name:      "nested headings build a path and drop empty sections",
			file:      "Design/Flora.md",
			maxTokens: 1000,
			overlap:   100,
			content: strings.Join([]string{
				"# 🌱 Flora",
				"# Flora",
				"# Ashroot",
				"A pale root that grows in ash.",
				"# Blightvine",
				"A creeping vine.",
			}, "\n"),
			expected: []Chunk{
				{File: "Design/Flora.md", Heading: "Ashroot", Content: "A pale root that grows in ash."},
				{File: "Design/Flora.md", Heading: "Blightvine", Content: "A creeping vine."},
			},
		},
		{
			name:      "multi-level hierarchy is preserved in the heading path",
			file:      "Planes/Shadowfell.md",
			maxTokens: 1000,
			overlap:   100,
			content: strings.Join([]string{
				"# 😈 Shadowfell",
				"## NPCs",
				"### Freya Roselle",
				"A grieving noble.",
			}, "\n"),
			expected: []Chunk{
				{File: "Planes/Shadowfell.md", Heading: "Shadowfell > NPCs > Freya Roselle", Content: "A grieving noble."},
			},
		},
		{
			name:      "base64 image definitions and references are stripped",
			file:      "Design/Books/Leaf.md",
			maxTokens: 1000,
			overlap:   100,
			content: strings.Join([]string{
				"# 🍃 Leaf",
				"Ashroot illustration below.",
				"![][image1]",
				"[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUg>",
			}, "\n"),
			expected: []Chunk{
				{File: "Design/Books/Leaf.md", Heading: "Leaf", Content: "Ashroot illustration below."},
			},
		},
		{
			name:      "inline image markup is stripped from prose",
			file:      "Design/Flora.md",
			maxTokens: 1000,
			overlap:   100,
			content: strings.Join([]string{
				"# Flora",
				"Ashroot ![a sketch](sketch.png) grows in ash.",
			}, "\n"),
			expected: []Chunk{
				{File: "Design/Flora.md", Heading: "Flora", Content: "Ashroot  grows in ash."},
			},
		},
		{
			name:      "anchor suffix is stripped from headings",
			file:      "Design/Flora.md",
			maxTokens: 1000,
			overlap:   100,
			content: strings.Join([]string{
				"# Mycothanullus {#mycothanullus}",
				"A fungal horror.",
			}, "\n"),
			expected: []Chunk{
				{File: "Design/Flora.md", Heading: "Mycothanullus", Content: "A fungal horror."},
			},
		},
		{
			name:      "content before any heading gets an empty heading path",
			file:      "Intro.md",
			maxTokens: 1000,
			overlap:   100,
			content:   "A preamble with no heading.",
			expected: []Chunk{
				{File: "Intro.md", Heading: "", Content: "A preamble with no heading."},
			},
		},
		{
			name:      "an empty document produces no chunks",
			file:      "Empty.md",
			maxTokens: 1000,
			overlap:   100,
			content:   "# Heading\n\n\n",
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ChunkMarkdown(tt.file, tt.content, tt.maxTokens, tt.overlap)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestChunkMarkdown_OversizedSplit(t *testing.T) {
	paras := []string{
		"Alpha alpha alpha alpha.",
		"Bravo bravo bravo bravo.",
		"Charlie charlie charlie.",
	}
	content := "# Big\n" + strings.Join(paras, "\n\n")
	maxTokens := 10 // each paragraph is ~7 tokens, so the section must split

	chunks := ChunkMarkdown("Big.md", content, maxTokens, 3)

	require.Greater(t, len(chunks), 1, "expected the oversized section to split")

	joined := ""
	for _, c := range chunks {
		require.Equal(t, "Big", c.Heading)
		require.Equal(t, "Big.md", c.File)
		joined += c.Content + "\n"
	}
	for _, p := range paras {
		require.Contains(t, joined, p)
	}
}

func TestEmbedText(t *testing.T) {
	tests := []struct {
		name     string
		chunk    Chunk
		expected string
	}{
		{
			name:     "heading is prepended for context",
			chunk:    Chunk{Heading: "Flora > Ashroot", Content: "A pale root."},
			expected: "Flora > Ashroot\n\nA pale root.",
		},
		{
			name:     "content alone when there is no heading",
			chunk:    Chunk{Content: "A pale root."},
			expected: "A pale root.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, EmbedText(tt.chunk))
		})
	}
}

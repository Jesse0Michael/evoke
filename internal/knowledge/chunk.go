package knowledge

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMaxTokens is the target maximum size of a chunk.
	DefaultMaxTokens = 1000
	// DefaultOverlap is how much of a chunk's tail is carried into the next one
	// when an oversized section has to be split.
	DefaultOverlap = 100
)

// Chunk is a single embeddable unit: one document section with the heading
// hierarchy that gives it context.
type Chunk struct {
	File    string // corpus-relative source path, e.g. "Design/Flora.md"
	Heading string // heading hierarchy, e.g. "Flora > Ashroot"
	Content string // the section's prose, image markup stripped out
}

var (
	headingRe   = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*$`)
	anchorRe    = regexp.MustCompile(`\s*\{#[^}]*\}\s*$`)
	leadingSym  = regexp.MustCompile(`^[^\p{L}\p{N}]+`)
	imageDefRe  = regexp.MustCompile(`^\s*\[[^\]]+\]:\s*<?\s*data:image`)
	imageRefRe  = regexp.MustCompile(`!\[[^\]]*\]\[[^\]]*\]`) // ![alt][ref]
	imageInline = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)  // ![alt](url)
)

// estTokens approximates the token count of a string. Embedding models tokenize
// at roughly four characters per token for English prose, which is accurate
// enough to bound chunk sizes without pulling in a real tokenizer.
func estTokens(s string) int {
	return utf8.RuneCountInString(s)/4 + 1
}

// cleanHeading strips markdown anchor suffixes and any leading emoji/symbols so
// the heading path reads as plain text (e.g. "🌱 Flora" -> "Flora").
func cleanHeading(raw string) string {
	s := anchorRe.ReplaceAllString(raw, "")
	s = leadingSym.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// stripImages removes base64 image definitions and image markup. Markdown
// exported from other tools often inlines images as huge base64 data URIs;
// embedding those would spend the model on noise, so they are dropped.
func stripImages(line string) (string, bool) {
	if imageDefRe.MatchString(line) {
		return "", true // drop the whole definition line
	}
	line = imageRefRe.ReplaceAllString(line, "")
	line = imageInline.ReplaceAllString(line, "")
	return line, false
}

// ChunkMarkdown splits a markdown document into heading-scoped chunks. Sections
// larger than maxTokens are further split on paragraph boundaries with
// overlapTokens of trailing context carried into the next chunk so meaning is
// not lost at the seam.
func ChunkMarkdown(file, content string, maxTokens, overlapTokens int) []Chunk {
	var chunks []Chunk
	var stack []heading
	var body []string

	flush := func() {
		path := headingPath(stack)
		text := strings.TrimSpace(strings.Join(body, "\n"))
		body = body[:0]
		if text == "" {
			return
		}
		for _, part := range splitOversized(text, maxTokens, overlapTokens) {
			chunks = append(chunks, Chunk{File: file, Heading: path, Content: part})
		}
	}

	for _, line := range strings.Split(content, "\n") {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			flush()
			level := len(m[1])
			title := cleanHeading(m[2])
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			if title != "" {
				stack = append(stack, heading{level: level, title: title})
			}
			continue
		}
		if stripped, drop := stripImages(line); !drop {
			body = append(body, stripped)
		}
	}
	flush()
	return chunks
}

// heading is one level of the open heading hierarchy while chunking.
type heading struct {
	level int
	title string
}

func headingPath(stack []heading) string {
	parts := make([]string, 0, len(stack))
	for _, h := range stack {
		parts = append(parts, h.title)
	}
	return strings.Join(parts, " > ")
}

// splitOversized breaks a section that exceeds maxTokens into paragraph-aligned
// pieces, prepending overlapTokens worth of the previous piece's tail to each
// subsequent piece.
func splitOversized(text string, maxTokens, overlapTokens int) []string {
	if estTokens(text) <= maxTokens {
		return []string{text}
	}
	paras := strings.Split(text, "\n\n")

	var out []string
	var cur []string
	curTokens := 0
	for _, p := range paras {
		pt := estTokens(p)
		if curTokens > 0 && curTokens+pt > maxTokens {
			out = append(out, strings.Join(cur, "\n\n"))
			// Seed the next piece with the tail of this one for overlap.
			cur, curTokens = overlapTail(cur, overlapTokens)
		}
		cur = append(cur, p)
		curTokens += pt
	}
	if len(cur) > 0 {
		out = append(out, strings.Join(cur, "\n\n"))
	}
	return out
}

func overlapTail(paras []string, overlapTokens int) ([]string, int) {
	var tail []string
	tokens := 0
	for i := len(paras) - 1; i >= 0 && tokens < overlapTokens; i-- {
		tail = append([]string{paras[i]}, tail...)
		tokens += estTokens(paras[i])
	}
	return tail, tokens
}

// EmbedText is the string actually sent to the embedding model: the heading
// path prepended to the content so the vector captures the section's context.
func EmbedText(c Chunk) string {
	if c.Heading == "" {
		return c.Content
	}
	return c.Heading + "\n\n" + c.Content
}

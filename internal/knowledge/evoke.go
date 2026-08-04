package knowledge

import (
	"fmt"
	"strings"

	"github.com/jesse0michael/evoke/pkg/evoke"
)

// renderEvokeMarkdown converts a parsed .evoke document into the markdown the
// chunker consumes, keeping only the declarations that state something about
// the world.
//
// Everything the image pipeline reads is dropped. APPEARANCE/APPAREL/
// ENVIRONMENT/PROMPT are generator input — tag soup, weights, and camera
// directives — and IMAGE/LORA/DETAILER are sampler configuration; none of it is
// prose a retrieval answer should quote. VOICE is dropped by the same rule: it
// describes a rendered voice for an audio target, not a fact a character should
// recite. CHAT and KNOWLEDGE are runtime config,
// and TAGS is selector metadata. SCENARIO is excluded because it is a transient
// situation rather than canon: embedding it would make a momentary scene setup
// permanently retrievable as world fact.
//
// Every negative channel is dropped outright. `!PERSONALITY cruel` means "not
// this"; a retrieved chunk carries no such frame, so storing it would feed a
// chat the inverse of canon. (compileSystemPrompt keeps negatives because it
// can relabel them "Traits to avoid:" for a live model. Retrieval can't.)
//
// The result is fresh markdown, so evoke's "#" comments never reach the heading
// regex. An empty string means the document has nothing worth embedding.
func renderEvokeMarkdown(doc *evoke.Document) string {
	// Merging a single document resolves `?` defaults, which are the effective
	// value when nothing overrides them, and drops disabled declarations.
	comp := evoke.Merge([]*evoke.Document{doc})

	// NAME is the only thing tying a chunk to its subject. Evoke files compose,
	// but the builder walks them one at a time, so an unnamed fragment
	// (winter-coat.evoke) would embed as prose about nobody.
	name := strings.TrimSpace(comp.Name)
	if name == "" {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", name)

	wrote := false
	section := func(title string, values []string) {
		lines := nonEmpty(values)
		if len(lines) == 0 {
			return
		}
		wrote = true
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", title, strings.Join(lines, "\n"))
	}

	section("Character", comp.Character)
	section("Personality", comp.Personality.Positive)
	section("Backstory", comp.Backstory)

	// A NAME with no body renders a bare heading, which chunks to nothing.
	// Report that as "skipped" rather than relying on the chunker to no-op.
	if !wrote {
		return ""
	}
	return b.String()
}

// nonEmpty trims values and drops the blank ones. Values cannot open a markdown
// heading and so need no escaping: "#" starts a comment in .evoke syntax, so the
// parser never produces a value beginning with one.
func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

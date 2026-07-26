package chat

import (
	"fmt"
	"strings"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// compileSystemPrompt renders the persistent character information into a
// deterministic system prompt: name, character, personality, backstory,
// and chat rules, all stated as fact. It carries only what is true
// for the whole conversation, so it can be re-sent on every request without
// re-asserting a scene. The starting scene lives in the opening turn instead
// (see compileOpening). Apparel and environment are image-generation concerns
// and are intentionally excluded from chat, as are image-only declarations
// (PROMPT/IMAGE/LORA/DETAILER samplers).
func compileSystemPrompt(comp *evoke.Composition) string {
	var sections []string
	add := func(s string) {
		if strings.TrimSpace(s) != "" {
			sections = append(sections, s)
		}
	}

	// Persistent identity.
	var id strings.Builder
	if comp.Name != "" {
		fmt.Fprintf(&id, "You are %s.", comp.Name)
	}
	if desc := joinList(comp.Character, "\n"); desc != "" {
		if id.Len() > 0 {
			id.WriteString("\n")
		}
		id.WriteString(desc)
	}
	add(id.String())

	add(labeled("Personality: ", joinList(comp.Personality.Positive, ", ")))
	add(labeled("Traits to avoid: ", joinList(comp.Personality.Negative, ", ")))
	add(labeled("Backstory:\n", joinList(comp.Backstory, "\n")))

	// Chat-specific system instructions carried as free text on the CHAT block.
	if comp.Chat != nil {
		add(joinList(comp.Chat.Instructions, "\n"))
	}

	return strings.Join(sections, "\n\n")
}

// compileOpening frames the scenario as the single opening turn that starts the
// conversation. It is sent once as the first user message — the character then
// responds — rather than living in the always-resent system prompt, so the scene
// sets the stage without being reasserted on every turn (which pulls a model back
// toward replaying the opening). Returns "" when there is no scenario.
func compileOpening(comp *evoke.Composition) string {
	scenario := strings.TrimSpace(comp.Scenario)
	if scenario == "" {
		return ""
	}
	return "Set the scene and begin in character. The situation:\n\n" + scenario
}

// joinList joins non-empty, trimmed values with sep.
func joinList(vals []string, sep string) string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, sep)
}

func labeled(label, content string) string {
	if content == "" {
		return ""
	}
	return label + content
}

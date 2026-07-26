package chat

import (
	"fmt"
	"strings"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// compileSystemPrompt renders the persistent character information and the
// starting scene into a deterministic system prompt. Persistent identity
// (name, character, personality, backstory, stable appearance, chat rules)
// is stated as fact; the starting scene (apparel, environment, scenario) is
// labeled as an initial state so the model can let it evolve. Image-only
// declarations (PROMPT/IMAGE/LORA/DETAILER samplers) are intentionally excluded.
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
	add(labeled("Appearance: ", joinList(comp.Appearance.Positive, ", ")))

	// Chat-specific system instructions carried as free text on the CHAT block.
	if comp.Chat != nil {
		add(joinList(comp.Chat.Instructions, "\n"))
	}

	// Starting situation: the initial scene, not permanent facts.
	var start []string
	if v := joinList(comp.Apparel.Positive, ", "); v != "" {
		start = append(start, "Wearing: "+v)
	}
	if v := joinList(comp.Environment.Positive, ", "); v != "" {
		start = append(start, "Setting: "+v)
	}
	if comp.Scenario != "" {
		start = append(start, comp.Scenario)
	}
	if len(start) > 0 {
		add("Starting situation:\n" + strings.Join(start, "\n"))
	}

	return strings.Join(sections, "\n\n")
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

// Explain renders a human-readable summary of the plan for dry-run inspection,
// including the exact command Evoke would launch. It contacts nothing.
func (p *Plan) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Character: %s\n", p.Display.CharacterName)
	fmt.Fprintf(&b, "Backend:   %s (managed)\n", p.Backend)
	fmt.Fprintf(&b, "Model:     %s -> %s\n", p.Display.Model, cmpOr(p.ModelPath, "(unmapped — set chat.models)"))
	fmt.Fprintf(&b, "Endpoint:  http://%s:%d/v1\n", p.Runtime.Host, p.Runtime.Port)
	fmt.Fprintf(&b, "Launch:    %s %s\n", p.Runtime.Executable, strings.Join(p.commandArgs(), " "))
	fmt.Fprintf(&b, "Context:   %d tokens (reserve %d, margin %d)\n",
		p.History.ContextWindow, p.Sampling.MaxOutputTokens, p.History.SafetyMargin)
	fmt.Fprintf(&b, "Sampling:  %s\n", describeSampling(p.Sampling))
	if len(p.Sources) > 0 {
		fmt.Fprintf(&b, "Sources:   %s\n", strings.Join(p.Sources, ", "))
	}
	for _, d := range p.Diagnostics {
		fmt.Fprintf(&b, "warning:   %s\n", d)
	}

	b.WriteString("\n=== System prompt ===\n")
	b.WriteString(p.SystemPrompt)
	b.WriteString("\n")
	return b.String()
}

func describeSampling(s Sampling) string {
	var parts []string
	if s.Temperature != nil {
		parts = append(parts, fmt.Sprintf("temperature=%g", *s.Temperature))
	}
	if s.TopP != nil {
		parts = append(parts, fmt.Sprintf("top_p=%g", *s.TopP))
	}
	if s.RepeatPenalty != nil {
		parts = append(parts, fmt.Sprintf("repeat_penalty=%g", *s.RepeatPenalty))
	}
	if s.Seed != nil {
		parts = append(parts, fmt.Sprintf("seed=%d", *s.Seed))
	}
	parts = append(parts, fmt.Sprintf("max_output_tokens=%d", s.MaxOutputTokens))
	if len(s.Stop) > 0 {
		parts = append(parts, fmt.Sprintf("stop=%v", s.Stop))
	}
	return strings.Join(parts, ", ")
}

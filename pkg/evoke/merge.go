package evoke

import (
	"fmt"
	"log/slog"
	"strings"
)

type Prompt struct {
	Positive []string
	Negative []string
}

// ImageStage holds the resolved configuration for a single IMAGE pipeline stage.
type ImageStage struct {
	Argument string
	Settings map[string]string
	Loras    []string
	Text     Prompt
	Disabled bool
}

// LoraDefinition holds the resolved configuration for a named LORA.
type LoraDefinition struct {
	Argument string
	Settings map[string]string
	Disabled bool
}

// DetailerConfig holds the resolved configuration for a named DETAILER.
type DetailerConfig struct {
	Argument string
	Settings map[string]string
	Loras    []string
	Text     Prompt
	Disabled bool
}

// ChatConfig holds the resolved configuration for the CHAT declaration:
// the backend/model/sampling/history settings plus any free-text lines,
// which are treated as extra chat-specific system instructions.
type ChatConfig struct {
	Settings     map[string]string
	Instructions []string
}

// KnowledgeSource holds the resolved configuration for a KNOWLEDGE declaration.
type KnowledgeSource struct {
	Argument string            // the DB filename (e.g., "lore.db")
	Settings map[string]string // optional settings (top_k, embed_model)
}

type Composition struct {
	Name        string
	Character   []string
	Personality Prompt
	Backstory   []string
	Voice       Prompt
	Appearance  Prompt
	Apparel     Prompt
	Environment Prompt
	Scenario    string
	Prompt      Prompt
	Images      []ImageStage
	Loras       []LoraDefinition
	Detailers   []DetailerConfig
	Chat        *ChatConfig       // Resolved CHAT configuration, nil when no CHAT declaration contributes
	Knowledge   []KnowledgeSource // Resolved KNOWLEDGE declarations
	Sources     []string          // Source file paths of merged documents
	Inputs      []string          // Original CLI input arguments
}

type channelKey struct {
	name     string
	argument string
	negative bool
}

// Merge resolves multiple parsed documents into a single Composition.
func Merge(docs []*Document) *Composition {
	contributions := make(map[channelKey][]contribution)
	for _, doc := range docs {
		for _, decl := range doc.Declarations {
			key := channelKey{name: decl.Name, argument: decl.Argument, negative: decl.Negative}
			contributions[key] = append(contributions[key], contribution{
				values:    decl.Values,
				isDefault: decl.Default,
				source:    doc.Source,
			})
		}
	}

	acc := func(name string, negative bool) []string {
		def, _ := LookupDeclaration(name)
		key := channelKey{name: name, negative: negative}
		return resolveChannel(def, contributions[key], negative)
	}

	singular := func(name string) string {
		def, _ := LookupDeclaration(name)
		key := channelKey{name: name, negative: false}
		vals := resolveChannel(def, contributions[key], false)
		if len(vals) > 0 {
			return strings.Join(vals, "\n")
		}
		return ""
	}

	comp := &Composition{
		Name:        singular("NAME"),
		Character:   acc("CHARACTER", false),
		Personality: Prompt{Positive: acc("PERSONALITY", false), Negative: acc("PERSONALITY", true)},
		Backstory:   acc("BACKSTORY", false),
		Voice:       Prompt{Positive: acc("VOICE", false), Negative: acc("VOICE", true)},
		Appearance:  Prompt{Positive: acc("APPEARANCE", false), Negative: acc("APPEARANCE", true)},
		Apparel:     Prompt{Positive: acc("APPAREL", false), Negative: acc("APPAREL", true)},
		Environment: Prompt{Positive: acc("ENVIRONMENT", false), Negative: acc("ENVIRONMENT", true)},
		Scenario:    singular("SCENARIO"),
		Prompt:      Prompt{Positive: acc("PROMPT", false), Negative: acc("PROMPT", true)},
	}

	// Collect unique source paths from input documents.
	seen := make(map[string]bool)
	for _, doc := range docs {
		if doc.Source != "" && !seen[doc.Source] {
			comp.Sources = append(comp.Sources, doc.Source)
			seen[doc.Source] = true
		}
	}

	// Resolve structured declarations.
	comp.Images = resolveImageStages(contributions)
	comp.Loras = resolveLoraDefinitions(contributions)
	comp.Detailers = resolveDetailerConfigs(contributions)
	comp.Chat = resolveChatConfig(contributions)
	comp.Knowledge = resolveKnowledgeSources(contributions)

	return comp
}

// resolveChatConfig resolves the singular CHAT declaration into a ChatConfig.
// It returns nil when no CHAT declaration (explicit or default) contributes.
func resolveChatConfig(contributions map[channelKey][]contribution) *ChatConfig {
	contribs := contributions[channelKey{name: "CHAT"}]
	if len(contribs) == 0 {
		return nil
	}
	settings, _, text := resolveStructuredChannel(contribs)
	return &ChatConfig{Settings: settings, Instructions: text}
}

// resolveKnowledgeSources resolves all KNOWLEDGE declarations into KnowledgeSource values.
func resolveKnowledgeSources(contributions map[channelKey][]contribution) []KnowledgeSource {
	args := collectArguments(contributions, "KNOWLEDGE")

	var sources []KnowledgeSource
	for _, arg := range args {
		key := channelKey{"KNOWLEDGE", arg, false}
		contribs := contributions[key]
		settings, _, _ := resolveStructuredChannel(contribs)
		sources = append(sources, KnowledgeSource{
			Argument: arg,
			Settings: settings,
		})
	}
	return sources
}

// resolveImageStages resolves all IMAGE declarations into ImageStage values.
func resolveImageStages(contributions map[channelKey][]contribution) []ImageStage {
	// Collect all unique IMAGE arguments.
	args := collectArguments(contributions, "IMAGE")

	var stages []ImageStage
	for _, arg := range args {
		posKey := channelKey{"IMAGE", arg, false}
		negKey := channelKey{"IMAGE", arg, true}

		posContribs := contributions[posKey]
		negContribs := contributions[negKey]

		hasExplicitNeg := hasExplicit(negContribs)

		// Resolve positive channel with field-level merge.
		settings, loras, text := resolveStructuredChannel(posContribs)

		// Resolve negative channel text.
		var negText []string
		for _, c := range negContribs {
			for _, v := range c.values {
				if !IsSetting(v) {
					negText = append(negText, v)
				}
			}
		}

		disabled := len(posContribs) == 0 && hasExplicitNeg
		if settings["disabled"] == "true" {
			disabled = true
		}

		stage := ImageStage{
			Argument: arg,
			Settings: settings,
			Loras:    loras,
			Text:     Prompt{Positive: text, Negative: negText},
			Disabled: disabled,
		}
		stages = append(stages, stage)
	}
	return stages
}

// resolveLoraDefinitions resolves all LORA declarations into LoraDefinition values.
func resolveLoraDefinitions(contributions map[channelKey][]contribution) []LoraDefinition {
	args := collectArguments(contributions, "LORA")

	var defs []LoraDefinition
	for _, arg := range args {
		key := channelKey{"LORA", arg, false}

		contribs := contributions[key]
		settings, _, _ := resolveStructuredChannel(contribs)

		disabled := settings["disabled"] == "true"

		defs = append(defs, LoraDefinition{
			Argument: arg,
			Settings: settings,
			Disabled: disabled,
		})
	}
	return defs
}

// resolveDetailerConfigs resolves all DETAILER declarations into DetailerConfig values.
func resolveDetailerConfigs(contributions map[channelKey][]contribution) []DetailerConfig {
	args := collectArguments(contributions, "DETAILER")

	var configs []DetailerConfig
	for _, arg := range args {
		posKey := channelKey{"DETAILER", arg, false}
		negKey := channelKey{"DETAILER", arg, true}

		posContribs := contributions[posKey]
		negContribs := contributions[negKey]

		hasExplicitNeg := hasExplicit(negContribs)

		settings, loras, text := resolveStructuredChannel(posContribs)

		var negText []string
		for _, c := range negContribs {
			for _, v := range c.values {
				if !IsSetting(v) {
					negText = append(negText, v)
				}
			}
		}

		// Disabled only when there's explicit negative with NO positive at all
		// (not even defaults). A default positive + explicit negative means
		// "use the default config with this negative prompt", not "disable".
		disabled := len(posContribs) == 0 && hasExplicitNeg
		if settings["disabled"] == "true" {
			disabled = true
		}

		configs = append(configs, DetailerConfig{
			Argument: arg,
			Settings: settings,
			Loras:    loras,
			Text:     Prompt{Positive: text, Negative: negText},
			Disabled: disabled,
		})
	}
	return configs
}

// resolveStructuredChannel resolves one channel of a structured declaration
// (IMAGE, LORA, DETAILER, CHAT, KNOWLEDGE) into settings, lora references, and
// prompt text.
//
// Settings layer per key rather than resolving as one atomic value: defaults
// apply first, then explicit blocks, and within each group the last file in
// composition order wins. That way a shot or pipeline file only has to name the
// settings it changes — `DETAILER face` with just `max_detection = 2` keeps the
// detector, sizes, and text it did not mention. Two files setting the same key
// is ordinary layering, not a conflict; the caller's argument order decides,
// which is the one place order-dependence is specified rather than accidental.
//
// Prompt text follows the ordinary channel rule instead, because it is content
// rather than configuration: explicit text suppresses default text (so `?` still
// means "only if nothing else contributed"), and text accumulates within
// whichever group is active. Lora references accumulate across everything.
func resolveStructuredChannel(contribs []contribution) (map[string]string, []string, []string) {
	if len(contribs) == 0 {
		return nil, nil, nil
	}

	var explicit, defaults []contribution
	for _, c := range contribs {
		if c.isDefault {
			defaults = append(defaults, c)
		} else {
			explicit = append(explicit, c)
		}
	}

	settings := make(map[string]string)
	var loras []string

	// Applied in two passes so an explicit setting always outranks a default
	// regardless of file order; within a pass, the last writer wins.
	apply := func(group []contribution) []string {
		var text []string
		for _, c := range group {
			for _, v := range c.values {
				switch k, val := ParseSetting(v); {
				case k == "lora":
					loras = appendUnique(loras, val)
				case k != "":
					settings[k] = val
				default:
					text = append(text, v)
				}
			}
		}
		return text
	}

	defaultText := apply(defaults)
	text := apply(explicit)
	if len(text) == 0 {
		text = defaultText
	}

	return settings, loras, dedupeValues(text)
}

// dedupeValues drops exact duplicates, comparing trimmed values while preserving
// the original text.
func dedupeValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		normalized := strings.TrimSpace(v)
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, v)
		}
	}
	return result
}

// collectArguments returns all unique argument values for a declaration name across all channels.
func collectArguments(contributions map[channelKey][]contribution, declName string) []string {
	seen := make(map[string]bool)
	var args []string
	for key := range contributions {
		if key.name == declName && !seen[key.argument] {
			seen[key.argument] = true
			args = append(args, key.argument)
		}
	}
	// Sort for deterministic output.
	sortArgs(args)
	return args
}

func hasExplicit(contribs []contribution) bool {
	for _, c := range contribs {
		if !c.isDefault {
			return true
		}
	}
	return false
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func sortArgs(args []string) {
	// Simple insertion sort for small slices.
	for i := 1; i < len(args); i++ {
		for j := i; j > 0 && args[j] < args[j-1]; j-- {
			args[j], args[j-1] = args[j-1], args[j]
		}
	}
}

type contribution struct {
	values    []string
	isDefault bool
	source    string
}

func resolveChannel(def Definition, contribs []contribution, negative bool) []string {
	if len(contribs) == 0 {
		return nil
	}

	var explicit, defaults []contribution
	for _, c := range contribs {
		if c.isDefault {
			defaults = append(defaults, c)
		} else {
			explicit = append(explicit, c)
		}
	}

	active := explicit
	if len(active) == 0 {
		active = defaults
	}

	if def.Merge == MergeSingular {
		if len(explicit) > 1 {
			channel := "positive"
			if negative {
				channel = "negative"
			}
			sources := make([]string, len(explicit))
			for i, e := range explicit {
				sources[i] = e.source
			}
			slog.Warn(fmt.Sprintf("conflict: multiple explicit %s values for singular declaration %s", channel, def.Name), "sources", sources)
		}
		if len(active) > 0 {
			return active[0].values
		}
		return nil
	}

	var values []string
	for _, c := range active {
		values = append(values, c.values...)
	}
	return dedupeValues(values)
}

// ImageStageByArgument returns the ImageStage with the given argument, or nil if not found.
func (c *Composition) ImageStageByArgument(arg string) *ImageStage {
	for i := range c.Images {
		if c.Images[i].Argument == arg {
			return &c.Images[i]
		}
	}
	return nil
}

// LoraByArgument returns the LoraDefinition with the given argument, or nil if not found.
func (c *Composition) LoraByArgument(arg string) *LoraDefinition {
	for i := range c.Loras {
		if c.Loras[i].Argument == arg {
			return &c.Loras[i]
		}
	}
	return nil
}

// DetailerByArgument returns the DetailerConfig with the given argument, or nil if not found.
func (c *Composition) DetailerByArgument(arg string) *DetailerConfig {
	for i := range c.Detailers {
		if c.Detailers[i].Argument == arg {
			return &c.Detailers[i]
		}
	}
	return nil
}

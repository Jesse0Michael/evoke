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

type Composition struct {
	Name        string
	Character   []string
	Personality Prompt
	Backstory   []string
	Appearance  Prompt
	Apparel     Prompt
	Environment Prompt
	Scenario    string
	Prompt      Prompt
	Images      []ImageStage
	Loras       []LoraDefinition
	Detailers   []DetailerConfig
	Sources     []string // Source file paths of merged documents
	Inputs      []string // Original CLI input arguments
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

	return comp
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

// resolveStructuredChannel performs field-level default merge for structured declarations.
// It returns merged settings, accumulated lora references, and prompt text lines.
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

	if len(explicit) > 1 {
		sources := make([]string, len(explicit))
		for i, e := range explicit {
			sources[i] = e.source
		}
		slog.Warn("conflict: multiple explicit contributions for structured declaration; using first", "sources", sources)
	}

	// Parse defaults into base settings + text.
	baseSettings := make(map[string]string)
	var baseLoras []string
	var baseText []string
	for _, c := range defaults {
		for _, v := range c.values {
			if k, val := ParseSetting(v); k != "" {
				if k == "lora" {
					baseLoras = appendUnique(baseLoras, val)
				} else {
					if _, exists := baseSettings[k]; !exists {
						baseSettings[k] = val
					}
				}
			} else {
				baseText = append(baseText, v)
			}
		}
	}

	if len(explicit) == 0 {
		return baseSettings, baseLoras, baseText
	}

	// Parse explicit contribution.
	explicitSettings := make(map[string]string)
	var explicitLoras []string
	var explicitText []string
	for _, v := range explicit[0].values {
		if k, val := ParseSetting(v); k != "" {
			if k == "lora" {
				explicitLoras = appendUnique(explicitLoras, val)
			} else {
				explicitSettings[k] = val
			}
		} else {
			explicitText = append(explicitText, v)
		}
	}

	// Field-level overlay: explicit settings override defaults.
	merged := make(map[string]string)
	for k, v := range baseSettings {
		merged[k] = v
	}
	for k, v := range explicitSettings {
		merged[k] = v
	}

	// Loras accumulate across explicit and defaults.
	var mergedLoras []string
	for _, l := range baseLoras {
		mergedLoras = appendUnique(mergedLoras, l)
	}
	for _, l := range explicitLoras {
		mergedLoras = appendUnique(mergedLoras, l)
	}

	// If explicit has text, it replaces all default text.
	mergedText := baseText
	if len(explicitText) > 0 {
		mergedText = explicitText
	}

	return merged, mergedLoras, mergedText
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

	var result []string
	seen := make(map[string]bool)
	for _, c := range active {
		for _, v := range c.values {
			normalized := strings.TrimSpace(v)
			if !seen[normalized] {
				seen[normalized] = true
				result = append(result, v)
			}
		}
	}
	return result
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

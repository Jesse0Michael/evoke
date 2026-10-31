// Package audio compiles a Composition into speech and renders it through a
// local text-to-speech runtime.
//
// The split mirrors internal/chat: a Composition plus trusted local settings
// compile into a validated Plan, and the Plan is what the runtime executes. A
// .evoke file names a model the way it names an IMAGE checkpoint; where such
// models live and which binary loads them is machine-specific and comes from
// settings instead.
package audio

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// DefaultModel is the Hugging Face repo id used when no AUDIO model is set.
// VoxCPM2 is the default because it reads a free-text speaker description,
// which is exactly what a VOICE block holds.
const DefaultModel = "mlx-community/VoxCPM2-bf16"

// DefaultExecutable is the one-shot synthesis binary. It loads the model,
// writes a file and exits, so nothing stays resident between invocations.
const DefaultExecutable = "mlx_audio.tts.generate"

// DefaultFormat is the container written when AUDIO names none. WAV is the one
// format the runtime writes without an ffmpeg install.
const DefaultFormat = "wav"

// Trusted holds machine-local configuration. It never comes from a .evoke
// file: a portable declaration names a model, and this says which binary runs
// it and where local files are searched for.
type Trusted struct {
	// Executable is the synthesis binary (name on PATH or absolute path).
	Executable string
	// ModelPaths are directories searched for a local model directory and for
	// the reference clip named by an AUDIO `reference` setting.
	ModelPaths []string
}

// Plan is a validated, fully resolved synthesis request. Everything the
// runtime needs is decided here so execution has no decisions left to make.
type Plan struct {
	Executable string
	Model      string
	// Instruct is the merged VOICE text — the description of the speaker.
	Instruct string
	// Reference is a resolved path to a clip to clone, empty when none is set.
	// It pins an identity that a description alone cannot reproduce between
	// runs, so when both are present the clip decides who speaks.
	Reference string
	// ReferenceText is the transcript of Reference. Empty makes the runtime
	// transcribe the clip itself.
	ReferenceText string
	// Format is the output container, without a leading dot.
	Format string
	// Settings are the validated AUDIO settings that tune synthesis, keyed by
	// their declaration name. They stay in that canonical form rather than as
	// one transport's arguments, because both transports carry them: the
	// one-shot runtime as CLI flags, the resident server as JSON fields.
	Settings map[string]string
	// Name is the output file prefix, taken from NAME.
	Name string
	// Diagnostics are non-fatal problems: settings this runtime cannot honor,
	// and declarations it has no use for. Compiling never fails on these, so a
	// portable file still renders, but nothing is lost silently.
	Diagnostics []string
}

// settingFlags maps an AUDIO setting name to the one-shot runtime flag it
// becomes. It is a whitelist rather than a passthrough: settings arrive from
// file content, and forwarding an arbitrary name would let a file choose the
// binary's arguments. A setting absent here is rejected by Compile, so this
// table also defines which settings exist at all.
var settingFlags = map[string]string{
	"voice":       "--voice",
	"speed":       "--speed",
	"cfg_scale":   "--cfg_scale",
	"ddpm_steps":  "--ddpm_steps",
	"temperature": "--temperature",
	"top_p":       "--top_p",
	"top_k":       "--top_k",
	"lang_code":   "--lang_code",
	"max_tokens":  "--max_tokens",
}

// settingFields maps an AUDIO setting name to the field it becomes in the
// resident server's /v1/audio/speech body. It is deliberately smaller than
// settingFlags: the server's request schema has no cfg_scale or ddpm_steps, so
// those two reach the one-shot runtime and nothing else. Settings not listed
// here are reported by Diagnose rather than dropped in silence.
var settingFields = map[string]string{
	"voice":       "voice",
	"speed":       "speed",
	"temperature": "temperature",
	"top_p":       "top_p",
	"top_k":       "top_k",
	"lang_code":   "lang_code",
	"max_tokens":  "max_tokens",
}

// numericFields are the server fields that must be encoded as JSON numbers.
// The declaration carries every setting as text, and the server's schema types
// these, so sending "1.2" where it expects 1.2 fails validation.
var numericFields = map[string]bool{
	"speed":       true,
	"temperature": true,
	"top_p":       true,
	"top_k":       true,
	"max_tokens":  true,
}

// settingsHandledElsewhere are AUDIO settings that become something other than
// a direct flag, so an unknown-setting diagnostic must not fire for them.
var settingsHandledElsewhere = map[string]bool{
	"model":     true,
	"reference": true,
	"ref_text":  true,
	"format":    true,
}

// Compile resolves a Composition and trusted settings into a Plan.
func Compile(comp *evoke.Composition, trusted Trusted) (*Plan, error) {
	p := &Plan{
		Executable: cmpOr(trusted.Executable, DefaultExecutable),
		Model:      DefaultModel,
		Format:     DefaultFormat,
		Settings:   map[string]string{},
		Name:       comp.Name,
	}

	p.Instruct = strings.Join(comp.Voice.Positive, ", ")

	// A negative description has nowhere to go: the runtime takes one positive
	// instruction string. The channel still parses and merges, so report it
	// rather than dropping it without a word.
	if len(comp.Voice.Negative) > 0 {
		p.Diagnostics = append(p.Diagnostics,
			"!VOICE is ignored: a speech runtime takes one positive description and has no negative channel")
	}

	settings := map[string]string{}
	if comp.Audio != nil {
		settings = comp.Audio.Settings
		// Free text on the AUDIO block describes the speaker too, so it joins
		// the instruction after VOICE rather than being discarded.
		if len(comp.Audio.Instructions) > 0 {
			extra := strings.Join(comp.Audio.Instructions, ", ")
			p.Instruct = strings.TrimPrefix(p.Instruct+", "+extra, ", ")
		}
	}

	for _, key := range sortedKeys(settings) {
		if _, ok := settingFlags[key]; ok {
			p.Settings[key] = settings[key]
			continue
		}
		if !settingsHandledElsewhere[key] {
			p.Diagnostics = append(p.Diagnostics, fmt.Sprintf("AUDIO %s is not a known setting and was ignored", key))
		}
	}

	if model := settings["model"]; model != "" {
		p.Model = resolveModel(model, trusted.ModelPaths)
	}
	if format := settings["format"]; format != "" {
		p.Format = strings.TrimPrefix(strings.ToLower(format), ".")
	}
	p.ReferenceText = settings["ref_text"]

	if ref := settings["reference"]; ref != "" {
		resolved, ok := resolveFile(ref, trusted.ModelPaths)
		if !ok {
			return nil, fmt.Errorf("AUDIO reference %q not found (searched the working directory and chat.model_paths)", ref)
		}
		p.Reference = resolved
	}

	if p.Instruct == "" && p.Reference == "" && p.Settings["voice"] == "" {
		return nil, fmt.Errorf("no VOICE description, AUDIO reference or voice preset: nothing describes who is speaking")
	}

	return p, nil
}

// VoicePreset is the named voice a model ships (AUDIO's `voice` setting),
// empty when the speaker is described instead.
func (p *Plan) VoicePreset() string { return p.Settings["voice"] }

// Args builds the runtime arguments for one synthesis of text into dir.
func (p *Plan) Args(text, dir, prefix string) []string {
	args := []string{
		"--model", p.Model,
		"--text", text,
		"--output_path", dir,
		"--file_prefix", prefix,
		"--audio_format", p.Format,
	}
	if p.Instruct != "" {
		args = append(args, "--instruct", p.Instruct)
	}
	if p.Reference != "" {
		args = append(args, "--ref_audio", p.Reference)
		if p.ReferenceText != "" {
			args = append(args, "--ref_text", p.ReferenceText)
		}
	}
	for _, key := range sortedKeys(p.Settings) {
		args = append(args, settingFlags[key], p.Settings[key])
	}
	return args
}

// Body returns the settings as /v1/audio/speech fields, together with the
// names of any the server's schema has no field for. Those are returned rather
// than logged here so the caller decides how to surface them — the one-shot
// runtime honors them, so they are only a loss on this transport.
func (p *Plan) Body() (fields map[string]any, unsupported []string) {
	fields = map[string]any{}
	for _, key := range sortedKeys(p.Settings) {
		field, ok := settingFields[key]
		if !ok {
			unsupported = append(unsupported, key)
			continue
		}
		if numericFields[field] {
			if n, err := strconv.ParseFloat(p.Settings[key], 64); err == nil {
				fields[field] = n
				continue
			}
			unsupported = append(unsupported, key)
			continue
		}
		fields[field] = p.Settings[key]
	}
	return fields, unsupported
}

// resolveModel maps an AUDIO model reference to what the runtime loads. A name
// holding a separator, or one matching a local directory, is a path; anything
// else passes through as a repo id for the runtime to fetch.
func resolveModel(name string, dirs []string) string {
	if info, err := os.Stat(name); err == nil && info.IsDir() {
		return name
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return name
}

// resolveFile finds a file named relative to the working directory or inside
// one of dirs. Absolute paths are used as given.
func resolveFile(name string, dirs []string) (string, bool) {
	if filepath.IsAbs(name) {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			return name, true
		}
		return "", false
	}
	candidates := append([]string{name}, func() []string {
		out := make([]string, 0, len(dirs))
		for _, dir := range dirs {
			out = append(out, filepath.Join(dir, name))
		}
		return out
	}()...)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate, true
			}
			return abs, true
		}
	}
	return "", false
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

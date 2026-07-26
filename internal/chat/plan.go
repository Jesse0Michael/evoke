// Package chat compiles resolved Evoke declarations into an interactive
// chat session against a local LLM backend. It separates three concerns:
// compilation (Composition -> Plan), runtime lifecycle (the managed backend
// process behind the Lease seam), and transport (the OpenAI-compatible Client).
//
// Evoke owns the backend: for each chat session it starts a llama.cpp
// `llama-server` child process with the model and runtime settings resolved
// from the .evoke file, waits for it to become healthy, talks to it, and shuts
// it down when the session ends. It never leaves the process running.
package chat

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// Role identifies the author of a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single structured chat message. Evoke owns the transcript and
// resends it as needed; the backend request API is treated as stateless.
type Message struct {
	Role    Role
	Content string
}

// backendLlamaCpp is the only supported backend driver in this version.
const backendLlamaCpp = "llama.cpp"

// TrustedConfig carries the machine-specific, trusted values that a portable
// .evoke file refers to by name. The caller assembles it from local settings
// and the environment before compilation; a shareable .evoke file names a model
// file (like an IMAGE checkpoint), and this says where such files live.
type TrustedConfig struct {
	// Executable is the llama-server binary (name on PATH or absolute path).
	Executable string
	// ModelDirs are the directories searched (recursively) for the model file
	// named in a CHAT declaration — the "where my GGUFs live" for this machine,
	// analogous to ComfyUI's models directory for checkpoints.
	ModelDirs []string
	// Host and Port are the loopback endpoint the managed server binds to.
	Host string
	Port int
}

// RuntimeSpec is everything needed to launch the managed backend process.
type RuntimeSpec struct {
	Executable    string
	Host          string
	Port          int
	ContextWindow int
	GPULayers     int
}

// Sampling holds per-response generation settings. Optional values are nil when
// the declaration does not set them, so the backend applies its own default.
type Sampling struct {
	Temperature     *float64
	TopP            *float64
	MaxOutputTokens int
	RepeatPenalty   *float64
	Seed            *int
	Stop            []string
}

// HistoryPolicy configures the deterministic sliding-window context budget.
type HistoryPolicy struct {
	ContextWindow  int
	SafetyMargin   int
	MinRecentTurns int
}

// InputBudget is the maximum number of tokens available for the request input,
// after reserving room for the response and a safety margin.
func (h HistoryPolicy) InputBudget(maxOutputTokens int) int {
	return h.ContextWindow - maxOutputTokens - h.SafetyMargin
}

// Plan is the validated, immutable chat execution plan produced by Compile.
// By the time it exists, everything needed by the runtime is fixed; the live
// loop never re-parses or re-merges declarations.
type Plan struct {
	Backend      string
	Runtime      RuntimeSpec
	Model        string // logical model name (for display)
	ModelPath    string // resolved GGUF path, from trusted config
	SystemPrompt string
	Opening      string // scenario framing seeded once as the first turn ("" if none)
	Sampling     Sampling
	History      HistoryPolicy
	Display      Display
	Sources      []string
	Diagnostics  []string
}

// Display holds human-facing metadata for the interactive session.
type Display struct {
	CharacterName string
	Backend       string
	Model         string
	ContextWindow int
}

const (
	defaultContextWindow   = 8192
	defaultMaxOutputTokens = 512
	defaultSafetyMargin    = 256
	defaultMinRecentTurns  = 1
	defaultGPULayers       = 99
	defaultHost            = "127.0.0.1"
	defaultPort            = 8080
	defaultExecutable      = "llama-server"
)

// Compile turns a resolved Composition and trusted local configuration into a
// validated Plan. It performs all validation that can happen before launching
// a backend, returning a joined error describing every problem found. It does
// not touch the filesystem or the network — path/binary existence is checked at
// launch time, keeping compilation pure and independently testable.
func Compile(comp *evoke.Composition, trusted TrustedConfig) (*Plan, error) {
	if comp == nil || comp.Chat == nil {
		return nil, errors.New("no effective CHAT declaration in the resolved composition")
	}

	s := settings(comp.Chat.Settings)
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	plan := &Plan{
		Sources: comp.Sources,
	}

	// Backend driver. Only llama.cpp (managed) is supported in this version.
	plan.Backend = strings.ToLower(cmpOr(s.str("backend"), backendLlamaCpp))
	if plan.Backend != backendLlamaCpp {
		fail("unsupported chat backend %q (only %q is supported)", plan.Backend, backendLlamaCpp)
	}

	// Model reference (required): the GGUF file name, resolved to a path by
	// searching the configured model directories (like an IMAGE checkpoint).
	// A miss is a diagnostic here and a hard error at launch, so compilation
	// stays pure and succeeds on a machine without the model present.
	model := s.str("model")
	if model == "" {
		fail("CHAT is missing a model reference")
	} else {
		plan.Model = model
		switch path, ok := resolveModelPath(model, trusted.ModelDirs); {
		case ok:
			plan.ModelPath = path
		case len(trusted.ModelDirs) == 0:
			plan.Diagnostics = append(plan.Diagnostics,
				fmt.Sprintf("model %q cannot be resolved: no chat.model_paths configured in settings", model))
		default:
			plan.Diagnostics = append(plan.Diagnostics,
				fmt.Sprintf("model file %q not found under chat.model_paths: %s", model, strings.Join(trusted.ModelDirs, ", ")))
		}
	}

	// Runtime / launch settings.
	plan.Runtime = RuntimeSpec{
		Executable:    cmpOr(trusted.Executable, defaultExecutable),
		Host:          cmpOr(trusted.Host, defaultHost),
		Port:          cmpOrInt(trusted.Port, defaultPort),
		ContextWindow: s.intDefault("context_window", defaultContextWindow, fail),
		GPULayers:     s.intDefault("gpu_layers", defaultGPULayers, fail),
	}

	// Sampling.
	plan.Sampling = Sampling{
		Temperature:     s.float("temperature", fail),
		TopP:            s.float("top_p", fail),
		RepeatPenalty:   s.float("repeat_penalty", fail),
		Seed:            s.int("seed", fail),
		MaxOutputTokens: s.intDefault("max_output_tokens", defaultMaxOutputTokens, fail),
		Stop:            s.list("stop"),
	}
	if plan.Sampling.MaxOutputTokens <= 0 {
		fail("max_output_tokens must be positive, got %d", plan.Sampling.MaxOutputTokens)
	}

	// History / context policy. The context window is shared: the backend is
	// launched with it and the budget is computed against it.
	plan.History = HistoryPolicy{
		ContextWindow:  plan.Runtime.ContextWindow,
		SafetyMargin:   s.intDefault("safety_margin", defaultSafetyMargin, fail),
		MinRecentTurns: s.intDefault("min_recent_turns", defaultMinRecentTurns, fail),
	}
	if plan.History.ContextWindow <= 0 {
		fail("context_window must be positive, got %d", plan.History.ContextWindow)
	}
	if plan.History.SafetyMargin < 0 {
		fail("safety_margin must not be negative, got %d", plan.History.SafetyMargin)
	}
	if plan.History.ContextWindow > 0 && plan.Sampling.MaxOutputTokens+plan.History.SafetyMargin >= plan.History.ContextWindow {
		fail("output reserve (%d) plus safety margin (%d) leaves no room in the context window (%d)",
			plan.Sampling.MaxOutputTokens, plan.History.SafetyMargin, plan.History.ContextWindow)
	}

	// System prompt is deterministic given the same composition + settings.
	plan.SystemPrompt = compileSystemPrompt(comp)
	if plan.SystemPrompt == "" {
		fail("compiled system prompt is empty; the character declarations carry no chat-relevant content")
	}

	// The scenario, if any, seeds the opening turn rather than the system prompt.
	plan.Opening = compileOpening(comp)

	plan.Display = Display{
		CharacterName: cmpOr(comp.Name, "assistant"),
		Backend:       plan.Backend,
		Model:         cmpOr(plan.Model, "(backend default)"),
		ContextWindow: plan.History.ContextWindow,
	}

	plan.Diagnostics = append(plan.Diagnostics, s.unused()...)

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return plan, nil
}

// commandArgs returns the llama-server arguments (excluding the executable) for
// launching the managed backend. It is the single source of truth for the
// launch command.
func (p *Plan) commandArgs() []string {
	return []string{
		"--model", p.ModelPath,
		"--host", p.Runtime.Host,
		"--port", strconv.Itoa(p.Runtime.Port),
		"--ctx-size", strconv.Itoa(p.Runtime.ContextWindow),
		"-ngl", strconv.Itoa(p.Runtime.GPULayers),
	}
}

// resolveModelPath resolves the model named in a CHAT declaration to an
// absolute GGUF path. It mirrors how a checkpoint filename is located in a
// models directory: an absolute path is used as-is; otherwise each configured
// directory is tried first as a direct (possibly nested) path, then searched
// recursively by file name. A ".gguf" extension is optional. It returns the
// first match.
func resolveModelPath(name string, dirs []string) (string, bool) {
	if name == "" {
		return "", false
	}
	if filepath.IsAbs(name) {
		return name, fileExists(name)
	}

	// Direct join: supports bare file names and explicit relative subpaths.
	candidates := []string{name}
	if filepath.Ext(name) == "" {
		candidates = append(candidates, name+".gguf")
	}
	for _, dir := range dirs {
		for _, c := range candidates {
			if p := filepath.Join(dir, c); fileExists(p) {
				return p, true
			}
		}
	}

	// Recursive search by base name, so a nested layout still resolves.
	wanted := map[string]bool{filepath.Base(name): true}
	if filepath.Ext(name) == "" {
		wanted[filepath.Base(name)+".gguf"] = true
	}
	for _, dir := range dirs {
		var found string
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // skip unreadable entries
			}
			if wanted[d.Name()] {
				found = path
				return fs.SkipAll
			}
			return nil
		})
		if found != "" {
			return found, true
		}
	}
	return "", false
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// cmpOr returns a if non-empty, otherwise b.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// cmpOrInt returns a if non-zero, otherwise b.
func cmpOrInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// settingsReader is a small typed accessor over the resolved CHAT settings map
// that records which keys were consumed so unrecognized ones can be surfaced.
type settingsReader struct {
	m    map[string]string
	seen map[string]bool
}

func settings(m map[string]string) *settingsReader {
	if m == nil {
		m = map[string]string{}
	}
	return &settingsReader{m: m, seen: make(map[string]bool)}
}

func (r *settingsReader) str(key string) string {
	r.seen[key] = true
	return strings.TrimSpace(r.m[key])
}

func (r *settingsReader) list(key string) []string {
	raw := r.str(key)
	if raw == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (r *settingsReader) float(key string, fail func(string, ...any)) *float64 {
	raw := r.str(key)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		fail("invalid %s %q: must be a number", key, raw)
		return nil
	}
	return &v
}

func (r *settingsReader) int(key string, fail func(string, ...any)) *int {
	raw := r.str(key)
	if raw == "" {
		return nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		fail("invalid %s %q: must be an integer", key, raw)
		return nil
	}
	return &v
}

func (r *settingsReader) intDefault(key string, def int, fail func(string, ...any)) int {
	if v := r.int(key, fail); v != nil {
		return *v
	}
	return def
}

// unused returns diagnostics for CHAT settings keys that were never read.
func (r *settingsReader) unused() []string {
	var out []string
	for k := range r.m {
		if !r.seen[k] {
			out = append(out, fmt.Sprintf("ignored unknown CHAT setting %q", k))
		}
	}
	slices.Sort(out)
	return out
}

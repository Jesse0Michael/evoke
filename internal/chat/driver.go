package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Backend driver identifiers accepted by the CHAT `backend` setting.
const (
	backendLlamaCpp = "llama.cpp"
	backendMLX      = "mlx"
)

// driver captures the only things that differ between backend runtimes: the
// binary to launch, the arguments it takes, and what a CHAT `model` reference
// names for that runtime. The OpenAI-compatible transport, the health probe,
// process ownership, and the history budget are all shared, so a new runtime is
// an entry in this table rather than a branch in the session logic.
type driver struct {
	// executable is the binary launched when local settings name none.
	executable string
	// launchArgs builds the process arguments, excluding the executable.
	launchArgs func(p *Plan) []string
	// resolveModel maps a CHAT model reference to whatever the runtime loads.
	// ok is false when the reference names something local that is absent.
	resolveModel func(name string, dirs []string) (string, bool)
	// validateModel reports whether the resolved reference is loadable. It runs
	// at launch, not at compile time, so a plan still compiles on a machine
	// that lacks the model.
	validateModel func(p *Plan) error
	// ignored names CHAT settings this runtime has no equivalent for. They are
	// surfaced as diagnostics so a portable file never silently loses meaning.
	ignored []string
}

var drivers = map[string]driver{
	backendLlamaCpp: {
		executable: "llama-server",
		launchArgs: func(p *Plan) []string {
			return []string{
				"--model", p.ModelPath,
				"--host", p.Runtime.Host,
				"--port", strconv.Itoa(p.Runtime.Port),
				"--ctx-size", strconv.Itoa(p.Runtime.ContextWindow),
				"-ngl", strconv.Itoa(p.Runtime.GPULayers),
			}
		},
		resolveModel: resolveGGUFPath,
		validateModel: func(p *Plan) error {
			if info, err := os.Stat(p.ModelPath); err != nil || info.IsDir() {
				return fmt.Errorf("model file not found: %s", p.ModelPath)
			}
			return nil
		},
	},
	backendMLX: {
		executable: "mlx_lm.server",
		// mlx_lm.server has no context-size or GPU-offload flag: MLX runs on
		// unified memory and takes no context cap, so context_window stays a
		// purely Evoke-side history budget rather than a launch argument.
		launchArgs: func(p *Plan) []string {
			return []string{
				"--model", p.ModelPath,
				"--host", p.Runtime.Host,
				"--port", strconv.Itoa(p.Runtime.Port),
			}
		},
		resolveModel: resolveMLXModel,
		validateModel: func(p *Plan) error {
			// A repo id is fetched by the runtime; there is nothing local to check.
			if isRepoID(p.ModelPath) {
				return nil
			}
			if !dirExists(p.ModelPath) {
				return fmt.Errorf("model directory not found: %s", p.ModelPath)
			}
			return nil
		},
		ignored: []string{"gpu_layers"},
	},
}

func driverFor(backend string) (driver, bool) {
	d, ok := drivers[backend]
	return d, ok
}

// backendNames returns the supported backend identifiers in sorted order, for
// error messages.
func backendNames() []string {
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// resolveMLXModel resolves a CHAT model reference for mlx_lm.server. An MLX
// model is a directory of weights plus tokenizer, not a single file, and a bare
// Hugging Face repo id is a valid reference the runtime downloads on demand. A
// local directory therefore wins when one is present, and a repo id passes
// through for the runtime to fetch.
func resolveMLXModel(name string, dirs []string) (string, bool) {
	if name == "" {
		return "", false
	}
	if filepath.IsAbs(name) {
		return name, dirExists(name)
	}
	for _, dir := range dirs {
		if p := filepath.Join(dir, name); dirExists(p) {
			return p, true
		}
	}
	if isRepoID(name) {
		return name, true
	}
	return "", false
}

// isRepoID reports whether a reference looks like a Hugging Face repo id
// ("mlx-community/Qwen3-8B-4bit"), which mlx_lm resolves and downloads itself.
// It is deliberately narrow — exactly one separator and no relative-path
// marker — so a local path that is merely absent still reports as unresolved
// instead of being handed to the runtime as a download that cannot succeed.
func isRepoID(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "/") {
		return false
	}
	parts := strings.Split(name, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

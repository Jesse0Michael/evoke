package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jesse0michael/evoke/internal/audio"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// Audio speaks a line in a composition's voice.
//
// Arguments split by what they are: selectors, paths and registry references
// compose the character, and everything left over is the line to speak. The
// text is runtime input rather than a contribution, so it is pulled out before
// resolution — literalDocument suppresses every `?` default including VOICE,
// which is right for an image prompt that states its own scene and exactly
// wrong here, where the line says nothing about who is speaking.
func Audio(args []string, verbose bool) int {
	fs := flag.NewFlagSet("audio", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	output := fs.String("o", ".", "directory to write the audio file to")
	fs.StringVar(output, "output", ".", "directory to write the audio file to")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	inputArgs, text := splitSpokenText(fs.Args())

	if text == "" {
		fmt.Fprintln(os.Stderr, "evoke audio: nothing to speak — pass the line as an argument, e.g. evoke audio sumi \"Sit down.\"")
		return 2
	}

	ctx := context.Background()

	settings, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke audio: %v\n", err)
		return 1
	}

	manifest, err := manifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke audio: %v\n", err)
		return 1
	}

	var docs []*evoke.Document
	if len(inputArgs) > 0 {
		res, err := prepareResolution(ctx, inputArgs, settings, manifest, verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke audio: %v\n", err)
			return 1
		}
		defer func() { _ = res.Close() }()

		if res.manifestChanged {
			if err := saveManifest(manifest); err != nil {
				fmt.Fprintf(os.Stderr, "evoke audio: failed to save manifest: %v\n", err)
				return 1
			}
		}

		docs, err = res.documents(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke audio: %v\n", err)
			return 1
		}
		fmt.Println()
	}

	composition := evoke.Merge(docs)

	if verbose {
		fmt.Println("=== Composition ===")
		fmt.Println(evoke.Render(composition))
	}

	plan, err := audio.Compile(composition, trustedAudio(settings))
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke audio: %v\n", err)
		return 1
	}

	for _, d := range plan.Diagnostics {
		fmt.Fprintf(os.Stderr, "warning: %s\n", d)
	}

	fmt.Printf("voice: %s\n", describeVoice(plan))
	fmt.Printf("text:  %s\n", text)

	result, err := audio.Generate(ctx, plan, text, *output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke audio: %v\n", err)
		return 1
	}

	if verbose {
		fmt.Printf("\n=== Command ===\n%s\n\n", result.Command)
	}

	fmt.Printf("saved: %s\n", result.Path)
	return 0
}

// splitSpokenText separates composition inputs from the line to speak. Anything
// that is not a selector, path or registry reference is text, and multiple such
// arguments join in order so a line can be passed in pieces.
func splitSpokenText(args []string) (inputs []string, text string) {
	var spoken []string
	for _, raw := range args {
		if classifyInput(raw).Kind == inputLiteral {
			spoken = append(spoken, raw)
			continue
		}
		inputs = append(inputs, raw)
	}
	return inputs, strings.Join(spoken, " ")
}

// trustedAudio reads the machine-local half of the configuration. The model
// search path is shared with chat: both resolve a file name a portable
// declaration gave them against directories only this machine knows.
func trustedAudio(s *Settings) audio.Trusted {
	t := audio.Trusted{}
	if s != nil && s.Chat != nil {
		t.ModelPaths = s.Chat.ModelPaths
	}
	if exe := os.Getenv("EVOKE_AUDIO_EXECUTABLE"); exe != "" {
		t.Executable = exe
	}
	return t
}

// trustedAudioServer reads the machine-local configuration for the resident
// voice backend a chat session talks to, parallel to trustedAudio for the
// one-shot runtime.
func trustedAudioServer(cfg voiceConfig) audio.ServerTrusted {
	return audio.ServerTrusted{
		Executable: cfg.Executable,
		Host:       cfg.Host,
		Port:       cfg.Port,
	}
}

// describeVoice summarizes what decided the speaker, so the reported line says
// which input actually took effect.
func describeVoice(p *audio.Plan) string {
	switch {
	case p.Reference != "" && p.Instruct != "":
		return fmt.Sprintf("%s (cloned from %s)", p.Instruct, p.Reference)
	case p.Reference != "":
		return "cloned from " + p.Reference
	case p.Instruct != "":
		return p.Instruct
	default:
		return "preset " + p.VoicePreset()
	}
}

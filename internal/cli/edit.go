package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jesse0michael/evoke/internal/generate"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// Edit resolves inputs the same way `image` does, then redraws a source image
// through the composition's architecture instead of sampling from noise. The
// source is uploaded once and reused across every generation in the batch.
func Edit(args []string, verbose bool) int {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	batch := fs.Int("b", 1, "number of images to generate")
	var source string
	fs.StringVar(&source, "i", "", "source image to edit")
	fs.StringVar(&source, "input", "", "source image to edit")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *batch < 1 {
		fmt.Fprintln(os.Stderr, "evoke edit: -b must be at least 1")
		return 2
	}

	if source == "" {
		fmt.Fprintln(os.Stderr, "evoke edit: -i is required")
		return 2
	}

	ctx := context.Background()

	gen, code := comfyClient("edit", verbose)
	if code != 0 {
		return code
	}

	// Upload before resolving inputs so an unreadable source or an unreachable
	// backend fails before any selector is rolled.
	image, err := gen.Upload(ctx, source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke edit: %v\n", err)
		return 1
	}
	fmt.Printf("%s => %s\n", source, image)

	return composeAndSubmit(ctx, "edit", fs.Args(), verbose, func(ctx context.Context, composition *evoke.Composition) (*generate.Result, error) {
		return gen.Edit(ctx, composition, image)
	}, *batch)
}

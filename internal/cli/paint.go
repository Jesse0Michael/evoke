package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jesse0michael/evoke/internal/generate"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// Paint resolves inputs the same way `image` and `edit` do, then alters a source
// image according to the composition's PROMPT through an instruction-edit model.
// The source is uploaded once and reused across every generation in the batch.
func Paint(args []string, verbose bool) int {
	fs := flag.NewFlagSet("paint", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	batch := fs.Int("b", 1, "number of images to generate")
	var source string
	fs.StringVar(&source, "i", "", "source image to paint")
	fs.StringVar(&source, "input", "", "source image to paint")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *batch < 1 {
		fmt.Fprintln(os.Stderr, "evoke paint: -b must be at least 1")
		return 2
	}

	if source == "" {
		fmt.Fprintln(os.Stderr, "evoke paint: -i is required")
		return 2
	}

	ctx := context.Background()

	gen, code := comfyClient("paint", verbose)
	if code != 0 {
		return code
	}

	// Upload before resolving inputs so an unreadable source or an unreachable
	// backend fails before any selector is rolled.
	image, err := gen.Upload(ctx, source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke paint: %v\n", err)
		return 1
	}
	fmt.Printf("%s => %s\n", source, image)

	return composeAndSubmit(ctx, "paint", fs.Args(), verbose, func(ctx context.Context, composition *evoke.Composition) (*generate.Result, error) {
		return gen.Paint(ctx, composition, image)
	}, *batch)
}

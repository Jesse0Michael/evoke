package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jesse0michael/evoke/pkg/evoke/parser"
	"github.com/jesse0michael/evoke/pkg/evoke/selector"
	"github.com/jesse0michael/evoke/pkg/evoke/validate"
)

// Generate runs tag-based source selection against a directory of .evoke files
// and reports which files were selected for each selector.
func Generate(args []string) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory to search for .evoke files")
	seedStr := fs.String("selection-seed", "0", "seed for deterministic random selection")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	selectorArgs := fs.Args()
	if len(selectorArgs) == 0 {
		fmt.Fprintln(os.Stderr, "evoke generate: at least one selector is required")
		return 2
	}

	seed, err := strconv.ParseUint(*seedStr, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke generate: invalid --selection-seed: %v\n", err)
		return 2
	}

	// Parse selectors.
	selectors := make([]selector.Selector, 0, len(selectorArgs))
	for _, raw := range selectorArgs {
		sel, err := selector.Parse(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
			return 2
		}
		selectors = append(selectors, sel)
	}

	// Load candidate .evoke files from the directory.
	candidates, err := loadCandidates(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
		return 1
	}
	if len(candidates) == 0 {
		fmt.Fprintf(os.Stderr, "evoke generate: no .evoke files found in %s\n", *dir)
		return 1
	}

	// Select files.
	selections, err := selector.Select(candidates, selectors, seed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
		return 1
	}

	// Report selections.
	for _, s := range selections {
		fmt.Printf("%s\n  selected: %s\n\n", s.Selector.Raw, s.Source.Path)
	}

	return 0
}

// loadCandidates walks dir for .evoke files, parses and validates each one,
// and returns the valid candidates.
func loadCandidates(dir string) ([]selector.SourceDocument, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var candidates []selector.SourceDocument
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".evoke" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}
		doc, err := parser.Parse(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			continue
		}
		if err := validate.Document(doc); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			continue
		}
		candidates = append(candidates, selector.SourceDocument{
			Path:     entry.Name(),
			Document: doc,
		})
	}
	return candidates, nil
}

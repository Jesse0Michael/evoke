package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
)

// IndexCmd rebuilds the local file index and prints stats.
func IndexCmd(args []string, _ bool) int {
	fs := flag.NewFlagSet("evoke index", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()

	settings, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke index: %v\n", err)
		return 1
	}

	roots, err := persistentRoots(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke index: %v\n", err)
		return 1
	}

	idx, err := openDefaultIndex()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke index: %v\n", err)
		return 1
	}
	defer func() { _ = idx.Close() }()

	if err := idx.rebuild(ctx, roots); err != nil {
		fmt.Fprintf(os.Stderr, "evoke index: rebuild failed: %v\n", err)
		return 1
	}

	for _, root := range roots {
		fmt.Printf("indexing %s (%s)\n", root.Path, root.Kind)
		if err := idx.refreshRoot(ctx, root); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		}
	}

	stats, err := idx.rootStats(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke index: %v\n", err)
		return 1
	}

	total := 0
	totalErrors := 0
	for _, s := range stats {
		if s.ErrorCount > 0 {
			fmt.Printf("  %s (%s): %d files, %d errors\n", s.Path, s.Kind, s.FileCount, s.ErrorCount)
		} else {
			fmt.Printf("  %s (%s): %d files\n", s.Path, s.Kind, s.FileCount)
		}
		total += s.FileCount
		totalErrors += s.ErrorCount
	}
	if totalErrors > 0 {
		fmt.Printf("\ntotal: %d roots, %d files, %d errors\n", len(stats), total, totalErrors)
	} else {
		fmt.Printf("\ntotal: %d roots, %d files\n", len(stats), total)
	}
	return 0
}

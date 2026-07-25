package cli

import (
	"context"
	"fmt"
	"os"
)

// IndexCmd refreshes the local file index and prints stats.
func IndexCmd(args []string, verbose bool) int {

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

	if err := idx.pruneRoots(ctx, roots); err != nil {
		fmt.Fprintf(os.Stderr, "evoke index: %v\n", err)
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
		if verbose {
			fmt.Println()
			errs, err := idx.fileErrors(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "evoke index: %v\n", err)
				return 1
			}
			for _, fe := range errs {
				fmt.Printf("%s\n  %s\n", fe.Path, fe.ParseError)
			}
		}
	} else {
		fmt.Printf("\ntotal: %d roots, %d files\n", len(stats), total)
	}
	return 0
}

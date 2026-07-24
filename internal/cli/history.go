package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jesse0michael/evoke/internal/generate"
	"github.com/jesse0michael/evoke/internal/generate/comfyui"
	"github.com/kelseyhightower/envconfig"
)

// HistoryCmd displays recent generations and their outputs.
func HistoryCmd(args []string, verbose bool) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	limit := fs.Int("n", 10, "number of recent entries to show")
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	j, err := openDefaultJournal()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke history: %v\n", err)
		return 1
	}
	defer func() { _ = j.Close() }()

	ctx := context.Background()
	entries, err := j.Recent(ctx, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke history: %v\n", err)
		return 1
	}

	if len(entries) == 0 {
		fmt.Println("No generation history.")
		return 0
	}

	// Try to connect to the backend for output resolution.
	var resolver generate.OutputResolver
	var cfg generateConfig
	if err := envconfig.Process("", &cfg); err == nil {
		gen := comfyui.New(cfg.ComfyURL)
		resolver = gen
	}

	for i, entry := range entries {
		if i > 0 {
			fmt.Println()
		}
		printEntry(ctx, entry, resolver, verbose)
	}

	return 0
}

func printEntry(ctx context.Context, entry JournalEntry, resolver generate.OutputResolver, verbose bool) {
	age := formatAge(time.Since(entry.CreatedAt))
	fmt.Printf("┌─ %s (%s ago)\n", entry.CreatedAt.Local().Format("2006-01-02 15:04:05"), age)
	fmt.Printf("│  backend: %s\n", entry.Backend)
	fmt.Printf("│  inputs:  %s\n", entry.Inputs)

	if verbose {
		fmt.Printf("│  id:      %s\n", entry.PromptID)
	}

	// Try to resolve outputs.
	if resolver != nil {
		outputs, err := resolver.ResolveOutputs(ctx, entry.PromptID)
		if err == nil && len(outputs) > 0 {
			fmt.Printf("│  outputs:\n")
			for _, o := range outputs {
				path := o.Filename
				if o.Subfolder != "" {
					path = o.Subfolder + "/" + o.Filename
				}
				fmt.Printf("│    %s [%s]\n", path, o.Type)
			}
		} else if err == nil {
			fmt.Printf("│  outputs: (pending)\n")
		}
	}
	fmt.Printf("└─\n")
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m > 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	default:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%dd", days)
	}
}

// inputsSummary creates a human-readable summary of the inputs for journal recording.
func inputsSummary(inputs []string) string {
	return strings.Join(inputs, " ")
}

// Command evoke is the CLI for the Evoke declarative character/asset format.
// main owns command dispatch and usage; each command's implementation lives in
// internal/cli.
package main

import (
	"fmt"
	"os"

	"github.com/jesse0michael/evoke/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches a command and returns the process exit code.
func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}

	// Extract global -v/--verbose before command dispatch.
	var verbose bool
	var filtered []string
	for _, a := range args {
		if a == "-v" || a == "--verbose" {
			verbose = true
		} else {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == 0 {
		usage()
		return 2
	}

	cmd, rest := filtered[0], filtered[1:]
	switch cmd {
	case "login":
		return cli.Login(rest, verbose)
	case "generate":
		return cli.Generate(rest, verbose)
	case "settings":
		return cli.SettingsCmd(rest, verbose)
	case "index":
		return cli.IndexCmd(rest, verbose)
	case "queue":
		return cli.QueueCmd(rest, verbose)
	case "clear":
		return cli.ClearCmd(rest, verbose)
	case "history":
		return cli.HistoryCmd(rest, verbose)
	case "view":
		return cli.ViewCmd(rest, verbose)
	case "completion":
		return cli.CompletionCmd(rest, verbose)
	case "__complete":
		return cli.Complete(rest)
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "evoke: unknown command %q\n\n", cmd)
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `evoke - declarative composition for AI characters and generative assets

Usage:
    evoke login       Sign in to the registry
    evoke generate    Compose evoke files together by tag or reference and send it through a configured pipeline
    evoke queue       View the current generation queue
    evoke clear       Clear the generation queue
    evoke history     View recent generation history and outputs
    evoke view        Interactive image viewer for recent output
    evoke settings    Manage user settings
    evoke index       Update the local file index

`)
}

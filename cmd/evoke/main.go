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
	case "image":
		return cli.Image(rest, verbose)
	case "edit":
		return cli.Edit(rest, verbose)
	case "paint":
		return cli.Paint(rest, verbose)
	case "chat":
		return cli.Chat(rest, verbose)
	case "inspect":
		return cli.Inspect(rest, verbose)
	case "knowledge":
		return cli.KnowledgeCmd(rest, verbose)
	case "settings":
		return cli.SettingsCmd(rest, verbose)

	case "push":
		return cli.Push(rest, verbose)
	case "pull":
		return cli.Pull(rest, verbose)
	case "queue":
		return cli.QueueCmd(rest, verbose)
	case "clear":
		return cli.ClearCmd(rest, verbose)
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

Chat:
    evoke chat        Compose evoke files into a character and start an interactive chat
    evoke knowledge   Build a RAG knowledge database from markdown and .evoke files

Image:
    evoke image       Compose evoke files and submit to a generation pipeline
    evoke edit        Redraw an existing image through a composition
    evoke paint       Alter an existing image by instruction (instruction-edit model)
    evoke inspect     List files matching a tag, or show what the selected files compose into
    evoke queue       View the current generation queue
    evoke clear       Clear the generation queue
    evoke view        Interactive image viewer for recent output

Registry:
    evoke login       Sign in to the registry
    evoke push        Push a .evoke file to the registry
    evoke pull        Download a registry artifact to the local library
    evoke settings    Manage user settings

`)
}

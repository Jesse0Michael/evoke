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

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "login":
		return cli.Login(rest)
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
    evoke login   Sign in to the registry with Google

`)
}

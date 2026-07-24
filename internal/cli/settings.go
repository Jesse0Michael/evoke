package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
)

// SettingsCmd manages the user settings file.
func SettingsCmd(args []string, _ bool) int {
	fs := flag.NewFlagSet("settings", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sub := fs.Args()
	if len(sub) == 0 {
		return settingsShow()
	}

	switch sub[0] {
	case "set":
		return settingsSet(sub[1:])
	case "remove":
		return settingsRemove(sub[1:])
	default:
		fmt.Fprintf(os.Stderr, "evoke settings: unknown subcommand %q\n", sub[0])
		settingsUsage()
		return 2
	}
}

func settingsShow() int {
	s, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings: %v\n", err)
		return 1
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings: %v\n", err)
		return 1
	}
	fmt.Println(string(data))
	return 0
}

func settingsSet(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "evoke settings set: requires <key> <value>")
		settingsUsage()
		return 2
	}
	key, value := args[0], args[1]

	s, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings set: %v\n", err)
		return 1
	}

	switch key {
	case "path":
		abs, err := expandPath(value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke settings set: %v\n", err)
			return 1
		}
		if slices.Contains(s.Paths, abs) {
			fmt.Fprintf(os.Stderr, "path already configured: %s\n", abs)
			return 0
		}
		s.Paths = append(s.Paths, abs)
	default:
		fmt.Fprintf(os.Stderr, "evoke settings set: unknown key %q\n", key)
		settingsUsage()
		return 2
	}

	if err := saveSettings(s); err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings set: %v\n", err)
		return 1
	}
	return 0
}

func settingsRemove(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "evoke settings remove: requires <key> <value>")
		settingsUsage()
		return 2
	}
	key, value := args[0], args[1]

	s, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings remove: %v\n", err)
		return 1
	}

	switch key {
	case "path":
		abs, err := expandPath(value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke settings remove: %v\n", err)
			return 1
		}
		idx := slices.Index(s.Paths, abs)
		if idx == -1 {
			fmt.Fprintf(os.Stderr, "path not configured: %s\n", abs)
			return 0
		}
		s.Paths = slices.Delete(s.Paths, idx, idx+1)
	default:
		fmt.Fprintf(os.Stderr, "evoke settings remove: unknown key %q\n", key)
		settingsUsage()
		return 2
	}

	if err := saveSettings(s); err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings remove: %v\n", err)
		return 1
	}
	return 0
}

func settingsUsage() {
	fmt.Fprint(os.Stderr, `Usage:
    evoke settings                        Show current settings
    evoke settings set path <dir>         Add a source path
    evoke settings remove path <dir>      Remove a source path
`)
}

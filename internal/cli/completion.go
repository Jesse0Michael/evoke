package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Complete outputs completion candidates for the current word.
// Called by the shell completion script via `evoke __complete <args...>`.
func Complete(args []string) int {
	// The last arg is the word being completed; preceding args give context.
	// Shell scripts pass: evoke __complete generate <words...> <current>
	if len(args) < 2 {
		// Complete subcommands.
		for _, cmd := range []string{"login", "generate", "queue", "clear", "history", "view", "settings", "index", "completion"} {
			fmt.Println(cmd)
		}
		return 0
	}

	subcmd := args[0]
	// current is the word being completed (may be empty).
	current := args[len(args)-1]

	switch subcmd {
	case "generate":
		return completeGenerate(current)
	default:
		return 0
	}
}

// completeGenerate outputs completion candidates for `evoke generate`.
func completeGenerate(current string) int {
	// Registry references.
	if strings.HasPrefix(current, "@") {
		return completeRegistryRefs(current)
	}

	// Local paths (starts with ./ or ../ or /).
	if strings.HasPrefix(current, "./") || strings.HasPrefix(current, "../") || strings.HasPrefix(current, "/") {
		return completeLocalPaths(current)
	}

	// Flags.
	if strings.HasPrefix(current, "-") {
		for _, f := range []string{"-b", "-v", "--verbose"} {
			if strings.HasPrefix(f, current) {
				fmt.Println(f)
			}
		}
		return 0
	}

	// Default: complete tags from the index.
	return completeTags(current)
}

// completeTags queries the index.db for all known tags and file names matching the prefix.
func completeTags(prefix string) int {
	idx, err := openDefaultIndex()
	if err != nil {
		return 0 // silently fail — don't break the shell
	}
	defer func() { _ = idx.Close() }()

	// If prefix contains "+", complete the last segment as a tag only.
	var tagPrefix string
	var base string
	if i := strings.LastIndex(prefix, "+"); i >= 0 {
		base = prefix[:i+1]
		tagPrefix = prefix[i+1:]
	} else {
		tagPrefix = prefix
	}

	tags, err := idx.allTags(context.Background(), tagPrefix)
	if err != nil {
		return 0
	}

	for _, tag := range tags {
		fmt.Println(base + tag)
	}

	// Also suggest file names (as local path completions) when not in multi-tag mode.
	if base == "" {
		names, err := idx.allFileNames(context.Background(), tagPrefix)
		if err != nil {
			return 0
		}
		for _, name := range names {
			fmt.Println(name + ".evoke")
		}
	}

	return 0
}

// completeRegistryRefs lists registry references from the manifest.
func completeRegistryRefs(prefix string) int {
	m, err := manifest()
	if err != nil {
		return 0
	}
	for ref := range m.Artifacts {
		if strings.HasPrefix(ref, prefix) {
			fmt.Println(ref)
		}
	}
	return 0
}

// completeLocalPaths lists .evoke files matching the partial path.
func completeLocalPaths(prefix string) int {
	dir := "."
	if i := strings.LastIndex(prefix, "/"); i >= 0 {
		dir = prefix[:i+1]
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	for _, e := range entries {
		name := dir + e.Name()
		if e.IsDir() {
			name += "/"
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if e.IsDir() || strings.HasSuffix(e.Name(), ".evoke") {
			fmt.Println(name)
		}
	}
	return 0
}

package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Tag manages local tag associations for .evoke files — e.g. marking a
// favorite — without writing into the file itself or its TAGS block. They
// live in tags.json (see Tags in home.go) rather than the SQLite index,
// which is a disposable cache rebuilt from file content: a favorite isn't
// derived from any file, so it would be lost on the next rescan if it lived
// there instead.
func Tag(args []string, verbose bool) int {
	fs := flag.NewFlagSet("tag", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sub := fs.Args()
	if len(sub) == 0 {
		tagUsage()
		return 2
	}

	switch sub[0] {
	case "add":
		return tagAdd(sub[1:], verbose)
	case "remove":
		return tagRemove(sub[1:], verbose)
	case "list":
		return tagList(sub[1:], verbose)
	default:
		fmt.Fprintf(os.Stderr, "evoke tag: unknown subcommand %q\n", sub[0])
		tagUsage()
		return 2
	}
}

func tagAdd(args []string, verbose bool) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "evoke tag add: requires <target> <tag>...")
		tagUsage()
		return 2
	}

	key, err := resolveTagTarget(context.Background(), args[0], verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke tag add: %v\n", err)
		return 1
	}

	t, err := tags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke tag add: %v\n", err)
		return 1
	}

	current := t.Files[key]
	for _, tag := range args[1:] {
		if !slices.Contains(current, tag) {
			current = append(current, tag)
		}
	}
	sort.Strings(current)
	t.Files[key] = current

	if err := saveTags(t); err != nil {
		fmt.Fprintf(os.Stderr, "evoke tag add: %v\n", err)
		return 1
	}
	printTags(key, current)
	return 0
}

func tagRemove(args []string, verbose bool) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "evoke tag remove: requires <target> <tag>...")
		tagUsage()
		return 2
	}

	key, err := resolveTagTarget(context.Background(), args[0], verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke tag remove: %v\n", err)
		return 1
	}

	t, err := tags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke tag remove: %v\n", err)
		return 1
	}

	remove := args[1:]
	remaining := make([]string, 0, len(t.Files[key]))
	for _, tag := range t.Files[key] {
		if !slices.Contains(remove, tag) {
			remaining = append(remaining, tag)
		}
	}
	if len(remaining) == 0 {
		delete(t.Files, key)
	} else {
		t.Files[key] = remaining
	}

	if err := saveTags(t); err != nil {
		fmt.Fprintf(os.Stderr, "evoke tag remove: %v\n", err)
		return 1
	}
	printTags(key, remaining)
	return 0
}

func tagList(args []string, verbose bool) int {
	t, err := tags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke tag list: %v\n", err)
		return 1
	}

	if len(args) == 0 {
		keys := make([]string, 0, len(t.Files))
		for key := range t.Files {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			printTags(key, t.Files[key])
		}
		return 0
	}

	key, err := resolveTagTarget(context.Background(), args[0], verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke tag list: %v\n", err)
		return 1
	}
	printTags(key, t.Files[key])
	return 0
}

func printTags(key string, tags []string) {
	if len(tags) == 0 {
		fmt.Printf("%s => (none)\n", key)
		return
	}
	fmt.Printf("%s => %s\n", key, strings.Join(tags, " "))
}

// resolveTagTarget resolves a tag command's target the same way image, chat,
// and inspect resolve theirs (a tag selector, local path, or @namespace/name),
// and requires it name exactly one file — tagging an ambiguous selector match
// would silently pick one, and the next tag lookup could pick a different
// one. It returns the key tags.json stores associations under (see tagKey).
func resolveTagTarget(ctx context.Context, raw string, verbose bool) (string, error) {
	ci := classifyInput(raw)
	if ci.Kind == inputLiteral {
		return "", fmt.Errorf("cannot tag prompt literal %q; pass a tag, file path, or @namespace/name", raw)
	}

	settings, err := settings()
	if err != nil {
		return "", err
	}
	manifest, err := manifest()
	if err != nil {
		return "", err
	}

	var idx *sqliteIndex
	var roots []sourceRoot
	if ci.Kind == inputSelector || ci.Kind == inputLocalPath {
		idx, roots, err = openIndexRoots(ctx, settings, verbose)
		if err != nil {
			return "", err
		}
		defer func() { _ = idx.Close() }()
	}

	candidates, changed, err := inspectResolve(ctx, ci, idx, roots, manifest, settings)
	if err != nil {
		return "", err
	}
	if changed {
		if err := saveManifest(manifest); err != nil {
			return "", fmt.Errorf("failed to save manifest: %w", err)
		}
	}

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("%q matched no files", raw)
	case 1:
		return tagKey(ci, candidates[0], roots), nil
	default:
		// Full paths, not just names — two files sharing a basename (two
		// "greenhouse" pipelines, say) would otherwise print the same name
		// twice with no way to tell them apart.
		paths := make([]string, len(candidates))
		for i, c := range candidates {
			paths[i] = c.Path
		}
		return "", fmt.Errorf("%q is ambiguous, matches:\n  %s", raw, strings.Join(paths, "\n  "))
	}
}

// tagKey returns the identity tags.json stores associations under for a
// resolved target. A registry reference already has a unique namespace/name;
// a source file's key is its path relative to whichever configured root
// contains it — the same identity sanitizeSources embeds into generated
// images — falling back to the basename only for a file outside every root.
// Root-relative rather than bare basename because two files can share a name
// in different collections, and a favorite must not jump from one to the
// other just because they're both named "greenhouse.evoke". Unlike
// displayPath, there is no working-directory fallback tier: a key that
// changed with wherever the command happened to be run from would silently
// split one favorite into two.
func tagKey(ci classifiedInput, candidate indexCandidate, roots []sourceRoot) string {
	if ci.Kind == inputRegistryRef {
		return ci.Namespace + "/" + ci.Name
	}
	for _, root := range roots {
		if rel, err := filepath.Rel(root.Path, candidate.Path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Base(candidate.Path)
}

func tagUsage() {
	fmt.Fprint(os.Stderr, `Usage:
    evoke tag add <target> <tag>...      Add one or more tags to a file
    evoke tag remove <target> <tag>...   Remove one or more tags from a file
    evoke tag list [target]              List tags for one file, or every tagged file

<target> is a tag selector, local path, or @namespace/name reference — the
same targeting image, chat, and inspect use. It must resolve to exactly one
file. Tags set this way are local to this machine: they are never written
into the .evoke file and never affect anyone else who uses it.
`)
}

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// Inspect resolves one or more targets (tag selectors, local paths, or registry
// references) the same way image and chat do, then shows what they compose into.
// When every target resolves to exactly one file, it lists those files and
// prints their merged composition — a preview of what image/chat would compose.
// When any target is ambiguous (a selector matching several files), it instead
// lists the matches per target so you can narrow the selection.
func Inspect(args []string, verbose bool) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	inputArgs := fs.Args()
	if len(inputArgs) == 0 {
		fmt.Fprintln(os.Stderr, "evoke inspect: at least one target is required (a tag, file path, or @namespace/name)")
		return 2
	}

	ctx := context.Background()

	settings, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke inspect: %v\n", err)
		return 1
	}
	manifest, err := manifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke inspect: %v\n", err)
		return 1
	}

	st := newChatStyle(os.Stdout, nil)

	// Classify every target up front so we know whether the index is needed
	// (selectors and bare-name file lookups) and can reject prompt literals,
	// which are not files to inspect.
	classified := make([]classifiedInput, len(inputArgs))
	needsIndex := false
	for i, raw := range inputArgs {
		ci := classifyInput(raw)
		if ci.Kind == inputLiteral {
			fmt.Fprintf(os.Stderr, "evoke inspect: cannot inspect prompt literal %q; pass a tag, file path, or @namespace/name\n", raw)
			return 2
		}
		classified[i] = ci
		if ci.Kind == inputSelector || ci.Kind == inputLocalPath {
			needsIndex = true
		}
	}

	var idx *sqliteIndex
	var roots []sourceRoot
	if needsIndex {
		idx, roots, err = openIndexRoots(ctx, settings, verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke inspect: %v\n", err)
			return 1
		}
		defer func() { _ = idx.Close() }()
	}

	// Resolve each target to its matching files, in the order given.
	results := make([]inspectResult, len(classified))
	manifestChanged := false
	for i, ci := range classified {
		candidates, changed, err := inspectResolve(ctx, ci, idx, roots, manifest, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke inspect: %v\n", err)
			return 1
		}
		manifestChanged = manifestChanged || changed
		results[i] = inspectResult{input: ci, candidates: candidates}
	}
	if manifestChanged {
		if err := saveManifest(manifest); err != nil {
			fmt.Fprintf(os.Stderr, "evoke inspect: failed to save manifest: %v\n", err)
			return 1
		}
	}

	// Detail mode when every target pins down exactly one file; otherwise list
	// the candidates so the caller can see what an ambiguous selector matches.
	for _, r := range results {
		if len(r.candidates) != 1 {
			inspectGroups(os.Stdout, results, st)
			return 0
		}
	}
	return inspectComposition(os.Stdout, results, roots, st)
}

// inspectResult holds a target and the files it resolved to.
type inspectResult struct {
	input      classifiedInput
	candidates []indexCandidate
}

// inspectResolve resolves a single classified target to its matching files. A
// path or registry reference always yields exactly one file (registry refs may
// pull, reporting a manifest change); a selector yields every match.
func inspectResolve(ctx context.Context, ci classifiedInput, idx *sqliteIndex, roots []sourceRoot, manifest *Manifest, settings *Settings) ([]indexCandidate, bool, error) {
	switch ci.Kind {
	case inputLocalPath:
		_, path, err := resolveLocalPathWithIndex(ctx, ci.Raw, idx)
		if err != nil {
			return nil, false, err
		}
		return []indexCandidate{{Path: path, Name: filepath.Base(path)}}, false, nil

	case inputRegistryRef:
		_, changed, err := resolveRegistryRef(ctx, ci, manifest, settings)
		if err != nil {
			return nil, false, err
		}
		path, err := registryOnDiskPath(manifest, ci)
		if err != nil {
			return nil, changed, err
		}
		return []indexCandidate{{Path: path, Name: filepath.Base(path)}}, changed, nil

	case inputSelector:
		candidates, err := inspectMatches(ctx, ci.Raw, idx, roots)
		if err != nil {
			return nil, false, err
		}
		return candidates, false, nil

	default:
		return nil, false, fmt.Errorf("cannot inspect %q", ci.Raw)
	}
}

// inspectComposition prints the one file each target resolved to, then the
// merged composition of all of them — the same merge image and chat perform.
func inspectComposition(w io.Writer, results []inspectResult, roots []sourceRoot, st chatStyle) int {
	docs := make([]*evoke.Document, 0, len(results))
	for _, r := range results {
		path := r.candidates[0].Path
		doc, err := resolveLocalPathDoc(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke inspect: %v\n", err)
			return 1
		}
		docs = append(docs, doc)
		fmt.Fprintf(w, "%s => %s\n", r.input.Raw, st.character(displayPath(path, roots)))
	}

	fmt.Fprintln(w)
	fmt.Fprint(w, evoke.Render(evoke.Merge(docs)))
	return 0
}

// inspectGroups lists the files each target matched. With more than one target,
// each group is headed by its raw input so an ambiguous selector is easy to
// spot; a target that matched nothing is called out explicitly.
func inspectGroups(w io.Writer, results []inspectResult, st chatStyle) {
	multi := len(results) > 1
	for i, r := range results {
		if multi {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, st.character(r.input.Raw+":"))
		}
		if len(r.candidates) == 0 {
			fmt.Fprintln(w, st.dim("  (no matches)"))
			continue
		}
		inspectList(w, r.candidates, st)
	}
}

// inspectMatches returns every file matching a selector, from the index and the
// working directory, deduplicated by path. Unlike generate's resolveSelector it
// never picks one — inspect wants the whole set.
func inspectMatches(ctx context.Context, raw string, idx *sqliteIndex, roots []sourceRoot) ([]indexCandidate, error) {
	sel, err := evoke.ParseSelector(raw)
	if err != nil {
		return nil, err
	}

	candidates, err := idx.find(ctx, roots, sel.Tags)
	if err != nil {
		return nil, fmt.Errorf("index lookup failed: %w", err)
	}
	candidates = append(candidates, findInCwd(sel)...)
	candidates = deduplicateCandidates(candidates)

	// A stale index can miss recent files; refresh and retry once when empty.
	if len(candidates) == 0 {
		for _, root := range roots {
			if _, refreshErr := idx.refreshRoot(ctx, root); refreshErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to refresh %s: %v\n", root.Path, refreshErr)
			}
		}
		candidates, err = idx.find(ctx, roots, sel.Tags)
		if err != nil {
			return nil, fmt.Errorf("index lookup failed after refresh: %w", err)
		}
		candidates = append(candidates, findInCwd(sel)...)
		candidates = deduplicateCandidates(candidates)
	}

	return candidates, nil
}

// inspectList prints one line per matching file: its filename and tags, sorted
// by filename. The filename is the exact token to pass back to `evoke inspect`.
func inspectList(w io.Writer, candidates []indexCandidate, st chatStyle) {
	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(filepath.Base(candidates[i].Path)) < strings.ToLower(filepath.Base(candidates[j].Path))
	})

	nameWidth := 0
	for _, c := range candidates {
		if n := len(filepath.Base(c.Path)); n > nameWidth {
			nameWidth = n
		}
	}
	if nameWidth > 32 {
		nameWidth = 32
	}

	for _, c := range candidates {
		line := st.character(fmt.Sprintf("%-*s", nameWidth, filepath.Base(c.Path)))
		if tags := fileTags(c.Path); len(tags) > 0 {
			line += "  " + st.dim("["+strings.Join(tags, ", ")+"]")
		}
		fmt.Fprintln(w, line)
	}
}

// fileTags reads a file's declared tags. Best-effort: an unreadable or
// unparseable file yields no tags rather than an error.
func fileTags(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	doc, err := evoke.Parse(data)
	if err != nil {
		return nil
	}
	return doc.Metadata.Tags
}

// registryOnDiskPath returns the local library path backing a registry
// reference, from the manifest entry if present, else the default library path.
func registryOnDiskPath(manifest *Manifest, ci classifiedInput) (string, error) {
	homeDir, err := home()
	if err != nil {
		return "", err
	}
	if art, ok := manifest.Artifacts[ci.Raw]; ok && art.File != "" {
		p := art.File
		if !filepath.IsAbs(p) {
			p = filepath.Join(homeDir, p)
		}
		return p, nil
	}
	return libraryPath(homeDir, ci.Namespace, ci.Name), nil
}

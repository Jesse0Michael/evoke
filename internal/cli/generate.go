package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jesse0michael/evoke/internal/client"
	"github.com/jesse0michael/evoke/internal/generate/comfyui"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/kelseyhightower/envconfig"
)

type generateConfig struct {
	ComfyURL string `envconfig:"COMFY_URL" default:"http://127.0.0.1:8188"`
}

// Image resolves inputs (selectors, local paths, registry references),
// merges the selected .evoke documents, and submits the result to ComfyUI.
func Image(args []string, verbose bool) int {
	fs := flag.NewFlagSet("image", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	batch := fs.Int("b", 1, "number of images to generate")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *batch < 1 {
		fmt.Fprintln(os.Stderr, "evoke image: -b must be at least 1")
		return 2
	}

	var cfg generateConfig
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "evoke image: %v\n", err)
		return 1
	}

	// Classify all args and extract the xN batch count and xall enumeration marker.
	var classified []classifiedInput
	for _, raw := range fs.Args() {
		classified = append(classified, classifyInput(raw))
	}
	classified, inlineBatch, enumerate := extractBatch(classified)
	if inlineBatch > 0 {
		*batch = inlineBatch
	}

	inputArgs := make([]string, 0, len(classified))
	for _, ci := range classified {
		inputArgs = append(inputArgs, ci.Raw)
	}

	if len(inputArgs) == 0 {
		fmt.Fprintln(os.Stderr, "evoke image: at least one input is required")
		return 2
	}

	ctx := context.Background()

	// Load home configuration.
	settings, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke image: %v\n", err)
		return 1
	}

	manifest, err := manifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke image: %v\n", err)
		return 1
	}

	// Resolve inputs (local paths, registry refs, literals, selectors) through
	// the shared resolution pipeline.
	res, err := prepareResolution(ctx, inputArgs, settings, manifest, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke image: %v\n", err)
		return 1
	}
	defer func() { _ = res.Close() }()

	if res.manifestChanged {
		if err := saveManifest(manifest); err != nil {
			fmt.Fprintf(os.Stderr, "evoke image: failed to save manifest: %v\n", err)
			return 1
		}
	}

	gen := comfyui.New(cfg.ComfyURL)
	gen.Verbose = verbose

	// By default every generation re-resolves selectors in CLI order, re-rolling
	// random picks for variety. With xall the selector picks are enumerated up
	// front instead, and the batch count becomes a per-combination multiplier.
	total := *batch
	docsFor := func(int) ([]*evoke.Document, error) { return res.documents(ctx) }

	if enumerate {
		variants, err := res.variants(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke image: %v\n", err)
			return 1
		}
		total = len(variants) * *batch
		docsFor = func(i int) ([]*evoke.Document, error) { return res.documentsFor(variants[i / *batch]) }
	}

	for i := range total {
		if total > 1 {
			fmt.Printf("\n--- batch %d/%d ---\n", i+1, total)
		}

		docs, err := docsFor(i)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke image: %v\n", err)
			return 1
		}

		fmt.Println()

		// Merge and generate.
		composition := evoke.Merge(docs)
		composition.Inputs = inputArgs

		if verbose {
			fmt.Println("=== Composition ===")
			fmt.Println(evoke.Render(composition))
		}

		genResult, err := gen.Generate(ctx, composition)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke image: %v\n", err)
			return 1
		}

		if verbose && genResult.Payload != "" {
			fmt.Println("=== ComfyUI Request ===")
			fmt.Println(genResult.Payload)
			fmt.Println()
		}

		fmt.Println(genResult.Message)
	}

	return 0
}

// resolveLocalPathDoc reads, parses, and validates a local .evoke file.
func resolveLocalPathDoc(raw string) (*evoke.Document, error) {
	path, err := resolveLocalFilePath(raw)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	doc, err := evoke.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	doc.Source = path
	if err := evoke.Validate(doc); err != nil {
		return nil, fmt.Errorf("validation failed for %s: %w", path, err)
	}
	return doc, nil
}

// resolveLocalPathWithIndex tries to resolve a local path, falling back to the index by filename.
func resolveLocalPathWithIndex(ctx context.Context, raw string, idx *sqliteIndex) (*evoke.Document, string, error) {
	doc, err := resolveLocalPathDoc(raw)
	if err == nil {
		path, _ := resolveLocalFilePath(raw)
		return doc, path, nil
	}

	// Fall back to index lookup by filename (bare name like "ill.evoke").
	if idx == nil {
		return nil, "", err
	}
	name := strings.TrimSuffix(filepath.Base(raw), ".evoke")
	indexPath, findErr := idx.findByName(ctx, name)
	if findErr != nil {
		return nil, "", err // return original error
	}

	data, readErr := os.ReadFile(indexPath)
	if readErr != nil {
		return nil, "", fmt.Errorf("failed to read %s: %w", indexPath, readErr)
	}
	doc, parseErr := evoke.Parse(data)
	if parseErr != nil {
		return nil, "", fmt.Errorf("failed to parse %s: %w", indexPath, parseErr)
	}
	doc.Source = indexPath
	if valErr := evoke.Validate(doc); valErr != nil {
		return nil, "", fmt.Errorf("validation failed for %s: %w", indexPath, valErr)
	}
	return doc, indexPath, nil
}

// resolveRegistryRef resolves a @namespace/name reference through the manifest and library.
// It pulls from the registry if the artifact is not locally available.
func resolveRegistryRef(ctx context.Context, ci classifiedInput, manifest *Manifest, settings *Settings) (*evoke.Document, bool, error) {
	if err := validateRegistrySlug(ci.Namespace); err != nil {
		return nil, false, fmt.Errorf("invalid registry reference %q: %w", ci.Raw, err)
	}
	if err := validateRegistrySlug(ci.Name); err != nil {
		return nil, false, fmt.Errorf("invalid registry reference %q: %w", ci.Raw, err)
	}

	homeDir, err := home()
	if err != nil {
		return nil, false, err
	}

	libPath := libraryPath(homeDir, ci.Namespace, ci.Name)

	// Check manifest for existing entry.
	if art, ok := manifest.Artifacts[ci.Raw]; ok {
		fullPath := art.File
		if !filepath.IsAbs(fullPath) {
			fullPath = homeDir + "/" + fullPath
		}
		data, err := os.ReadFile(fullPath)
		if err == nil {
			doc, err := evoke.Parse(data)
			if err != nil {
				return nil, false, fmt.Errorf("failed to parse library file %s: %w", fullPath, err)
			}
			doc.Source = ci.Raw
			if err := evoke.Validate(doc); err != nil {
				return nil, false, fmt.Errorf("validation failed for library file %s: %w", fullPath, err)
			}
			return doc, false, nil
		}
		// File missing from library — fall through to pull.
	}

	// Pull from registry.
	registryURL := settings.Registry
	if registryURL == "" {
		registryURL = "http://localhost:8080"
	}

	doc, sha, err := pullFromRegistry(ctx, registryURL, ci.Namespace, ci.Name, libPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to pull %s: %w", ci.Raw, err)
	}

	// Update manifest.
	relPath := "library/" + ci.Namespace + "/" + ci.Name + ".evoke"
	manifest.Artifacts[ci.Raw] = Artifact{
		File:     relPath,
		Registry: registryURL,
		Revision: "latest",
		SHA256:   sha,
	}

	return doc, true, nil
}

// pullFromRegistry downloads a .evoke file from the registry and writes it to the library.
func pullFromRegistry(ctx context.Context, registryURL, namespace, name, libPath string) (*evoke.Document, string, error) {
	c, err := client.NewClient(registryURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create registry client: %w", err)
	}

	// List versions to find the latest.
	listResp, err := c.ListVersions(ctx, namespace, name)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list versions: %w", err)
	}
	defer func() { _ = listResp.Body.Close() }()

	if listResp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("registry returned %s for %s/%s", listResp.Status, namespace, name)
	}

	parsed, err := client.ParseListVersionsResponse(listResp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse version list: %w", err)
	}
	versions := parsed.GetJSON200()
	if versions == nil || len(versions.Versions) == 0 {
		return nil, "", fmt.Errorf("no versions found for %s/%s", namespace, name)
	}

	// Get the latest version (highest version number).
	latest := versions.Versions[0]
	for _, v := range versions.Versions[1:] {
		if v.Version > latest.Version {
			latest = v
		}
	}

	// Pull the version content.
	getResp, err := c.GetVersion(ctx, namespace, name, latest.Version)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get version: %w", err)
	}
	defer func() { _ = getResp.Body.Close() }()

	if getResp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("registry returned %s for version %d", getResp.Status, latest.Version)
	}

	data, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read version content: %w", err)
	}

	// Parse and validate before writing.
	doc, err := evoke.Parse(data)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse pulled content: %w", err)
	}
	doc.Source = namespace + "/" + name
	if err := evoke.Validate(doc); err != nil {
		return nil, "", fmt.Errorf("validation failed for pulled content: %w", err)
	}

	// Calculate SHA-256.
	h := sha256.Sum256(data)
	sha := hex.EncodeToString(h[:])

	// Write to library.
	if err := os.MkdirAll(filepath.Dir(libPath), 0o755); err != nil {
		return nil, "", fmt.Errorf("failed to create library directory: %w", err)
	}
	if err := os.WriteFile(libPath, data, 0o644); err != nil {
		return nil, "", fmt.Errorf("failed to write library file: %w", err)
	}

	return doc, sha, nil
}

// resolveSelector resolves a tag/declaration selector through the index and cwd,
// choosing one of the matching files.
func resolveSelector(ctx context.Context, raw string, idx *sqliteIndex, roots []sourceRoot, affinityTags []string) (*evoke.Document, string, error) {
	candidates, sel, err := selectorCandidates(ctx, raw, idx, roots)
	if err != nil {
		return nil, "", err
	}
	return pickCandidate(candidates, sel, raw, affinityTags, idx, ctx)
}

// selectorCandidates resolves a selector to every .evoke file that matches it,
// refreshing the index once when the first lookup finds nothing.
func selectorCandidates(ctx context.Context, raw string, idx *sqliteIndex, roots []sourceRoot) ([]indexCandidate, evoke.Selector, error) {
	sel, err := evoke.ParseSelector(raw)
	if err != nil {
		return nil, sel, err
	}

	candidates, err := idx.find(ctx, roots, sel.Tags)
	if err != nil {
		return nil, sel, fmt.Errorf("index lookup failed: %w", err)
	}

	// Also check .evoke files in the immediate working directory.
	candidates = append(candidates, findInCwd(sel)...)
	candidates = deduplicateCandidates(candidates)

	if len(candidates) == 0 {
		// Refresh and retry once.
		for _, root := range roots {
			if _, refreshErr := idx.refreshRoot(ctx, root); refreshErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to refresh %s: %v\n", root.Path, refreshErr)
			}
		}
		candidates, err = idx.find(ctx, roots, sel.Tags)
		if err != nil {
			return nil, sel, fmt.Errorf("index lookup failed after refresh: %w", err)
		}
		if len(candidates) == 0 {
			return nil, sel, fmt.Errorf("no files match selector %q", raw)
		}
	}

	return candidates, sel, nil
}

// pickCandidate selects a candidate (weighted by affinity tag overlap if provided),
// reads/parses/validates it, and confirms it matches.
func pickCandidate(candidates []indexCandidate, sel evoke.Selector, raw string, affinityTags []string, idx *sqliteIndex, ctx context.Context) (*evoke.Document, string, error) {
	var chosen indexCandidate
	if len(candidates) == 1 {
		chosen = candidates[0]
	} else if len(affinityTags) == 0 || idx == nil {
		chosen = candidates[rand.IntN(len(candidates))]
	} else {
		chosen = pickByAffinity(ctx, candidates, affinityTags, idx)
	}

	return loadCandidate(chosen, sel, raw)
}

// loadCandidate reads, parses, and validates a chosen file, applying the
// implicit base-name tag the indexer adds and confirming the file still matches
// the selector it was found by.
func loadCandidate(chosen indexCandidate, sel evoke.Selector, raw string) (*evoke.Document, string, error) {
	data, err := os.ReadFile(chosen.Path)
	if err != nil {
		return nil, "", fmt.Errorf("selected file %s is no longer accessible: %w", chosen.Path, err)
	}

	doc, err := evoke.Parse(data)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse %s: %w", chosen.Path, err)
	}
	doc.Source = chosen.Path
	if err := evoke.Validate(doc); err != nil {
		return nil, "", fmt.Errorf("validation failed for %s: %w", chosen.Path, err)
	}

	// Add filename (without extension) as an implicit tag, matching what the indexer does.
	baseName := strings.ToLower(strings.TrimSuffix(chosen.Name, ".evoke"))
	if baseName != "" && !containsTag(doc.Metadata.Tags, baseName) {
		doc.Metadata.Tags = append(doc.Metadata.Tags, baseName)
	}

	if !evoke.MatchSelector(doc, sel) {
		return nil, "", fmt.Errorf("selected file %s no longer matches selector %q (file may have changed)", chosen.Path, raw)
	}

	return doc, chosen.Path, nil
}

// pickByAffinity selects a candidate by tag overlap with the files already
// resolved for this composition, rolling among ties.
func pickByAffinity(ctx context.Context, candidates []indexCandidate, affinityTags []string, idx *sqliteIndex) indexCandidate {
	affinitySet := make(map[string]bool, len(affinityTags))
	for _, t := range affinityTags {
		affinitySet[t] = true
	}

	return pickTopAffinity(candidates, affinitySet, func(c indexCandidate) []string {
		tags, err := idx.tagsForFile(ctx, c.Path)
		if err != nil {
			return nil
		}
		return tags
	})
}

// pickTopAffinity scores each candidate by how many affinity tags it carries
// and rolls among the highest scorers, so a file sharing a broad tag (a
// franchise) loses to one that also shares a specific tag (the character it
// was made for). Candidates sharing nothing are excluded entirely, unless no
// candidate shares anything, in which case the roll covers them all.
func pickTopAffinity(candidates []indexCandidate, affinity map[string]bool, tagsFor func(indexCandidate) []string) indexCandidate {
	best := 0
	var top []indexCandidate
	for _, c := range candidates {
		score := 0
		for _, t := range tagsFor(c) {
			if affinity[t] {
				score++
			}
		}
		switch {
		case score == 0 || score < best:
			continue
		case score > best:
			best = score
			top = []indexCandidate{c}
		default:
			top = append(top, c)
		}
	}

	if len(top) == 0 {
		return candidates[rand.IntN(len(candidates))]
	}
	return top[rand.IntN(len(top))]
}

// findInCwd checks .evoke files in the immediate working directory against a selector.
func findInCwd(sel evoke.Selector) []indexCandidate {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return nil
	}
	var candidates []indexCandidate
	for _, e := range entries {
		if e.IsDir() || !e.Type().IsRegular() || filepath.Ext(e.Name()) != ".evoke" {
			continue
		}
		path := filepath.Join(cwd, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		doc, err := evoke.Parse(data)
		if err != nil {
			continue
		}
		baseName := strings.ToLower(strings.TrimSuffix(e.Name(), ".evoke"))
		if baseName != "" && !containsTag(doc.Metadata.Tags, baseName) {
			doc.Metadata.Tags = append(doc.Metadata.Tags, baseName)
		}
		if !evoke.MatchSelector(doc, sel) {
			continue
		}
		candidates = append(candidates, indexCandidate{
			Path:         path,
			RelativePath: e.Name(),
			Name:         e.Name(),
			RootPath:     cwd,
		})
	}
	return candidates
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// deduplicateCandidates removes duplicate candidates by file path.
func deduplicateCandidates(candidates []indexCandidate) []indexCandidate {
	seen := make(map[string]bool, len(candidates))
	result := make([]indexCandidate, 0, len(candidates))
	for _, c := range candidates {
		if seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		result = append(result, c)
	}
	return result
}

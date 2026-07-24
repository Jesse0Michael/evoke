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

// Generate resolves inputs (selectors, local paths, registry references),
// merges the selected .evoke documents, and submits the result to ComfyUI.
func Generate(args []string, verbose bool) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	batch := fs.Int("b", 1, "number of images to generate")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *batch < 1 {
		fmt.Fprintln(os.Stderr, "evoke generate: -b must be at least 1")
		return 2
	}

	var cfg generateConfig
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
		return 1
	}

	inputArgs := fs.Args()
	if len(inputArgs) == 0 {
		fmt.Fprintln(os.Stderr, "evoke generate: at least one input is required")
		return 2
	}

	ctx := context.Background()

	// Load home configuration.
	settings, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
		return 1
	}

	manifest, err := manifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
		return 1
	}

	// Classify inputs.
	classified := make([]classifiedInput, 0, len(inputArgs))
	for _, raw := range inputArgs {
		classified = append(classified, classifyInput(raw))
	}

	// Check if we need the index (only for selector inputs).
	needsIndex := false
	for _, ci := range classified {
		if ci.Kind == inputSelector {
			needsIndex = true
			break
		}
	}

	var idx *sqliteIndex
	var roots []sourceRoot
	if needsIndex {
		roots, err = persistentRoots(settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
			return 1
		}

		idx, err = openDefaultIndex()
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
			return 1
		}
		defer func() { _ = idx.Close() }()

		// Ensure all roots are indexed.
		for _, root := range roots {
			if err := idx.ensureRoot(ctx, root); err != nil {
				fmt.Fprintf(os.Stderr, "evoke generate: failed to index %s: %v\n", root.Path, err)
				return 1
			}
		}
	}

	// Resolve static inputs (local paths, registry refs, literals) once.
	var staticDocs []*evoke.Document
	var selectorInputs []classifiedInput
	manifestChanged := false

	for _, ci := range classified {
		switch ci.Kind {
		case inputLocalPath:
			doc, path, err := resolveLocalPathWithIndex(ctx, ci.Raw, idx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
				return 1
			}
			fmt.Printf("%s\n  selected: %s (local path)\n", ci.Raw, path)
			staticDocs = append(staticDocs, doc)

		case inputRegistryRef:
			doc, changed, err := resolveRegistryRef(ctx, ci, manifest, settings)
			if err != nil {
				fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
				return 1
			}
			if changed {
				manifestChanged = true
			}
			fmt.Printf("%s\n  selected: %s (registry)\n", ci.Raw, libraryPath("~/.evoke", ci.Namespace, ci.Name))
			staticDocs = append(staticDocs, doc)

		case inputSelector:
			selectorInputs = append(selectorInputs, ci)

		case inputLiteral:
			doc := &evoke.Document{
				Declarations: []*evoke.Declaration{{Name: "PROMPT", Values: []string{ci.Raw}}},
			}
			fmt.Printf("%s\n  added as literal prompt\n", ci.Raw)
			staticDocs = append(staticDocs, doc)
		}
	}

	if manifestChanged {
		if err := saveManifest(manifest); err != nil {
			fmt.Fprintf(os.Stderr, "evoke generate: failed to save manifest: %v\n", err)
			return 1
		}
	}

	gen := comfyui.New(cfg.ComfyURL)
	gen.Verbose = verbose

	// Open the journal for recording generations.
	j, jErr := openDefaultJournal()
	if jErr != nil && verbose {
		fmt.Fprintf(os.Stderr, "evoke generate: warning: could not open journal: %v\n", jErr)
	}
	if j != nil {
		defer func() { _ = j.Close() }()
	}

	for i := range *batch {
		if *batch > 1 {
			fmt.Printf("\n--- batch %d/%d ---\n", i+1, *batch)
		}

		// Resolve selectors in CLI order, accumulating affinity tags from each pick.
		docs := make([]*evoke.Document, len(staticDocs), len(staticDocs)+len(selectorInputs))
		copy(docs, staticDocs)

		var affinityTags []string
		for _, ci := range selectorInputs {
			doc, path, err := resolveSelector(ctx, ci.Raw, idx, roots, affinityTags)
			if err != nil {
				fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
				return 1
			}
			fmt.Printf("%s\n  selected: %s (selector)\n", ci.Raw, path)
			docs = append(docs, doc)

			// Accumulate tags from the resolved file for affinity.
			if idx != nil {
				if tags, err := idx.tagsForFile(ctx, path); err == nil {
					affinityTags = append(affinityTags, tags...)
				}
			}
			affinityTags = append(affinityTags, doc.Metadata.Tags...)
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
			fmt.Fprintf(os.Stderr, "evoke generate: %v\n", err)
			return 1
		}

		// Record the generation in the journal.
		if j != nil && genResult.PromptID != "" {
			_ = j.Record(ctx, genResult.PromptID, "comfyui", inputsSummary(inputArgs))
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

// resolveSelector resolves a tag/declaration selector through the index and cwd.
// affinityTags, if non-empty, are used to prefer candidates that share tags with
// previously resolved selectors.
func resolveSelector(ctx context.Context, raw string, idx *sqliteIndex, roots []sourceRoot, affinityTags []string) (*evoke.Document, string, error) {
	sel, err := evoke.ParseSelector(raw)
	if err != nil {
		return nil, "", err
	}

	// Collect candidates from cwd (in-memory) and the persistent index.
	cwdCandidates, err := findInCwd(sel)
	if err != nil {
		return nil, "", fmt.Errorf("cwd lookup failed: %w", err)
	}

	indexCandidates, err := idx.find(ctx, roots, sel.Tags)
	if err != nil {
		return nil, "", fmt.Errorf("index lookup failed: %w", err)
	}

	candidates := deduplicateCandidates(append(cwdCandidates, indexCandidates...))

	if len(candidates) == 0 {
		// Refresh and retry once.
		for _, root := range roots {
			if refreshErr := idx.refreshRoot(ctx, root); refreshErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to refresh %s: %v\n", root.Path, refreshErr)
			}
		}
		indexCandidates, err = idx.find(ctx, roots, sel.Tags)
		if err != nil {
			return nil, "", fmt.Errorf("index lookup failed after refresh: %w", err)
		}
		candidates = deduplicateCandidates(append(cwdCandidates, indexCandidates...))
		if len(candidates) == 0 {
			return nil, "", fmt.Errorf("no files match selector %q", raw)
		}
	}

	return pickCandidate(candidates, sel, raw, affinityTags, idx, ctx)
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
	if baseName != "" {
		hasTag := false
		for _, t := range doc.Metadata.Tags {
			if t == baseName {
				hasTag = true
				break
			}
		}
		if !hasTag {
			doc.Metadata.Tags = append(doc.Metadata.Tags, baseName)
		}
	}

	if !evoke.MatchSelector(doc, sel) {
		return nil, "", fmt.Errorf("selected file %s no longer matches selector %q (index may be stale; run 'evoke index')", chosen.Path, raw)
	}

	return doc, chosen.Path, nil
}

// pickByAffinity selects a candidate by filtering to those sharing tags with the affinity set.
// If any candidates match at least one affinity tag, pick randomly from that filtered set.
// If none match, pick randomly from all candidates.
func pickByAffinity(ctx context.Context, candidates []indexCandidate, affinityTags []string, idx *sqliteIndex) indexCandidate {
	affinitySet := make(map[string]bool, len(affinityTags))
	for _, t := range affinityTags {
		affinitySet[t] = true
	}

	var filtered []indexCandidate
	for _, c := range candidates {
		tags, err := idx.tagsForFile(ctx, c.Path)
		if err != nil {
			continue
		}
		for _, t := range tags {
			if affinitySet[t] {
				filtered = append(filtered, c)
				break
			}
		}
	}

	if len(filtered) > 0 {
		return filtered[rand.IntN(len(filtered))]
	}
	return candidates[rand.IntN(len(candidates))]
}

// findInCwd walks the current working directory and returns candidates matching the selector.
// This is done in-memory without persisting to the index DB.
func findInCwd(sel evoke.Selector) ([]indexCandidate, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	homeDir, _ := home()
	discovered, err := walkRoot(cwd, homeDir)
	if err != nil {
		return nil, err
	}

	var candidates []indexCandidate
	for _, df := range discovered {
		data, err := os.ReadFile(df.Path)
		if err != nil {
			continue
		}
		doc, err := evoke.Parse(data)
		if err != nil {
			continue
		}
		// Add filename (without extension) as an implicit tag for matching.
		baseName := strings.ToLower(strings.TrimSuffix(df.Name, ".evoke"))
		if baseName != "" {
			hasTag := false
			for _, t := range doc.Metadata.Tags {
				if t == baseName {
					hasTag = true
					break
				}
			}
			if !hasTag {
				doc.Metadata.Tags = append(doc.Metadata.Tags, baseName)
			}
		}
		if !evoke.MatchSelector(doc, sel) {
			continue
		}
		candidates = append(candidates, indexCandidate{
			Path:         df.Path,
			RelativePath: df.RelativePath,
			Name:         df.Name,
			RootPath:     cwd,
		})
	}
	return candidates, nil
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

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// resolution holds the prepared inputs for composing .evoke documents. Static
// inputs (local paths, registry refs, literals) are resolved once up front;
// selectors are resolved on each call to documents() so repeated resolution
// (e.g. generate's batch loop) can re-roll random selector picks.
type resolution struct {
	staticDocs      []*evoke.Document
	selectorInputs  []classifiedInput
	idx             *sqliteIndex
	roots           []sourceRoot
	manifestChanged bool
}

// prepareResolution classifies inputs, ensures the file index is available when
// selectors are present, and resolves all static inputs once. It is shared by
// the generate and chat commands so both compose files through one pipeline.
// The caller must call Close when done.
func prepareResolution(ctx context.Context, inputArgs []string, settings *Settings, manifest *Manifest, verbose bool) (*resolution, error) {
	classified := make([]classifiedInput, 0, len(inputArgs))
	for _, raw := range inputArgs {
		classified = append(classified, classifyInput(raw))
	}

	res := &resolution{}

	needsIndex := false
	for _, ci := range classified {
		if ci.Kind == inputSelector {
			needsIndex = true
			break
		}
	}

	if needsIndex {
		idx, roots, err := openIndexRoots(ctx, settings, verbose)
		if err != nil {
			return nil, err
		}
		res.idx = idx
		res.roots = roots
	}

	for _, ci := range classified {
		switch ci.Kind {
		case inputLocalPath:
			doc, path, err := resolveLocalPathWithIndex(ctx, ci.Raw, res.idx)
			if err != nil {
				res.close()
				return nil, err
			}
			fmt.Printf("%s => %s\n", ci.Raw, displayPath(path, res.roots))
			res.staticDocs = append(res.staticDocs, doc)

		case inputRegistryRef:
			doc, changed, err := resolveRegistryRef(ctx, ci, manifest, settings)
			if err != nil {
				res.close()
				return nil, err
			}
			if changed {
				res.manifestChanged = true
			}
			fmt.Printf("%s => %s\n", ci.Raw, ci.Namespace+"/"+ci.Name+".evoke")
			res.staticDocs = append(res.staticDocs, doc)

		case inputSelector:
			res.selectorInputs = append(res.selectorInputs, ci)

		case inputLiteral:
			doc := &evoke.Document{
				Declarations: []*evoke.Declaration{
					{Name: "PROMPT", Values: []string{ci.Raw}},
					{Name: "APPAREL"},
					{Name: "ENVIRONMENT"},
					{Name: "SCENARIO"},
				},
			}
			res.staticDocs = append(res.staticDocs, doc)
		}
	}

	return res, nil
}

// openIndexRoots opens the file index and refreshes every persistent source
// root, returning the index and the roots. It is shared by resolution and the
// inspect command so both discover files through the same index. The caller
// owns the returned index and must Close it.
func openIndexRoots(ctx context.Context, settings *Settings, verbose bool) (*sqliteIndex, []sourceRoot, error) {
	roots, err := persistentRoots(settings)
	if err != nil {
		return nil, nil, err
	}

	idx, err := openDefaultIndex()
	if err != nil {
		return nil, nil, err
	}

	if err := idx.pruneRoots(ctx, roots); err != nil {
		_ = idx.Close()
		return nil, nil, fmt.Errorf("failed to prune index: %w", err)
	}
	for _, root := range roots {
		result, err := idx.refreshRoot(ctx, root)
		if err != nil {
			_ = idx.Close()
			return nil, nil, fmt.Errorf("failed to index %s: %w", root.Path, err)
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "indexed %s (%d files", root.Path, result.Total)
			if result.Added > 0 || result.Updated > 0 || result.Removed > 0 {
				fmt.Fprintf(os.Stderr, ";")
				if result.Added > 0 {
					fmt.Fprintf(os.Stderr, " +%d", result.Added)
				}
				if result.Updated > 0 {
					fmt.Fprintf(os.Stderr, " ~%d", result.Updated)
				}
				if result.Removed > 0 {
					fmt.Fprintf(os.Stderr, " -%d", result.Removed)
				}
			}
			fmt.Fprintf(os.Stderr, ")\n")
		}
	}

	return idx, roots, nil
}

// documents resolves selectors (re-rolling random picks each call) and returns
// the full ordered document set for one composition.
func (r *resolution) documents(ctx context.Context) ([]*evoke.Document, error) {
	docs := make([]*evoke.Document, len(r.staticDocs), len(r.staticDocs)+len(r.selectorInputs))
	copy(docs, r.staticDocs)

	var affinityTags []string
	for _, ci := range r.selectorInputs {
		doc, path, err := resolveSelector(ctx, ci.Raw, r.idx, r.roots, affinityTags)
		if err != nil {
			return nil, err
		}
		fmt.Printf("%s => %s\n", ci.Raw, displayPath(path, r.roots))
		docs = append(docs, doc)

		if r.idx != nil {
			if tags, err := r.idx.tagsForFile(ctx, path); err == nil {
				affinityTags = append(affinityTags, tags...)
			}
		}
		affinityTags = append(affinityTags, doc.Metadata.Tags...)
	}
	return docs, nil
}

// variantPick is one selector input paired with the file chosen to fill it.
type variantPick struct {
	input     string
	selector  evoke.Selector
	candidate indexCandidate
}

// variants enumerates every combination of files matching the selector inputs.
// The leftmost selector varies slowest and candidates are ordered by path, so
// the sequence is reproducible across runs. Static inputs are shared by every
// combination, so inputs with no selectors yield exactly one. Enumerating more
// than maxBatch combinations is an error rather than a silent truncation: a
// caller asking for every variation should not quietly receive a subset.
func (r *resolution) variants(ctx context.Context) ([][]variantPick, error) {
	slots := make([][]variantPick, 0, len(r.selectorInputs))
	total := 1

	for _, ci := range r.selectorInputs {
		candidates, sel, err := selectorCandidates(ctx, ci.Raw, r.idx, r.roots)
		if err != nil {
			return nil, err
		}
		slices.SortFunc(candidates, func(a, b indexCandidate) int {
			return strings.Compare(a.Path, b.Path)
		})

		slot := make([]variantPick, 0, len(candidates))
		for _, c := range candidates {
			slot = append(slot, variantPick{input: ci.Raw, selector: sel, candidate: c})
		}
		slots = append(slots, slot)
		total *= len(slot)
	}

	if total > maxBatch {
		return nil, fmt.Errorf("xall would generate %d combinations (%s), more than the limit of %d; narrow the selection or drop xall",
			total, describeSlots(slots), maxBatch)
	}

	return crossProduct(slots), nil
}

// crossProduct builds every combination of one pick per slot, with the leftmost
// slot varying slowest. With no slots it yields a single empty combination.
func crossProduct(slots [][]variantPick) [][]variantPick {
	combos := [][]variantPick{{}}
	for _, slot := range slots {
		next := make([][]variantPick, 0, len(combos)*len(slot))
		for _, combo := range combos {
			for _, pick := range slot {
				row := make([]variantPick, len(combo), len(combo)+1)
				copy(row, combo)
				next = append(next, append(row, pick))
			}
		}
		combos = next
	}
	return combos
}

// describeSlots renders per-selector match counts for the enumeration limit
// error, e.g. "character 12 x pose 40".
func describeSlots(slots [][]variantPick) string {
	parts := make([]string, 0, len(slots))
	for _, slot := range slots {
		if len(slot) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d", slot[0].input, len(slot)))
	}
	return strings.Join(parts, " x ")
}

// documentsFor loads the document set for one enumerated combination, ordered
// the same way documents orders it: static inputs first, then selector inputs.
func (r *resolution) documentsFor(picks []variantPick) ([]*evoke.Document, error) {
	docs := make([]*evoke.Document, len(r.staticDocs), len(r.staticDocs)+len(picks))
	copy(docs, r.staticDocs)

	for _, p := range picks {
		doc, path, err := loadCandidate(p.candidate, p.selector, p.input)
		if err != nil {
			return nil, err
		}
		fmt.Printf("%s => %s\n", p.input, displayPath(path, r.roots))
		docs = append(docs, doc)
	}
	return docs, nil
}

// displayPath renders a resolved .evoke file path for resolution output as a
// short, readable location: relative to the source root that contains it (e.g.
// "chat/companion.evoke"), else relative to the working directory, else the
// path unchanged.
func displayPath(path string, roots []sourceRoot) string {
	for _, root := range roots {
		if rel, err := filepath.Rel(root.Path, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

// close releases the index without reporting a persistence error (used on the
// error path).
func (r *resolution) close() {
	if r.idx != nil {
		_ = r.idx.Close()
		r.idx = nil
	}
}

// Close releases resources held by the resolution.
func (r *resolution) Close() error {
	if r.idx != nil {
		err := r.idx.Close()
		r.idx = nil
		return err
	}
	return nil
}

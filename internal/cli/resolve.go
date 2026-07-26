package cli

import (
	"context"
	"fmt"

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
func prepareResolution(ctx context.Context, inputArgs []string, settings *Settings, manifest *Manifest) (*resolution, error) {
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
		roots, err := persistentRoots(settings)
		if err != nil {
			return nil, err
		}
		res.roots = roots

		idx, err := openDefaultIndex()
		if err != nil {
			return nil, err
		}
		res.idx = idx

		for _, root := range roots {
			if err := idx.ensureRoot(ctx, root); err != nil {
				_ = idx.Close()
				return nil, fmt.Errorf("failed to index %s: %w", root.Path, err)
			}
		}
	}

	for _, ci := range classified {
		switch ci.Kind {
		case inputLocalPath:
			doc, path, err := resolveLocalPathWithIndex(ctx, ci.Raw, res.idx)
			if err != nil {
				res.close()
				return nil, err
			}
			fmt.Printf("%s\n  selected: %s (local path)\n", ci.Raw, path)
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
			fmt.Printf("%s\n  selected: %s (registry)\n", ci.Raw, libraryPath("~/.evoke", ci.Namespace, ci.Name))
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
			fmt.Printf("%s\n  added as literal prompt\n", ci.Raw)
			res.staticDocs = append(res.staticDocs, doc)
		}
	}

	return res, nil
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
		fmt.Printf("%s\n  selected: %s (selector)\n", ci.Raw, path)
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

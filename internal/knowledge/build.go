package knowledge

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jesse0michael/evoke/pkg/evoke"
)

// skipDirs are directories that never hold corpus content. They are only
// skipped below the walk root, so passing e.g. -input ./site still works.
var skipDirs = map[string]bool{
	"node_modules": true,
}

const (
	markdownExt = ".md"
	evokeExt    = ".evoke"
)

// corpusExt reports whether a file name is one the builder indexes.
func corpusExt(name string) bool {
	ext := filepath.Ext(name)
	return strings.EqualFold(ext, markdownExt) || strings.EqualFold(ext, evokeExt)
}

// BuildOptions configures a knowledge database build.
type BuildOptions struct {
	// Input is the root directory of the corpus.
	Input string
	// Output is the path of the database file to write.
	Output string
	// EmbedModel and EmbedURL select the embedding endpoint.
	EmbedModel string
	EmbedURL   string
	// MaxTokens and Overlap bound chunk size and the context carried across a
	// split. Zero values use the package defaults.
	MaxTokens int
	Overlap   int
	// Exclude holds glob patterns matched against each file's corpus-relative
	// path and its base name.
	Exclude []string
	// Progress, when set, is called once per file as it is processed.
	Progress func(FileProgress)
}

// FileProgress reports one processed file.
type FileProgress struct {
	File   string
	Chunks int
}

// BuildResult summarizes a completed build.
type BuildResult struct {
	Output string
	Files  int
	Chunks int
	Model  string
	Dims   int
}

// Build walks the corpus, chunks every markdown and .evoke file, embeds each
// chunk, and writes the vector database. The database is built into a
// temporary file and renamed into place only on success, so a failed rebuild
// (an unreachable embedding endpoint, say) leaves the existing database intact.
func Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	if opts.Input == "" {
		return nil, fmt.Errorf("input directory is required")
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = DefaultMaxTokens
	}
	if opts.Overlap < 0 {
		opts.Overlap = DefaultOverlap
	}
	for _, pattern := range opts.Exclude {
		if _, err := filepath.Match(pattern, "probe"); err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %w", pattern, err)
		}
	}

	files, err := corpusFiles(opts.Input, opts.Exclude)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no markdown or evoke files found under %s", opts.Input)
	}

	embedder := NewEmbedder(opts.EmbedModel, opts.EmbedURL)
	result := &BuildResult{Output: opts.Output, Files: len(files), Model: embedder.Model()}

	// The database is only created once the first vector reveals the model's
	// dimensionality, so the schema is never guessed.
	var store *Store
	var tmpPath string
	defer func() {
		if store != nil {
			_ = store.Close()
		}
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	id := 0
	for _, path := range files {
		rel, err := filepath.Rel(opts.Input, path)
		if err != nil {
			rel = path
		}
		chunks, err := fileChunks(path, filepath.ToSlash(rel), opts)
		if err != nil {
			return nil, err
		}

		for _, c := range chunks {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			vec, err := embedder.Embed(ctx, EmbedText(c))
			if err != nil {
				return nil, err
			}
			if store == nil {
				result.Dims = len(vec)
				store, tmpPath, err = createTemp(opts, embedder.Model(), len(vec))
				if err != nil {
					return nil, err
				}
			}
			id++
			if err := store.Insert(id, c, vec); err != nil {
				return nil, err
			}
		}

		result.Chunks += len(chunks)
		if opts.Progress != nil {
			opts.Progress(FileProgress{File: filepath.ToSlash(rel), Chunks: len(chunks)})
		}
	}

	if store == nil {
		return nil, fmt.Errorf("no chunks were produced from %s", opts.Input)
	}
	if err := store.Close(); err != nil {
		return nil, fmt.Errorf("failed to close database: %w", err)
	}
	store = nil
	if err := os.Rename(tmpPath, opts.Output); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", opts.Output, err)
	}
	tmpPath = ""

	return result, nil
}

// fileChunks reads one corpus file and splits it into chunks. Markdown is
// chunked as-is; a .evoke file is parsed and re-rendered as markdown first, so
// only world facts reach the index and the chunker sees real heading structure
// instead of evoke's "#" comments. Chunks keep the source path either way.
func fileChunks(path, rel string, opts BuildOptions) ([]Chunk, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	content := string(raw)
	if strings.EqualFold(filepath.Ext(path), evokeExt) {
		doc, err := evoke.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		// A fragment with no NAME, or one carrying only generator input,
		// renders to nothing and contributes no chunks.
		content = renderEvokeMarkdown(doc)
		if content == "" {
			return nil, nil
		}
	}

	return ChunkMarkdown(rel, content, opts.MaxTokens, opts.Overlap), nil
}

// createTemp opens the database in a sibling temp file of the output so the
// final rename stays on one filesystem.
func createTemp(opts BuildOptions, model string, dims int) (*Store, string, error) {
	dir := filepath.Dir(opts.Output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("failed to create output directory %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(opts.Output)+".*")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create temporary database: %w", err)
	}
	tmpPath := f.Name()
	_ = f.Close()

	source, err := filepath.Abs(opts.Input)
	if err != nil {
		source = opts.Input
	}
	store, err := CreateStore(tmpPath, Meta{EmbedModel: model, Dims: dims, Source: source})
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, "", err
	}
	return store, tmpPath, nil
}

// corpusFiles walks root and returns the sorted set of markdown and .evoke
// files, skipping hidden entries, known non-content directories, and anything
// matching an exclude pattern.
func corpusFiles(root string, exclude []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			// Never skip the walk root itself, only directories beneath it.
			if path == root {
				return nil
			}
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !corpusExt(name) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if excluded(filepath.ToSlash(rel), name, exclude) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk %s: %w", root, err)
	}
	return files, nil
}

// excluded reports whether a file matches any exclude pattern, by relative path
// or by base name, so both `--exclude index.md` and `--exclude Design/*` work.
func excluded(rel, base string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

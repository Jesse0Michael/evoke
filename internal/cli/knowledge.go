package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jesse0michael/evoke/internal/knowledge"
	"github.com/jesse0michael/evoke/internal/ollama"
)

// defaultKnowledgeDB is the database name used when --output is not given.
const defaultKnowledgeDB = "knowledge.db"

// KnowledgeCmd builds the RAG vector database that a KNOWLEDGE declaration
// refers to: it walks a directory of markdown and .evoke files, splits each
// file into heading-scoped chunks, embeds them, and writes a SQLite database.
//
// The database is written to the working directory, and --output is an ordinary
// path. chat resolves a KNOWLEDGE db= against chat.model_paths, but that is a
// search path — often a directory another application owns — so it is never a
// write destination. When the built database is not resolvable, the command
// says so and prints the setting that would make it resolvable.
func KnowledgeCmd(args []string, verbose bool) int {
	fs := flag.NewFlagSet("knowledge", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	output := fs.String("output", "", "database to write (default: ./"+defaultKnowledgeDB+")")
	fs.StringVar(output, "o", "", "database to write (shorthand)")
	model := fs.String("model", knowledge.DefaultEmbedModel, "embedding model")
	url := fs.String("url", "", "ollama-compatible API base URL (default: chat.embed_url setting, else "+knowledge.DefaultEmbedURL+")")
	maxTokens := fs.Int("max-tokens", knowledge.DefaultMaxTokens, "target maximum tokens per chunk")
	overlap := fs.Int("overlap", knowledge.DefaultOverlap, "token overlap between split chunks")
	var exclude stringList
	fs.Var(&exclude, "exclude", "glob of files to skip, matched on relative path or base name (repeatable)")

	// flag stops parsing at the first non-flag argument, so resume after each
	// positional. This accepts flags before or after the directory, since
	// `evoke knowledge ./docs --exclude index.md` is the natural thing to type.
	var positional []string
	remaining := args
	for {
		if err := fs.Parse(remaining); err != nil {
			return 2
		}
		remaining = fs.Args()
		if len(remaining) == 0 {
			break
		}
		positional = append(positional, remaining[0])
		remaining = remaining[1:]
	}

	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "evoke knowledge: exactly one input directory is required")
		fmt.Fprintln(os.Stderr, "\nusage: evoke knowledge <dir> [--output db] [--model name] [--exclude glob]")
		return 2
	}

	input, err := expandPath(positional[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke knowledge: %v\n", err)
		return 1
	}
	info, err := os.Stat(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke knowledge: %v\n", err)
		return 1
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "evoke knowledge: %s is not a directory\n", input)
		return 1
	}

	settings, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke knowledge: %v\n", err)
		return 1
	}
	modelPaths := knowledgeModelPaths(settings)

	out, err := resolveKnowledgeOutput(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke knowledge: %v\n", err)
		return 1
	}

	embedURL := *url
	if embedURL == "" && settings.Chat != nil {
		embedURL = settings.Chat.EmbedURL
	}
	if embedURL == "" {
		embedURL = knowledge.DefaultEmbedURL
	}

	// Interrupt cancels mid-build; the temporary database is discarded and any
	// existing one at the output path is left untouched.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The build embeds every chunk through an ollama-compatible endpoint, so
	// make sure one is serving the model first — starting a local server when
	// none is, and stopping it again when the build finishes.
	embedder, err := ollama.Ensure(ctx, embedURL, []string{*model}, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke knowledge: %v\n", err)
		return 1
	}
	defer func() { _ = embedder.Close(context.WithoutCancel(ctx)) }()
	if embedder.Started() {
		fmt.Printf("Started ollama for embedding\n")
	}

	opts := knowledge.BuildOptions{
		Input:      input,
		Output:     out,
		EmbedModel: *model,
		EmbedURL:   embedURL,
		MaxTokens:  *maxTokens,
		Overlap:    *overlap,
		Exclude:    exclude,
	}
	if verbose {
		opts.Progress = func(p knowledge.FileProgress) {
			fmt.Printf("  %s: %d chunks\n", p.File, p.Chunks)
		}
	}

	result, err := knowledge.Build(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke knowledge: %v\n", err)
		return 1
	}

	fmt.Printf("Wrote %s\n", result.Output)
	fmt.Printf("  %d files, %d chunks, %s (%d dimensions)\n", result.Files, result.Chunks, result.Model, result.Dims)

	base := filepath.Base(result.Output)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	fmt.Printf("\nReference it from a .evoke file:\n\n    KNOWLEDGE %s db=%s\n", name, base)

	// chat looks a db= up by name under chat.model_paths, so a database outside
	// those directories will not resolve. Say so once, with the fix, instead of
	// writing into a search path that another application may own.
	if !underAny(result.Output, modelPaths) {
		fmt.Printf("\nTo make it resolvable by chat:\n\n    evoke settings set chat.model_path %s\n",
			filepath.Dir(result.Output))
	}
	fmt.Println()
	return 0
}

// knowledgeModelPaths returns the configured chat model directories, expanded
// to absolute paths. These are the directories a KNOWLEDGE db= setting is
// resolved against.
func knowledgeModelPaths(s *Settings) []string {
	if s == nil || s.Chat == nil {
		return nil
	}
	var dirs []string
	for _, p := range s.Chat.ModelPaths {
		if abs, err := expandPath(p); err == nil {
			dirs = append(dirs, abs)
		}
	}
	return dirs
}

// resolveKnowledgeOutput returns the absolute path to write. --output is an
// ordinary path, relative to the working directory like any other CLI; the
// default is ./knowledge.db.
func resolveKnowledgeOutput(output string) (string, error) {
	if output == "" {
		output = defaultKnowledgeDB
	}
	return expandPath(output)
}

// underAny reports whether path sits inside one of the directories. A KNOWLEDGE
// db= is resolved by walking the model paths recursively, so any depth counts.
func underAny(path string, dirs []string) bool {
	for _, dir := range dirs {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// stringList collects a repeatable string flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

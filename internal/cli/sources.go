package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// sourceKind identifies the origin of a source root.
type sourceKind string

const (
	sourceKindCurrent     sourceKind = "current"
	sourceKindEnvironment sourceKind = "environment"
	sourceKindConfigured  sourceKind = "configured"
	sourceKindLibrary     sourceKind = "library"
)

// sourceRoot represents a directory from which .evoke files are discovered.
type sourceRoot struct {
	Path string
	Kind sourceKind
}

// inputKind classifies a generate input argument.
type inputKind int

const (
	inputSelector inputKind = iota
	inputLocalPath
	inputRegistryRef
	inputLiteral
)

// classifiedInput holds a classified generate argument.
type classifiedInput struct {
	Raw       string
	Kind      inputKind
	Namespace string // for registry refs
	Name      string // for registry refs
}

// classifyInput determines whether a raw CLI argument is a registry reference,
// a local path, a literal prompt string, or a tag/declaration selector.
func classifyInput(raw string) classifiedInput {
	if strings.HasPrefix(raw, "@") {
		ns, name := parseRegistryRef(raw)
		return classifiedInput{Raw: raw, Kind: inputRegistryRef, Namespace: ns, Name: name}
	}
	if isLocalPath(raw) {
		return classifiedInput{Raw: raw, Kind: inputLocalPath}
	}
	if strings.Contains(raw, " ") {
		return classifiedInput{Raw: raw, Kind: inputLiteral}
	}
	return classifiedInput{Raw: raw, Kind: inputSelector}
}

// parseRegistryRef splits @namespace/name into its parts.
func parseRegistryRef(ref string) (string, string) {
	trimmed := strings.TrimPrefix(ref, "@")
	ns, name, _ := strings.Cut(trimmed, "/")
	return ns, name
}

// isLocalPath reports whether the argument looks like a file path rather than a selector.
func isLocalPath(arg string) bool {
	if strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") || filepath.IsAbs(arg) {
		return true
	}
	if strings.HasSuffix(arg, ".evoke") {
		return true
	}
	return false
}

// resolveLocalFilePath resolves a local path input to an existing .evoke file.
// It tries the path as-is, then with .evoke appended.
func resolveLocalFilePath(raw string) (string, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q: %w", raw, err)
	}

	if _, err := os.Stat(abs); err == nil {
		return abs, nil
	}

	if filepath.Ext(abs) != ".evoke" {
		withExt := abs + ".evoke"
		if _, err := os.Stat(withExt); err == nil {
			return withExt, nil
		}
	}

	return "", fmt.Errorf("file not found: %s", raw)
}

// persistentRoots returns the source roots that should be stored in the index DB.
// This excludes the current working directory, which is ephemeral.
func persistentRoots(settings *Settings) ([]sourceRoot, error) {
	seen := make(map[string]bool)
	var roots []sourceRoot

	add := func(path string, kind sourceKind) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		roots = append(roots, sourceRoot{Path: path, Kind: kind})
	}

	// 1. EVOKE_PATH directories.
	if envPath := os.Getenv("EVOKE_PATH"); envPath != "" {
		for _, p := range filepath.SplitList(envPath) {
			abs, err := expandPath(p)
			if err != nil {
				continue
			}
			add(abs, sourceKindEnvironment)
		}
	}

	// 2. settings.json sourcePaths.
	if settings != nil {
		expanded, err := settings.paths()
		if err != nil {
			return nil, fmt.Errorf("failed to expand source paths: %w", err)
		}
		for _, p := range expanded {
			add(p, sourceKindConfigured)
		}
	}

	// 3. ~/.evoke/library.
	libDir, err := library()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve library directory: %w", err)
	}
	add(libDir, sourceKindLibrary)

	return roots, nil
}

// skipDirs are directory names to skip during recursive walks.
var skipDirs = map[string]bool{
	".git": true,
	".hg":  true,
	".svn": true,
}

// walkRoot recursively discovers .evoke files under a source root.
// It skips VCS directories and does not follow directory symlinks.
// It skips evokeHomeDir if it appears inside the walked root.
func walkRoot(root string, evokeHomeDir string) ([]discoveredFile, error) {
	var files []discoveredFile
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve root %q: %w", root, err)
	}

	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] {
				return fs.SkipDir
			}
			// Skip EVOKE_HOME if it appears inside another root.
			if evokeHomeDir != "" && path == evokeHomeDir && path != absRoot {
				return fs.SkipDir
			}
			// Don't follow directory symlinks.
			if d.Type()&fs.ModeSymlink != 0 {
				return fs.SkipDir
			}
			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}
		if filepath.Ext(path) != ".evoke" {
			return nil
		}

		// Skip underscore-prefixed files (excluded by convention).
		if strings.HasPrefix(filepath.Base(path), "_") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil // skip
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			rel = path
		}

		baseName := strings.TrimSuffix(filepath.Base(path), ".evoke")

		files = append(files, discoveredFile{
			Path:         path,
			RelativePath: rel,
			Name:         baseName,
			Size:         info.Size(),
			ModifiedNS:   info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk %q: %w", root, err)
	}
	return files, nil
}

// discoveredFile is a .evoke file found during a directory walk.
type discoveredFile struct {
	Path         string
	RelativePath string
	Name         string
	Size         int64
	ModifiedNS   int64
}

// validateRegistrySlug checks that a namespace or name component is safe.
func validateRegistrySlug(s string) error {
	if s == "" {
		return fmt.Errorf("empty slug")
	}
	if strings.Contains(s, "..") {
		return fmt.Errorf("slug %q contains '..'", s)
	}
	if strings.ContainsAny(s, "/\\") {
		return fmt.Errorf("slug %q contains path separator", s)
	}
	if filepath.IsAbs(s) {
		return fmt.Errorf("slug %q is an absolute path", s)
	}
	return nil
}

// libraryPath returns the filesystem path for a registry artifact within the library.
func libraryPath(evokeHomeDir, namespace, name string) string {
	return filepath.Join(evokeHomeDir, "library", namespace, name+".evoke")
}

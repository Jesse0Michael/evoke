package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyInput(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantKind inputKind
		wantNS   string
		wantName string
	}{
		{
			name:     "registry reference",
			raw:      "@jesse/ashley",
			wantKind: inputRegistryRef,
			wantNS:   "jesse",
			wantName: "ashley",
		},
		{
			name:     "relative path with dot-slash",
			raw:      "./ashley",
			wantKind: inputLocalPath,
		},
		{
			name:     "relative path with parent",
			raw:      "../shared/ashley",
			wantKind: inputLocalPath,
		},
		{
			name:     "absolute path",
			raw:      "/home/me/evoke/ashley.evoke",
			wantKind: inputLocalPath,
		},
		{
			name:     "filename with evoke extension",
			raw:      "ashley.evoke",
			wantKind: inputLocalPath,
		},
		{
			name:     "bare tag selector",
			raw:      "character",
			wantKind: inputSelector,
		},
		{
			name:     "multi-tag selector",
			raw:      "character+nurse",
			wantKind: inputSelector,
		},
		{
			name:     "faceted selector",
			raw:      "c:nurse",
			wantKind: inputSelector,
		},
		{
			name:     "literal prompt string with spaces",
			raw:      "a female scientist in a science lab",
			wantKind: inputLiteral,
		},
		{
			name:     "literal prompt short phrase",
			raw:      "dark forest",
			wantKind: inputLiteral,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyInput(tt.raw)
			require.Equal(t, tt.wantKind, got.Kind)
			if tt.wantNS != "" {
				require.Equal(t, tt.wantNS, got.Namespace)
			}
			if tt.wantName != "" {
				require.Equal(t, tt.wantName, got.Name)
			}
		})
	}
}

func TestResolveLocalFilePath(t *testing.T) {
	dir := t.TempDir()

	// Create a test .evoke file.
	evokePath := filepath.Join(dir, "ashley.evoke")
	require.NoError(t, os.WriteFile(evokePath, []byte("NAME\n    Ashley\n"), 0o644))

	tests := []struct {
		name      string
		raw       string
		wantPath  string
		wantError bool
	}{
		{
			name:     "exact file with extension",
			raw:      evokePath,
			wantPath: evokePath,
		},
		{
			name:     "infers evoke extension",
			raw:      filepath.Join(dir, "ashley"),
			wantPath: evokePath,
		},
		{
			name:      "missing file",
			raw:       filepath.Join(dir, "nonexistent"),
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveLocalFilePath(tt.raw)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantPath, got)
		})
	}
}

func TestPersistentRoots(t *testing.T) {
	evokeHomeDir := t.TempDir()
	t.Setenv("EVOKE_HOME", evokeHomeDir)
	t.Setenv("EVOKE_PATH", "")

	settings := &Settings{
		Paths: []string{"/tmp/test-source"},
	}

	roots, err := persistentRoots(settings)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(roots), 2) // configured + library

	require.Equal(t, sourceKindConfigured, roots[0].Kind)
	require.Equal(t, sourceKindLibrary, roots[len(roots)-1].Kind)

	// cwd should not be included.
	for _, r := range roots {
		require.NotEqual(t, sourceKindCurrent, r.Kind)
	}
}

func TestPersistentRootsWithEvokePath(t *testing.T) {
	evokeHomeDir := t.TempDir()
	envDir := t.TempDir()
	t.Setenv("EVOKE_HOME", evokeHomeDir)
	t.Setenv("EVOKE_PATH", envDir)

	roots, err := persistentRoots(&Settings{})
	require.NoError(t, err)

	kinds := make([]sourceKind, len(roots))
	for i, r := range roots {
		kinds[i] = r.Kind
	}
	require.Contains(t, kinds, sourceKindEnvironment)
}

func TestPersistentRootsDeduplicates(t *testing.T) {
	evokeHomeDir := t.TempDir()
	t.Setenv("EVOKE_HOME", evokeHomeDir)

	sharedDir := t.TempDir()

	// Set EVOKE_PATH and Paths to the same directory (should be deduped).
	t.Setenv("EVOKE_PATH", sharedDir)

	roots, err := persistentRoots(&Settings{Paths: []string{sharedDir}})
	require.NoError(t, err)

	paths := make(map[string]int)
	for _, r := range roots {
		paths[r.Path]++
	}
	for p, count := range paths {
		require.Equal(t, 1, count, "path %q appeared %d times", p, count)
	}
}

func TestWalkRoot(t *testing.T) {
	dir := t.TempDir()

	// Create a directory tree with .evoke files.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "characters", "modern"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "apparel"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))

	writeFile := func(path, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644))
	}

	writeFile("characters/modern/ashley.evoke", "NAME\n    Ashley\n")
	writeFile("characters/elara.evoke", "NAME\n    Elara\n")
	writeFile("apparel/scrubs.evoke", "APPAREL\n    scrubs\n")
	writeFile(".git/ignored.evoke", "NAME\n    Hidden\n")
	writeFile("readme.txt", "not an evoke file")

	files, err := walkRoot(dir, "")
	require.NoError(t, err)
	require.Len(t, files, 3)

	names := make(map[string]bool)
	for _, f := range files {
		names[f.Name] = true
		require.NotEmpty(t, f.Path)
		require.NotEmpty(t, f.RelativePath)
		require.Greater(t, f.Size, int64(0))
		require.Greater(t, f.ModifiedNS, int64(0))
	}
	require.True(t, names["ashley"])
	require.True(t, names["elara"])
	require.True(t, names["scrubs"])
}

func TestWalkRootSkipsEvokeHome(t *testing.T) {
	dir := t.TempDir()
	homeDir := filepath.Join(dir, ".evoke")

	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "library"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "top.evoke"), []byte("NAME\n    Top\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "library", "hidden.evoke"), []byte("NAME\n    Hidden\n"), 0o644))

	files, err := walkRoot(dir, homeDir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "top", files[0].Name)
}

func TestValidateRegistrySlug(t *testing.T) {
	tests := []struct {
		name      string
		slug      string
		wantError bool
	}{
		{name: "valid", slug: "ashley", wantError: false},
		{name: "valid with hyphen", slug: "nurse-scrubs", wantError: false},
		{name: "empty", slug: "", wantError: true},
		{name: "dotdot", slug: "..", wantError: true},
		{name: "contains dotdot", slug: "foo../bar", wantError: true},
		{name: "path separator", slug: "foo/bar", wantError: true},
		{name: "backslash", slug: "foo\\bar", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegistrySlug(tt.slug)
			require.Equal(t, tt.wantError, err != nil)
		})
	}
}

func TestLibraryPath(t *testing.T) {
	got := libraryPath("/home/user/.evoke", "jesse", "ashley")
	require.Equal(t, filepath.Join("/home/user/.evoke", "library", "jesse", "ashley.evoke"), got)
}

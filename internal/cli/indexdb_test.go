package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestIndex(t *testing.T) *sqliteIndex {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test-index.db")
	idx, err := openIndex(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func createEvokeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

func TestEnsureRoot(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "ashley.evoke", `TAGS
    character
    nurse

NAME
    Ashley

APPEARANCE
    dark hair
`)

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	// Should find by tag.
	candidates, err := idx.find(t.Context(), []sourceRoot{root}, []string{"character"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "ashley", candidates[0].Name)

	// Calling ensureRoot again should be idempotent.
	require.NoError(t, idx.ensureRoot(t.Context(), root))
}

func TestFindByTags(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "nurse.evoke", `TAGS
    character
    nurse
    medical

NAME
    Gwen
`)
	createEvokeFile(t, dir, "doctor.evoke", `TAGS
    character
    doctor
    medical

NAME
    House
`)

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	tests := []struct {
		name      string
		tags      []string
		wantCount int
	}{
		{
			name:      "single tag matches both",
			tags:      []string{"medical"},
			wantCount: 2,
		},
		{
			name:      "specific tag matches one",
			tags:      []string{"nurse"},
			wantCount: 1,
		},
		{
			name:      "multiple tags intersect",
			tags:      []string{"character", "nurse"},
			wantCount: 1,
		},
		{
			name:      "no match",
			tags:      []string{"fantasy"},
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := idx.find(t.Context(), []sourceRoot{root}, tt.tags)
			require.NoError(t, err)
			require.Len(t, candidates, tt.wantCount)
		})
	}
}

func TestFindAcrossRoots(t *testing.T) {
	idx := newTestIndex(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir1, "local-nurse.evoke", `TAGS
    nurse

APPAREL
    local scrubs
`)
	createEvokeFile(t, dir2, "global-nurse.evoke", `TAGS
    nurse

APPAREL
    global scrubs
`)

	root1 := sourceRoot{Path: dir1, Kind: sourceKindCurrent}
	root2 := sourceRoot{Path: dir2, Kind: sourceKindLibrary}
	require.NoError(t, idx.ensureRoot(t.Context(), root1))
	require.NoError(t, idx.ensureRoot(t.Context(), root2))

	// Should find candidates from all roots.
	candidates, err := idx.find(t.Context(), []sourceRoot{root1, root2}, []string{"nurse"})
	require.NoError(t, err)
	require.Len(t, candidates, 2)
}

func TestUpsertFile(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	file := indexedFile{
		RootPath:     dir,
		Path:         filepath.Join(dir, "test.evoke"),
		RelativePath: "test.evoke",
		Name:         "test",
		Size:         100,
		ModifiedNS:   1234567890,
		Tags:         []string{"test-tag"},
		Declarations: []string{"NAME"},
	}
	require.NoError(t, idx.upsertFile(t.Context(), file))

	candidates, err := idx.find(t.Context(), []sourceRoot{root}, []string{"test-tag"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	// Update the file.
	file.Tags = []string{"updated-tag"}
	file.ModifiedNS = 9999999999
	require.NoError(t, idx.upsertFile(t.Context(), file))

	// Old tag should not match.
	candidates, err = idx.find(t.Context(), []sourceRoot{root}, []string{"test-tag"})
	require.NoError(t, err)
	require.Empty(t, candidates)

	// New tag should match.
	candidates, err = idx.find(t.Context(), []sourceRoot{root}, []string{"updated-tag"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
}

func TestRemoveFile(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "remove-me.evoke", `TAGS
    removable

NAME
    Gone
`)

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	candidates, err := idx.find(t.Context(), []sourceRoot{root}, []string{"removable"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	require.NoError(t, idx.removeFile(t.Context(), candidates[0].Path))

	candidates, err = idx.find(t.Context(), []sourceRoot{root}, []string{"removable"})
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestRebuild(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "keep.evoke", `TAGS
    keeper

NAME
    Kept
`)

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	require.NoError(t, idx.rebuild(t.Context(), []sourceRoot{root}))

	candidates, err := idx.find(t.Context(), []sourceRoot{root}, []string{"keeper"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
}

func TestRootStats(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "a.evoke", "TAGS\n    alpha\n\nNAME\n    A\n")
	createEvokeFile(t, dir, "b.evoke", "TAGS\n    beta\n\nNAME\n    B\n")

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	stats, err := idx.rootStats(t.Context())
	require.NoError(t, err)
	require.Len(t, stats, 1)
	require.Equal(t, dir, stats[0].Path)
	require.Equal(t, sourceKindCurrent, stats[0].Kind)
	require.Equal(t, 2, stats[0].FileCount)
}

func TestRefreshRoot(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "existing.evoke", `TAGS
    original

NAME
    Original
`)

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	// Add a new file.
	createEvokeFile(t, dir, "added.evoke", `TAGS
    fresh

NAME
    Added
`)

	require.NoError(t, idx.refreshRoot(t.Context(), root))

	candidates, err := idx.find(t.Context(), []sourceRoot{root}, []string{"fresh"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "added", candidates[0].Name)
}

func TestParseErrorDoesNotBlockIndex(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	// Valid file.
	createEvokeFile(t, dir, "good.evoke", "TAGS\n    valid\n\nNAME\n    Good\n")
	// Invalid file — unknown declaration.
	createEvokeFile(t, dir, "bad.evoke", "UNKNOWNDECL\n    stuff\n")

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	// Good file should be findable.
	candidates, err := idx.find(t.Context(), []sourceRoot{root}, []string{"valid"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	// Bad file should be indexed but not appear in find (parse_error is set).
	stats, err := idx.rootStats(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, stats[0].FileCount)
}

func TestRemoveRoot(t *testing.T) {
	idx := newTestIndex(t)
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "file.evoke", "TAGS\n    removable\n\nNAME\n    File\n")

	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	require.NoError(t, idx.removeRoot(t.Context(), dir))

	stats, err := idx.rootStats(t.Context())
	require.NoError(t, err)
	require.Empty(t, stats)
}

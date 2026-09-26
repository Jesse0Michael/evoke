package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveTagTarget(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.evoke"), []byte("TAGS\n    shared\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "beta.evoke"), []byte("TAGS\n    shared\n"), 0o644))

	// Two different files sharing a basename in different collections under
	// the same root — the case a bare-basename key would collapse into one.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "farm"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "office"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "farm", "greenhouse.evoke"), []byte("TAGS\n    farm\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "office", "greenhouse.evoke"), []byte("TAGS\n    office\n"), 0o644))

	tests := []struct {
		name    string
		raw     string
		wantKey string
		wantErr bool
	}{
		{
			name:    "bare selector resolving to one file via its implicit basename tag",
			raw:     "alpha",
			wantKey: "alpha.evoke",
		},
		{
			name:    "local path",
			raw:     filepath.Join(dir, "beta.evoke"),
			wantKey: "beta.evoke",
		},
		{
			name:    "selector matching several files is ambiguous",
			raw:     "shared",
			wantErr: true,
		},
		{
			name:    "selector matching nothing",
			raw:     "nonexistent",
			wantErr: true,
		},
		{
			name:    "prompt literal cannot be tagged",
			raw:     "a girl in a forest",
			wantErr: true,
		},
		{
			name:    "same basename in different collections keys by root-relative path",
			raw:     filepath.Join(dir, "farm", "greenhouse.evoke"),
			wantKey: filepath.Join("farm", "greenhouse.evoke"),
		},
		{
			name:    "the other collection's copy gets a distinct key",
			raw:     filepath.Join(dir, "office", "greenhouse.evoke"),
			wantKey: filepath.Join("office", "greenhouse.evoke"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("EVOKE_HOME", t.TempDir())
			require.NoError(t, saveSettings(&Settings{Paths: []string{dir}}))

			key, err := resolveTagTarget(context.Background(), tt.raw, false)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantKey, key)
		})
	}
}

// TestResolveTagTargetAmbiguousMessage guards against the ambiguity error
// regressing to bare basenames, which would print the same name twice with
// no way to tell the matches apart.
func TestResolveTagTargetAmbiguousMessage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "farm"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "office"), 0o755))
	farmPath := filepath.Join(dir, "farm", "greenhouse.evoke")
	officePath := filepath.Join(dir, "office", "greenhouse.evoke")
	require.NoError(t, os.WriteFile(farmPath, []byte("TAGS\n    farm\n"), 0o644))
	require.NoError(t, os.WriteFile(officePath, []byte("TAGS\n    office\n"), 0o644))

	t.Setenv("EVOKE_HOME", t.TempDir())
	require.NoError(t, saveSettings(&Settings{Paths: []string{dir}}))

	_, err := resolveTagTarget(context.Background(), "greenhouse", false)
	require.Error(t, err)
	require.ErrorContains(t, err, farmPath)
	require.ErrorContains(t, err, officePath)
}

// TestTagSameBasenameDifferentCollections is the end-to-end regression for
// the bug a bare-basename key had: tagging one "greenhouse.evoke" silently
// tagged every other file sharing that name.
func TestTagSameBasenameDifferentCollections(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "farm"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "office"), 0o755))
	farmPath := filepath.Join(dir, "farm", "greenhouse.evoke")
	officePath := filepath.Join(dir, "office", "greenhouse.evoke")
	require.NoError(t, os.WriteFile(farmPath, []byte("TAGS\n    farm\n"), 0o644))
	require.NoError(t, os.WriteFile(officePath, []byte("TAGS\n    office\n"), 0o644))

	t.Setenv("EVOKE_HOME", t.TempDir())
	require.NoError(t, saveSettings(&Settings{Paths: []string{dir}}))

	require.Equal(t, 0, Tag([]string{"add", farmPath, "favorite"}, false))
	require.Equal(t, 0, Tag([]string{"add", officePath, "office-only"}, false))

	got, err := tags()
	require.NoError(t, err)
	require.Equal(t, []string{"favorite"}, got.Files[filepath.Join("farm", "greenhouse.evoke")])
	require.Equal(t, []string{"office-only"}, got.Files[filepath.Join("office", "greenhouse.evoke")])
}

func TestTagAddRemoveList(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aela.evoke"), []byte("NAME\n    Aela\n"), 0o644))

	t.Setenv("EVOKE_HOME", t.TempDir())
	require.NoError(t, saveSettings(&Settings{Paths: []string{dir}}))

	require.Equal(t, 0, Tag([]string{"add", "aela", "favorite"}, false))
	got, err := tags()
	require.NoError(t, err)
	require.Equal(t, []string{"favorite"}, got.Files["aela.evoke"])

	// Adding again, plus a second tag, dedups and sorts.
	require.Equal(t, 0, Tag([]string{"add", "aela", "favorite", "nsfw"}, false))
	got, err = tags()
	require.NoError(t, err)
	require.Equal(t, []string{"favorite", "nsfw"}, got.Files["aela.evoke"])

	require.Equal(t, 0, Tag([]string{"remove", "aela", "favorite"}, false))
	got, err = tags()
	require.NoError(t, err)
	require.Equal(t, []string{"nsfw"}, got.Files["aela.evoke"])

	// Removing the last tag drops the entry entirely rather than leaving an
	// empty list around.
	require.Equal(t, 0, Tag([]string{"remove", "aela", "nsfw"}, false))
	got, err = tags()
	require.NoError(t, err)
	require.NotContains(t, got.Files, "aela.evoke")
}

func TestTagInvalidArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no subcommand", args: nil},
		{name: "unknown subcommand", args: []string{"frobnicate"}},
		{name: "add missing tag", args: []string{"add", "aela"}},
		{name: "remove missing tag", args: []string{"remove", "aela"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("EVOKE_HOME", t.TempDir())
			require.Equal(t, 2, Tag(tt.args, false))
		})
	}
}

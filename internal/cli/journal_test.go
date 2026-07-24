package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJournal_RecordAndRecent(t *testing.T) {
	dir := t.TempDir()
	j, err := openJournal(filepath.Join(dir, "journal.db"))
	require.NoError(t, err)
	defer func() { _ = j.Close() }()

	ctx := t.Context()

	// Empty journal returns nothing.
	entries, err := j.Recent(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, entries)

	// Record some entries.
	require.NoError(t, j.Record(ctx, "test-prompt-1", "comfyui", "sumi.evoke character"))
	require.NoError(t, j.Record(ctx, "test-prompt-2", "comfyui", "pine-forest.evoke"))
	require.NoError(t, j.Record(ctx, "test-prompt-3", "comfyui", "watercolor-card.evoke"))

	// Recent returns in reverse chronological order.
	entries, err = j.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	require.Equal(t, "test-prompt-3", entries[0].PromptID)
	require.Equal(t, "test-prompt-2", entries[1].PromptID)
	require.Equal(t, "test-prompt-1", entries[2].PromptID)
	require.Equal(t, "comfyui", entries[0].Backend)
	require.Equal(t, "watercolor-card.evoke", entries[0].Inputs)

	// Limit works.
	entries, err = j.Recent(ctx, 2)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "test-prompt-3", entries[0].PromptID)
	require.Equal(t, "test-prompt-2", entries[1].PromptID)
}

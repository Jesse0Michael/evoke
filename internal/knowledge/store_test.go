package knowledge

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// readChunk mirrors a row of the chunks table as a consumer sees it, with the
// embedding already decoded from its BLOB.
type readChunk struct {
	File      string
	Heading   string
	Content   string
	Embedding []float32
}

// TestStoreInsert builds a tiny database, inserts chunks with their vectors, and
// reads every row back with an independent little-endian decoder — the same
// decoding a consumer performs. This guards the byte layout of the embedding
// BLOB, which is the database's public contract.
func TestStoreInsert(t *testing.T) {
	dim := 4
	path := filepath.Join(t.TempDir(), "test.db")

	store, err := CreateStore(path, Meta{EmbedModel: "test-model", Dims: dim, Source: "/corpus"})
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	seed := []readChunk{
		{File: "a.md", Heading: "Fire", Content: "flames", Embedding: []float32{1, 0, 0, 0}},
		{File: "b.md", Heading: "Water", Content: "waves", Embedding: []float32{0, 1, 0, 0}},
		{File: "c.md", Heading: "Earth", Content: "stone", Embedding: []float32{0, 0, 1, -0.5}},
	}
	for i, s := range seed {
		chunk := Chunk{File: s.File, Heading: s.Heading, Content: s.Content}
		require.NoError(t, store.Insert(i+1, chunk, s.Embedding))
	}

	rows, err := store.db.Query("SELECT file, heading, content, embedding FROM chunks ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var got []readChunk
	for rows.Next() {
		var c readChunk
		var blob []byte
		require.NoError(t, rows.Scan(&c.File, &c.Heading, &c.Content, &blob))
		require.Len(t, blob, dim*4, "embedding blob for %q is the wrong size", c.Heading)
		c.Embedding = make([]float32, len(blob)/4)
		require.NoError(t, binary.Read(bytes.NewReader(blob), binary.LittleEndian, &c.Embedding))
		got = append(got, c)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, seed, got)
}

// TestStoreInsertDimensionMismatch ensures a wrong-sized vector is rejected at
// write time rather than producing a database consumers cannot decode uniformly.
func TestStoreInsertDimensionMismatch(t *testing.T) {
	tests := []struct {
		name      string
		vec       []float32
		wantError bool
	}{
		{name: "correct dimensions", vec: []float32{1, 2, 3, 4}, wantError: false},
		{name: "too few dimensions", vec: []float32{1, 2, 3}, wantError: true},
		{name: "too many dimensions", vec: []float32{1, 2, 3, 4, 5}, wantError: true},
		{name: "empty vector", vec: []float32{}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.db")
			store, err := CreateStore(path, Meta{EmbedModel: "test-model", Dims: 4})
			require.NoError(t, err)
			defer func() { _ = store.Close() }()

			err = store.Insert(1, Chunk{File: "a.md", Heading: "Fire", Content: "flames"}, tt.vec)

			require.Equal(t, tt.wantError, err != nil)
		})
	}
}

func TestCreateStore_InvalidDims(t *testing.T) {
	_, err := CreateStore(filepath.Join(t.TempDir(), "test.db"), Meta{EmbedModel: "test-model", Dims: 0})
	require.Error(t, err)
}

// TestReadMeta covers both the recorded-metadata case and a database written
// before metadata existed, which must still load.
func TestReadMeta(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, path string)
		expected Meta
	}{
		{
			name: "metadata written by CreateStore round trips",
			setup: func(t *testing.T, path string) {
				t.Helper()
				store, err := CreateStore(path, Meta{EmbedModel: "test-model", Dims: 4, Source: "/corpus"})
				require.NoError(t, err)
				require.NoError(t, store.Close())
			},
			expected: Meta{EmbedModel: "test-model", Dims: 4, Source: "/corpus"},
		},
		{
			name: "a database with no meta table reports an empty Meta",
			setup: func(t *testing.T, path string) {
				t.Helper()
				writeLegacyDB(t, path, nil)
			},
			expected: Meta{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.db")
			tt.setup(t, path)

			db, err := sql.Open("sqlite3", path)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			meta, err := readMeta(db)

			require.NoError(t, err)
			require.Equal(t, tt.expected, meta)
		})
	}
}

// writeLegacyDB creates a chunks-only database, the shape produced before the
// meta table existed. Such databases must keep loading.
func writeLegacyDB(t *testing.T, path string, chunks []readChunk) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`CREATE TABLE chunks (
		id INTEGER PRIMARY KEY,
		file TEXT NOT NULL,
		heading TEXT NOT NULL,
		content TEXT NOT NULL,
		embedding BLOB NOT NULL
	)`)
	require.NoError(t, err)

	for i, c := range chunks {
		blob, err := serializeFloat32(c.Embedding)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO chunks (id, file, heading, content, embedding) VALUES (?, ?, ?, ?, ?)`,
			i+1, c.File, c.Heading, c.Content, blob)
		require.NoError(t, err)
	}
}

package knowledge

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"fmt"
	"strconv"

	_ "github.com/mattn/go-sqlite3" // sqlite driver
)

// Meta records how a knowledge database was built. The reader checks it so a
// database embedded with one model is never queried with vectors from another —
// a mismatch is not an error at the SQL layer, it just silently returns
// meaningless similarity scores.
type Meta struct {
	EmbedModel string // the embedding model that produced every vector
	Dims       int    // vector dimensionality
	Source     string // the corpus root the database was built from
}

const (
	metaKeyModel  = "embed_model"
	metaKeyDims   = "embed_dims"
	metaKeySource = "source"
)

// Store writes the SQLite database that holds chunk text and their vectors.
type Store struct {
	db  *sql.DB
	dim int
}

// CreateStore creates a knowledge database at path, replacing any schema
// already there. A vector database is a rebuildable artifact, so a build always
// starts from an empty schema rather than appending to stale rows.
func CreateStore(path string, meta Meta) (*Store, error) {
	if meta.Dims <= 0 {
		return nil, fmt.Errorf("embedding dimensionality must be positive, got %d", meta.Dims)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	s := &Store{db: db, dim: meta.Dims}
	if err := s.initSchema(meta); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) initSchema(meta Meta) error {
	stmts := []string{
		`DROP TABLE IF EXISTS chunks`,
		`DROP TABLE IF EXISTS meta`,
		`CREATE TABLE chunks (
			id INTEGER PRIMARY KEY,
			file TEXT NOT NULL,
			heading TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding BLOB NOT NULL
		)`,
		`CREATE TABLE meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to initialize schema: %w", err)
		}
	}

	rows := [][2]string{
		{metaKeyModel, meta.EmbedModel},
		{metaKeyDims, strconv.Itoa(meta.Dims)},
		{metaKeySource, meta.Source},
	}
	for _, r := range rows {
		if _, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)`, r[0], r[1]); err != nil {
			return fmt.Errorf("failed to write metadata: %w", err)
		}
	}
	return nil
}

// Insert stores a chunk and its embedding under the given id.
func (s *Store) Insert(id int, c Chunk, vec []float32) error {
	if len(vec) != s.dim {
		return fmt.Errorf("embedding has %d dimensions, expected %d", len(vec), s.dim)
	}
	serialized, err := serializeFloat32(vec)
	if err != nil {
		return fmt.Errorf("failed to serialize embedding: %w", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO chunks (id, file, heading, content, embedding) VALUES (?, ?, ?, ?, ?)`,
		id, c.File, c.Heading, c.Content, serialized,
	); err != nil {
		return fmt.Errorf("failed to insert chunk: %w", err)
	}
	return nil
}

// serializeFloat32 packs a vector as little-endian float32 bytes. This byte
// layout is the database's public contract: consumers read the BLOB and decode
// it directly, so no vector extension is needed to query the knowledge base.
func serializeFloat32(vec []float32) ([]byte, error) {
	buf := new(bytes.Buffer)
	buf.Grow(len(vec) * 4)
	if err := binary.Write(buf, binary.LittleEndian, vec); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// readMeta loads the meta table. It returns a zero Meta and no error when the
// table is absent: databases built before metadata was recorded are still valid
// and simply skip the model check.
func readMeta(db *sql.DB) (Meta, error) {
	var exists int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='meta'`).Scan(&exists)
	if err != nil {
		return Meta{}, fmt.Errorf("failed to inspect database schema: %w", err)
	}
	if exists == 0 {
		return Meta{}, nil
	}

	rows, err := db.Query(`SELECT key, value FROM meta`)
	if err != nil {
		return Meta{}, fmt.Errorf("failed to read metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var meta Meta
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Meta{}, fmt.Errorf("failed to scan metadata row: %w", err)
		}
		switch key {
		case metaKeyModel:
			meta.EmbedModel = value
		case metaKeyDims:
			meta.Dims, _ = strconv.Atoi(value)
		case metaKeySource:
			meta.Source = value
		}
	}
	if err := rows.Err(); err != nil {
		return Meta{}, fmt.Errorf("failed to iterate metadata: %w", err)
	}
	return meta, nil
}

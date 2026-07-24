package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3" // SQLite driver

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

const indexSchema = `
CREATE TABLE IF NOT EXISTS source_roots (
    id   INTEGER PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    id            INTEGER PRIMARY KEY,
    root_id       INTEGER NOT NULL,
    path          TEXT NOT NULL UNIQUE,
    relative_path TEXT NOT NULL,
    name          TEXT NOT NULL,
    size          INTEGER NOT NULL,
    modified_ns   INTEGER NOT NULL,
    parse_error   TEXT,
    FOREIGN KEY (root_id) REFERENCES source_roots(id)
);

CREATE TABLE IF NOT EXISTS tags (
    file_id INTEGER NOT NULL,
    tag     TEXT NOT NULL,
    PRIMARY KEY (file_id, tag),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS declarations (
    file_id     INTEGER NOT NULL,
    declaration TEXT NOT NULL,
    PRIMARY KEY (file_id, declaration),
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS tags_by_tag ON tags(tag);
CREATE INDEX IF NOT EXISTS declarations_by_name ON declarations(declaration);
CREATE INDEX IF NOT EXISTS files_by_root_and_name ON files(root_id, name);
`

// indexedFile holds metadata about a .evoke file for indexing.
type indexedFile struct {
	RootPath     string
	Path         string
	RelativePath string
	Name         string
	Size         int64
	ModifiedNS   int64
	Tags         []string
	Declarations []string
	ParseError   string
}

// indexCandidate is a file returned from an index query.
type indexCandidate struct {
	Path         string
	RelativePath string
	Name         string
	RootPath     string
}

// indexRootStat reports indexing stats for a source root.
type indexRootStat struct {
	Path       string
	Kind       sourceKind
	FileCount  int
	ErrorCount int
}

// sqliteIndex implements the local file index backed by an embedded SQLite database.
type sqliteIndex struct {
	db *sql.DB
}

// openIndex opens or creates the SQLite index database at the given path.
func openIndex(path string) (*sqliteIndex, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("failed to open index: %w", err)
	}
	if _, err := db.Exec(indexSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize index schema: %w", err)
	}
	return &sqliteIndex{db: db}, nil
}

// openDefaultIndex opens the index at the default Evoke home location.
func openDefaultIndex() (*sqliteIndex, error) {
	homeDir, err := home()
	if err != nil {
		return nil, err
	}
	return openIndex(homeDir + "/index.db")
}

func (idx *sqliteIndex) Close() error {
	return idx.db.Close()
}

func (idx *sqliteIndex) ensureRoot(ctx context.Context, root sourceRoot) error {
	var exists bool
	err := idx.db.QueryRowContext(ctx, "SELECT 1 FROM source_roots WHERE path = ?", root.Path).Scan(&exists)
	if err == nil {
		return nil // already indexed
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check root: %w", err)
	}

	// Root not indexed — add it and scan.
	if _, err := idx.db.ExecContext(ctx, "INSERT INTO source_roots (path, kind) VALUES (?, ?)", root.Path, string(root.Kind)); err != nil {
		return fmt.Errorf("failed to insert root: %w", err)
	}

	return idx.scanRoot(ctx, root)
}

func (idx *sqliteIndex) refreshRoot(ctx context.Context, root sourceRoot) error {
	// Ensure the root exists in the DB.
	var rootID int64
	err := idx.db.QueryRowContext(ctx, "SELECT id FROM source_roots WHERE path = ?", root.Path).Scan(&rootID)
	if err == sql.ErrNoRows {
		return idx.ensureRoot(ctx, root)
	}
	if err != nil {
		return fmt.Errorf("failed to query root: %w", err)
	}

	homeDir, _ := home()
	discovered, err := walkRoot(root.Path, homeDir)
	if err != nil {
		return fmt.Errorf("failed to walk root %q: %w", root.Path, err)
	}

	// Build a set of discovered paths.
	discoveredPaths := make(map[string]discoveredFile, len(discovered))
	for _, f := range discovered {
		discoveredPaths[f.Path] = f
	}

	// Get existing indexed files for this root.
	rows, err := idx.db.QueryContext(ctx, "SELECT id, path, size, modified_ns FROM files WHERE root_id = ?", rootID)
	if err != nil {
		return fmt.Errorf("failed to query files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type existingFile struct {
		id         int64
		path       string
		size       int64
		modifiedNS int64
	}
	var existing []existingFile
	for rows.Next() {
		var f existingFile
		if err := rows.Scan(&f.id, &f.path, &f.size, &f.modifiedNS); err != nil {
			return fmt.Errorf("failed to scan file: %w", err)
		}
		existing = append(existing, f)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate files: %w", err)
	}

	existingPaths := make(map[string]existingFile, len(existing))
	for _, f := range existing {
		existingPaths[f.path] = f
	}

	// Remove files that no longer exist.
	for _, f := range existing {
		if _, ok := discoveredPaths[f.path]; !ok {
			if err := idx.removeFileByID(ctx, f.id); err != nil {
				return err
			}
		}
	}

	// Add or update files.
	for _, df := range discovered {
		ef, exists := existingPaths[df.Path]
		if exists && ef.size == df.Size && ef.modifiedNS == df.ModifiedNS {
			continue // unchanged
		}
		indexed := parseAndIndex(root.Path, df)
		if err := idx.upsertFile(ctx, indexed); err != nil {
			return err
		}
	}

	return nil
}

func (idx *sqliteIndex) find(ctx context.Context, roots []sourceRoot, tags []string) ([]indexCandidate, error) {
	var all []indexCandidate
	for _, root := range roots {
		candidates, err := idx.findInRoot(ctx, root.Path, tags)
		if err != nil {
			return nil, err
		}
		all = append(all, candidates...)
	}
	return all, nil
}

func (idx *sqliteIndex) findInRoot(ctx context.Context, rootPath string, tags []string) ([]indexCandidate, error) {
	var rootID int64
	err := idx.db.QueryRowContext(ctx, "SELECT id FROM source_roots WHERE path = ?", rootPath).Scan(&rootID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query root: %w", err)
	}

	// Build the query dynamically.
	query := "SELECT f.path, f.relative_path, f.name FROM files f WHERE f.root_id = ? AND f.parse_error IS NULL"
	args := []any{rootID}

	// Tag filters: file must have ALL requested tags.
	for _, tag := range tags {
		query += " AND f.id IN (SELECT file_id FROM tags WHERE tag = ?)"
		args = append(args, strings.ToLower(tag))
	}

	rows, err := idx.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []indexCandidate
	for rows.Next() {
		var c indexCandidate
		if err := rows.Scan(&c.Path, &c.RelativePath, &c.Name); err != nil {
			return nil, fmt.Errorf("failed to scan candidate: %w", err)
		}
		c.RootPath = rootPath
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

func (idx *sqliteIndex) upsertFile(ctx context.Context, file indexedFile) error {
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Ensure the root exists.
	var rootID int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM source_roots WHERE path = ?", file.RootPath).Scan(&rootID)
	if err != nil {
		return fmt.Errorf("failed to find root for file: %w", err)
	}

	// Delete existing entry if present.
	var existingID int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM files WHERE path = ?", file.Path).Scan(&existingID)
	if err == nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM tags WHERE file_id = ?", existingID); err != nil {
			return fmt.Errorf("failed to clear tags: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM declarations WHERE file_id = ?", existingID); err != nil {
			return fmt.Errorf("failed to clear declarations: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE id = ?", existingID); err != nil {
			return fmt.Errorf("failed to delete old file: %w", err)
		}
	}

	// Insert the file.
	var parseErr *string
	if file.ParseError != "" {
		parseErr = &file.ParseError
	}
	result, err := tx.ExecContext(ctx,
		"INSERT INTO files (root_id, path, relative_path, name, size, modified_ns, parse_error) VALUES (?, ?, ?, ?, ?, ?, ?)",
		rootID, file.Path, file.RelativePath, file.Name, file.Size, file.ModifiedNS, parseErr,
	)
	if err != nil {
		return fmt.Errorf("failed to insert file: %w", err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get file id: %w", err)
	}

	// Insert tags.
	for _, tag := range file.Tags {
		if _, err := tx.ExecContext(ctx, "INSERT INTO tags (file_id, tag) VALUES (?, ?)", fileID, strings.ToLower(tag)); err != nil {
			return fmt.Errorf("failed to insert tag: %w", err)
		}
	}

	// Insert declarations.
	for _, decl := range file.Declarations {
		if _, err := tx.ExecContext(ctx, "INSERT INTO declarations (file_id, declaration) VALUES (?, ?)", fileID, strings.ToUpper(decl)); err != nil {
			return fmt.Errorf("failed to insert declaration: %w", err)
		}
	}

	return tx.Commit()
}

func (idx *sqliteIndex) removeFile(ctx context.Context, path string) error {
	var fileID int64
	err := idx.db.QueryRowContext(ctx, "SELECT id FROM files WHERE path = ?", path).Scan(&fileID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to find file: %w", err)
	}
	return idx.removeFileByID(ctx, fileID)
}

func (idx *sqliteIndex) removeFileByID(ctx context.Context, id int64) error {
	if _, err := idx.db.ExecContext(ctx, "DELETE FROM tags WHERE file_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete tags: %w", err)
	}
	if _, err := idx.db.ExecContext(ctx, "DELETE FROM declarations WHERE file_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete declarations: %w", err)
	}
	if _, err := idx.db.ExecContext(ctx, "DELETE FROM files WHERE id = ?", id); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

func (idx *sqliteIndex) removeRoot(ctx context.Context, rootPath string) error {
	var rootID int64
	err := idx.db.QueryRowContext(ctx, "SELECT id FROM source_roots WHERE path = ?", rootPath).Scan(&rootID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to find root: %w", err)
	}

	// Delete all files (and their tags/declarations via cascade or manual).
	rows, err := idx.db.QueryContext(ctx, "SELECT id FROM files WHERE root_id = ?", rootID)
	if err != nil {
		return fmt.Errorf("failed to query files: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var fileID int64
		if err := rows.Scan(&fileID); err != nil {
			return fmt.Errorf("failed to scan file id: %w", err)
		}
		if err := idx.removeFileByID(ctx, fileID); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate files: %w", err)
	}

	if _, err := idx.db.ExecContext(ctx, "DELETE FROM source_roots WHERE id = ?", rootID); err != nil {
		return fmt.Errorf("failed to delete root: %w", err)
	}
	return nil
}

func (idx *sqliteIndex) rebuild(ctx context.Context, roots []sourceRoot) error {
	// Drop all data.
	for _, table := range []string{"tags", "declarations", "files", "source_roots"} {
		if _, err := idx.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("failed to clear %s: %w", table, err)
		}
	}

	// Re-index all roots.
	for _, root := range roots {
		if err := idx.ensureRoot(ctx, root); err != nil {
			return err
		}
	}
	return nil
}

func (idx *sqliteIndex) rootStats(ctx context.Context) ([]indexRootStat, error) {
	rows, err := idx.db.QueryContext(ctx,
		`SELECT sr.path, sr.kind, COUNT(f.id), COUNT(f.parse_error)
		 FROM source_roots sr
		 LEFT JOIN files f ON f.root_id = sr.id
		 GROUP BY sr.id
		 ORDER BY sr.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var stats []indexRootStat
	for rows.Next() {
		var s indexRootStat
		var kind string
		if err := rows.Scan(&s.Path, &kind, &s.FileCount, &s.ErrorCount); err != nil {
			return nil, fmt.Errorf("failed to scan stat: %w", err)
		}
		s.Kind = sourceKind(kind)
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// scanRoot walks the root directory and indexes all discovered .evoke files.
func (idx *sqliteIndex) scanRoot(ctx context.Context, root sourceRoot) error {
	homeDir, _ := home()
	discovered, err := walkRoot(root.Path, homeDir)
	if err != nil {
		return fmt.Errorf("failed to walk root %q: %w", root.Path, err)
	}

	for _, df := range discovered {
		indexed := parseAndIndex(root.Path, df)
		if err := idx.upsertFile(ctx, indexed); err != nil {
			return err
		}
	}
	return nil
}

// parseAndIndex parses a discovered file and extracts index metadata.
func parseAndIndex(rootPath string, df discoveredFile) indexedFile {
	indexed := indexedFile{
		RootPath:     rootPath,
		Path:         df.Path,
		RelativePath: df.RelativePath,
		Name:         df.Name,
		Size:         df.Size,
		ModifiedNS:   df.ModifiedNS,
	}

	data, err := os.ReadFile(df.Path) //nolint:gosec // path is from controlled source root walking
	if err != nil {
		indexed.ParseError = err.Error()
		return indexed
	}

	doc, err := evoke.Parse(data)
	if err != nil {
		indexed.ParseError = err.Error()
		return indexed
	}

	if err := evoke.Validate(doc); err != nil {
		indexed.ParseError = err.Error()
		return indexed
	}

	indexed.Tags = doc.Metadata.Tags

	// Add filename (without extension) as an implicit tag.
	baseName := strings.ToLower(strings.TrimSuffix(df.Name, ".evoke"))
	if baseName != "" && !containsTag(indexed.Tags, baseName) {
		indexed.Tags = append(indexed.Tags, baseName)
	}

	// Collect unique declaration names (TAGS is metadata, not a declaration).
	seen := make(map[string]bool)
	for _, decl := range doc.Declarations {
		if !seen[decl.Name] {
			seen[decl.Name] = true
			indexed.Declarations = append(indexed.Declarations, decl.Name)
		}
	}

	return indexed
}

// allTags returns distinct tags from the index matching the given prefix.
func (idx *sqliteIndex) allTags(ctx context.Context, prefix string) ([]string, error) {
	query := "SELECT DISTINCT tag FROM tags WHERE tag LIKE ? ORDER BY tag"
	rows, err := idx.db.QueryContext(ctx, query, strings.ToLower(prefix)+"%")
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// tagsForFile returns all tags for the file at the given path.
func (idx *sqliteIndex) tagsForFile(ctx context.Context, path string) ([]string, error) {
	query := `SELECT t.tag FROM tags t JOIN files f ON t.file_id = f.id WHERE f.path = ?`
	rows, err := idx.db.QueryContext(ctx, query, path)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags for file: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// allFileNames returns distinct file names from the index matching the given prefix.
func (idx *sqliteIndex) allFileNames(ctx context.Context, prefix string) ([]string, error) {
	query := "SELECT DISTINCT name FROM files WHERE name LIKE ? AND parse_error IS NULL ORDER BY name"
	rows, err := idx.db.QueryContext(ctx, query, strings.ToLower(prefix)+"%")
	if err != nil {
		return nil, fmt.Errorf("failed to query file names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan file name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// findByName returns the absolute path of the first indexed file with the given name.
func (idx *sqliteIndex) findByName(ctx context.Context, name string) (string, error) {
	var path string
	err := idx.db.QueryRowContext(ctx,
		"SELECT path FROM files WHERE name = ? AND parse_error IS NULL LIMIT 1",
		name,
	).Scan(&path)
	if err != nil {
		return "", fmt.Errorf("no indexed file named %q", name)
	}
	return path, nil
}

package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver

	"github.com/jesse0michael/evoke/internal/generate/comfyui"
)

// maxSourceScan bounds how many output images a single call to
// buildSourceIndex will read metadata from. It runs in the background (see
// runDiscover), so this is not a wait the user sits through — it exists only
// so a single background pass can't run indefinitely against a truly
// enormous backlog. A fresh index facing anything under this many images
// (almost every real output directory) finishes catching up in one pass.
const maxSourceScan = 20000

// sourceScanCommitInterval commits the scan's inserts periodically during a
// long catch-up pass, so quitting mid-scan (or a crash) loses at most this
// many images' worth of work instead of the entire pass.
const sourceScanCommitInterval = 200

// sourceIndexSchema records every output image's declared sources, keyed so
// "the examples for this source, newest first" is one indexed query. Unlike a
// cache of only the most recent image, this keeps the full history: that's
// the whole point of the sqlite index over the JSON cache it replaced —
// `evoke discover` can page back through every past example of a file, not
// just the latest one.
const sourceIndexSchema = `
CREATE TABLE IF NOT EXISTS source_images (
	source_key  TEXT NOT NULL,
	image_path  TEXT NOT NULL,
	mod_time_ns INTEGER NOT NULL,
	PRIMARY KEY (source_key, image_path)
);
CREATE INDEX IF NOT EXISTS source_images_key_time ON source_images(source_key, mod_time_ns DESC);

CREATE TABLE IF NOT EXISTS scan_state (
	id                 INTEGER PRIMARY KEY CHECK (id = 1),
	scanned_through_ns INTEGER NOT NULL
);
`

// sourceIndex is every output image's recorded sources, matched by a source's
// embedded identity (see sanitizeSources) and queried newest first. The zero
// value (nil db) answers every query empty, so a browser opened before the
// background scan finishes — or with nothing to scan at all — needs no nil
// check at the call site.
type sourceIndex struct {
	db *sql.DB
}

// sourceDBPath is where the index persists — a disposable, rebuildable cache
// like index.db, not user-authored data, so it lives as a plain file rather
// than needing any migration story: deleting it just costs one full rescan.
func sourceDBPath() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "source-images.db"), nil
}

// openSourceIndex opens the on-disk index, creating its schema if this is the
// first run.
func openSourceIndex() (sourceIndex, error) {
	path, err := sourceDBPath()
	if err != nil {
		return sourceIndex{}, err
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return sourceIndex{}, fmt.Errorf("failed to open source index: %w", err)
	}
	if _, err := db.Exec(sourceIndexSchema); err != nil {
		_ = db.Close()
		return sourceIndex{}, fmt.Errorf("failed to prepare source index schema: %w", err)
	}
	return sourceIndex{db: db}, nil
}

// close releases the underlying database handle. Safe to call on a zero-value
// sourceIndex.
func (idx sourceIndex) close() {
	if idx.db != nil {
		_ = idx.db.Close()
	}
}

// scannedThrough reports the mod time of the newest image the index has
// already read metadata from, or the zero time for a fresh index.
func (idx sourceIndex) scannedThrough() time.Time {
	if idx.db == nil {
		return time.Time{}
	}
	var ns int64
	if err := idx.db.QueryRow(`SELECT scanned_through_ns FROM scan_state WHERE id = 1`).Scan(&ns); err != nil {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// imagesFor returns every output image whose metadata named path as a source,
// newest first, matching first by the path's root-relative identity and
// falling back to its basename (for a generation that ran outside every
// configured root, so the source was embedded by basename alone). A row whose
// image has since been deleted is dropped rather than returned, so a later
// call doesn't keep offering a path that no longer resolves to anything.
func (idx sourceIndex) imagesFor(path string, roots []sourceRoot) []string {
	if idx.db == nil {
		return nil
	}
	if images := idx.queryImages(rootRelativeKey(path, roots)); len(images) > 0 {
		return images
	}
	return idx.queryImages("base:" + filepath.Base(path))
}

// queryImages returns every image recorded under key, newest first.
func (idx sourceIndex) queryImages(key string) []string {
	rows, err := idx.db.Query(`SELECT image_path FROM source_images WHERE source_key = ? ORDER BY mod_time_ns DESC`, key)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var images, stale []string
	for rows.Next() {
		var image string
		if err := rows.Scan(&image); err != nil {
			continue
		}
		if _, err := os.Stat(image); err != nil {
			stale = append(stale, image)
			continue
		}
		images = append(images, image)
	}

	for _, image := range stale {
		_, _ = idx.db.Exec(`DELETE FROM source_images WHERE source_key = ? AND image_path = ?`, key, image)
	}
	return images
}

// buildSourceIndex brings the on-disk index up to date and returns it. Only
// output images newer than the last scan are read at all, so a session
// opened after an earlier one only has to account for what's new.
func buildSourceIndex(outputDir string) sourceIndex {
	idx, err := openSourceIndex()
	if err != nil {
		return sourceIndex{}
	}

	images, err := loadImages(outputDir)
	if err != nil {
		return idx
	}

	scannedThrough := idx.scannedThrough()
	type pendingImage struct {
		path    string
		modTime time.Time
	}
	var pending []pendingImage
	for _, img := range images {
		info, err := os.Stat(img.path)
		if err != nil {
			continue
		}
		if !info.ModTime().After(scannedThrough) {
			// loadImages is newest first: everything from here on was
			// already accounted for by an earlier scan.
			break
		}
		pending = append(pending, pendingImage{img.path, info.ModTime()})
	}
	if len(pending) == 0 {
		return idx
	}

	// Oldest-pending-first, capped: a run that can't clear the whole backlog
	// still advances scannedThrough forward, so the next run picks up past
	// what this one finished instead of redoing the same newest images
	// without ever reaching the rest.
	var tx *sql.Tx
	through := scannedThrough
	processed := 0
	for i := len(pending) - 1; i >= 0 && processed < maxSourceScan; i-- {
		p := pending[i]
		if tx == nil {
			if tx, err = idx.db.Begin(); err != nil {
				return idx
			}
		}

		if meta, err := comfyui.ReadPNG(p.path); err == nil {
			for _, src := range meta.Sources {
				insertSourceImage(tx, src, p.path, p.modTime)
				if base := filepath.Base(src); base != src {
					insertSourceImage(tx, "base:"+base, p.path, p.modTime)
				}
			}
		}

		through = p.modTime
		processed++
		if processed%sourceScanCommitInterval == 0 {
			commitScanState(tx, through)
			tx = nil
		}
	}
	if tx != nil {
		commitScanState(tx, through)
	}

	return idx
}

// insertSourceImage records image as a source named by key. The (key, image)
// pair is a stable identity once scanned — an image's own file never changes
// which sources it named — so a conflict only means an earlier, interrupted
// run already wrote this row, and it's left as is.
func insertSourceImage(tx *sql.Tx, key, image string, modTime time.Time) {
	_, _ = tx.Exec(`INSERT OR IGNORE INTO source_images (source_key, image_path, mod_time_ns) VALUES (?, ?, ?)`,
		key, image, modTime.UnixNano())
}

// commitScanState records how far the scan has gotten and commits the batch
// of inserts alongside it in the same transaction, so a crash can't leave
// scanned_through_ns ahead of the rows it claims to have written.
func commitScanState(tx *sql.Tx, through time.Time) {
	_, _ = tx.Exec(`INSERT INTO scan_state (id, scanned_through_ns) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET scanned_through_ns = excluded.scanned_through_ns`, through.UnixNano())
	_ = tx.Commit()
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/jesse0michael/evoke/internal/generate/comfyui"
)

// maxSourceScan bounds how many output images a single call to
// buildSourceIndex will read metadata from. It runs in the background (see
// runDiscover), so this is not a wait the user sits through — it exists only
// so a single background pass can't run indefinitely against a truly
// enormous backlog. A fresh cache facing anything under this many images
// (almost every real output directory) finishes catching up in one pass.
const maxSourceScan = 20000

// sourceCacheSaveInterval flushes progress to disk periodically during a long
// catch-up scan, so quitting mid-scan (or a crash) loses at most this many
// images' worth of work instead of the entire pass.
const sourceCacheSaveInterval = 200

// sourceIndex maps a source's embedded identity (see sanitizeSources) to the
// most recent output image that named it. A "base:"-prefixed key holds the
// basename-only fallback for a file whose generation ran outside every
// configured root and so was embedded by basename alone.
type sourceIndex map[string]string

// recentFor returns the newest output image whose metadata names path as a
// source, matching first by the path's root-relative identity and falling
// back to its basename.
func (idx sourceIndex) recentFor(path string, roots []sourceRoot) string {
	if idx == nil {
		return ""
	}
	if img, ok := idx[rootRelativeKey(path, roots)]; ok {
		return img
	}
	if img, ok := idx["base:"+filepath.Base(path)]; ok {
		return img
	}
	return ""
}

// sourceCacheEntry is one source's most recent image, persisted so a later
// session doesn't have to reread every output image's metadata to rebuild
// what an earlier session already worked out.
type sourceCacheEntry struct {
	Image string    `json:"image"`
	ModAt time.Time `json:"mod_at"`
}

// sourceCacheFile is the on-disk cache: every source's most recent image,
// plus how far the scan has gotten so the next run only reads what's new.
type sourceCacheFile struct {
	ScannedThrough time.Time                   `json:"scanned_through"`
	Entries        map[string]sourceCacheEntry `json:"entries"`
}

// sourceCachePath is where the cache persists — a disposable, rebuildable
// index like index.db, not user-authored data, so it lives as a plain file
// rather than needing any migration story: deleting it just costs one full
// rescan.
func sourceCachePath() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "source-cache.json"), nil
}

// loadSourceCache reads the persisted cache, or an empty one if it doesn't
// exist yet or fails to parse — a corrupt cache is rebuilt from scratch
// rather than treated as fatal, since it costs nothing but a rescan.
func loadSourceCache() *sourceCacheFile {
	cache := &sourceCacheFile{Entries: map[string]sourceCacheEntry{}}
	path, err := sourceCachePath()
	if err != nil {
		return cache
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cache
	}
	_ = json.Unmarshal(data, cache)
	if cache.Entries == nil {
		cache.Entries = map[string]sourceCacheEntry{}
	}
	return cache
}

// saveSourceCache writes the cache. Best-effort: a failed write costs the
// next session a rescan, not correctness, so it is not reported anywhere.
func saveSourceCache(cache *sourceCacheFile) error {
	path, err := sourceCachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// buildSourceIndex brings the persisted cache up to date and returns it as a
// sourceIndex. Three things keep repeated calls cheap: a cached entry whose
// image is still on disk is trusted without reopening it; an entry whose
// image has been deleted is dropped so a stale pointer can't be shown; and
// only output images newer than the last scan are read at all.
func buildSourceIndex(outputDir string) sourceIndex {
	cache := loadSourceCache()
	dirty := pruneMissingSourceCacheEntries(cache)

	images, err := loadImages(outputDir)
	if err != nil {
		if dirty {
			_ = saveSourceCache(cache)
		}
		return sourceIndexFromCache(cache)
	}

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
		if !info.ModTime().After(cache.ScannedThrough) {
			// loadImages is newest first: everything from here on was
			// already accounted for by an earlier scan.
			break
		}
		pending = append(pending, pendingImage{img.path, info.ModTime()})
	}

	// Oldest-pending-first, capped: a run that can't clear the whole backlog
	// still advances ScannedThrough forward, so the next run picks up past
	// what this one finished instead of redoing the same newest images
	// without ever reaching the rest.
	processed := 0
	for i := len(pending) - 1; i >= 0 && processed < maxSourceScan; i-- {
		p := pending[i]
		meta, err := comfyui.ReadPNG(p.path)
		if err != nil {
			continue
		}
		for _, src := range meta.Sources {
			mergeSourceCacheEntry(cache, src, p.path, p.modTime)
			if base := filepath.Base(src); base != src {
				mergeSourceCacheEntry(cache, "base:"+base, p.path, p.modTime)
			}
		}
		cache.ScannedThrough = p.modTime
		processed++
		dirty = true
		if processed%sourceCacheSaveInterval == 0 {
			_ = saveSourceCache(cache)
		}
	}

	if dirty {
		_ = saveSourceCache(cache)
	}
	return sourceIndexFromCache(cache)
}

// mergeSourceCacheEntry records image as key's source, keeping whichever of
// the new and any existing image is newer.
func mergeSourceCacheEntry(cache *sourceCacheFile, key, image string, modTime time.Time) {
	if existing, ok := cache.Entries[key]; ok && !modTime.After(existing.ModAt) {
		return
	}
	cache.Entries[key] = sourceCacheEntry{Image: image, ModAt: modTime}
}

// pruneMissingSourceCacheEntries drops every entry whose image no longer
// exists on disk, reporting whether it removed anything.
func pruneMissingSourceCacheEntries(cache *sourceCacheFile) bool {
	dirty := false
	for key, entry := range cache.Entries {
		if _, err := os.Stat(entry.Image); err != nil {
			delete(cache.Entries, key)
			dirty = true
		}
	}
	return dirty
}

func sourceIndexFromCache(cache *sourceCacheFile) sourceIndex {
	idx := make(sourceIndex, len(cache.Entries))
	for key, entry := range cache.Entries {
		idx[key] = entry.Image
	}
	return idx
}

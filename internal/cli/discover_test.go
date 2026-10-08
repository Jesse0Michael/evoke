package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// discoverPress applies a sequence of events to a discover model, laying out
// and painting after each as the input loop does, matching the viewer's
// press helper.
func discoverPress(m discoverModel, events ...event) discoverModel {
	for _, ev := range events {
		m.handle(ev)
		m.layout()
		m.paint()
	}
	return m
}

func writeEvokeFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("NAME\n    test\n"), 0o644))
}

func TestBuildDiscoverTree(t *testing.T) {
	dir := t.TempDir()
	writeEvokeFile(t, filepath.Join(dir, "alpha.evoke"))
	writeEvokeFile(t, filepath.Join(dir, "characters", "aria.evoke"))
	writeEvokeFile(t, filepath.Join(dir, "characters", "beta.evoke"))
	// A directory with no .evoke files anywhere beneath it must not appear.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "empty"), 0o755))

	roots := []sourceRoot{{Path: dir, Kind: sourceKindCurrent}}
	tree := buildDiscoverTree(roots)

	// Directories sort before files, both alphabetically, and nothing wraps
	// the root in a node of its own — the top level is the root's own
	// contents.
	require.Len(t, tree.children, 2)
	require.Equal(t, "characters", tree.children[0].name)
	require.True(t, tree.children[0].isDir)
	require.Equal(t, "alpha.evoke", tree.children[1].name)
	require.False(t, tree.children[1].isDir)

	characters := tree.children[0]
	require.Len(t, characters.children, 2)
	require.Equal(t, "aria.evoke", characters.children[0].name)
	require.Equal(t, "beta.evoke", characters.children[1].name)
}

func TestBuildDiscoverTreeEmptyRoot(t *testing.T) {
	dir := t.TempDir()
	roots := []sourceRoot{{Path: dir, Kind: sourceKindCurrent}}
	tree := buildDiscoverTree(roots)
	require.Empty(t, tree.children)
}

// TestBuildDiscoverTreeMergesRoots covers the actual point of browsing
// several roots: a file directly in one sits beside a file directly in
// another at the same top level, with no root-labeled node between them.
func TestBuildDiscoverTreeMergesRoots(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	writeEvokeFile(t, filepath.Join(dirA, "alpha.evoke"))
	writeEvokeFile(t, filepath.Join(dirB, "beta.evoke"))

	roots := []sourceRoot{
		{Path: dirA, Kind: sourceKindCurrent},
		{Path: dirB, Kind: sourceKindConfigured},
	}
	tree := buildDiscoverTree(roots)

	require.Len(t, tree.children, 2)
	require.Equal(t, "alpha.evoke", tree.children[0].name)
	require.Equal(t, "beta.evoke", tree.children[1].name)
}

// syntheticTree builds a small tree without touching the filesystem, for
// tests that only exercise navigation. It mirrors what buildDiscoverTree
// produces for a single root: the top level is the root's own contents, not a
// node wrapping them.
func syntheticTree() *discoverNode {
	characters := &discoverNode{name: "characters", isDir: true, children: []*discoverNode{
		{name: "aria.evoke", path: "/root/characters/aria.evoke"},
		{name: "beta.evoke", path: "/root/characters/beta.evoke"},
	}}
	return &discoverNode{isDir: true, children: []*discoverNode{
		characters,
		{name: "alpha.evoke", path: "/root/alpha.evoke"},
	}}
}

func TestDiscoverModelNavigation(t *testing.T) {
	tests := []struct {
		name      string
		keys      []string
		wantDepth int
		wantIdx   int
		wantName  string
		wantIsDir bool
	}{
		{name: "starts at the top listing", wantDepth: 1, wantIdx: 0, wantName: "characters", wantIsDir: true},
		{name: "down moves to the file", keys: []string{"down"}, wantDepth: 1, wantIdx: 1, wantName: "alpha.evoke"},
		{name: "down clamps at the end", keys: []string{"down", "down", "down"}, wantDepth: 1, wantIdx: 1, wantName: "alpha.evoke"},
		{name: "up clamps at the start", keys: []string{"up"}, wantDepth: 1, wantIdx: 0, wantName: "characters", wantIsDir: true},
		{name: "right on a file does nothing", keys: []string{"down", "right"}, wantDepth: 1, wantIdx: 1, wantName: "alpha.evoke"},
		{name: "left at the top level does nothing", keys: []string{"left"}, wantDepth: 1, wantIdx: 0, wantName: "characters", wantIsDir: true},
		{name: "right enters a directory", keys: []string{"right"}, wantDepth: 2, wantIdx: 0, wantName: "aria.evoke"},
		{
			name:      "drills into a directory and back out",
			keys:      []string{"right", "down", "left"},
			wantDepth: 1, wantIdx: 0, wantName: "characters", wantIsDir: true,
		},
		{
			name:      "navigates to a file inside a directory",
			keys:      []string{"right", "down"},
			wantDepth: 2, wantIdx: 1, wantName: "beta.evoke",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newDiscoverModel(syntheticTree(), nil, sourceIndex{})
			m = discoverPress(m, keys(tt.keys...)...)

			require.Len(t, m.stack, tt.wantDepth)
			sel := m.selected()
			require.NotNil(t, sel)
			require.Equal(t, tt.wantIdx, m.current().idx)
			require.Equal(t, tt.wantName, sel.name)
			require.Equal(t, tt.wantIsDir, sel.isDir)
			require.Equal(t, discoverBrowse, m.mode)
		})
	}
}

// TestDiscoverModelImageNavigation covers stepping through a file's past
// examples with [ and ] — independent keys from the tree cursor's arrows —
// clamped at both ends and reset the moment the selection moves to a
// different file.
func TestDiscoverModelImageNavigation(t *testing.T) {
	t.Setenv("EVOKE_HOME", t.TempDir())
	dir := t.TempDir()
	newest := writeSourceImage(t, dir, "three.png", []string{"alpha.evoke"}, time.Minute)
	middle := writeSourceImage(t, dir, "two.png", []string{"alpha.evoke"}, time.Hour)
	oldest := writeSourceImage(t, dir, "one.png", []string{"alpha.evoke"}, 2*time.Hour)
	sources := testSourceIndex(t, dir)

	m := newDiscoverModel(syntheticTree(), []sourceRoot{{Path: "/root"}}, sources)
	m = discoverPress(m, key("down")) // select "alpha.evoke"

	require.Equal(t, newest, m.currentImagePath())
	require.Contains(t, m.statusBar(), "1/3")
	require.Contains(t, m.controlsBar(), "[ ] examples")

	m = discoverPress(m, key("]"))
	require.Equal(t, middle, m.currentImagePath())
	require.Contains(t, m.statusBar(), "2/3")

	m = discoverPress(m, key("]"))
	require.Equal(t, oldest, m.currentImagePath())
	require.Contains(t, m.statusBar(), "3/3")

	m = discoverPress(m, key("]")) // clamps at the oldest example
	require.Equal(t, oldest, m.currentImagePath())

	m = discoverPress(m, key("["))
	require.Equal(t, middle, m.currentImagePath())

	// Stepping off the file and back resets to the newest example.
	m = discoverPress(m, key("up"), key("down"))
	require.Equal(t, newest, m.currentImagePath())
}

// TestDiscoverModelImageNavigationSingleExample covers the common case: one
// example offers nothing to page through, so neither the position nor the
// step hint should appear.
func TestDiscoverModelImageNavigationSingleExample(t *testing.T) {
	t.Setenv("EVOKE_HOME", t.TempDir())
	dir := t.TempDir()
	only := writeSourceImage(t, dir, "one.png", []string{"alpha.evoke"}, time.Minute)
	sources := testSourceIndex(t, dir)

	m := newDiscoverModel(syntheticTree(), []sourceRoot{{Path: "/root"}}, sources)
	m = discoverPress(m, key("down"), key("]")) // select "alpha.evoke", attempt to step

	require.Equal(t, only, m.currentImagePath())
	require.NotContains(t, m.statusBar(), "1/1")
	require.NotContains(t, m.controlsBar(), "[ ] examples")
}

func TestDiscoverModelTagPrompt(t *testing.T) {
	t.Run("t opens the prompt only on a file", func(t *testing.T) {
		m := newDiscoverModel(syntheticTree(), nil, sourceIndex{})
		m = discoverPress(m, key("t"))
		require.Equal(t, discoverBrowse, m.mode, "the root listing entry is a directory")

		m = discoverPress(m, key("down"), key("t"))
		require.Equal(t, discoverPrompt, m.mode)
	})

	t.Run("esc cancels without submitting", func(t *testing.T) {
		var called bool
		m := newDiscoverModel(syntheticTree(), nil, sourceIndex{})
		m.runTag = func(string, []string) (string, error) { called = true; return "", nil }

		m = discoverPress(m, key("down"), key("t"))
		m = discoverPress(m, key("a"), key("b"), key("esc"))

		require.Equal(t, discoverBrowse, m.mode)
		require.Empty(t, m.promptInput)
		require.False(t, called)
	})

	t.Run("enter submits the typed tags for the selected file", func(t *testing.T) {
		var gotPath string
		var gotTags []string
		m := newDiscoverModel(syntheticTree(), nil, sourceIndex{})
		m.runTag = func(path string, tags []string) (string, error) {
			gotPath, gotTags = path, tags
			return "/root/alpha.evoke => favorite", nil
		}

		m = discoverPress(m, key("down"), key("t"))
		m = discoverPress(m, keys("f", "a", "v")...)
		m = discoverPress(m, key("enter"))

		require.Equal(t, discoverBrowse, m.mode)
		require.Equal(t, "/root/alpha.evoke", gotPath)
		require.Equal(t, []string{"fav"}, gotTags)
		require.Equal(t, "/root/alpha.evoke => favorite", m.noteMsg)
	})

	t.Run("a blank line submits nothing", func(t *testing.T) {
		var called bool
		m := newDiscoverModel(syntheticTree(), nil, sourceIndex{})
		m.runTag = func(string, []string) (string, error) { called = true; return "", nil }

		m = discoverPress(m, key("down"), key("t"), key("enter"))

		require.Equal(t, discoverBrowse, m.mode)
		require.False(t, called)
	})

	t.Run("a failure is reported in the status bar", func(t *testing.T) {
		m := newDiscoverModel(syntheticTree(), nil, sourceIndex{})
		m.runTag = func(string, []string) (string, error) { return "", fmt.Errorf("boom") }

		m = discoverPress(m, key("down"), key("t"), key("f"), key("enter"))

		require.Equal(t, "boom", m.errMsg)
	})
}

// TestDiscoverDetailLines covers the right panel's text: the local tags
// recorded by `evoke tag`, the file dumped verbatim, and a rule between two
// sections but never above one standing on its own.
func TestDiscoverDetailLines(t *testing.T) {
	// A comment and a TAGS block: both survive a dump, neither would survive
	// a parse and render.
	const src = "# a comment\nTAGS\n    demo\n\nNAME\n    aria\n"
	fileLines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	rule := horizontalRule(60)

	tests := []struct {
		name      string
		hasImage  bool
		localTags []string
		// wantAbove is everything the panel renders ahead of the file dump.
		wantAbove []string
	}{
		{
			name: "nothing above the file means no rule at all",
		},
		{
			name:      "an image is ruled off from the file",
			hasImage:  true,
			wantAbove: []string{rule},
		},
		{
			name:      "tags lead, then a rule, when there is no image",
			localTags: []string{"favorite"},
			wantAbove: []string{section("Tags"), "  favorite", rule},
		},
		{
			name:      "tags sit between image and file, ruled off from each",
			hasImage:  true,
			localTags: []string{"favorite", "wip"},
			wantAbove: []string{rule, section("Tags"), "  favorite, wip", rule},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("EVOKE_HOME", t.TempDir())
			dir := t.TempDir()
			path := filepath.Join(dir, "aria.evoke")
			require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

			roots := []sourceRoot{{Path: dir}}
			if len(tt.localTags) > 0 {
				require.NoError(t, saveTags(&Tags{Files: map[string][]string{
					rootRelativeKey(path, roots): tt.localTags,
				}}))
			}

			lines := discoverDetailLines(path, roots, 60, tt.hasImage)

			require.Equal(t, slices.Concat(tt.wantAbove, fileLines), lines)
		})
	}
}

// TestDiscoverLayoutImageGap covers the panel collapsing when there is no
// image to show, rather than leaving a blank gap above the detail text.
func TestDiscoverLayoutImageGap(t *testing.T) {
	t.Run("no image leaves no gap", func(t *testing.T) {
		m := newDiscoverModel(syntheticTree(), nil, sourceIndex{}) // zero-value sources: nothing resolves
		m = discoverPress(m, key("down"))                          // select the file "alpha.evoke"

		require.Equal(t, 0, m.imgRows)
		require.Equal(t, 1, m.detailTop)
	})

	t.Run("an image reserves its own block", func(t *testing.T) {
		t.Setenv("EVOKE_HOME", t.TempDir())
		dir := t.TempDir()
		writeSourceImage(t, dir, "recent.png", []string{"alpha.evoke"}, time.Minute)
		sources := testSourceIndex(t, dir)
		m := newDiscoverModel(syntheticTree(), []sourceRoot{{Path: "/root"}}, sources)
		m = discoverPress(m, key("down")) // select the file "alpha.evoke"

		require.Positive(t, m.imgRows)
		require.Equal(t, m.imgRows+2, m.detailTop)
	})
}

func TestIsUnsafeDiscoverRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	require.True(t, isUnsafeDiscoverRoot(string(filepath.Separator)))
	require.True(t, isUnsafeDiscoverRoot(home))
	require.False(t, isUnsafeDiscoverRoot(filepath.Join(home, "code")))
	require.False(t, isUnsafeDiscoverRoot(t.TempDir()))
}

func TestRootRelativeKey(t *testing.T) {
	roots := []sourceRoot{
		{Path: "/home/user/characters"},
		{Path: "/home/user/library"},
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "relative to the first matching root", path: "/home/user/characters/aria.evoke", want: "aria.evoke"},
		{name: "relative to a nested root path", path: "/home/user/library/farm/greenhouse.evoke", want: filepath.Join("farm", "greenhouse.evoke")},
		{name: "outside every root falls back to basename", path: "/elsewhere/aria.evoke", want: "aria.evoke"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, rootRelativeKey(tt.path, roots))
		})
	}
}

// pngChunk builds one PNG chunk: a 4-byte length, the type and data, and their
// CRC32 — the minimum ReadPNG needs to walk past a chunk it doesn't parse.
func pngChunk(ctype string, data []byte) []byte {
	var buf bytes.Buffer
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(data)))
	buf.Write(length)
	body := append([]byte(ctype), data...)
	buf.Write(body)
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(body))
	buf.Write(crc)
	return buf.Bytes()
}

// writeSourceImage writes a minimal PNG whose "evoke" tEXt chunk names
// sources, the same chunk the generator writes and ReadPNG parses.
func writeSourceImage(t *testing.T, dir, name string, sources []string, age time.Duration) string {
	t.Helper()
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], 64)
	binary.BigEndian.PutUint32(ihdr[4:8], 64)

	srcJSON, err := json.Marshal(sources)
	require.NoError(t, err)
	evokeData := append([]byte("evoke\x00"), []byte(fmt.Sprintf(`{"sources":%s}`, srcJSON))...)

	png := []byte("\x89PNG\r\n\x1a\n")
	png = append(png, pngChunk("IHDR", ihdr)...)
	png = append(png, pngChunk("tEXt", evokeData)...)
	png = append(png, pngChunk("IEND", nil)...)

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, png, 0o600))
	require.NoError(t, os.Chtimes(path, time.Now().Add(-age), time.Now().Add(-age)))
	return path
}

// testSourceIndex builds a source index over dir's output images and closes
// it when the test finishes.
func testSourceIndex(t *testing.T, dir string) sourceIndex {
	t.Helper()
	idx := buildSourceIndex(dir)
	t.Cleanup(idx.close)
	return idx
}

// TestBuildSourceIndexImagesFor covers matching: the root-relative identity,
// the basename fallback for a source embedded outside every root, no match at
// all, and — the reason the JSON cache was replaced — every example for a
// source surviving rather than only the newest, ordered newest first.
func TestBuildSourceIndexImagesFor(t *testing.T) {
	t.Setenv("EVOKE_HOME", t.TempDir())
	dir := t.TempDir()
	roots := []sourceRoot{{Path: "/root/characters"}}

	older := writeSourceImage(t, dir, "old.png", []string{"aria.evoke"}, time.Hour)
	newer := writeSourceImage(t, dir, "new.png", []string{"aria.evoke"}, time.Minute)
	// Generated with the file outside every root, so it was embedded by
	// basename alone.
	outside := writeSourceImage(t, dir, "outside.png", []string{"/elsewhere/beta.evoke"}, time.Minute)
	writeSourceImage(t, dir, "unrelated.png", []string{"gamma.evoke"}, time.Minute)

	idx := testSourceIndex(t, dir)

	require.Equal(t, []string{newer, older}, idx.imagesFor("/root/characters/aria.evoke", roots))
	require.Equal(t, []string{outside}, idx.imagesFor("/root/characters/beta.evoke", roots))
	require.Empty(t, idx.imagesFor("/root/characters/delta.evoke", roots))
}

func TestSourceIndexZeroValueIsSafe(t *testing.T) {
	var idx sourceIndex
	require.Empty(t, idx.imagesFor("/anything.evoke", nil))
	idx.close()
}

// TestBuildSourceIndexInvalidatesMissingImages covers the only way a recorded
// example needs to change without a new image appearing: the file it pointed
// at was deleted (e.g. by `evoke view`'s d), which must stop being offered as
// an example rather than pointing at nothing.
func TestBuildSourceIndexInvalidatesMissingImages(t *testing.T) {
	t.Setenv("EVOKE_HOME", t.TempDir())
	dir := t.TempDir()
	roots := []sourceRoot{{Path: "/root/characters"}}

	img := writeSourceImage(t, dir, "one.png", []string{"aria.evoke"}, time.Minute)
	require.Equal(t, []string{img}, testSourceIndex(t, dir).imagesFor("/root/characters/aria.evoke", roots))

	require.NoError(t, os.Remove(img))
	require.Empty(t, testSourceIndex(t, dir).imagesFor("/root/characters/aria.evoke", roots))
}

// TestBuildSourceIndexIsIncremental covers the point of persisting the index:
// a second call only has to read the image generated since the first call,
// not reparse everything again. This is asserted through behavior — both
// images end up recorded — since scanned_through_ns is an implementation
// detail, but a stale value would make the second image invisible entirely.
func TestBuildSourceIndexIsIncremental(t *testing.T) {
	t.Setenv("EVOKE_HOME", t.TempDir())
	dir := t.TempDir()
	roots := []sourceRoot{{Path: "/root/characters"}}

	older := writeSourceImage(t, dir, "one.png", []string{"aria.evoke"}, time.Hour)
	require.Equal(t, []string{older}, testSourceIndex(t, dir).imagesFor("/root/characters/aria.evoke", roots))

	newer := writeSourceImage(t, dir, "two.png", []string{"aria.evoke"}, time.Minute)
	require.Equal(t, []string{newer, older}, testSourceIndex(t, dir).imagesFor("/root/characters/aria.evoke", roots))
}

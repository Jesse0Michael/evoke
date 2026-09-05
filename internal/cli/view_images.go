package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// viewImage is one navigable output file.
type viewImage struct {
	path      string
	filename  string
	subfolder string
}

// label is the status-bar name: the containing group plus the file name, since
// IMAGE groups are what separate one shoot from another.
func (v viewImage) label() string {
	if v.subfolder != "" {
		return filepath.Base(v.subfolder) + "/" + v.filename
	}
	return v.filename
}

// loadImages returns every image under outputDir, newest first. The tmp/
// subtree holds per-stage debug frames, which are reached through their final
// image rather than browsed directly.
func loadImages(outputDir string) ([]viewImage, error) {
	type fileEntry struct {
		path    string
		modTime int64
	}
	var files []fileEntry

	_ = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(outputDir, path)
		if strings.Contains(rel, "tmp/") {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png", ".jpg", ".jpeg", ".webp":
			files = append(files, fileEntry{path: path, modTime: info.ModTime().UnixMilli()})
		}
		return nil
	})

	sort.Slice(files, func(i, j int) bool { return files[i].modTime > files[j].modTime })

	imgs := make([]viewImage, 0, len(files))
	for _, f := range files {
		rel, _ := filepath.Rel(outputDir, f.path)
		sub := filepath.Dir(rel)
		if sub == "." {
			sub = ""
		}
		imgs = append(imgs, viewImage{path: f.path, filename: filepath.Base(f.path), subfolder: sub})
	}
	return imgs, nil
}

// resolveOutputDir returns the image output directory: EVOKE_OUTPUT_DIR, else
// the configured output_path. Nothing is guessed — where the backend writes is
// machine-specific, and a guess that happens to name a real directory browses
// the wrong images silently, while one that does not is indistinguishable from
// no configuration at all.
func resolveOutputDir(s *Settings) string {
	if dir := os.Getenv("EVOKE_OUTPUT_DIR"); dir != "" {
		return dir
	}
	if s != nil {
		return s.OutputPath
	}
	return ""
}

// timestampIndexPattern matches the timestamp_index pair a debug frame shares
// with the final image it came from.
var timestampIndexPattern = regexp.MustCompile(`(\d{10,}_\d+)`)

// findDebugImages returns the per-stage frames saved alongside an image, in
// pipeline order. They exist only for generations run with -v.
func findDebugImages(outputDir, filename string) []string {
	matches := timestampIndexPattern.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return nil
	}

	entries, err := os.ReadDir(filepath.Join(outputDir, "tmp"))
	if err != nil {
		return nil
	}

	var results []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), matches[1]+"_") {
			results = append(results, filepath.Join(outputDir, "tmp", e.Name()))
		}
	}
	sort.Strings(results)
	return results
}

// --- terminal image rendering ---

// drawImage renders an image into the pane with chafa, at the current cursor
// position.
//
// chafa is given the terminal directly, in cooked mode: it probes the terminal
// to pick its output format, and a probe that goes unanswered — because the
// viewer is holding the terminal in raw mode, or because its output was piped —
// falls back to character art on a terminal that supports real graphics. This
// is also why the output is not captured or cached: a pipe has no capabilities
// to detect.
func drawImage(path string, cols, rows int) {
	// The terminal stays in raw mode and chafa keeps stdin: it needs the real
	// terminal to detect kitty/sixel/iterm support, and dropping to cooked mode
	// for the duration loses every keystroke typed while it runs — pending
	// canonical input does not survive the switch back to raw.
	cmd := exec.Command("chafa", fmt.Sprintf("--size=%dx%d", cols, rows), path)
	cmd.Stdout = os.Stdout
	_ = cmd.Run()
}

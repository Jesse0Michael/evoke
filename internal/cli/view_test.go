package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/jesse0michael/evoke/internal/generate/comfyui"
)

// testImages builds a navigable list without touching the filesystem: the model
// only reads a file once a window size is known, and these tests drive
// navigation.
func testImages(n int) []viewImage {
	imgs := make([]viewImage, 0, n)
	for i := range n {
		name := fmt.Sprintf("test-image-%c.png", 'a'+i)
		imgs = append(imgs, viewImage{path: "/out/" + name, filename: name})
	}
	return imgs
}

func key(name string) event { return event{key: name} }

func wheel(n int) event { return event{wheel: n} }

// press applies a sequence of events, painting after each as the input loop
// does.
func press(m viewModel, events ...event) viewModel {
	for _, ev := range events {
		m.handle(ev)
		m.layout()
		m.paint()
	}
	return m
}

func keys(names ...string) []event {
	events := make([]event, 0, len(names))
	for _, n := range names {
		events = append(events, key(n))
	}
	return events
}

// sized returns a laid-out model whose image renders and frame writes are
// captured rather than sent to a terminal.
func sized(m viewModel, drawn *[]string, out *bytes.Buffer) viewModel {
	m.out = out
	m.draw = func(path string, _, _ int) { *drawn = append(*drawn, filepath.Base(path)) }
	m.layout()
	m.load()
	m.paint()
	return m
}

func TestViewModelNavigation(t *testing.T) {
	tests := []struct {
		name      string
		pageSize  int
		keys      []string
		wantIndex int
		wantShown int
	}{
		{name: "starts at newest", keys: nil, wantIndex: 0, wantShown: 8},
		{name: "advances", keys: []string{"right", "right"}, wantIndex: 2, wantShown: 8},
		{name: "retreats", keys: []string{"right", "right", "left"}, wantIndex: 1, wantShown: 8},
		{name: "holds at newest", keys: []string{"left", "left"}, wantIndex: 0, wantShown: 8},
		{name: "vim keys navigate", keys: []string{"l", "l", "h"}, wantIndex: 1, wantShown: 8},

		// Up and down jump by 50, clamped to the ends of the list.
		{name: "down jumps to the oldest", keys: []string{"down"}, wantIndex: 7, wantShown: 8},
		{name: "up jumps back to the newest", keys: []string{"down", "up"}, wantIndex: 0, wantShown: 8},

		// With a page size, navigating past the end reveals the next page
		// rather than stopping.
		{name: "page bounds navigation", pageSize: 3, keys: []string{"right", "right"}, wantIndex: 2, wantShown: 3},
		{name: "past the end reveals a page", pageSize: 3, keys: []string{"right", "right", "right"}, wantIndex: 3, wantShown: 6},
		{name: "a jump reveals every page it crosses", pageSize: 3, keys: []string{"down"}, wantIndex: 7, wantShown: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newViewModel("/out", testImages(8), tt.pageSize)
			m = press(m, keys(tt.keys...)...)

			require.Equal(t, tt.wantIndex, m.idx)
			require.Equal(t, tt.wantShown, m.shown)
			require.Equal(t, modeBrowse, m.mode)
		})
	}
}

// TestViewModelNewer covers the only rescan there is: pressing left at the
// newest image looks for output generated since the viewer opened.
func TestViewModelNewer(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("test"), 0o600))
		require.NoError(t, os.Chtimes(path, time.Now().Add(-age), time.Now().Add(-age)))
		return path
	}

	older := write("test-image-old.png", time.Hour)
	images, err := loadImages(dir)
	require.NoError(t, err)
	require.Len(t, images, 1)

	m := newViewModel(dir, images, 0)

	// Nothing new yet: left at the newest image stays put.
	m = press(m, key("left"))
	require.Equal(t, 0, m.idx)
	require.Len(t, m.images, 1)

	newer := write("test-image-new.png", time.Minute)
	m = press(m, key("left"))

	require.Len(t, m.images, 2)
	require.Equal(t, 2, m.shown)
	// Landing on the oldest of the new batch is a step left from where the
	// selection already was.
	require.Equal(t, 0, m.idx)
	require.Equal(t, newer, m.images[0].path)
	require.Equal(t, older, m.images[1].path)
}

func TestViewModelDelete(t *testing.T) {
	dir := t.TempDir()
	images := make([]viewImage, 0, 3)
	paths := make([]string, 0, 3)
	for _, name := range []string{"test-image-a.png", "test-image-b.png", "test-image-c.png"} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("test"), 0o600))
		images = append(images, viewImage{path: path, filename: name})
		paths = append(paths, path)
	}

	var drawn []string
	out := &bytes.Buffer{}
	m := sized(newViewModel(dir, images, 0), &drawn, out)

	// d arms the confirmation; only a second d removes the file.
	m = press(m, keys("right", "d")...)
	require.Equal(t, modeConfirmDelete, m.mode)
	require.Contains(t, out.String(), "any key cancel  d again to delete")

	m = press(m, key("left"))
	require.Equal(t, modeBrowse, m.mode)
	require.Len(t, m.images, 3)
	require.FileExists(t, paths[1])

	m = press(m, keys("d", "d")...)
	require.Equal(t, modeBrowse, m.mode)
	require.Len(t, m.images, 2)
	require.Equal(t, 2, m.shown)
	require.Equal(t, "test-image-c.png", m.images[m.idx].filename)
	require.NoFileExists(t, paths[1])
}

// TestViewModelDeleteLastImage covers the teardown path: the model must survive
// being painted and keyed after its last image is gone.
func TestViewModelDeleteLastImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-image-a.png")
	require.NoError(t, os.WriteFile(path, []byte("test"), 0o600))

	var drawn []string
	m := sized(newViewModel(dir, []viewImage{{path: path, filename: "test-image-a.png"}}, 0), &drawn, &bytes.Buffer{})
	m = press(m, keys("d", "d")...)

	require.Empty(t, m.images)
	require.Equal(t, "No images remaining.", m.exitMsg)
	require.NoFileExists(t, path)
	require.NotPanics(t, func() { press(m, keys("right", "t")...) })
}

// TestViewRedrawsImageOnlyWhenItChanges is the guard against the viewer feeling
// slow: chafa is the expensive part of a frame, so scrolling, arming a delete,
// or any other in-place change must repaint from state alone.
func TestViewRedrawsImageOnlyWhenItChanges(t *testing.T) {
	var drawn []string
	m := sized(newViewModel("/out", testImages(4), 0), &drawn, &bytes.Buffer{})
	require.Equal(t, []string{"test-image-a.png"}, drawn)

	m = press(m,
		wheel(1),
		wheel(-1),
		key("d"),
		key("x"),
	)
	require.Equal(t, []string{"test-image-a.png"}, drawn, "only the image may trigger chafa")

	m = press(m, key("right"))
	require.Equal(t, []string{"test-image-a.png", "test-image-b.png"}, drawn)

	// A resize refits the image, so it has to be drawn again.
	func() { m.size = func() (int, int) { return 100, 30 }; m.layout(); m.paint() }()
	require.Len(t, drawn, 3)
}

// TestViewDetailsScroll covers reading a record longer than the column: the
// wheel scrolls it, and the navigation keys stay with the image.
func TestViewDetailsScroll(t *testing.T) {
	var drawn []string
	m := sized(newViewModel("/out", testImages(2), 0), &drawn, &bytes.Buffer{})

	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%d", i)
	}
	m.details.setLines(lines)
	require.Equal(t, 0, m.details.offset)

	m = press(m, wheel(1))
	require.Equal(t, wheelLines, m.details.offset)

	m = press(m, wheel(-1), wheel(-1))
	require.Equal(t, 0, m.details.offset, "scrolling up stops at the top")

	// The last line must be reachable and be the end of the scroll.
	for range 200 {
		m = press(m, wheel(1))
	}
	require.Equal(t, len(lines)-m.details.height, m.details.offset)
	require.Equal(t, "line-199", m.details.visible()[m.details.height-1])

	m = press(m, key("right"))
	require.Equal(t, 0, m.details.offset, "changing image resets the scroll")
}

// TestViewPaintsOverThePreviousFrame is the ghosting guard: the details column
// is padded to its own width at a fixed column, so a shorter record cannot
// leave the tail of a longer one on screen, and the image pane is cleared
// before a new image is drawn into it.
func TestViewPaintsOverThePreviousFrame(t *testing.T) {
	var drawn []string
	out := &bytes.Buffer{}
	m := sized(newViewModel("/out", testImages(2), 0), &drawn, out)

	m.details.setLines([]string{"short", strings.Repeat("x", 60)})
	out.Reset()
	m.paint()

	frame := out.String()
	// Every details row is positioned at the same column and padded to 44.
	for row := 1; row <= 38; row++ {
		require.Contains(t, frame, fmt.Sprintf("\033[%d;%dH", row, m.detailCol), "row %d not positioned", row)
	}
	require.Contains(t, frame, "short"+strings.Repeat(" ", 39))
	require.Contains(t, frame, strings.Repeat("x", 44), "an overlong line is trimmed to the column")

	// Changing image clears the pane and homes the cursor for chafa.
	out.Reset()
	next := press(m, key("right"))
	require.Contains(t, out.String(), kittyDeleteImages)
	require.Contains(t, out.String(), "\033[1;1H")
	require.Contains(t, out.String(), strings.Repeat(" ", next.imgCols))
}

func TestViewControlsBar(t *testing.T) {
	tests := []struct {
		name     string
		mode     viewMode
		hasDebug bool
		flashKey string
		want     string
	}{
		{name: "browse", mode: modeBrowse, want: "← → navigate  c copy path    o finder  y copy image    d delete  q quit"},
		{name: "confirming a copy", mode: modeBrowse, flashKey: "c", want: "← → navigate  c copy path ✓  o finder  y copy image    d delete  q quit"},
		{name: "browse with debug frames", mode: modeBrowse, hasDebug: true, want: "← → navigate  t debug  c copy path    o finder  y copy image    d delete  q quit"},
		{name: "confirming a delete", mode: modeConfirmDelete, want: "any key cancel  d again to delete"},
		{name: "stepping debug frames", mode: modeDebug, want: "← → navigate  q/esc back"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newViewModel("/out", testImages(2), 0)
			m.mode, m.hasDebug, m.flashKey = tt.mode, tt.hasDebug, tt.flashKey

			// The scroll keys and the ±50 jump are deliberately unadvertised.
			require.Equal(t, tt.want, stripSGR(m.controlsBar()))
		})
	}
}

// TestViewControlsBarDoesNotShift covers the reserved marker column: lighting a
// glyph must not move anything beside it.
func TestViewControlsBarDoesNotShift(t *testing.T) {
	for _, key := range []string{"c", "y"} {
		t.Run(key, func(t *testing.T) {
			m := newViewModel("/out", testImages(2), 0)
			idle := ansi.StringWidth(stripSGR(m.controlsBar()))
			m.flashKey = key

			require.Equal(t, idle, ansi.StringWidth(stripSGR(m.controlsBar())))
		})
	}
}

func TestViewFileActions(t *testing.T) {
	// The action seams are stubbed, so nothing here reaches the clipboard or
	// Finder; each case records the path its action was handed.
	tests := []struct {
		name         string
		keys         []string
		err          error
		wantCalls    []string
		wantConfirms int
		wantErrMsg   string
	}{
		{name: "c copies the path", keys: []string{"c"}, wantCalls: []string{"copyText:/out/test-image-a.png"}, wantConfirms: 1},
		{name: "y copies the image", keys: []string{"y"}, wantCalls: []string{"copyImage:/out/test-image-a.png"}, wantConfirms: 1},

		// Finder coming to the front is its own confirmation.
		{name: "o reveals without confirming", keys: []string{"o"}, wantCalls: []string{"reveal:/out/test-image-a.png"}, wantConfirms: 0},

		{name: "an action applies to the selected file", keys: []string{"right", "c"}, wantCalls: []string{"copyText:/out/test-image-b.png"}, wantConfirms: 1},

		// A failure has to stay readable, so it persists instead of flashing.
		{name: "a failure is reported instead", keys: []string{"y"}, err: errors.New("only PNG images can be copied"), wantCalls: []string{"copyImage:/out/test-image-a.png"}, wantConfirms: 0, wantErrMsg: "only PNG images can be copied"},
		{name: "the next key clears a failure", keys: []string{"y", "right"}, err: errors.New("only PNG images can be copied"), wantCalls: []string{"copyImage:/out/test-image-a.png"}, wantConfirms: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			confirms := 0
			record := func(label string) func(string) error {
				return func(path string) error {
					calls = append(calls, label+":"+path)
					return tt.err
				}
			}

			out := &bytes.Buffer{}
			m := newViewModel("/out", testImages(8), 0)
			m.out = out
			m.copyText, m.copyImage, m.reveal = record("copyText"), record("copyImage"), record("reveal")
			m.sleep = func(time.Duration) { confirms++ }
			m.layout()
			out.Reset()

			m = press(m, keys(tt.keys...)...)

			require.Equal(t, tt.wantCalls, calls)
			require.Equal(t, tt.wantConfirms, confirms)
			// The glyph is withdrawn before the loop repaints, so it survives only
			// in what was written to the screen.
			require.Equal(t, tt.wantConfirms > 0, strings.Contains(out.String(), flashGlyph))
			require.Equal(t, tt.wantErrMsg, m.errMsg)
			require.Empty(t, m.flashKey)
			require.Equal(t, modeBrowse, m.mode)
		})
	}
}

func TestViewStatusBar(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   string
	}{
		{name: "no action taken", errMsg: "", want: "[1/8]  test-image-a.png"},

		// A success shows in the controls bar instead; only a failure lands here.
		{name: "a failed action", errMsg: "only PNG images can be copied", want: "[1/8]  test-image-a.png   only PNG images can be copied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newViewModel("/out", testImages(8), 0)
			m.errMsg = tt.errMsg

			require.Equal(t, tt.want, stripSGR(m.statusBar()))
		})
	}
}

func stripSGR(s string) string {
	for {
		i := strings.Index(s, "\033[")
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], 'm')
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

func TestFormatDetails(t *testing.T) {
	meta := comfyui.Metadata{
		Model:     "test-checkpoint.safetensors",
		Width:     1216,
		Height:    832,
		Positive:  "test-appearance, test-apparel, test-environment",
		Negative:  "test-negative",
		Sampler:   "euler_ancestral",
		Scheduler: "karras",
		Steps:     40,
		CFG:       4.0,
		Denoise:   1.0,
		// Above 2^53: the value a float64 decode would have rounded.
		Seed:      18286102337276285000,
		FileSize:  2 * 1024 * 1024,
		LoRAs:     []comfyui.LoRA{{Name: "test-lora.safetensors", Model: 0.8, Clip: 0.7}},
		Detailers: []comfyui.Detailer{{Name: "face", Steps: 20, CFG: 4.0, Seed: 42, Positive: "test-detailer"}},
		Upscale:   comfyui.Upscale{Model: "test-upscale.pth", Factor: 1.5, Seed: 7},
		Sources:   []string{"/src/pipelines/test-pipeline.evoke", "/src/characters/test-character.evoke"},
		Inputs:    []string{"test-pipeline", "test-character"},
	}

	out := strings.Join(formatDetails(meta, "/out/test-image.png", 44), "\n")

	for _, want := range []string{
		"1216x832", "2 MB", "test-pipeline test-character",
		"test-checkpoint.safetensors", "euler_ancestral / karras", "40", "4.0",
		"18286102337276285000", // the full seed, not a rounded one
		"test-lora.safetensors", "(0.8/0.7)", "test-appearance", "test-negative",
		"upscale", "test-upscale.pth", "1.5x", "face", "test-detailer",
	} {
		require.Contains(t, out, want)
	}

	// Sources are listed by file name; the path they were resolved from is not
	// what identifies a source at a glance.
	require.Contains(t, out, "test-character.evoke")
	require.NotContains(t, out, "/src/characters")

	// Nothing may exceed the column width, or the frame layout shifts.
	for i, line := range strings.Split(out, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(line), 44, "line %d overflows the column", i)
	}
}

// TestFormatStageDetails covers the debug frames: each is one pass, and the
// column must describe that pass rather than the finished image.
func TestFormatStageDetails(t *testing.T) {
	meta := comfyui.Metadata{
		Model:   "test-checkpoint.safetensors",
		Seed:    11,
		Steps:   40,
		Upscale: comfyui.Upscale{Model: "test-upscale.pth", Seed: 22, Steps: 10},
		Detailers: []comfyui.Detailer{
			{Name: "face", Seed: 33, Steps: 20},
			{Name: "upper_body", Seed: 44, Steps: 25},
		},
	}

	tests := []struct {
		name     string
		filename string
		want     string
		wantSeed string
	}{
		{name: "base frame", filename: "1700000000_0_1_base_00001_.png", want: "base", wantSeed: "11"},
		{name: "upscale frame", filename: "1700000000_0_2_upscale_00001_.png", want: "upscale", wantSeed: "22"},
		{name: "detailer frame", filename: "1700000000_0_3_face_00001_.png", want: "face", wantSeed: "33"},
		{name: "multi-word detailer frame", filename: "1700000000_0_5_upper_body_00001_.png", want: "upper_body", wantSeed: "44"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := strings.Join(formatStageDetails(meta, tt.filename, 44), "\n")

			require.Contains(t, out, tt.want)
			require.Contains(t, out, tt.wantSeed)
		})
	}
}

// TestDecodeEvents covers the input decoder. It is the whole reason the viewer
// sees anything: a wrong split here reads as dead keys.
func TestDecodeEvents(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []event
	}{
		{name: "a letter", input: "d", want: []event{{key: "d"}}},
		{name: "space", input: " ", want: []event{{key: " "}}},
		{name: "ctrl-c", input: "\x03", want: []event{{key: "ctrl+c"}}},
		{name: "arrows", input: "\x1b[A\x1b[B\x1b[C\x1b[D",
			want: []event{{key: "up"}, {key: "down"}, {key: "right"}, {key: "left"}}},

		// A lone ESC is the Escape key; terminals emit a sequence in one write,
		// so nothing follows it in the same chunk.
		{name: "escape alone", input: "\x1b", want: []event{{key: "esc"}}},

		// A held-down key arrives as one chunk and is applied as one burst.
		{name: "repeated arrows", input: "\x1b[C\x1b[C\x1b[C",
			want: []event{{key: "right"}, {key: "right"}, {key: "right"}}},

		{name: "wheel up", input: "\x1b[<64;10;20M", want: []event{{wheel: -1}}},
		{name: "wheel down", input: "\x1b[<65;10;20M", want: []event{{wheel: 1}}},

		// Clicks and releases are consumed, never delivered: the viewer binds
		// no mouse button, and leaving them in the buffer would desync it.
		{name: "click is dropped", input: "\x1b[<0;10;20M", want: nil},
		{name: "release is dropped", input: "\x1b[<65;10;20m", want: nil},
		{name: "mouse then key", input: "\x1b[<65;1;1Mq", want: []event{{wheel: 1}, {key: "q"}}},

		// A chunk that ends mid-sequence yields what it can; the rest is
		// dropped rather than mis-read as a keypress.
		{name: "truncated sequence", input: "q\x1b[", want: []event{{key: "q"}}},
		{name: "truncated mouse", input: "q\x1b[<65;1", want: []event{{key: "q"}}},
		{name: "empty", input: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, decodeEvents([]byte(tt.input)))
		})
	}
}

// TestViewRunPaintsAndQuits drives the loop over a scripted input stream, the
// way the terminal feeds it.
func TestViewRunPaintsAndQuits(t *testing.T) {
	var drawn []string
	out := &bytes.Buffer{}

	m := newViewModel("/out", testImages(4), 0)
	m.out = out
	m.draw = func(path string, _, _ int) { drawn = append(drawn, filepath.Base(path)) }

	// One chunk per read, as a terminal delivers them: a burst of arrows, then
	// a quit.
	require.NoError(t, m.run(&chunkReader{chunks: []string{"\x1b[C\x1b[C", "q"}}))

	require.Equal(t, 2, m.idx)
	require.Contains(t, out.String(), "[3/4]")
	// The burst is applied before painting, so it costs one chafa run, not two.
	require.Equal(t, []string{"test-image-a.png", "test-image-c.png"}, drawn)
}

// chunkReader delivers each string as one Read, the way a terminal delivers a
// keystroke or an escape sequence.
type chunkReader struct{ chunks []string }

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks = r.chunks[1:]
	return n, nil
}

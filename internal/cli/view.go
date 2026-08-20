package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/jesse0michael/evoke/internal/generate/comfyui"
)

// Terminal control. The viewer drives the terminal directly rather than through
// a TUI framework, because the image pane belongs to chafa: real terminal
// graphics (kitty, sixel, iterm) are placements the terminal owns, not cells a
// frame renderer can diff, and any renderer that repaints a line would erase
// them. Everything here is painted at absolute positions instead.
const (
	altScreenOn  = "\033[?1049h"
	altScreenOff = "\033[?1049l"
	cursorHide   = "\033[?25l"
	cursorShow   = "\033[?25h"
	eraseScreen  = "\033[2J"
	// Button reporting in SGR encoding. Without it the wheel arrives as up and
	// down arrows, which the viewer binds to a ±50 jump.
	mouseOn  = "\033[?1000h\033[?1006h"
	mouseOff = "\033[?1006l\033[?1000l"
	// Graphics placements are not cell content and outlive an erase, so the
	// previous image is deleted explicitly before the next one is drawn.
	kittyDeleteImages = "\033_Ga=d\033\\"
)

// wheelLines is how far one wheel notch scrolls the details pane.
const wheelLines = 3

// ViewCmd launches the interactive image viewer for recent output.
func ViewCmd(args []string, _ bool) int {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	pageSize := fs.Int("n", 0, "images navigable at a time; navigating past the end reveals the next page (0 = all)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := runViewer(context.Background(), *pageSize); err != nil {
		fmt.Fprintf(os.Stderr, "evoke view: %v\n", err)
		return 1
	}
	return 0
}

func runViewer(_ context.Context, pageSize int) error {
	outputDir := resolveOutputDir()
	if outputDir == "" {
		return fmt.Errorf("could not resolve output directory (set EVOKE_OUTPUT_DIR)")
	}

	// The full scan is cheap (a stat walk); metadata parsing and rendering are
	// per-displayed-image, so nothing is gained by capping what we load.
	images, err := loadImages(outputDir)
	if err != nil {
		return err
	}
	if len(images) == 0 {
		fmt.Println("No image files found.")
		return nil
	}

	fd := int(os.Stdin.Fd())
	cooked, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}

	fmt.Fprint(os.Stdout, altScreenOn+cursorHide+mouseOn+eraseScreen)
	defer func() {
		fmt.Fprint(os.Stdout, kittyDeleteImages+mouseOff+cursorShow+altScreenOff)
		_ = term.Restore(fd, cooked)
	}()

	m := newViewModel(outputDir, images, pageSize)
	m.out = os.Stdout
	m.draw = drawImage
	m.size = func() (int, int) {
		w, h, err := term.GetSize(fd)
		if err != nil || w == 0 || h == 0 {
			return 120, 40
		}
		return w, h
	}

	if err := m.run(os.Stdin); err != nil {
		return err
	}
	if m.exitMsg != "" {
		// Printed after the deferred restore puts the primary screen back.
		defer fmt.Println(m.exitMsg)
	}
	return nil
}

// viewMode selects which image the panes describe and which keys are live.
type viewMode int

const (
	modeBrowse viewMode = iota
	modeDebug
	modeConfirmDelete
)

// metaCacheLimit bounds the parsed-metadata cache. Entries are small; the cap
// exists so a long session over thousands of images cannot grow without bound.
const metaCacheLimit = 512

// viewModel holds the viewer's state. Navigation is explicit rather than
// captured in closures, so it can be driven by synthetic events in a test.
type viewModel struct {
	outputDir string
	images    []viewImage
	idx       int
	// shown bounds navigation; revealing a page raises it toward len(images).
	shown    int
	pageSize int

	mode      viewMode
	debugImgs []string
	debugIdx  int
	// hasDebug is resolved when the selection changes, not when the controls
	// bar is painted: it reads a directory.
	hasDebug bool

	details detailPane
	meta    map[string]comfyui.Metadata

	width       int
	height      int
	imgCols     int
	imgRows     int
	detailCol   int
	detailWidth int

	// painted is the image currently on screen, so navigating within one image
	// (scrolling, arming a delete) never re-runs chafa.
	painted     string
	paintedCols int
	paintedRows int

	// Seams for the terminal, so tests drive the model without one.
	draw func(path string, cols, rows int)
	size func() (int, int)
	out  io.Writer

	exitMsg string
}

func newViewModel(outputDir string, images []viewImage, pageSize int) viewModel {
	shown := len(images)
	if pageSize > 0 && pageSize < shown {
		shown = pageSize
	}
	return viewModel{
		outputDir: outputDir,
		images:    images,
		shown:     shown,
		pageSize:  pageSize,
		meta:      map[string]comfyui.Metadata{},
		draw:      func(string, int, int) {},
		size:      func() (int, int) { return 120, 40 },
		out:       io.Discard,
	}
}

// run is the input loop. A whole read is decoded and applied before painting,
// so a held-down arrow coalesces into one repaint rather than one per repeat —
// which is what keeps fast navigation from queuing up chafa runs.
func (m *viewModel) run(in io.Reader) error {
	m.layout()
	m.load()
	m.paint()

	buf := make([]byte, 512)
	for {
		n, err := in.Read(buf)
		if err != nil {
			return nil
		}

		for _, ev := range decodeEvents(buf[:n]) {
			if m.handle(ev) {
				return nil
			}
		}

		m.layout()
		m.paint()
	}
}

// handle applies one event, reporting whether the viewer should exit.
func (m *viewModel) handle(ev event) bool {
	if len(m.images) == 0 {
		return true
	}
	if ev.wheel != 0 {
		m.details.scroll(ev.wheel * wheelLines)
		return false
	}

	switch m.mode {
	case modeConfirmDelete:
		m.mode = modeBrowse
		if ev.key == "d" || ev.key == "D" {
			return m.remove()
		}
		return false

	case modeDebug:
		switch ev.key {
		case "q", "Q", "esc", "t", "T", "ctrl+c":
			m.mode = modeBrowse
			m.debugImgs, m.debugIdx = nil, 0
			m.load()
		case "right", "l", "n", " ":
			if m.debugIdx < len(m.debugImgs)-1 {
				m.debugIdx++
				m.load()
			}
		case "left", "h", "p":
			if m.debugIdx > 0 {
				m.debugIdx--
				m.load()
			}
		}
		return false
	}

	switch ev.key {
	case "q", "Q", "esc", "ctrl+c":
		return true

	case "right", "l", "n", " ":
		m.forward(1)

	case "left", "h", "p":
		if m.idx > 0 {
			m.idx--
			m.load()
		} else {
			// Already at the newest: this is the moment to look for newer output.
			m.newer()
		}

	case "down":
		m.forward(50)

	case "up":
		m.idx -= 50
		if m.idx < 0 {
			m.idx = 0
		}
		m.load()

	case "t", "T":
		if m.hasDebug {
			m.mode, m.debugImgs, m.debugIdx = modeDebug, findDebugImages(m.outputDir, m.images[m.idx].filename), 0
			m.load()
		}

	case "d", "D":
		m.mode = modeConfirmDelete
	}

	return false
}

// forward advances by n images, revealing further pages as it goes.
func (m *viewModel) forward(n int) {
	m.idx += n
	for m.shown < len(m.images) && m.idx >= m.shown {
		step := m.pageSize
		if step <= 0 {
			step = len(m.images)
		}
		m.shown += step
	}
	if m.shown > len(m.images) {
		m.shown = len(m.images)
	}
	if m.idx >= m.shown {
		m.idx = m.shown - 1
	}
	m.load()
}

// newer rescans for images generated since the viewer opened and prepends them,
// landing on the oldest of the new batch — the one adjacent to where the
// selection already was.
func (m *viewModel) newer() {
	fresh, err := loadImages(m.outputDir)
	if err != nil || len(fresh) == 0 || len(m.images) == 0 {
		return
	}

	added := 0
	for i, img := range fresh {
		if img.path == m.images[0].path {
			added = i
			break
		}
	}
	if added == 0 {
		return
	}

	m.images = append(fresh[:added:added], m.images...)
	m.shown += added
	m.idx = added - 1
	m.load()
}

// remove deletes the selected file and closes the gap it leaves, reporting
// whether that was the last image.
func (m *viewModel) remove() bool {
	if m.idx >= len(m.images) {
		return false
	}
	if err := os.Remove(m.images[m.idx].path); err != nil {
		return false
	}
	// Rebuilt rather than shifted in place: the slice came from a caller that
	// may still be holding it.
	m.images = append(append([]viewImage{}, m.images[:m.idx]...), m.images[m.idx+1:]...)
	if len(m.images) == 0 {
		m.exitMsg = "No images remaining."
		return true
	}
	if m.shown > len(m.images) {
		m.shown = len(m.images)
	}
	if m.idx >= m.shown {
		m.idx = m.shown - 1
	}
	m.load()
	return false
}

// layout divides the frame into the image pane, the details column, and the
// status and controls rows. The image is fitted two rows shorter than the
// column beside it: chafa ends its output with a newline, and a pane flush to
// the bars would scroll the frame.
func (m *viewModel) layout() {
	w, h := m.size()
	resized := w != m.width || h != m.height
	m.width, m.height = w, h

	detail := 44
	img := m.width - detail - 3
	if img < 30 {
		img = 30
		detail = m.width - img - 3
	}
	if detail < 10 {
		detail = 10
	}

	m.imgCols, m.imgRows = img, m.height-4
	m.detailCol, m.detailWidth = img+2, detail
	m.details.height = m.height - 2

	if resized {
		// chafa fitted the drawn image to the old pane.
		m.painted = ""
		fmt.Fprint(m.out, eraseScreen)
		m.load()
	}
}

// load reads the metadata for the current file into the details pane. Records
// are cached: stepping back through images already seen re-reads nothing.
func (m *viewModel) load() {
	path := m.current()
	if path == "" || m.detailWidth == 0 {
		return
	}

	meta, ok := m.meta[path]
	if !ok {
		meta, _ = comfyui.ReadPNG(path)
		if len(m.meta) > metaCacheLimit {
			m.meta = map[string]comfyui.Metadata{}
		}
		m.meta[path] = meta
	}

	if m.mode == modeDebug {
		m.details.setLines(formatStageDetails(meta, path, m.detailWidth))
	} else {
		m.details.setLines(formatDetails(meta, path, m.detailWidth))
		m.hasDebug = len(findDebugImages(m.outputDir, m.images[m.idx].filename)) > 0
	}
}

// current is the file both panes describe: the selected output, or the debug
// frame being stepped through.
func (m *viewModel) current() string {
	if m.mode == modeDebug {
		if m.debugIdx < len(m.debugImgs) {
			return m.debugImgs[m.debugIdx]
		}
		return ""
	}
	if m.idx < len(m.images) {
		return m.images[m.idx].path
	}
	return ""
}

// paint writes the frame. The details column and the bars are positioned
// absolutely and padded to their own width, so they overwrite the previous
// frame without touching the image pane; the image is redrawn only when it
// actually changes, and the pane is cleared first so a smaller image cannot
// leave the edges of a larger one behind.
func (m *viewModel) paint() {
	if len(m.images) == 0 || m.detailWidth == 0 {
		return
	}

	var b strings.Builder

	lines := m.details.visible()
	for row := 1; row <= m.height-2; row++ {
		line := ""
		if i := row - 1; i < len(lines) {
			line = lines[i]
		}
		fmt.Fprintf(&b, "\033[%d;%dH\033[2m│\033[0m %s", row, m.detailCol, pad(line, m.detailWidth))
	}

	fmt.Fprintf(&b, "\033[%d;1H\033[K%s", m.height-1, m.statusBar())
	fmt.Fprintf(&b, "\033[%d;1H\033[K%s", m.height, m.controlsBar())

	path := m.current()
	redraw := path != "" && (path != m.painted || m.imgCols != m.paintedCols || m.imgRows != m.paintedRows)
	if redraw {
		b.WriteString(kittyDeleteImages)
		blank := strings.Repeat(" ", m.imgCols)
		for row := 1; row <= m.height-2; row++ {
			fmt.Fprintf(&b, "\033[%d;1H%s", row, blank)
		}
		// chafa draws from wherever the cursor is left.
		b.WriteString("\033[1;1H")
	}

	_, _ = io.WriteString(m.out, b.String())

	if redraw {
		m.painted, m.paintedCols, m.paintedRows = path, m.imgCols, m.imgRows
		m.draw(path, m.imgCols, m.imgRows)
	}
}

func (m *viewModel) statusBar() string {
	if m.mode == modeDebug {
		name := ""
		if m.debugIdx < len(m.debugImgs) {
			name = m.debugImgs[m.debugIdx]
		}
		return fmt.Sprintf("\033[33m[debug %d/%d]\033[0m  %s", m.debugIdx+1, len(m.debugImgs), name)
	}

	count := fmt.Sprintf("%d/%d", m.idx+1, m.shown)
	if m.shown < len(m.images) {
		count += "+"
	}
	return fmt.Sprintf("\033[1m[%s]\033[0m  %s", count, m.images[m.idx].label())
}

func (m *viewModel) controlsBar() string {
	switch m.mode {
	case modeConfirmDelete:
		return "\033[1;31m← → cancel  d again to delete\033[0m"
	case modeDebug:
		return "\033[2m← → navigate  q/esc back\033[0m"
	}

	hints := []string{"← → navigate"}
	if m.hasDebug {
		hints = append(hints, "t debug")
	}
	hints = append(hints, "d delete", "q quit")
	return "\033[2m" + strings.Join(hints, "  ") + "\033[0m"
}

// pad squares a rendered line off to w columns so it overwrites whatever the
// previous frame left in the column.
func pad(s string, w int) string {
	if width := ansi.StringWidth(s); width > w {
		return ansi.Truncate(s, w, "")
	} else if width < w {
		return s + strings.Repeat(" ", w-width)
	}
	return s
}

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// flashDuration is how long a confirmation glyph stays on screen. The input loop
// is blocked for it, which is deliberate: there is nothing to do while a copy
// confirms, and keystrokes typed meanwhile stay buffered by the tty and are
// handled the moment it returns — so nothing is lost, it just waits.
const flashDuration = 300 * time.Millisecond

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
	st, err := settings()
	if err != nil {
		return err
	}

	outputDir := resolveOutputDir(st)
	if outputDir == "" {
		return fmt.Errorf("no output directory configured\n  evoke settings set output_path <dir>   (e.g. ~/ComfyUI/output/images)")
	}
	if info, err := os.Stat(outputDir); err != nil || !info.IsDir() {
		return fmt.Errorf("output directory is not a directory: %s", outputDir)
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
	m.copyText = copyTextToClipboard
	m.copyImage = copyImageToClipboard
	m.reveal = revealInFinder
	m.runCommand = runEvokeImageCommand
	m.sleep = time.Sleep
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
	// modePrompt collects the inputs for an image-in command on the controls row.
	// The selected image is the source, so the line holds only what would follow
	// it on a command line — selectors, paths, and quoted prompt text.
	modePrompt
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

	mode viewMode
	// promptCmd is the command modePrompt is collecting for, which is also the
	// label on the input line.
	promptCmd   string
	promptInput string
	debugImgs   []string
	debugIdx    int
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

	// A file action reports itself in one of two places. flashKey is the action
	// whose glyph is currently lit in the controls bar, painted and withdrawn
	// inside the action itself so it is transient without the read loop needing a
	// timer to wake it. errMsg is a failure, which goes to the status bar instead
	// and persists until the next event, since it has to be readable.
	flashKey string
	errMsg   string
	// noteMsg is the counterpart for a success worth reading rather than
	// flashing — an edit's queue confirmation, which has no glyph to light.
	noteMsg string

	// Seams for the terminal, so tests drive the model without one.
	draw func(path string, cols, rows int)
	size func() (int, int)
	out  io.Writer

	// Seams for the file actions, so tests drive the keys without touching the
	// clipboard or launching Finder.
	copyText  func(path string) error
	copyImage func(path string) error
	reveal    func(path string) error
	// sleep is a seam so tests do not wait out a confirmation glyph.
	sleep func(time.Duration)
	// runCommand submits an image-in command and returns what it reported. A seam
	// so tests drive the prompt without spawning a process or reaching ComfyUI.
	runCommand func(cmd, source string, args []string) (string, error)

	exitMsg string
}

func newViewModel(outputDir string, images []viewImage, pageSize int) viewModel {
	shown := len(images)
	if pageSize > 0 && pageSize < shown {
		shown = pageSize
	}
	return viewModel{
		outputDir:  outputDir,
		images:     images,
		shown:      shown,
		pageSize:   pageSize,
		meta:       map[string]comfyui.Metadata{},
		draw:       func(string, int, int) {},
		size:       func() (int, int) { return 120, 40 },
		out:        io.Discard,
		copyText:   func(string) error { return nil },
		copyImage:  func(string) error { return nil },
		reveal:     func(string) error { return nil },
		sleep:      func(time.Duration) {},
		runCommand: func(string, string, []string) (string, error) { return "", nil },
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
	// The message belongs to the frame its action painted; the next event
	// replaces it.
	m.errMsg, m.noteMsg = "", ""
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

	case modePrompt:
		switch ev.key {
		case "esc", "ctrl+c":
			m.mode, m.promptInput = modeBrowse, ""
		case "enter":
			m.submitPrompt()
		case "backspace":
			if n := len(m.promptInput); n > 0 {
				m.promptInput = m.promptInput[:n-1]
			}
		default:
			// Single-byte printable only: decodeEvents splits a chunk per byte,
			// so a multi-byte rune arrives as its individual bytes and would be
			// appended as mojibake. Dropping it beats corrupting the line.
			if len(ev.key) == 1 && ev.key[0] >= 0x20 && ev.key[0] < 0x7f {
				m.promptInput += ev.key
			}
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
		case "left", "h":
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

	case "left", "h":
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

	case "c", "C":
		if m.act(m.copyText) {
			m.confirm("c")
		}

	case "o", "O":
		// Not confirmed: Finder coming to the front is the confirmation.
		m.act(m.reveal)

	case "y", "Y":
		if m.act(m.copyImage) {
			m.confirm("y")
		}

	case "e", "E":
		m.mode, m.promptCmd, m.promptInput = modePrompt, "edit", ""

	case "p", "P":
		m.mode, m.promptCmd, m.promptInput = modePrompt, "paint", ""

	case "d", "D":
		m.mode = modeConfirmDelete
	}

	return false
}

// act runs a file action against the selected file, reporting a failure in the
// status bar. It reports whether the action ran and succeeded, so the caller
// decides whether that is worth confirming.
func (m *viewModel) act(fn func(string) error) bool {
	path := m.current()
	if path == "" {
		return false
	}
	// EVOKE_OUTPUT_DIR may be relative, and a copied path that only resolves
	// from the viewer's working directory is not much use once it is pasted.
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if err := fn(path); err != nil {
		m.errMsg = err.Error()
		return false
	}
	return true
}

// submitPrompt runs the collected command against the selected image with the
// typed line as its remaining arguments, and reports the outcome in the status
// bar. A blank line cancels: reaching the prompt by mistake should cost nothing.
//
// The input loop is blocked while this runs, as it is for a confirmation glyph.
// Keystrokes typed meanwhile stay buffered by the tty and are handled the moment
// it returns.
func (m *viewModel) submitPrompt() {
	args := splitArgs(m.promptInput)
	cmd := m.promptCmd
	m.mode, m.promptInput = modeBrowse, ""
	if len(args) == 0 {
		return
	}

	source := m.current()
	if source == "" {
		return
	}
	// EVOKE_OUTPUT_DIR may be relative, and the edit runs from wherever the
	// viewer was launched.
	if abs, err := filepath.Abs(source); err == nil {
		source = abs
	}

	if m.height > 0 {
		fmt.Fprintf(m.out, "%s\033[%d;1H\033[K\033[2msubmitting…\033[0m", cursorHide, m.height)
	}

	out, err := m.runCommand(cmd, source, args)
	if err != nil {
		m.errMsg = firstLine(err.Error())
		return
	}
	m.noteMsg = firstLine(out)
}

// firstLine reduces a command's report to something a one-row bar can hold: the
// last non-empty line, which is the outcome — the lines above it are the
// per-input resolution trace.
func firstLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// confirm lights the glyph beside an action's own hint and withdraws it, which
// is what makes it transient without the read loop needing a timer to wake it.
// Only the controls row is written, so the unchanged image pane never goes near
// chafa.
func (m *viewModel) confirm(key string) {
	m.flashKey = key
	// layout has not run on a model that was never sized.
	if m.height > 0 {
		fmt.Fprintf(m.out, "%s\033[%d;1H\033[K%s", cursorHide, m.height, m.controlsBar())
	}
	m.sleep(flashDuration)
	m.flashKey = ""
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

	// The cursor is hidden on entry, but every frame re-asserts it: painting
	// leaves the cursor wherever the last write ended, and a visible block parked
	// beside the status row is louder than anything in it. The sequence is
	// idempotent, so 4 bytes a frame is cheaper than tracking who turned it back
	// on — chafa draws into the pane and restores what it found.
	b.WriteString(cursorHide)

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
		_, _ = io.WriteString(m.out, cursorHide)
	}

	// Typing is the one time the caret earns its place on screen, so it is
	// parked at the end of the input and shown — after any redraw, since chafa
	// leaves the cursor wherever its output ended.
	if m.mode == modePrompt {
		fmt.Fprintf(m.out, "\033[%d;%dH%s", m.height, m.promptCursorCol(), cursorShow)
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
	bar := fmt.Sprintf("\033[1m[%s]\033[0m  %s", count, m.images[m.idx].label())
	if m.errMsg != "" {
		bar += fmt.Sprintf("   \033[1;31m%s\033[0m", m.errMsg)
	}
	if m.noteMsg != "" {
		bar += fmt.Sprintf("   \033[1;32m%s\033[0m", m.noteMsg)
	}
	return bar
}

func (m *viewModel) controlsBar() string {
	switch m.mode {
	case modeConfirmDelete:
		return "\033[1;31many key cancel  d again to delete\033[0m"
	case modeDebug:
		return "\033[2m← → navigate  q/esc back\033[0m"
	case modePrompt:
		return "\033[2m" + m.promptLabel() + "\033[0m" + m.promptLine()
	}

	hints := []string{"← → navigate"}
	if m.hasDebug {
		hints = append(hints, "t debug")
	}
	// The confirmable actions always render their marker column, lit or blank, so
	// a confirmation cannot shift the row right and then back.
	hints = append(hints,
		"c copy path "+m.mark("c"),
		"o finder",
		"y copy image "+m.mark("y"),
		"e edit", "p paint", "d delete", "q quit")
	return "\033[2m" + strings.Join(hints, "  ") + "\033[0m"
}

// promptLabel names the command being collected for. Its display width is the
// column the typed text starts in, which is what the caret is placed against.
func (m *viewModel) promptLabel() string {
	return m.promptCmd + " ▸ "
}

// promptLine is the typed text as it appears on the controls row, kept to the
// space left beside the label. It truncates from the left rather than the
// right: the tail is where the caret is, so that is the end worth keeping.
func (m *viewModel) promptLine() string {
	room := m.width - ansi.StringWidth(m.promptLabel()) - 1
	if room < 1 {
		return ""
	}
	line := m.promptInput
	for ansi.StringWidth(line) > room {
		line = line[1:]
	}
	return line
}

// promptCursorCol is the 1-based column the caret sits in on the controls row.
func (m *viewModel) promptCursorCol() int {
	return ansi.StringWidth(m.promptLabel()) + ansi.StringWidth(m.promptLine()) + 1
}

// mark is the single cell beside an action's hint: the glyph while that action
// is confirming, a blank otherwise. The glyph breaks out of the bar's dim
// styling and hands it back, so the rest of the row is unaffected.
func (m *viewModel) mark(key string) string {
	if m.flashKey == key {
		return "\033[0;1;32m" + flashGlyph + "\033[0;2m"
	}
	return " "
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

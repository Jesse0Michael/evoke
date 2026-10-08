package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// DiscoverCmd launches the interactive browser for every .evoke file evoke
// knows about: the current directory plus every configured source root.
func DiscoverCmd(args []string, verbose bool) int {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := runDiscover(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "evoke discover: %v\n", err)
		return 1
	}
	return 0
}

func runDiscover(_ context.Context) error {
	st, err := settings()
	if err != nil {
		return err
	}

	roots, err := discoverRoots(st)
	if err != nil {
		return err
	}

	tree := buildDiscoverTree(roots)
	if len(tree.children) == 0 {
		fmt.Println("No .evoke files found.")
		return nil
	}

	// Reading every recent image's metadata to find what used a file as a
	// source is the slow part of opening the browser, so it runs in the
	// background: the tree is navigable immediately and the image pane fills
	// in the moment the scan finishes.
	sourcesReady := make(chan sourceIndex, 1)
	if outputDir := resolveOutputDir(st); outputDir != "" {
		go func() { sourcesReady <- buildSourceIndex(outputDir) }()
	} else {
		close(sourcesReady)
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

	m := newDiscoverModel(tree, roots, nil)
	m.sourcesReady = sourcesReady
	m.out = os.Stdout
	m.draw = drawImage
	m.runTag = runEvokeTagAdd
	m.size = func() (int, int) {
		w, h, err := term.GetSize(fd)
		if err != nil || w == 0 || h == 0 {
			return 120, 40
		}
		return w, h
	}

	return m.run(os.Stdin)
}

// discoverRoots returns every root evoke discover browses: the current
// working directory (found "locally", the same word the user reaches for),
// plus every persistent source root (EVOKE_PATH, configured paths, the
// library) that resolve, image, and chat already use for everything else.
func discoverRoots(settings *Settings) ([]sourceRoot, error) {
	var roots []sourceRoot
	seen := map[string]bool{}
	if wd, err := os.Getwd(); err == nil && !isUnsafeDiscoverRoot(wd) {
		roots = append(roots, sourceRoot{Path: wd, Kind: sourceKindCurrent})
		seen[wd] = true
	}

	persistent, err := persistentRoots(settings)
	if err != nil {
		return nil, err
	}
	for _, r := range persistent {
		if seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		roots = append(roots, r)
	}
	return roots, nil
}

// isUnsafeDiscoverRoot reports whether path is the filesystem root or the
// user's home directory — the two places where recursively walking the
// current directory for .evoke files stops being "look at where I'm
// working" and turns into scanning the whole disk. discover leaves the
// current directory out of the browser in that case; the configured source
// paths and library, which the user chose deliberately, still populate it.
func isUnsafeDiscoverRoot(path string) bool {
	if path == string(filepath.Separator) {
		return true
	}
	home, err := os.UserHomeDir()
	return err == nil && path == home
}

// discoverNode is one entry in the browser's tree: a source root, a directory
// beneath one, or a .evoke file. Only directories that contain a .evoke file
// somewhere beneath them appear — evoke discover finds evoke files, not a
// general-purpose file browser.
type discoverNode struct {
	name     string
	path     string
	isDir    bool
	children []*discoverNode
}

// buildDiscoverTree walks every root and merges their top-level entries into
// one tree, so a file directly in a configured path sits beside one directly
// in the current directory — nothing in the tree names a root by its
// filesystem path.
func buildDiscoverTree(roots []sourceRoot) *discoverNode {
	top := &discoverNode{isDir: true}
	homeDir, _ := home()

	for _, root := range roots {
		files, err := walkRoot(root.Path, homeDir)
		if err != nil || len(files) == 0 {
			continue
		}
		// anchor exists only to give addDiscoverFile a base path to build
		// nested directory nodes from; its own name is never shown.
		anchor := &discoverNode{path: root.Path, isDir: true}
		for _, f := range files {
			addDiscoverFile(anchor, f)
		}
		top.children = append(top.children, anchor.children...)
	}
	sortDiscoverChildren(top)
	return top
}

// addDiscoverFile inserts one discovered file into the tree, creating
// directory nodes along its relative path as needed.
func addDiscoverFile(root *discoverNode, f discoveredFile) {
	dir := root
	rel := filepath.ToSlash(filepath.Dir(f.RelativePath))
	walked := root.path
	if rel != "." {
		for _, seg := range strings.Split(rel, "/") {
			walked = filepath.Join(walked, seg)
			dir = findOrAddDir(dir, seg, walked)
		}
	}
	dir.children = append(dir.children, &discoverNode{name: filepath.Base(f.Path), path: f.Path})
}

// findOrAddDir returns the child directory node named name under parent,
// creating it if this is the first file discovered beneath it.
func findOrAddDir(parent *discoverNode, name, path string) *discoverNode {
	for _, c := range parent.children {
		if c.isDir && c.name == name {
			return c
		}
	}
	child := &discoverNode{name: name, path: path, isDir: true}
	parent.children = append(parent.children, child)
	return child
}

// sortDiscoverChildren orders one directory's entries — subdirectories first,
// then files, each alphabetically — and recurses, so the tree reads like a
// conventional file browser rather than filesystem walk order.
func sortDiscoverChildren(n *discoverNode) {
	sort.Slice(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if a.isDir != b.isDir {
			return a.isDir
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	for _, c := range n.children {
		if c.isDir {
			sortDiscoverChildren(c)
		}
	}
}

// navFrame is one level of the browser's drill-down stack: the directory on
// screen and where the cursor and scroll offset sit within it.
type navFrame struct {
	node   *discoverNode
	idx    int
	scroll int
}

// ensureVisible adjusts the scroll offset so the cursor stays within the
// visible window of rows.
func (f *navFrame) ensureVisible(rows int) {
	if rows <= 0 {
		return
	}
	if f.idx < f.scroll {
		f.scroll = f.idx
	}
	if f.idx >= f.scroll+rows {
		f.scroll = f.idx - rows + 1
	}
}

// discoverMode selects which keys are live.
type discoverMode int

const (
	discoverBrowse discoverMode = iota
	// discoverPrompt collects the tags for `evoke tag add` on the controls row.
	discoverPrompt
)

// detailCacheLimit bounds the parsed-detail cache, matching the viewer's own
// cap: entries are small, so the limit exists only to bound a long session.
const detailCacheLimit = 512

// discoverModel holds the browser's state.
type discoverModel struct {
	roots []sourceRoot
	stack []navFrame

	mode        discoverMode
	promptInput string

	sources sourceIndex
	// sourcesReady delivers the source index once the background scan
	// finishes; nil once received (or if there was nothing to scan).
	sourcesReady chan sourceIndex

	width, height           int
	treeWidth, treeHeight   int
	rightCol, rightWidth    int
	imgRows, imgCols        int
	detailTop, detailHeight int
	details                 detailPane
	detailCache             map[string][]string

	painted     string
	paintedCols int
	paintedRows int

	errMsg  string
	noteMsg string

	draw func(path string, cols, rows int)
	size func() (int, int)
	out  io.Writer
	// runTag submits `evoke tag add` for path with tags, returning what it
	// reported. A seam so tests drive the prompt without spawning a process.
	runTag func(path string, tags []string) (string, error)
}

func newDiscoverModel(tree *discoverNode, roots []sourceRoot, sources sourceIndex) discoverModel {
	return discoverModel{
		roots:       roots,
		stack:       []navFrame{{node: tree}},
		sources:     sources,
		detailCache: map[string][]string{},
		draw:        func(string, int, int) {},
		size:        func() (int, int) { return 120, 40 },
		out:         io.Discard,
		runTag:      func(string, []string) (string, error) { return "", nil },
	}
}

// current is the directory frame on screen.
func (m *discoverModel) current() *navFrame { return &m.stack[len(m.stack)-1] }

// selected is the highlighted entry in the current directory, or nil for an
// empty one.
func (m *discoverModel) selected() *discoverNode {
	f := m.current()
	children := f.node.children
	if f.idx < 0 || f.idx >= len(children) {
		return nil
	}
	return children[f.idx]
}

// moveCursor shifts the cursor within the current directory by n, clamped to
// its bounds.
func (m *discoverModel) moveCursor(n int) {
	f := m.current()
	f.idx += n
	if last := len(f.node.children) - 1; f.idx > last {
		f.idx = last
	}
	if f.idx < 0 {
		f.idx = 0
	}
}

// enter drills into the selected directory, doing nothing for a file or an
// empty directory.
func (m *discoverModel) enter() {
	sel := m.selected()
	if sel == nil || !sel.isDir {
		return
	}
	m.stack = append(m.stack, navFrame{node: sel})
}

// back climbs out of the current directory, doing nothing at the root level.
func (m *discoverModel) back() {
	if len(m.stack) > 1 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// run is the input loop, matching the viewer's: a whole terminal read is
// decoded and applied before painting, so a held-down arrow coalesces into
// one repaint.
func (m *discoverModel) run(in io.Reader) error {
	m.pollSources()
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
		m.pollSources()
		m.layout()
		m.paint()
	}
}

// pollSources checks, without blocking, whether the background source-image
// scan has finished, adopting its result the moment it has.
func (m *discoverModel) pollSources() {
	if m.sourcesReady == nil {
		return
	}
	select {
	case s := <-m.sourcesReady:
		m.sources = s
		m.sourcesReady = nil
		// The panel text records whether there was an image above it to rule
		// off from, and that has just changed for every file the scan found
		// one for.
		m.detailCache = map[string][]string{}
		m.load()
	default:
	}
}

// handle applies one event, reporting whether the browser should exit.
func (m *discoverModel) handle(ev event) bool {
	m.errMsg, m.noteMsg = "", ""

	if ev.wheel != 0 {
		m.details.scroll(ev.wheel * wheelLines)
		return false
	}

	if m.mode == discoverPrompt {
		switch ev.key {
		case "esc", "ctrl+c":
			m.mode, m.promptInput = discoverBrowse, ""
		case "enter":
			m.submitTag()
		case "backspace":
			if n := len(m.promptInput); n > 0 {
				m.promptInput = m.promptInput[:n-1]
			}
		default:
			// Single-byte printable only, matching the viewer's prompt: a
			// multi-byte rune arrives as its individual bytes and would be
			// appended as mojibake. Dropping it beats corrupting the line.
			if len(ev.key) == 1 && ev.key[0] >= 0x20 && ev.key[0] < 0x7f {
				m.promptInput += ev.key
			}
		}
		return false
	}

	switch ev.key {
	case "q", "Q", "esc", "ctrl+c":
		return true
	case "up":
		m.moveCursor(-1)
	case "down":
		m.moveCursor(1)
	case "right", "l":
		m.enter()
	case "left", "h":
		m.back()
	case "t", "T":
		if sel := m.selected(); sel != nil && !sel.isDir {
			m.mode, m.promptInput = discoverPrompt, ""
		}
	}
	m.load()
	return false
}

// submitTag runs `evoke tag add` for the selected file with the typed
// whitespace-separated tags, reporting the outcome in the status bar. A blank
// line cancels: reaching the prompt by mistake should cost nothing.
func (m *discoverModel) submitTag() {
	tagArgs := splitArgs(m.promptInput)
	m.mode, m.promptInput = discoverBrowse, ""
	if len(tagArgs) == 0 {
		return
	}

	sel := m.selected()
	if sel == nil || sel.isDir {
		return
	}

	if m.height > 0 {
		fmt.Fprintf(m.out, "%s\033[%d;1H\033[K\033[2msubmitting…\033[0m", cursorHide, m.height)
	}

	out, err := m.runTag(sel.path, tagArgs)
	if err != nil {
		m.errMsg = firstLine(err.Error())
		return
	}
	m.noteMsg = firstLine(out)
	// The tag panel is cached per path; drop it so the new tag shows at once.
	delete(m.detailCache, sel.path)
	m.load()
}

// runEvokeTagAdd runs `evoke tag add` for path with tags as a child process,
// the same way the viewer runs edit/paint — a resolution trace on stdout
// would otherwise land in the middle of this alt-screen frame.
func runEvokeTagAdd(path string, tags []string) (string, error) {
	return runEvokeSelf(context.Background(), append([]string{"tag", "add", path}, tags...))
}

// layout divides the frame into the tree pane, the image block (when the
// selected file has one) and the scrollable detail text, and the status and
// controls rows.
func (m *discoverModel) layout() {
	w, h := m.size()
	resized := w != m.width || h != m.height
	m.width, m.height = w, h

	tree := w * 2 / 5
	if tree < 24 {
		tree = 24
	}
	if tree > 56 {
		tree = 56
	}
	right := w - tree - 3
	if right < 20 {
		right = 20
		tree = w - right - 3
	}
	m.treeWidth, m.treeHeight = tree, h-2
	m.rightCol, m.rightWidth = tree+2, right
	m.imgCols = right

	if m.currentImagePath() == "" {
		// No image: the detail text owns the whole column, so there is no gap
		// where the image would have been.
		m.imgRows = 0
		m.detailTop = 1
	} else {
		imgRows := (h - 2) / 2
		if imgRows < 6 {
			imgRows = 6
		}
		if max := h - 4; imgRows > max {
			imgRows = max
		}
		m.imgRows = imgRows
		m.detailTop = imgRows + 2
	}
	m.detailHeight = (h - 2) - m.detailTop + 1
	if m.detailHeight < 0 {
		m.detailHeight = 0
	}
	m.details.height = m.detailHeight

	if resized {
		// chafa fitted the drawn image to the old pane, and the cached panel
		// text — its wrapping and its rule — was built for the old width.
		m.painted = ""
		m.detailCache = map[string][]string{}
		fmt.Fprint(m.out, eraseScreen)
		m.load()
	}
}

// currentImagePath is the recent output image for the selected file, or ""
// when it's a directory or nothing generated ever named it as a source.
func (m *discoverModel) currentImagePath() string {
	sel := m.selected()
	if sel == nil || sel.isDir {
		return ""
	}
	return m.sources.recentFor(sel.path, m.roots)
}

// load refreshes the right panel for the current selection: local tags and
// the file itself. Cached per path, so stepping back through files already
// seen re-reads nothing.
func (m *discoverModel) load() {
	sel := m.selected()
	if sel == nil || sel.isDir {
		m.details.setLines(nil)
		return
	}

	lines, ok := m.detailCache[sel.path]
	if !ok {
		lines = discoverDetailLines(sel.path, m.roots, m.detailWidthOrDefault(), m.currentImagePath() != "")
		if len(m.detailCache) > detailCacheLimit {
			m.detailCache = map[string][]string{}
		}
		m.detailCache[sel.path] = lines
	}
	m.details.setLines(lines)
}

// detailWidthOrDefault avoids wrapping to a width of zero before the first
// layout runs.
func (m *discoverModel) detailWidthOrDefault() int {
	if m.rightWidth > 0 {
		return m.rightWidth
	}
	return 60
}

// discoverDetailLines renders the right panel's text content: the local tags
// recorded by `evoke tag` — never the file's own declared TAGS, which is not
// what those are — and the file exactly as it is on disk. The file is read,
// not parsed: no merge, no render, no filtering, so its own TAGS block and
// comments show up the way they were written.
//
// hasImage says whether the image block is occupying the rows above, which
// makes it one of the sections a rule separates.
func discoverDetailLines(path string, roots []sourceRoot, width int, hasImage bool) []string {
	b := &detailBuilder{width: width}

	// A rule separates one section from another, so it is only ever drawn
	// with a section already above it to separate from. Nothing above, no
	// rule.
	above := hasImage
	rule := func() {
		if above {
			b.lines = append(b.lines, horizontalRule(width))
		}
	}

	if t, err := tags(); err == nil {
		if local := t.Files[rootRelativeKey(path, roots)]; len(local) > 0 {
			rule()
			b.lines = append(b.lines, section("Tags"))
			b.lines = append(b.lines, b.wrap(strings.Join(local, ", "), 2)...)
			above = true
		}
	}
	rule()

	data, err := os.ReadFile(path)
	if err != nil {
		b.lines = append(b.lines, b.wrap(err.Error(), 0)...)
		return b.lines
	}
	b.lines = append(b.lines, strings.Split(strings.TrimRight(string(data), "\n"), "\n")...)
	return b.lines
}

// horizontalRule divides two of the panel's sections. It matches the
// vertical rule drawn between the panes.
func horizontalRule(width int) string {
	if width < 1 {
		return ""
	}
	return "\033[2m" + strings.Repeat("─", width) + "\033[0m"
}

// paint writes the frame: the tree pane, the image block, the scrollable
// detail text beneath it, and the bars — following the viewer's discipline of
// absolute positioning and redrawing the image only when it changes.
func (m *discoverModel) paint() {
	if m.treeWidth == 0 {
		return
	}

	var b strings.Builder
	b.WriteString(cursorHide)

	tree := m.treeLines()
	for row := 1; row <= m.treeHeight; row++ {
		line := ""
		if i := row - 1; i < len(tree) {
			line = tree[i]
		}
		fmt.Fprintf(&b, "\033[%d;1H%s", row, pad(line, m.treeWidth))
		fmt.Fprintf(&b, "\033[%d;%dH\033[2m│\033[0m", row, m.treeWidth+1)
	}

	// The image area is cleared before the detail text is painted: when the
	// selection just changed from a file with an image to one without, the
	// detail text now covers those same rows and must be what's left on top.
	imgPath := m.currentImagePath()
	redraw := imgPath != m.painted || m.imgCols != m.paintedCols || m.imgRows != m.paintedRows
	if redraw {
		b.WriteString(kittyDeleteImages)
		clearRows := m.imgRows
		if m.paintedRows > clearRows {
			clearRows = m.paintedRows
		}
		blank := strings.Repeat(" ", m.imgCols)
		for row := 1; row <= clearRows; row++ {
			fmt.Fprintf(&b, "\033[%d;%dH%s", row, m.rightCol, blank)
		}
	}

	lines := m.details.visible()
	for row := m.detailTop; row <= m.height-2; row++ {
		line := ""
		if i := row - m.detailTop; i < len(lines) {
			line = lines[i]
		}
		fmt.Fprintf(&b, "\033[%d;%dH%s", row, m.rightCol, pad(line, m.rightWidth))
	}

	fmt.Fprintf(&b, "\033[%d;1H\033[K%s", m.height-1, m.statusBar())
	fmt.Fprintf(&b, "\033[%d;1H\033[K%s", m.height, m.controlsBar())

	if redraw && imgPath != "" {
		// chafa draws from wherever the cursor is left.
		fmt.Fprintf(&b, "\033[1;%dH", m.rightCol)
	}

	_, _ = io.WriteString(m.out, b.String())

	if redraw {
		m.painted, m.paintedCols, m.paintedRows = imgPath, m.imgCols, m.imgRows
		if imgPath != "" {
			m.draw(imgPath, m.imgCols, m.imgRows)
		}
		_, _ = io.WriteString(m.out, cursorHide)
	}

	if m.mode == discoverPrompt {
		fmt.Fprintf(m.out, "\033[%d;%dH%s", m.height, m.promptCursorCol(), cursorShow)
	}
}

// treeLines renders the visible portion of the current directory: a breadcrumb
// header, then one line per entry with the cursor kept in view.
func (m *discoverModel) treeLines() []string {
	f := m.current()
	rows := m.treeHeight - 1
	f.ensureVisible(rows)

	lines := make([]string, 0, m.treeHeight)
	lines = append(lines, "\033[2m"+m.breadcrumb()+"\033[0m")

	children := f.node.children
	end := f.scroll + rows
	if end > len(children) {
		end = len(children)
	}
	for i := f.scroll; i < end; i++ {
		lines = append(lines, discoverEntryLine(children[i], i == f.idx))
	}
	return lines
}

// breadcrumb names the directories drilled into so far as a relative path —
// never a filesystem path, since the top level merges several roots into one
// listing with no single path of its own.
func (m *discoverModel) breadcrumb() string {
	parts := make([]string, 0, len(m.stack))
	for _, f := range m.stack {
		if f.node.name != "" {
			parts = append(parts, f.node.name)
		}
	}
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

// discoverEntryLine renders one tree row: a cursor marker, then the name — a
// directory colored blue with a trailing slash and child count, a file left
// unstyled (and stripped of its .evoke extension) so the two are easy to
// tell apart at a glance.
func discoverEntryLine(n *discoverNode, active bool) string {
	marker := "  "
	if active {
		marker = "\033[1;32m▸ \033[0m"
	}

	if n.isDir {
		return marker + fmt.Sprintf("\033[1;34m%s/\033[0m \033[2m(%d)\033[0m", n.name, len(n.children))
	}
	return marker + strings.TrimSuffix(n.name, ".evoke")
}

// statusBar names the current selection and any pending message.
func (m *discoverModel) statusBar() string {
	name := ""
	if sel := m.selected(); sel != nil {
		name = strings.TrimSuffix(sel.name, ".evoke")
	}
	bar := fmt.Sprintf("\033[1m%s\033[0m", name)
	if m.errMsg != "" {
		bar += fmt.Sprintf("   \033[1;31m%s\033[0m", m.errMsg)
	}
	if m.noteMsg != "" {
		bar += fmt.Sprintf("   \033[1;32m%s\033[0m", m.noteMsg)
	}
	return bar
}

// controlsBar renders the key hints, or the tag prompt while it is open.
func (m *discoverModel) controlsBar() string {
	if m.mode == discoverPrompt {
		return "\033[2m" + m.promptLabel() + "\033[0m" + m.promptLine()
	}

	hints := []string{"↑ ↓ ← → navigate"}
	if sel := m.selected(); sel != nil && !sel.isDir {
		hints = append(hints, "t tag")
	}
	hints = append(hints, "q quit")
	return "\033[2m" + strings.Join(hints, "  ") + "\033[0m"
}

// promptLabel names the command being collected for.
func (m *discoverModel) promptLabel() string { return "tag ▸ " }

// promptLine is the typed text as it appears on the controls row, truncated
// from the left so the caret's end stays visible, matching the viewer.
func (m *discoverModel) promptLine() string {
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
func (m *discoverModel) promptCursorCol() int {
	return ansi.StringWidth(m.promptLabel()) + ansi.StringWidth(m.promptLine()) + 1
}

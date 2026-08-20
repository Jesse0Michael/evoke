package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/jesse0michael/evoke/internal/generate/comfyui"
)

// Styling is written as escape sequences rather than through lipgloss. The
// viewer owns the terminal — it runs without a frame renderer and hands the
// screen to chafa — and lipgloss's renderer probes the terminal on first use
// (OSC 11 background colour, then a cursor position report), reading the reply
// from the same stdin Bubble Tea's input reader is draining. The two block on
// each other and nothing is ever painted. Nothing in the viewer may talk to the
// terminal behind the paint loop's back.
func bold(s string) string    { return "\033[1m" + s + "\033[0m" }
func section(s string) string { return "\033[1;33m" + s + "\033[0m" }

// labelWidth is the gutter every field label is padded into, so values line up
// down the panel.
const labelWidth = 9

// detailBuilder accumulates the details panel. Every value is wrapped to the
// panel width rather than truncated: the panel scrolls, so nothing has to fit.
type detailBuilder struct {
	width int
	lines []string
}

func (b *detailBuilder) blank() {
	// Collapse leading and doubled separators so sections that render nothing
	// leave no gap behind.
	if len(b.lines) == 0 || b.lines[len(b.lines)-1] == "" {
		return
	}
	b.lines = append(b.lines, "")
}

func (b *detailBuilder) section(title string) {
	b.blank()
	b.lines = append(b.lines, section(title))
}

func (b *detailBuilder) wrap(text string, indent int) []string {
	width := b.width - indent
	if width < 8 {
		width = 8
	}
	// Word-wrapped first, then hard-wrapped: a single token longer than the
	// column (a checkpoint filename) has to break somewhere, or it pushes the
	// details pane over the image.
	wrapped := ansi.Wrap(ansi.Wordwrap(text, width, " -/,"), width, "")
	pad := strings.Repeat(" ", indent)
	out := strings.Split(wrapped, "\n")
	for i := range out {
		out[i] = pad + out[i]
	}
	return out
}

// field renders "Label   value", continuation lines hanging under the value.
func (b *detailBuilder) field(label, value string) {
	if value == "" {
		return
	}
	lines := b.wrap(value, labelWidth+1)
	lines[0] = bold(fmt.Sprintf("%-*s", labelWidth, label)) + " " +
		strings.TrimPrefix(lines[0], strings.Repeat(" ", labelWidth+1))
	b.lines = append(b.lines, lines...)
}

// block renders a labelled paragraph — prompts, which are long enough that a
// hanging indent wastes the panel.
func (b *detailBuilder) block(label, text string) {
	if text == "" {
		return
	}
	b.blank()
	b.lines = append(b.lines, bold(label))
	b.lines = append(b.lines, b.wrap(text, 2)...)
}

// sampling renders the fields every pass shares, so the base, the upscale, and
// each detailer read identically.
func (b *detailBuilder) sampling(sampler, scheduler string, steps int, cfg, denoise float64, seed uint64) {
	if sampler != "" {
		if scheduler != "" {
			sampler += " / " + scheduler
		}
		b.field("Sampler", sampler)
	}
	if steps > 0 {
		b.field("Steps", fmt.Sprintf("%d", steps))
	}
	if cfg > 0 {
		b.field("CFG", fmt.Sprintf("%.1f", cfg))
	}
	if denoise > 0 {
		b.field("Denoise", fmt.Sprintf("%.2f", denoise))
	}
	if seed != 0 {
		b.field("Seed", fmt.Sprintf("%d", seed))
	}
}

// detailPane is the scrollable right-hand column: rendered lines plus the
// offset the wheel moves.
type detailPane struct {
	lines  []string
	offset int
	height int
}

// setLines replaces the content, returning to the top — a new image starts at
// the top of its own record.
func (d *detailPane) setLines(lines []string) {
	d.lines = lines
	d.offset = 0
}

// scroll moves by n lines, clamped so the last line cannot be scrolled past.
func (d *detailPane) scroll(n int) {
	max := len(d.lines) - d.height
	if max < 0 {
		max = 0
	}
	d.offset += n
	if d.offset > max {
		d.offset = max
	}
	if d.offset < 0 {
		d.offset = 0
	}
}

// visible returns the lines currently in the pane.
func (d *detailPane) visible() []string {
	if d.offset >= len(d.lines) {
		return nil
	}
	end := d.offset + d.height
	if end > len(d.lines) {
		end = len(d.lines)
	}
	return d.lines[d.offset:end]
}

// formatDetails renders the whole generation record for the details panel.
func formatDetails(m comfyui.Metadata, path string, width int) []string {
	b := &detailBuilder{width: width}

	if path != "" {
		// OSC 8 hyperlink, kept to one line: the visible text is elided from the
		// left so the file name survives, and wrapping would split the escape.
		display := path
		if size := width - labelWidth - 1; size > 1 && len(display) > size {
			display = "…" + display[len(display)-size+1:]
		}
		b.lines = append(b.lines, bold(fmt.Sprintf("%-*s", labelWidth, "File"))+" "+
			fmt.Sprintf("\033]8;;file://%s\033\\%s\033]8;;\033\\", path, display))
	}
	if m.Width > 0 && m.Height > 0 {
		b.field("Size", fmt.Sprintf("%dx%d", m.Width, m.Height))
	}
	if m.FileSize > 0 {
		b.field("Disk", formatBytes(m.FileSize))
	}
	if len(m.Inputs) > 0 {
		b.field("Inputs", strings.Join(m.Inputs, " "))
	}

	if len(m.Sources) > 0 {
		b.section("Sources")
		for _, src := range m.Sources {
			b.lines = append(b.lines, b.wrap(filepath.Base(src), 2)...)
		}
	}

	b.blank()
	b.field("Model", m.Model)
	b.sampling(m.Sampler, m.Scheduler, m.Steps, m.CFG, m.Denoise, m.Seed)
	for _, l := range m.LoRAs {
		b.field("LoRA", fmt.Sprintf("%s (%.1f/%.1f)", l.Name, l.Model, l.Clip))
	}

	b.block("Positive", m.Positive)
	b.block("Negative", m.Negative)

	if m.Upscale.Model != "" || m.Upscale.Seed != 0 {
		b.section("upscale")
		b.field("Model", m.Upscale.Model)
		if m.Upscale.Factor > 0 {
			b.field("Factor", fmt.Sprintf("%.1fx", m.Upscale.Factor))
		}
		b.sampling(m.Upscale.Sampler, m.Upscale.Scheduler, m.Upscale.Steps, m.Upscale.CFG, m.Upscale.Denoise, m.Upscale.Seed)
	}

	for _, d := range m.Detailers {
		b.section(d.Name)
		b.sampling(d.Sampler, d.Scheduler, d.Steps, d.CFG, d.Denoise, d.Seed)
		b.block("+", d.Positive)
		b.block("-", d.Negative)
	}

	return b.lines
}

// formatStageDetails renders the single pass a debug frame came out of, matched
// by the stage name the generator built the filename from.
func formatStageDetails(m comfyui.Metadata, filename string, width int) []string {
	b := &detailBuilder{width: width}

	for _, d := range m.Detailers {
		if !strings.Contains(filename, "_"+d.Name+"_") {
			continue
		}
		b.field("Stage", d.Name)
		b.sampling(d.Sampler, d.Scheduler, d.Steps, d.CFG, d.Denoise, d.Seed)
		b.block("Positive", d.Positive)
		b.block("Negative", d.Negative)
		return b.lines
	}

	if strings.Contains(filename, "_upscale") {
		b.field("Stage", "upscale")
		b.field("Model", m.Upscale.Model)
		if m.Upscale.Factor > 0 {
			b.field("Factor", fmt.Sprintf("%.1fx", m.Upscale.Factor))
		}
		b.sampling(m.Upscale.Sampler, m.Upscale.Scheduler, m.Upscale.Steps, m.Upscale.CFG, m.Upscale.Denoise, m.Upscale.Seed)
		return b.lines
	}

	b.field("Stage", "base")
	b.field("Model", m.Model)
	b.sampling(m.Sampler, m.Scheduler, m.Steps, m.CFG, m.Denoise, m.Seed)
	b.block("Positive", m.Positive)
	b.block("Negative", m.Negative)
	return b.lines
}

func formatBytes(b int64) string {
	const (
		gb = 1024 * 1024 * 1024
		mb = 1024 * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.0f MB", float64(b)/float64(mb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

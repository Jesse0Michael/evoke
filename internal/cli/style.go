package cli

import (
	"io"
	"os"
	"regexp"

	"golang.org/x/term"
)

// Inline patterns applied to a completed (non-streamed) reply. Emphasis requires
// a non-space adjacent to the marker so bullets ("* item") and spaced arithmetic
// ("2 * 3") are not mistaken for emphasis. Bold is resolved first, so italic only
// ever sees genuine single-marker spans. Quoted spans (straight and curly) are
// bolded so dialogue stands out from narration/action text.
var (
	mdBold       = regexp.MustCompile(`\*\*(\S(?:.*?\S)?)\*\*`)
	mdItalic     = regexp.MustCompile(`\*(\S(?:.*?\S)?)\*`)
	mdQuote      = regexp.MustCompile(`"[^"]+"`)
	mdCurlyQuote = regexp.MustCompile(`“[^”]+”`)
)

// chatStyle applies optional ANSI styling to interactive chat output. All escape
// codes are centralized here so the chat loop stays free of terminal concerns.
// When styling is disabled, every method returns its input unchanged.
type chatStyle struct{ on bool }

// newChatStyle decides whether to emit ANSI styles. An explicit preference wins;
// otherwise styling is enabled only when out is a terminal and NO_COLOR is unset.
func newChatStyle(out io.Writer, pref *bool) chatStyle {
	if pref != nil {
		return chatStyle{on: *pref}
	}
	f, ok := out.(*os.File)
	on := ok && term.IsTerminal(int(f.Fd())) && os.Getenv("NO_COLOR") == ""
	return chatStyle{on: on}
}

func (s chatStyle) wrap(code, text string) string {
	if !s.on || text == "" {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

// user styles the "You" speaker label.
func (s chatStyle) user(text string) string { return s.wrap("36", text) }

// character styles the assistant/character speaker label.
func (s chatStyle) character(text string) string { return s.wrap("1;35", text) }

// dim styles secondary output such as diagnostics and hints.
func (s chatStyle) dim(text string) string { return s.wrap("2", text) }

// errorText styles error lines.
func (s chatStyle) errorText(text string) string { return s.wrap("31", text) }

// markdown renders minimal inline emphasis (**bold**, *italic*) to ANSI on a
// complete string. It is applied only to non-streamed replies, where the whole
// message is in hand, so no streaming markdown parser is needed. Returns the
// text unchanged when styling is disabled.
func (s chatStyle) markdown(text string) string {
	if !s.on {
		return text
	}
	// Use attribute-specific resets (bold-off 22, italic-off 23) rather than a
	// full reset (0) so nesting — e.g. an italic action inside a bold quote —
	// doesn't clear the outer style. Quotes are applied last so the emphasis they
	// wrap is preserved.
	text = mdBold.ReplaceAllString(text, "\033[1m$1\033[22m")
	text = mdItalic.ReplaceAllString(text, "\033[3m$1\033[23m")
	text = mdQuote.ReplaceAllString(text, "\033[1m${0}\033[22m")
	text = mdCurlyQuote.ReplaceAllString(text, "\033[1m${0}\033[22m")
	return text
}

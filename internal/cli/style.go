package cli

import (
	"io"
	"os"

	"golang.org/x/term"
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

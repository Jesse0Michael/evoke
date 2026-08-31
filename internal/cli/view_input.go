package cli

import "strconv"

// event is one decoded terminal input: a named key, or a wheel notch.
type event struct {
	key   string
	wheel int // -1 up, +1 down; 0 when this is a key event
}

// decodeEvents parses a chunk read from the terminal into events.
//
// A chunk is decoded whole rather than a byte at a time, which is what lets a
// lone ESC be told apart from the start of a sequence: terminals emit an escape
// sequence in a single write, so a trailing ESC with nothing after it is the
// Escape key. Decoding per chunk also coalesces a held-down arrow into one
// repaint instead of one per repeat.
func decodeEvents(b []byte) []event {
	var events []event

	for len(b) > 0 {
		if b[0] != 0x1b {
			events = append(events, event{key: keyName(b[0])})
			b = b[1:]
			continue
		}

		if len(b) == 1 {
			events = append(events, event{key: "esc"})
			return events
		}

		if b[1] != '[' {
			// Alt-modified key; the viewer binds none, so drop the pair.
			b = b[2:]
			continue
		}

		if len(b) > 2 && b[2] == '<' {
			ev, n := decodeMouse(b)
			if n == 0 {
				return events
			}
			if ev.wheel != 0 {
				events = append(events, ev)
			}
			b = b[n:]
			continue
		}

		if len(b) < 3 {
			return events
		}
		if name := arrowName(b[2]); name != "" {
			events = append(events, event{key: name})
		}
		b = b[3:]
	}

	return events
}

func keyName(c byte) string {
	switch c {
	case 0x03:
		return "ctrl+c"
	case 0x0d:
		return "enter"
	// Terminals in raw mode send DEL for the backspace key; BS arrives from
	// ctrl+h and from a few terminals configured the other way round.
	case 0x08, 0x7f:
		return "backspace"
	case ' ':
		return " "
	}
	return string(rune(c))
}

func arrowName(c byte) string {
	switch c {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'C':
		return "right"
	case 'D':
		return "left"
	}
	return ""
}

// decodeMouse parses an SGR mouse report (ESC [ < b ; x ; y M|m), returning the
// event and how many bytes it consumed. Buttons 64 and 65 are the wheel; every
// other report is consumed and ignored.
func decodeMouse(b []byte) (event, int) {
	end := -1
	for i := 3; i < len(b); i++ {
		if b[i] == 'M' || b[i] == 'm' {
			end = i
			break
		}
	}
	if end < 0 {
		return event{}, 0
	}

	// Only a press (M) scrolls; the matching release is consumed silently.
	button := 0
	for i := 3; i < end && b[i] != ';'; i++ {
		digit, err := strconv.Atoi(string(b[i]))
		if err != nil {
			return event{}, end + 1
		}
		button = button*10 + digit
	}

	if b[end] == 'M' {
		switch button {
		case 64:
			return event{wheel: -1}, end + 1
		case 65:
			return event{wheel: 1}, end + 1
		}
	}
	return event{}, end + 1
}

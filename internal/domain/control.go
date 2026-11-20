package domain

import "strings"

// The control characters below are not text: a terminal interprets them. ESC
// (0x1B) introduces ANSI and OSC sequences, CSI (0x9B) does so on its own in
// 8-bit mode, and the rest move the cursor, clear the screen or rewrite the
// title. Any value that comes from outside the process (the ssh configuration,
// the stderr of a master, the tunnel manifest) must lose them before it is
// rendered or handed to another binary.

// HasControlChars reports whether s contains a C0, C1 or DEL control character.
// It is the check the validators use to fail closed.
func HasControlChars(s string) bool {
	for _, r := range s {
		if isControl(r) {
			return true
		}
	}

	return false
}

// StripControlChars removes every control character from s except the tab, which
// is meaningful alignment inside a configuration or a log line. It is the
// rendering counterpart of HasControlChars: the text survives, the bytes the
// terminal would execute do not.
func StripControlChars(s string) string {
	if !HasControlChars(s) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		if isControl(r) && r != '\t' {
			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

// StripControlLines applies StripControlChars to every line of a multi-line text,
// which is how the configuration and the tunnel logs are shown.
func StripControlLines(lines []string) []string {
	out := make([]string, len(lines))
	for index, line := range lines {
		out[index] = StripControlChars(line)
	}

	return out
}

// isControl reports whether r is a C0, C1 or DEL control character.
func isControl(r rune) bool {
	return r < ' ' || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

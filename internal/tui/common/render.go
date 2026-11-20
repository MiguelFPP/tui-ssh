// Package common holds the shared rendering primitives of the TUI: the cell
// arithmetic every panel needs, the line helpers and the confirm and prompt
// modals. Nothing here performs I/O.
package common

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ResetStyle closes the attributes a clipped panel row may leave open.
const ResetStyle = "\x1b[m"

// ColumnSeparator joins the fields of a header, a footer or a status line.
const ColumnSeparator = " · "

// Fit splits a rendered view into exactly height rows, each padded to width cells.
func Fit(view string, width, height int) []string {
	lines := strings.Split(view, "\n")
	out := make([]string, 0, max(height, 0))

	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}

		out = append(out, Pad(line, width))
	}

	return out
}

// Pad clips line to width cells and fills the rest with spaces.
func Pad(line string, width int) string {
	if width <= 0 {
		return ""
	}

	line = Clip(line, width)
	if gap := width - lipgloss.Width(line); gap > 0 {
		return line + strings.Repeat(" ", gap)
	}

	return line
}

// PadRight pads a plain, unstyled string to width runes with spaces.
func PadRight(text string, width int) string {
	if width <= 0 {
		return text
	}

	gap := width - len([]rune(text))
	if gap <= 0 {
		return text
	}

	return text + strings.Repeat(" ", gap)
}

// Clip cuts a styled string to width visible cells, keeping the ANSI intact.
func Clip(s string, width int) string {
	if width <= 0 {
		return ""
	}

	var b strings.Builder
	visible := 0
	inEscape := false

	for _, r := range s {
		if r == 0x1b {
			inEscape = true
			b.WriteRune(r)

			continue
		}
		if inEscape {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}

			continue
		}
		if visible >= width {
			break
		}

		b.WriteRune(r)
		visible++
	}

	return b.String()
}

// Tail returns an ANSI line from its n-th visible cell, keeping the escapes of
// that cell.
func Tail(s string, cells int) string {
	if cells <= 0 {
		return s
	}

	var pending strings.Builder
	visible := 0
	inEscape := false

	for i, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
			pending.WriteRune(r)
		case inEscape:
			pending.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		case visible == cells:
			return pending.String() + s[i:]
		default:
			visible++
			pending.Reset()
		}
	}

	return ""
}

// Truncate shortens a plain string to width runes, appending an ellipsis.
func Truncate(text string, width int) string {
	if width <= 0 {
		return text
	}

	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}

	return string(runes[:width-1]) + "…"
}

// InnerCells returns the cells left by the frame of a box, never negative.
func InnerCells(size int) int {
	return max(size-2, 0)
}

// Clamp keeps value inside the inclusive range, tolerating a reversed range.
func Clamp(value, low, high int) int {
	if high < low {
		return low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}

	return value
}

// PadLines fills lines up to height, so a panel body never changes size.
func PadLines(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}

	return lines
}

// Window returns the visible slice of rows given an offset and a height.
func Window[T any](rows []T, offset, height int) []T {
	if height <= 0 || offset < 0 || offset >= len(rows) {
		return nil
	}

	end := min(offset+height, len(rows))

	return rows[offset:end]
}

package common

import (
	"strings"
	"testing"
)

func TestPadAndClip(t *testing.T) {
	if got := Pad("ab", 5); got != "ab   " {
		t.Errorf("Pad = %q, want two spaces of padding", got)
	}
	if got := Pad("abcdef", 3); got != "abc" {
		t.Errorf("Pad clip = %q, want abc", got)
	}
	if got := Pad("ab", 0); got != "" {
		t.Errorf("Pad with a zero width = %q, want empty", got)
	}
	if got := PadRight("ab", 4); got != "ab  " {
		t.Errorf("PadRight = %q", got)
	}
	if got := PadRight("abcdef", 3); got != "abcdef" {
		t.Errorf("PadRight must never truncate, got %q", got)
	}
}

func TestClipKeepsANSI(t *testing.T) {
	styled := "\x1b[31mabcdef\x1b[m"

	got := Clip(styled, 3)
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("Clip dropped the opening escape: %q", got)
	}
	if strings.Contains(got, "def") {
		t.Errorf("Clip kept visible text past the width: %q", got)
	}
}

func TestTailStartsAtTheGivenCell(t *testing.T) {
	got := Tail("abcdef", 3)
	if got != "def" {
		t.Errorf("Tail = %q, want def", got)
	}

	if got := Tail("abc", 0); got != "abc" {
		t.Errorf("Tail(0) = %q, want the whole string", got)
	}
	if got := Tail("abc", 10); got != "" {
		t.Errorf("Tail past the end = %q, want empty", got)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  string
	}{
		{in: "short", width: 10, want: "short"},
		{in: "truncated", width: 5, want: "trun…"},
		{in: "ab", width: 1, want: "…"},
		{in: "ab", width: 0, want: "ab"},
	}

	for _, tc := range cases {
		if got := Truncate(tc.in, tc.width); got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

func TestFit(t *testing.T) {
	rows := Fit("a\nb", 3, 4)

	if len(rows) != 4 {
		t.Fatalf("Fit returned %d rows, want 4", len(rows))
	}
	if rows[0] != "a  " {
		t.Errorf("rows[0] = %q", rows[0])
	}
	if rows[3] != "   " {
		t.Errorf("rows[3] = %q, want the padding row", rows[3])
	}
}

func TestInnerCells(t *testing.T) {
	cases := map[int]int{10: 8, 3: 1, 2: 0, 1: 0, 0: 0}
	for in, want := range cases {
		if got := InnerCells(in); got != want {
			t.Errorf("InnerCells(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestClamp(t *testing.T) {
	cases := []struct {
		value, low, high, want int
	}{
		{value: 5, low: 0, high: 10, want: 5},
		{value: -1, low: 0, high: 10, want: 0},
		{value: 11, low: 0, high: 10, want: 10},
		{value: 5, low: 10, high: 0, want: 10},
	}

	for _, tc := range cases {
		if got := Clamp(tc.value, tc.low, tc.high); got != tc.want {
			t.Errorf("Clamp(%d, %d, %d) = %d, want %d", tc.value, tc.low, tc.high, got, tc.want)
		}
	}
}

func TestPadLines(t *testing.T) {
	got := PadLines([]string{"a"}, 3)
	if len(got) != 3 || got[1] != "" || got[2] != "" {
		t.Errorf("PadLines = %v, want three rows", got)
	}
}

func TestWindow(t *testing.T) {
	rows := []int{1, 2, 3, 4, 5}

	if got := Window(rows, 1, 2); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("Window(1, 2) = %v, want [2 3]", got)
	}
	if got := Window(rows, 4, 3); len(got) != 1 || got[0] != 5 {
		t.Errorf("Window past the end = %v, want [5]", got)
	}
	if got := Window(rows, 9, 2); got != nil {
		t.Errorf("Window out of range = %v, want nil", got)
	}
	if got := Window(rows, 0, 0); got != nil {
		t.Errorf("Window with a zero height = %v, want nil", got)
	}
}

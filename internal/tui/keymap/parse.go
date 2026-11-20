package keymap

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Canonical names and modifiers understood by the parser.
const (
	keySpace    = "space"
	keyShiftTab = "shift+tab"
	modCtrl     = "ctrl+"
	modAlt      = "alt+"
	modShift    = "shift+"
)

// keyAliases maps the accepted long names to their canonical short form.
var keyAliases = map[string]string{
	"escape":   "esc",
	"return":   "enter",
	"pageup":   "pgup",
	"pagedown": "pgdown",
}

// namedKeys are the named keys ParseKey accepts.
var namedKeys = map[string]struct{}{
	"up":        {},
	"down":      {},
	"left":      {},
	"right":     {},
	"home":      {},
	"end":       {},
	"pgup":      {},
	"pgdown":    {},
	"enter":     {},
	"esc":       {},
	"tab":       {},
	"space":     {},
	"backspace": {},
	"delete":    {},
	"insert":    {},
	"f1":        {},
	"f2":        {},
	"f3":        {},
	"f4":        {},
	"f5":        {},
	"f6":        {},
	"f7":        {},
	"f8":        {},
	"f9":        {},
	"f10":       {},
	"f11":       {},
	"f12":       {},
}

// shiftArrows are the only shift+ combinations accepted besides shift+tab.
var shiftArrows = map[string]struct{}{
	modShift + "left":  {},
	modShift + "right": {},
}

// CanonicalKey normalizes raw into the canonical form used by the catalog and
// by the key strings of Bubble Tea. It never fails: unrecognized input falls
// back to its lower-cased form, so Match simply misses it.
func CanonicalKey(raw string) string {
	// A lone space is the space key; any other surrounding spacing is trimmed.
	if raw == " " {
		return keySpace
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if alias, ok := keyAliases[strings.ToLower(trimmed)]; ok {
		return alias
	}

	if modifier, r, ok := splitModifiedRune(trimmed); ok {
		return modifier + string(unicode.ToLower(r))
	}

	// Shift arrows normalize explicitly before the generic fallback.
	if isShiftArrow(trimmed) {
		return strings.ToLower(trimmed)
	}

	// A single rune keeps its case: "E" and "e" are different keys.
	if utf8.RuneCountInString(trimmed) == 1 {
		return trimmed
	}

	return strings.ToLower(trimmed)
}

// ParseKey validates raw and returns its canonical form. It accepts a single
// printable rune, the named keys, ctrl+/alt+ plus one printable rune, shift+tab
// and the shift+left / shift+right arrows. Reserved keys are rejected later, in
// Apply, not here.
func ParseKey(raw string) (string, error) {
	canonical := CanonicalKey(raw)

	switch {
	case canonical == "":
		return "", fmt.Errorf("keymap: tecla vacía")
	case isNamedKey(canonical), isShiftArrow(canonical), canonical == keyShiftTab:
		return canonical, nil
	}

	if _, _, ok := splitModifiedRune(canonical); ok {
		return canonical, nil
	}

	if utf8.RuneCountInString(canonical) == 1 {
		r, _ := utf8.DecodeRuneInString(canonical)
		if unicode.IsPrint(r) {
			return canonical, nil
		}
	}

	return "", fmt.Errorf("keymap: tecla inválida %q", raw)
}

// splitModifiedRune splits "ctrl+x" or "alt+x" when, and only when, the tail is
// exactly one printable rune.
func splitModifiedRune(raw string) (string, rune, bool) {
	lower := strings.ToLower(raw)

	for _, modifier := range []string{modCtrl, modAlt} {
		if !strings.HasPrefix(lower, modifier) {
			continue
		}

		tail := raw[len(modifier):]
		if utf8.RuneCountInString(tail) != 1 {
			return "", 0, false
		}

		r, _ := utf8.DecodeRuneInString(tail)
		if !unicode.IsPrint(r) {
			return "", 0, false
		}

		return modifier, r, true
	}

	return "", 0, false
}

func isNamedKey(key string) bool {
	_, ok := namedKeys[key]

	return ok
}

// isShiftArrow accepts only shift+left and shift+right, never a generic shift+x.
func isShiftArrow(key string) bool {
	_, ok := shiftArrows[strings.ToLower(key)]

	return ok
}

package hostlist

import (
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

// TestViewDoesNotEmitOSCFromAlias covers the terminal injection vector: an alias of
// the ssh configuration is attacker-influenced text (the file may come from a
// repository, a dotfile bundle or another tenant) and it reaches the terminal
// verbatim. An alias carrying an OSC sequence must never be emitted as such,
// because the terminal would execute it (title change, clipboard write, cursor
// movement).
func TestViewDoesNotEmitOSCFromAlias(t *testing.T) {
	payload := "evil\x1b]52;c;SGVsbG8=\x07"

	m := newList(domain.Host{Alias: payload})
	view := m.View()

	for _, bad := range []string{"\x1b]52", "\x07"} {
		if strings.Contains(view, bad) {
			t.Fatalf("View emitted the payload %q: %q", bad, view)
		}
	}
	if !strings.Contains(view, "evil") {
		t.Fatalf("View dropped the alias entirely instead of sanitizing it: %q", view)
	}
}

// TestViewDoesNotEmitControlSequencesFromTag covers the same vector from a tag,
// which the list renders as a chip and as a grouped header.
func TestViewDoesNotEmitControlSequencesFromTag(t *testing.T) {
	m := newList(domain.Host{Alias: "web", Tags: []string{"prod\x1b[2J"}})

	if view := m.View(); strings.Contains(view, "\x1b[2J") {
		t.Fatalf("View emitted a screen-clear sequence from a tag: %q", view)
	}
}

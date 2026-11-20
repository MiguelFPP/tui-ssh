package hostdetail

import (
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

// TestViewDoesNotEmitControlSequences covers the detail panel, which renders the
// alias, the tags, the declared directives and the resolved values verbatim. Any of
// them can come from the ssh configuration or from `ssh -G`, so a control character
// must not reach the terminal as a sequence.
func TestViewDoesNotEmitControlSequences(t *testing.T) {
	m := New(keymap.Default())
	m.SetBounds(60, 20)
	m.SetHost(domain.Host{
		Alias: "web\x1b]52;c;SGVsbG8=\x07",
		Tags:  []string{"prod\x1b[2J"},
		Declared: []domain.Directive{
			{Key: "hostname", Value: "10.0.0.1\x1b[31m"},
			{Key: "user", Value: "deployer\u009b2J"},
		},
	})

	view := m.View()
	for _, bad := range []string{"\x1b]52", "\x07", "\x1b[2J", "\u009b"} {
		if strings.Contains(view, bad) {
			t.Fatalf("View emitted %q: %q", bad, view)
		}
	}
}

// TestViewDoesNotEmitControlSequencesFromResolution covers the same panel once the
// authoritative resolution is applied: `ssh -G` output is external too.
func TestViewDoesNotEmitControlSequencesFromResolution(t *testing.T) {
	m := New(keymap.Default())
	m.SetBounds(60, 20)
	m.SetHost(domain.Host{Alias: "web"})
	m.SetResolved(domain.Resolved{
		Host: domain.ResolvedHost{
			Alias:    "web\x1b]0;x\x07",
			HostName: "10.0.0.1\x1b[2J",
			User:     "deployer",
			Port:     22,
		},
	})

	view := m.View()
	for _, bad := range []string{"\x1b]0", "\x07", "\x1b[2J"} {
		if strings.Contains(view, bad) {
			t.Fatalf("View emitted %q: %q", bad, view)
		}
	}
}

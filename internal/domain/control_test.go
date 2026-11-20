package domain

import (
	"strings"
	"testing"
)

// TestStripControlCharsKeepsTextAndTabs covers the sanitizer the adapters use: the
// bytes a terminal would execute go away, the printable text and the tabs that
// align a configuration or a log line stay.
func TestStripControlCharsKeepsTextAndTabs(t *testing.T) {
	raw := "Host\tweb\x1b]52;c;SGVsbG8=\x07\u009b2J normal"

	got := StripControlChars(raw)

	for _, bad := range []string{"\x1b", "\x07", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("StripControlChars kept %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, "Host\tweb") {
		t.Errorf("StripControlChars dropped the tab or the text: %q", got)
	}
	if !strings.HasSuffix(got, " normal") {
		t.Errorf("StripControlChars dropped the trailing text: %q", got)
	}
}

// TestStripControlCharsIsIdentityForPlainText keeps the fast path honest: a clean
// string must come back unchanged and must not be treated as unsafe.
func TestStripControlCharsIsIdentityForPlainText(t *testing.T) {
	const plain = "gd-vps  #prod  用户"

	if got := StripControlChars(plain); got != plain {
		t.Errorf("StripControlChars(%q) = %q, want it unchanged", plain, got)
	}
	if HasControlChars(plain) {
		t.Errorf("HasControlChars(%q) = true, want false", plain)
	}
}

// TestStripControlLinesAppliesToEveryLine covers the multi-line entry point used by
// the config viewer and the tunnel logs.
func TestStripControlLinesAppliesToEveryLine(t *testing.T) {
	got := StripControlLines([]string{"ok", "bad\x1b[2J", "\u009d52;c;x"})

	for index, line := range got {
		if HasControlChars(line) {
			t.Errorf("line %d kept a control character: %q", index, line)
		}
	}
	if got[1] != "bad[2J" {
		t.Errorf("line 1 = %q, want the escape byte removed only", got[1])
	}
}

// TestValidateForwardSpecRejectsControls covers the forward fields: they are
// rendered into the -L/-R/-D argument, so a control character must not survive.
func TestValidateForwardSpecRejectsControls(t *testing.T) {
	cases := []ForwardSpec{
		{
			Direction: ForwardLocal,
			Bind:      BindAddr{Addr: "127.0.0.1", Port: 8080},
			Target:    TargetAddr{Host: "db\x1b[2J", Port: 5432},
		},
		{
			Direction: ForwardLocal,
			Bind:      BindAddr{Addr: "127.0.0.1\x1b", Port: 8080},
			Target:    TargetAddr{Host: "db", Port: 5432},
		},
		{
			Name:      "pg\x1b]0;x\x07",
			Direction: ForwardLocal,
			Bind:      BindAddr{Addr: "127.0.0.1", Port: 8080},
			Target:    TargetAddr{Host: "db", Port: 5432},
		},
	}

	for index, spec := range cases {
		if err := spec.Validate(); err == nil {
			t.Errorf("case %d: Validate() = nil, want an error for %+v", index, spec)
		}
	}
}

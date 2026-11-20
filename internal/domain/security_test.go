package domain

import "testing"

// TestNormalizeTagRejectsC1Controls covers the gap left by the C0-only check: the
// range U+0080..U+009F is rendered as terminal control bytes in 8-bit mode, and
// U+009B is CSI on its own, so a tag carrying it would reach the terminal as an
// escape introducer instead of text.
func TestNormalizeTagRejectsC1Controls(t *testing.T) {
	for _, raw := range []string{"prod\u009b2J", "prod\u0085evil", "prod\u009d52;c;SGVsbG8="} {
		if tag, err := NormalizeTag(raw); err == nil {
			t.Errorf("NormalizeTag(%q) = %q, want an error", raw, tag)
		}
	}
}

// TestValidateAliasRejectsC1Controls covers the same gap in the alias validator,
// which is the last gate before an alias becomes an argument of ssh.
func TestValidateAliasRejectsC1Controls(t *testing.T) {
	for _, raw := range []string{"gd\u009bvps", "gd\u0085vps", "\u009b"} {
		if err := ValidateAlias(raw); err == nil {
			t.Errorf("ValidateAlias(%q) = nil, want an error", raw)
		}
	}
}

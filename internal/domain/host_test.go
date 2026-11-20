package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewPattern(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		value   string
		wild    bool
		negated bool
	}{
		{name: "literal", raw: "web", value: "web"},
		{name: "star", raw: "prod-*", value: "prod-*", wild: true},
		{name: "question", raw: "db?", value: "db?", wild: true},
		{name: "negated", raw: "!bastion", value: "bastion", negated: true},
		{name: "spaces trimmed", raw: "  web  ", value: "web"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewPattern(tc.raw)
			if got.Value != tc.value || got.Wild != tc.wild || got.Negated != tc.negated {
				t.Fatalf("NewPattern(%q) = %+v, want value=%q wild=%v negated=%v",
					tc.raw, got, tc.value, tc.wild, tc.negated)
			}
		})
	}
}

func TestPatternMatch(t *testing.T) {
	cases := []struct {
		pattern string
		alias   string
		want    bool
	}{
		{pattern: "web", alias: "web", want: true},
		{pattern: "web", alias: "web2", want: false},
		{pattern: "prod-*", alias: "prod-db", want: true},
		{pattern: "prod-*", alias: "staging-db", want: false},
		{pattern: "db?", alias: "db1", want: true},
		{pattern: "db?", alias: "db12", want: false},
		{pattern: "*", alias: "anything", want: true},
		{pattern: "a*b*c", alias: "axxbyyc", want: true},
		{pattern: "a*b*c", alias: "axxbyy", want: false},
	}

	for _, tc := range cases {
		if got := NewPattern(tc.pattern).Match(tc.alias); got != tc.want {
			t.Errorf("pattern %q match %q = %v, want %v", tc.pattern, tc.alias, got, tc.want)
		}
	}
}

func TestHostMatches(t *testing.T) {
	host := Host{
		Alias:    "gd-vps",
		Tags:     []string{"prod", "vps"},
		Declared: []Directive{{Key: "hostname", Value: "207.244.252.156"}},
	}

	cases := []struct {
		filter string
		want   bool
	}{
		{filter: "", want: true},
		{filter: "gd", want: true},
		{filter: "GD-VPS", want: true},
		{filter: "prod", want: true},
		{filter: "207.244", want: true},
		{filter: "nope", want: false},
	}

	for _, tc := range cases {
		if got := host.Matches(tc.filter); got != tc.want {
			t.Errorf("Matches(%q) = %v, want %v", tc.filter, got, tc.want)
		}
	}
}

func TestHostHasTag(t *testing.T) {
	host := Host{Alias: "web", Tags: []string{"prod"}}

	if !host.HasTag("PROD") {
		t.Error("HasTag should compare case-insensitively")
	}
	if host.HasTag("db") {
		t.Error("HasTag reported a tag the host does not carry")
	}
	if host.HasTag("  ") {
		t.Error("an empty tag must never match")
	}
}

func TestValidateAlias(t *testing.T) {
	valid := []string{"web", "gd-vps", "user@host", "a.b_c-d"}
	for _, alias := range valid {
		if err := ValidateAlias(alias); err != nil {
			t.Errorf("ValidateAlias(%q) = %v, want nil", alias, err)
		}
	}

	invalid := []string{"", "   ", "-oProxyCommand=evil", "two words", "tab\there"}
	for _, alias := range invalid {
		if err := ValidateAlias(alias); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ValidateAlias(%q) = %v, want ErrInvalidArgument", alias, err)
		}
	}
}

func TestParseResolved(t *testing.T) {
	lines := []string{
		"hostname 207.244.252.156",
		"user deployer",
		"port 2222",
		"identityfile ~/.ssh/id_ed25519_gd_vps",
		"identityfile ~/.ssh/id_rsa",
		"proxyjump jump.example.com",
		"identitiesonly yes",
		"forwardagent no",
		"hostname ignored-second-value",
	}

	resolved := ParseResolved("gd-vps", lines)

	if resolved.HostName != "207.244.252.156" {
		t.Errorf("HostName = %q, want the first value", resolved.HostName)
	}
	if resolved.User != "deployer" {
		t.Errorf("User = %q", resolved.User)
	}
	if resolved.Port != 2222 {
		t.Errorf("Port = %d", resolved.Port)
	}
	if len(resolved.IdentityFiles) != 2 {
		t.Fatalf("IdentityFiles = %v, want both identities", resolved.IdentityFiles)
	}
	if resolved.ProxyJump != "jump.example.com" {
		t.Errorf("ProxyJump = %q", resolved.ProxyJump)
	}
	if !resolved.IdentitiesOnly {
		t.Error("IdentitiesOnly should be true for yes")
	}
	if got := resolved.Options["forwardagent"]; len(got) != 1 || got[0] != "no" {
		t.Errorf("Options[forwardagent] = %v", got)
	}
}

func TestParseResolvedIgnoresGarbage(t *testing.T) {
	resolved := ParseResolved("web", []string{"", "   ", "port not-a-number", "\tuser\talice\t"})

	if resolved.Port != 0 {
		t.Errorf("Port = %d, want 0 for a non-numeric value", resolved.Port)
	}
	if resolved.User != "alice" {
		t.Errorf("User = %q, want alice with tabs trimmed", resolved.User)
	}
}

func TestApproximateResolved(t *testing.T) {
	host := Host{
		Alias: "gd-vps",
		Declared: []Directive{
			{Key: "hostname", Value: "207.244.252.156"},
			{Key: "user", Value: "deployer"},
			{Key: "identityfile", Value: "~/.ssh/id_ed25519"},
			{Key: "identitiesonly", Value: "yes"},
		},
	}

	resolved := ApproximateResolved(host)

	if resolved.HostName != "207.244.252.156" || resolved.User != "deployer" {
		t.Errorf("resolution = %+v", resolved)
	}
	if resolved.Port != 22 {
		t.Errorf("Port = %d, want the default 22", resolved.Port)
	}
	if !resolved.IdentitiesOnly {
		t.Error("IdentitiesOnly should follow the literal directive")
	}
}

func TestResolvedDestination(t *testing.T) {
	resolved := ResolvedHost{HostName: "example.com", User: "alice", Port: 2222}
	if got := resolved.Destination(); got != "alice@example.com:2222" {
		t.Errorf("Destination() = %q", got)
	}

	plain := ResolvedHost{HostName: "example.com"}
	if got := plain.Destination(); got != "example.com" {
		t.Errorf("Destination() = %q", got)
	}
}

func TestSummaryRowsKeepOrder(t *testing.T) {
	rows := (ResolvedHost{Alias: "web", HostName: "h", User: "u", Port: 22}).SummaryRows()

	want := []string{"alias", "hostname", "user", "port", "identity", "proxyjump", "identitiesonly"}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, key := range want {
		if rows[i].Key != key {
			t.Errorf("row %d = %q, want %q", i, rows[i].Key, key)
		}
	}
	if rows[4].Value != "-" {
		t.Errorf("identity with no files = %q, want -", rows[4].Value)
	}
}

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags([]string{" Prod, db ", "prod", "CLIENTE-X"})
	if err != nil {
		t.Fatalf("NormalizeTags: %v", err)
	}

	want := []string{"prod", "db", "cliente-x"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("NormalizeTags = %v, want %v", got, want)
	}
}

func TestNormalizeTagsRejects(t *testing.T) {
	// An all-blank list is how the prompt clears the tags: it normalizes to empty.
	empty, err := NormalizeTags([]string{"   "})
	if err != nil || len(empty) != 0 {
		t.Errorf("NormalizeTags(blank) = %v, %v; want an empty list and no error", empty, err)
	}

	long := strings.Repeat("x", MaxTagBytes+1)
	if _, err := NormalizeTags([]string{long}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("long tag = %v, want ErrInvalidArgument", err)
	}

	many := make([]string, MaxTagsPerHost+1)
	for i := range many {
		many[i] = "t" + string(rune('a'+i%26)) + strings.Repeat("z", i%3)
	}
	if _, err := NormalizeTags(many); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("too many tags = %v, want ErrInvalidArgument", err)
	}
}

func TestTagIndexAll(t *testing.T) {
	index := TagIndex{"a": {"prod", "db"}, "b": {"prod"}}

	all := index.All()
	seen := make(map[string]int, len(all))
	for _, tag := range all {
		seen[tag]++
	}

	if len(all) != 2 || seen["prod"] != 1 || seen["db"] != 1 {
		t.Errorf("All() = %v, want the distinct tags once each", all)
	}
}

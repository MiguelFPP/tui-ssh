package sshconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHostsStripsControlCharsFromAlias covers the trust boundary of the parser: the
// ssh configuration is external text and the alias it yields is rendered in the list
// and handed to ssh as an argument.
func TestHostsStripsControlCharsFromAlias(t *testing.T) {
	dir := t.TempDir()
	config := "Host web\x1b]52;c;SGVsbG8=\x07\n  HostName 10.0.0.1\n"

	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	hosts, err := serviceFor(t, dir).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(hosts))
	}

	if strings.ContainsAny(hosts[0].Alias, "\x1b\x07") {
		t.Errorf("Alias kept a control character: %q", hosts[0].Alias)
	}
	if !strings.HasPrefix(hosts[0].Alias, "web") {
		t.Errorf("Alias = %q, want the printable text preserved", hosts[0].Alias)
	}
}

// TestHostsStripsControlCharsFromDirectives covers the values the detail panel
// renders verbatim.
func TestHostsStripsControlCharsFromDirectives(t *testing.T) {
	dir := t.TempDir()
	config := "Host web\n  HostName 10.0.0.1\x1b[2J\n  User deployer\u009b2J\n"

	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	hosts, err := serviceFor(t, dir).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(hosts))
	}

	host := hosts[0]
	for _, key := range []string{"hostname", "user"} {
		value, ok := host.DeclaredValue(key)
		if !ok {
			t.Fatalf("%s missing from the parsed directives", key)
		}
		if strings.ContainsAny(value, "\x1b\u009b") {
			t.Errorf("%s kept a control character: %q", key, value)
		}
	}
}

// TestSourceStripsControlChars covers the read-only viewer, which renders the file
// lines verbatim.
func TestSourceStripsControlChars(t *testing.T) {
	dir := t.TempDir()
	config := "Host web\n  HostName 10.0.0.1\x1b[2J\n"

	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	source, err := serviceFor(t, dir).Source(context.Background())
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	for _, line := range source.Lines {
		if strings.ContainsRune(line, '\x1b') {
			t.Errorf("Source kept an escape byte: %q", line)
		}
	}
}

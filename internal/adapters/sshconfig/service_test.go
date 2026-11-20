package sshconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

// serviceFor builds a service rooted at the fixture directory, with home pointing
// inside it so that ~ and relative includes resolve predictably.
func serviceFor(t *testing.T, dir string) *Service {
	t.Helper()

	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	service := New(filepath.Join(abs, "config"))
	service.home = abs

	return service
}

func aliases(hosts []domain.Host) []string {
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, host.Alias)
	}

	return out
}

func TestHostsSimple(t *testing.T) {
	service := serviceFor(t, "testdata/simple")

	hosts, err := service.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}

	want := []string{"web", "db"}
	if got := aliases(hosts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("aliases = %v, want %v (wildcards and negations are not connectable)", got, want)
	}

	web := hosts[0]
	if web.Alias != "web" || web.SourceLine != 1 {
		t.Errorf("web = %+v", web)
	}
	if hostname, ok := web.DeclaredValue("hostname"); !ok || hostname != "10.0.0.1" {
		t.Errorf("web hostname = %q, %v", hostname, ok)
	}

	// A block with several aliases keeps the first one: ssh accepts every pattern,
	// but the TUI needs one connectable name per row.
	if db := hosts[1]; db.Alias != "db" || len(db.Patterns) != 2 {
		t.Errorf("db = %+v, want the first pattern as alias and both kept", db)
	}
}

func TestHostsIncludes(t *testing.T) {
	service := serviceFor(t, "testdata/includes")

	hosts, err := service.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}

	want := []string{"included-one", "included-two", "own", "nested"}
	if got := aliases(hosts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
}

func TestReportIncludesFilesAndWarnings(t *testing.T) {
	service := serviceFor(t, "testdata/includes")

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	if report.Hosts != 4 {
		t.Errorf("Hosts = %d, want 4", report.Hosts)
	}
	if len(report.Files) != 4 {
		t.Errorf("Files = %d, want the main file and three includes: %+v", len(report.Files), report.Files)
	}
	if report.Files[0].Included {
		t.Error("the main file must not be flagged as included")
	}
	if !report.Files[1].Included {
		t.Error("the included files must be flagged")
	}
	if !report.OK() {
		t.Error("a readable configuration should report OK")
	}
}

func TestHostsQuirks(t *testing.T) {
	service := serviceFor(t, "testdata/quirks")

	hosts, err := service.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}

	byAlias := make(map[string]domain.Host, len(hosts))
	for _, host := range hosts {
		byAlias[host.Alias] = host
	}

	quoted, ok := byAlias["quoted"]
	if !ok {
		t.Fatalf("missing quoted host, got %v", aliases(hosts))
	}
	if hostname, _ := quoted.DeclaredValue("hostname"); hostname != "quoted.example.com" {
		t.Errorf("quoted hostname = %q, want the quotes stripped", hostname)
	}

	commented, ok := byAlias["commented"]
	if !ok {
		t.Fatal("missing commented host")
	}
	if hostname, _ := commented.DeclaredValue("hostname"); hostname != "host.example.com" {
		t.Errorf("commented hostname = %q, want the trailing comment removed", hostname)
	}

	// A `Match all` block does not interrupt the Host blocks that follow it.
	if _, ok := byAlias["after-match"]; !ok {
		t.Errorf("missing after-match host, got %v", aliases(hosts))
	}

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	var warnedMatch bool
	for _, warning := range report.Warnings {
		if strings.Contains(warning, "Match") {
			warnedMatch = true
		}
	}
	if !warnedMatch {
		t.Errorf("warnings = %v, want one about the unsupported Match block", report.Warnings)
	}
}

func TestHostsEqualsSyntax(t *testing.T) {
	service := serviceFor(t, "testdata/equals")

	hosts, err := service.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}

	if len(hosts) != 1 || hosts[0].Alias != "equals" {
		t.Fatalf("hosts = %v, want [equals]", aliases(hosts))
	}
	if hostname, _ := hosts[0].DeclaredValue("hostname"); hostname != "equals.example.com" {
		t.Errorf("hostname = %q with Key=Value syntax", hostname)
	}
}

func TestHostsIncludeCycle(t *testing.T) {
	service := serviceFor(t, "testdata/cycle")

	hosts, err := service.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v, did not expect an error for a cycle", err)
	}

	// The cycle is broken once, so both files contribute their hosts.
	if len(hosts) != 2 {
		t.Fatalf("hosts = %v, want real and from-loop", aliases(hosts))
	}

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	var warned bool
	for _, warning := range report.Warnings {
		if strings.Contains(warning, "circular") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("warnings = %v, want one about the circular include", report.Warnings)
	}
}

func TestHostsMissingConfig(t *testing.T) {
	service := New(filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := service.Hosts(context.Background())
	if !errors.Is(err, domain.ErrConfigMissing) {
		t.Fatalf("Hosts = %v, want ErrConfigMissing", err)
	}

	source, err := service.Source(context.Background())
	if !errors.Is(err, domain.ErrConfigMissing) {
		t.Fatalf("Source = %v, want ErrConfigMissing", err)
	}
	if source.Lines != nil {
		t.Errorf("Source.Lines = %v, want nil", source.Lines)
	}
}

func TestHostsTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	big := strings.Repeat("Host a\n", domain.MaxConfigBytes/7+1)
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	service := New(path)
	service.home = dir

	_, err := service.Hosts(context.Background())
	if !errors.Is(err, domain.ErrTooLarge) {
		t.Fatalf("Hosts = %v, want ErrTooLarge", err)
	}
}

func TestSourceIsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	lines := make([]string, domain.MaxTunnelLogLines+50)
	for i := range lines {
		lines[i] = "Host h" + strings.Repeat("x", 0)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	service := New(path)
	service.home = dir

	source, err := service.Source(context.Background())
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	if !source.Truncated {
		t.Error("Source should be flagged as truncated")
	}
	if len(source.Lines) != domain.MaxTunnelLogLines {
		t.Errorf("len(Lines) = %d, want %d", len(source.Lines), domain.MaxTunnelLogLines)
	}
	if source.TotalLines != domain.MaxTunnelLogLines+50 {
		t.Errorf("TotalLines = %d, want the real line count", source.TotalLines)
	}
}

func TestHostsCancelledContext(t *testing.T) {
	service := serviceFor(t, "testdata/simple")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.Hosts(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Hosts = %v, want context.Canceled", err)
	}
}

func TestIncludeDepthIsBounded(t *testing.T) {
	dir := t.TempDir()

	// A chain of files that include the next one, deeper than the allowed limit.
	for i := 0; i < domain.MaxIncludeDepth+3; i++ {
		name := filepath.Join(dir, "level"+itoa(i))
		next := "level" + itoa(i+1)
		content := "Include " + next + "\n\nHost h" + itoa(i) + "\n    HostName x\n"

		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	service := New(filepath.Join(dir, "level0"))
	service.home = dir

	hosts, err := service.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}

	if len(hosts) > domain.MaxIncludeDepth+1 {
		t.Errorf("hosts = %d, want the depth limit to stop the recursion", len(hosts))
	}

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	var warned bool
	for _, warning := range report.Warnings {
		if strings.Contains(warning, "profundidad") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("warnings = %v, want one about the depth limit", report.Warnings)
	}
}

func TestSplitDirectiveAndComments(t *testing.T) {
	cases := []struct {
		line      string
		key       string
		value     string
		commented string
	}{
		{line: "Host web", key: "host", value: "web", commented: "Host web"},
		{line: "  User   deploy  ", key: "user", value: "deploy", commented: "  User   deploy  "},
		{line: "Port=2222", key: "port", value: "2222", commented: "Port=2222"},
		{line: `HostName "host #notacomment"`, key: "hostname", value: "host #notacomment", commented: `HostName "host #notacomment"`},
		{line: "Host web # trailing", key: "host", value: "web", commented: "Host web "},
	}

	for _, tc := range cases {
		// Production strips the comment before splitting, so the test mirrors it.
		key, value := splitDirective(stripComment(tc.line))
		if key != tc.key || value != tc.value {
			t.Errorf("splitDirective(%q) = %q, %q; want %q, %q", tc.line, key, value, tc.key, tc.value)
		}
		if got := stripComment(tc.line); got != tc.commented {
			t.Errorf("stripComment(%q) = %q, want %q", tc.line, got, tc.commented)
		}
	}
}

func TestParsePatterns(t *testing.T) {
	patterns := parsePatterns("web prod-* !bastion")

	if len(patterns) != 3 {
		t.Fatalf("patterns = %v, want 3", patterns)
	}
	if patterns[1].Wild != true || patterns[2].Negated != true {
		t.Errorf("patterns = %+v", patterns)
	}

	// The first literal pattern names the host; wildcards and negations never do.
	if got := firstLiteral(patterns); got != "web" {
		t.Errorf("firstLiteral = %q, want web", got)
	}
	if got := firstLiteral([]domain.Pattern{domain.NewPattern("*")}); got != "" {
		t.Errorf("firstLiteral(wildcard) = %q, want empty", got)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	digits := make([]byte, 0, 4)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	return string(digits)
}

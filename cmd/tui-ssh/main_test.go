package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/config"
)

func TestVersionLine(t *testing.T) {
	originalVersion, originalCommit := version, commit
	t.Cleanup(func() { version, commit = originalVersion, originalCommit })

	version, commit = "0.1.0", "dev"
	if got := versionLine(); got != "tui-ssh 0.1.0" {
		t.Errorf("versionLine = %q, want tui-ssh 0.1.0", got)
	}

	version, commit = "0.1.0", "abc1234"
	if got := versionLine(); got != "tui-ssh 0.1.0 (abc1234)" {
		t.Errorf("versionLine = %q, want the commit appended", got)
	}
}

func TestPrintThemes(t *testing.T) {
	var buf bytes.Buffer
	printThemes(&buf)

	out := buf.String()
	if !strings.Contains(out, "tokyonight-night") {
		t.Errorf("themes = %q, want the catalog", out)
	}
	if !strings.Contains(out, "(default)") {
		t.Errorf("themes = %q, want the default flagged", out)
	}
}

func TestPrintThemesToleratesNilWriter(t *testing.T) {
	printThemes(nil)
}

func TestPrintKeys(t *testing.T) {
	var buf bytes.Buffer
	printKeys(&buf)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 10 {
		t.Fatalf("keys printed %d lines, want the whole catalog", len(lines))
	}
	if !strings.HasPrefix(lines[0], "accion\tscope\tteclas\ttipo") {
		t.Errorf("header = %q", lines[0])
	}

	var configurable, fixed int
	for _, line := range lines[1:] {
		if strings.HasSuffix(line, "configurable") {
			configurable++
		}
		if strings.HasSuffix(line, "fija") {
			fixed++
		}
	}
	if configurable == 0 || fixed == 0 {
		t.Errorf("printed %d configurable and %d fixed, want both kinds", configurable, fixed)
	}
}

func TestPrintConfig(t *testing.T) {
	var buf bytes.Buffer

	cfg := config.Config{
		ConfigPath:    "/home/u/.config/tui-ssh/config.json",
		Theme:         "dracula",
		SSHConfig:     "/home/u/.ssh/config",
		SSHBinary:     "ssh",
		RuntimeDir:    "/run/user/1000/tui-ssh",
		DataDir:       "/home/u/.config/tui-ssh",
		ListWidth:     42,
		ListCollapsed: true,
		DetailLayout:  "single",
		Keys:          map[string][]string{"quit": {"Q"}},
	}

	if err := printConfig(&buf, cfg); err != nil {
		t.Fatalf("printConfig: %v", err)
	}

	var out struct {
		ConfigPath    string              `json:"config_path"`
		Theme         string              `json:"theme"`
		SSHConfig     string              `json:"ssh_config"`
		ListWidth     int                 `json:"list_width"`
		ListCollapsed bool                `json:"list_collapsed"`
		DetailLayout  string              `json:"detail_layout"`
		Keys          map[string][]string `json:"keys"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if out.Theme != "dracula" || out.SSHConfig != "/home/u/.ssh/config" {
		t.Errorf("output = %+v", out)
	}
	if out.ListWidth != 42 || !out.ListCollapsed || out.DetailLayout != "single" {
		t.Errorf("layout = %d, %v, %q", out.ListWidth, out.ListCollapsed, out.DetailLayout)
	}
	if got := out.Keys["quit"]; len(got) != 1 || got[0] != "Q" {
		t.Errorf("keys = %v", out.Keys)
	}
}

func TestPrintConfigDefaultsAndEmptyKeys(t *testing.T) {
	var buf bytes.Buffer

	if err := printConfig(&buf, config.Config{}); err != nil {
		t.Fatalf("printConfig: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"theme": "tokyonight-night"`) {
		t.Errorf("output = %q, want the default theme", out)
	}
	// A never-nil map keeps the JSON stable.
	if !strings.Contains(out, `"keys": {}`) {
		t.Errorf("output = %q, want an empty keys object", out)
	}
	if !strings.Contains(out, `"detail_layout": "split"`) {
		t.Errorf("output = %q, want the split default", out)
	}
}

func TestPrintConfigRejectsUnknownTheme(t *testing.T) {
	var buf bytes.Buffer

	if err := printConfig(&buf, config.Config{Theme: "not-a-theme"}); err == nil {
		t.Fatal("printConfig accepted an unknown theme")
	}
}

func TestPrintConfigToleratesNilWriter(t *testing.T) {
	if err := printConfig(nil, config.Config{}); err != nil {
		t.Fatalf("printConfig(nil): %v", err)
	}
}

func TestDetailLayoutName(t *testing.T) {
	cases := map[string]string{"": "split", "single": "single", "split": "split", "weird": "split"}
	for raw, want := range cases {
		if got := detailLayoutName(raw); got != want {
			t.Errorf("detailLayoutName(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRuntimeDirPrecedence(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

	if got := runtimeDir(config.Config{RuntimeDir: "/custom"}); got != "/custom" {
		t.Errorf("runtimeDir = %q, want the explicit value", got)
	}
	if got := runtimeDir(config.Config{}); got != "/run/user/1000/tui-ssh" {
		t.Errorf("runtimeDir = %q, want the XDG directory", got)
	}
}

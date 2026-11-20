package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// env returns a getenv that resolves from the given map.
func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestParseDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := Parse([]string{"--ssh-config", "/etc/ssh/ssh_config"}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Theme != "" {
		t.Errorf("Theme = %q, want empty for main to apply the default", cfg.Theme)
	}
	if cfg.SSHBinary != "ssh" {
		t.Errorf("SSHBinary = %q, want ssh", cfg.SSHBinary)
	}
	if cfg.Editor != "vim" {
		t.Errorf("Editor = %q, want vim", cfg.Editor)
	}
	if cfg.SSHConfig != "/etc/ssh/ssh_config" {
		t.Errorf("SSHConfig = %q", cfg.SSHConfig)
	}
	if cfg.ConfigPath != "" {
		t.Errorf("ConfigPath = %q, want empty when no file was loaded", cfg.ConfigPath)
	}

	wantSave := filepath.Join(dir, "tui-ssh", "config.json")
	if cfg.SavePath != wantSave {
		t.Errorf("SavePath = %q, want %q", cfg.SavePath, wantSave)
	}
	if cfg.DataDir != filepath.Join(dir, "tui-ssh") {
		t.Errorf("DataDir = %q, want the XDG tui-ssh directory", cfg.DataDir)
	}
}

func TestParseCatalogFlagsSkipValidation(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--list-themes"}, {"--list-keys"}} {
		cfg, err := Parse(args, env(nil))
		if err != nil {
			t.Fatalf("Parse(%v): %v", args, err)
		}

		if !cfg.ShowVersion && !cfg.ListThemes && !cfg.ListKeys {
			t.Errorf("Parse(%v) = %+v, want the catalog flag set", args, cfg)
		}
	}
}

func TestParseRejectsExtraArguments(t *testing.T) {
	if _, err := Parse([]string{"extra"}, env(nil)); err == nil {
		t.Fatal("Parse accepted a positional argument")
	}
}

func TestParseEnvFallbacks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := Parse(nil, env(map[string]string{
		"EDITOR":              "nano -w",
		"SSH":                 "/usr/local/bin/ssh",
		"TUI_SSH_THEME":       "dracula",
		"TUI_SSH_RUNTIME_DIR": filepath.Join(dir, "runtime"),
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Editor != "nano -w" {
		t.Errorf("Editor = %q", cfg.Editor)
	}
	if cfg.SSHBinary != "/usr/local/bin/ssh" {
		t.Errorf("SSHBinary = %q", cfg.SSHBinary)
	}
	if cfg.Theme != "dracula" {
		t.Errorf("Theme = %q", cfg.Theme)
	}
	if cfg.RuntimeDir != filepath.Join(dir, "runtime") {
		t.Errorf("RuntimeDir = %q", cfg.RuntimeDir)
	}
}

func TestParseFlagsWinOverEnv(t *testing.T) {
	cfg, err := Parse([]string{"--theme", "gruvbox-dark"}, env(map[string]string{"TUI_SSH_THEME": "dracula"}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Theme != "gruvbox-dark" {
		t.Errorf("Theme = %q, want the flag to win", cfg.Theme)
	}
}

func TestParseRejectsEmptyEditor(t *testing.T) {
	if _, err := Parse([]string{"--editor", "  "}, env(nil)); !errors.Is(err, ErrEditorRequired) {
		t.Fatalf("Parse = %v, want ErrEditorRequired", err)
	}
}

func TestParseRejectsEmptySSH(t *testing.T) {
	if _, err := Parse([]string{"--ssh", "  "}, env(nil)); !errors.Is(err, ErrSSHRequired) {
		t.Fatalf("Parse = %v, want ErrSSHRequired", err)
	}
}

func TestParseLoadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	body := `{
  "theme": "catppuccin-mocha",
  "list_width": 42,
  "list_collapsed": true,
  "detail_layout": "single",
  "keys": {"quit": ["Q"], "host_up": "k"}
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Parse([]string{"--config", path}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Theme != "catppuccin-mocha" {
		t.Errorf("Theme = %q", cfg.Theme)
	}
	if cfg.ListWidth != 42 || !cfg.ListCollapsed {
		t.Errorf("layout = %d, %v", cfg.ListWidth, cfg.ListCollapsed)
	}
	if cfg.DetailLayout != "single" {
		t.Errorf("DetailLayout = %q", cfg.DetailLayout)
	}
	if got := cfg.Keys["quit"]; len(got) != 1 || got[0] != "Q" {
		t.Errorf("keys[quit] = %v", got)
	}
	if got := cfg.Keys["host_up"]; len(got) != 1 || got[0] != "k" {
		t.Errorf("keys[host_up] = %v, want the single-string form accepted", got)
	}
	if cfg.ConfigPath != path {
		t.Errorf("ConfigPath = %q", cfg.ConfigPath)
	}
}

func TestParseExplicitMissingFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")

	if _, err := Parse([]string{"--config", path}, env(nil)); !errors.Is(err, ErrConfigFile) {
		t.Fatalf("Parse = %v, want ErrConfigFile", err)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := os.WriteFile(path, []byte(`{"theme": "dracula", "unknown": 1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Parse([]string{"--config", path}, env(nil)); !errors.Is(err, ErrConfigFormat) {
		t.Fatalf("Parse = %v, want ErrConfigFormat", err)
	}
}

func TestParseRejectsNegativeWidth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := os.WriteFile(path, []byte(`{"list_width": -1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Parse([]string{"--config", path}, env(nil)); !errors.Is(err, ErrConfigFormat) {
		t.Fatalf("Parse = %v, want ErrConfigFormat", err)
	}
}

func TestParseRejectsInvalidDetailLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := os.WriteFile(path, []byte(`{"detail_layout": "three"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Parse([]string{"--config", path}, env(nil)); !errors.Is(err, ErrConfigFormat) {
		t.Fatalf("Parse = %v, want ErrConfigFormat", err)
	}
}

func TestInitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")

	cfg, err := Parse([]string{"--init-config", "--config", path}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.InitConfig || cfg.ConfigPath != path {
		t.Fatalf("cfg = %+v", cfg)
	}

	if err := WriteDefault(cfg.ConfigPath, false); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != BaseFile {
		t.Errorf("written file = %q, want BaseFile", data)
	}

	// Without --force a second write must fail instead of overwriting.
	if err := WriteDefault(cfg.ConfigPath, false); !errors.Is(err, ErrConfigExists) {
		t.Fatalf("WriteDefault = %v, want ErrConfigExists", err)
	}
	if err := WriteDefault(cfg.ConfigPath, true); err != nil {
		t.Fatalf("WriteDefault(force): %v", err)
	}
}

func TestPrintConfigPathSkipsValidation(t *testing.T) {
	cfg, err := Parse([]string{"--print-config"}, env(map[string]string{"TUI_SSH_SSHCONFIG": "/custom/ssh_config"}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !cfg.PrintConfig {
		t.Error("PrintConfig should be set")
	}
	if cfg.SSHConfig != "/custom/ssh_config" {
		t.Errorf("SSHConfig = %q", cfg.SSHConfig)
	}
	if cfg.SSHBinary != "ssh" {
		t.Errorf("SSHBinary = %q, want the default", cfg.SSHBinary)
	}
}

func TestSaveLayoutPreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	body := `{"theme": "dracula", "detail_layout": "single", "keys": {"quit": ["Q"]}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := SaveLayout(path, 50, true); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got["theme"] != "dracula" {
		t.Errorf("theme = %v, want it preserved", got["theme"])
	}
	if got["detail_layout"] != "single" {
		t.Errorf("detail_layout = %v, want it preserved", got["detail_layout"])
	}
	if got["list_width"] != float64(50) || got["list_collapsed"] != true {
		t.Errorf("layout = %v, %v", got["list_width"], got["list_collapsed"])
	}
	if _, ok := got["keys"]; !ok {
		t.Error("keys were dropped by SaveLayout")
	}
}

func TestSaveDetailLayoutPreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := SaveLayout(path, 44, false); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	if err := SaveDetailLayout(path, "single"); err != nil {
		t.Fatalf("SaveDetailLayout: %v", err)
	}

	cfg, err := Parse([]string{"--config", path}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.ListWidth != 44 {
		t.Errorf("ListWidth = %d, want it preserved", cfg.ListWidth)
	}
	if cfg.DetailLayout != "single" {
		t.Errorf("DetailLayout = %q", cfg.DetailLayout)
	}
}

func TestSaveLayoutRejectsNegative(t *testing.T) {
	if err := SaveLayout(filepath.Join(t.TempDir(), "c.json"), -1, false); !errors.Is(err, ErrConfigFormat) {
		t.Fatalf("SaveLayout = %v, want ErrConfigFormat", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}

	got, err := expandHome("~/.ssh/config")
	if err != nil {
		t.Fatalf("expandHome: %v", err)
	}
	if got != filepath.Join(home, ".ssh", "config") {
		t.Errorf("expandHome = %q, want it under the home", got)
	}

	abs := filepath.Join(string(filepath.Separator), "etc", "ssh", "ssh_config")
	got, err = expandHome(abs)
	if err != nil {
		t.Fatalf("expandHome(abs): %v", err)
	}
	if got != abs {
		t.Errorf("expandHome(abs) = %q, want it unchanged", got)
	}
}

func TestResolveSSHConfigDefault(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}

	if got := resolveSSHConfig(""); got != filepath.Join(home, ".ssh", "config") {
		t.Errorf("resolveSSHConfig(\"\") = %q, want ~/.ssh/config", got)
	}
}

func TestDecodeFileRejectsTrailingData(t *testing.T) {
	if _, err := decodeFile([]byte(`{} {}`), "x.json"); !errors.Is(err, ErrConfigFormat) {
		t.Fatalf("decodeFile = %v, want ErrConfigFormat", err)
	}
}

func TestKeyListRejectsNull(t *testing.T) {
	if _, err := decodeFile([]byte(`{"keys": {"quit": null}}`), "x.json"); !errors.Is(err, ErrConfigFormat) {
		t.Fatalf("decodeFile = %v, want ErrConfigFormat for null", err)
	}
}

func TestWriteFileAtomicCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "dir", "config.json")

	if err := WriteDefault(path, false); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("permissions = %o, want 600", mode)
	}
}

func TestUsageMentionsEveryFlag(t *testing.T) {
	flags := []string{
		"--version", "--list-themes", "--list-keys", "--ssh-config", "--ssh",
		"--editor", "--runtime-dir", "--debug-log", "--config", "--theme",
		"--init-config", "--force", "--print-config",
	}

	for _, flag := range flags {
		if !strings.Contains(Usage, flag) {
			t.Errorf("Usage does not document %s", flag)
		}
	}
}

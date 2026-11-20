// Command tui-ssh is a visual manager for the OpenSSH configuration: it lists the
// configured hosts, filters them by tag, opens interactive sessions and owns the
// background port forwardings.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/adapters/resolver"
	"github.com/MiguelFPP/tui-ssh/internal/adapters/sshcmd"
	"github.com/MiguelFPP/tui-ssh/internal/adapters/sshconfig"
	"github.com/MiguelFPP/tui-ssh/internal/adapters/tags"
	"github.com/MiguelFPP/tui-ssh/internal/adapters/tunnel"
	"github.com/MiguelFPP/tui-ssh/internal/config"
	"github.com/MiguelFPP/tui-ssh/internal/ports"
	"github.com/MiguelFPP/tui-ssh/internal/tui/app"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
	"github.com/MiguelFPP/tui-ssh/internal/tui/styles"
)

// version and commit are overridden at build time with
// -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "0.1.0"
	commit  = "dev"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, config.ErrHelp) {
			fmt.Fprintln(os.Stdout, config.Usage)
			return
		}

		fmt.Fprintf(os.Stderr, "tui-ssh: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}

	if cfg.ShowVersion {
		fmt.Fprintln(os.Stdout, versionLine())

		return nil
	}

	// The catalogs are static: listing them neither reads the config file nor
	// starts the TUI.
	if cfg.ListThemes {
		printThemes(os.Stdout)

		return nil
	}
	if cfg.ListKeys {
		printKeys(os.Stdout)

		return nil
	}

	// --init-config writes the template and exits; the path goes to stdout so it
	// can be piped, and no ssh check runs.
	if cfg.InitConfig {
		if err := config.WriteDefault(cfg.ConfigPath, cfg.Force); err != nil {
			return err
		}

		fmt.Fprintln(os.Stdout, cfg.ConfigPath)

		return nil
	}

	// --print-config dumps the resolved values without starting the TUI.
	if cfg.PrintConfig {
		if err := printConfig(os.Stdout, cfg); err != nil {
			return err
		}

		return nil
	}

	keys, err := keymap.Default().Apply(cfg.Keys)
	if err != nil {
		return fmt.Errorf("aplicar atajos: %w", err)
	}

	// The palette is settled before the adapters so every style token is ready
	// when the views render.
	theme := styles.DefaultTheme()
	if cfg.Theme != "" {
		var ok bool
		theme, ok = styles.ThemeByName(cfg.Theme)
		if !ok {
			return fmt.Errorf("tema desconocido %q; disponibles: %s", cfg.Theme, strings.Join(styles.ThemeNames(), ", "))
		}
	}
	styles.Apply(theme)

	sshConfigSvc := sshconfig.New(cfg.SSHConfig)

	resolveSvc := resolver.New(cfg.SSHBinary, cfg.SSHConfig)

	sessionSvc := sshcmd.New(cfg.SSHBinary)

	editorSvc, err := sshcmd.NewConfigEditor(cfg.Editor)
	if err != nil {
		return fmt.Errorf("construir el servicio de editor: %w", err)
	}

	tagsSvc := tags.New(tags.Files{
		Tags:     filepath.Join(cfg.DataDir, "tags.json"),
		Forwards: filepath.Join(cfg.DataDir, "forwards.json"),
	})

	// An unusable runtime directory is not fatal: the tunnel panel keeps its state
	// in memory and reports why it cannot persist.
	var tunnelSvc ports.TunnelService = tunnel.New(cfg.SSHBinary, cfg.SSHConfig, runtimeDir(cfg))

	deps := app.Deps{
		Config:  cfg,
		SSH:     sshConfigSvc,
		Tags:    tagsSvc,
		Resolve: resolveSvc,
		Session: sessionSvc,
		Tunnels: tunnelSvc,
		Editor:  editorSvc,
		Keys:    keys,
	}
	if cfg.SavePath != "" {
		// Without a resolved config path there is nowhere to persist the layout.
		deps.SaveLayout = func(width int, collapsed bool) error {
			return config.SaveLayout(cfg.SavePath, width, collapsed)
		}
		deps.SaveDetailLayout = func(layout string) error {
			return config.SaveDetailLayout(cfg.SavePath, layout)
		}
	}

	program := tea.NewProgram(app.New(deps))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("ejecutar la TUI: %w", err)
	}

	return nil
}

// runtimeDir is where the control sockets, the logs and the manifest live. It
// prefers the explicit configuration, then XDG_RUNTIME_DIR, then the user cache.
func runtimeDir(cfg config.Config) string {
	if strings.TrimSpace(cfg.RuntimeDir) != "" {
		return cfg.RuntimeDir
	}

	if dir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); dir != "" {
		return filepath.Join(dir, "tui-ssh")
	}

	if cache, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cache) != "" {
		return filepath.Join(cache, "tui-ssh", "runtime")
	}

	return ""
}

// versionLine prints tui-ssh <version> and appends the commit when one was
// injected by the release build; local builds keep the default "dev" marker.
func versionLine() string {
	if commit == "" || commit == "dev" {
		return "tui-ssh " + version
	}

	return fmt.Sprintf("tui-ssh %s (%s)", version, commit)
}

// printThemes writes the theme catalog, one row per theme, flagging the default.
func printThemes(w io.Writer) {
	if w == nil {
		w = io.Discard
	}

	def := styles.DefaultTheme().Name
	for _, theme := range styles.Themes() {
		suffix := ""
		if theme.Name == def {
			suffix = " (default)"
		}

		fmt.Fprintf(w, "%-20s %s%s\n", theme.Name, theme.Description, suffix)
	}
}

// printKeys writes the default bindings, one row per action, with the effective
// keys. User overrides are not applied on purpose: this documents the defaults.
func printKeys(w io.Writer) {
	if w == nil {
		w = io.Discard
	}

	keys := keymap.Default()

	fmt.Fprintln(w, "accion\tscope\tteclas\ttipo")
	for _, binding := range keys.All() {
		kind := "configurable"
		if !binding.Configurable {
			kind = "fija"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", binding.Action, binding.Scope, keys.Label(binding.Action), kind)
	}
}

// printConfig writes the resolved configuration as indented JSON. It only reads
// cfg: the theme is validated against the catalog but the palette and the keymap
// are not applied, because no view is rendered.
func printConfig(w io.Writer, cfg config.Config) error {
	if w == nil {
		w = io.Discard
	}

	name := strings.TrimSpace(cfg.Theme)
	if name == "" {
		name = styles.DefaultTheme().Name
	}
	if _, ok := styles.ThemeByName(name); !ok {
		return fmt.Errorf("tema desconocido %q; disponibles: %s", name, strings.Join(styles.ThemeNames(), ", "))
	}

	// A fresh, never-nil map keeps the JSON stable: with no overrides the output
	// is "keys": {} instead of null.
	keys := make(map[string][]string, len(cfg.Keys))
	for action, list := range cfg.Keys {
		keys[action] = append([]string{}, list...)
	}

	out := struct {
		ConfigPath    string              `json:"config_path"`
		Theme         string              `json:"theme"`
		SSHConfig     string              `json:"ssh_config"`
		SSHBinary     string              `json:"ssh_binary"`
		RuntimeDir    string              `json:"runtime_dir"`
		DataDir       string              `json:"data_dir"`
		ListWidth     int                 `json:"list_width"`
		ListCollapsed bool                `json:"list_collapsed"`
		DetailLayout  string              `json:"detail_layout"`
		Keys          map[string][]string `json:"keys"`
	}{
		ConfigPath:    cfg.ConfigPath,
		Theme:         name,
		SSHConfig:     cfg.SSHConfig,
		SSHBinary:     cfg.SSHBinary,
		RuntimeDir:    cfg.RuntimeDir,
		DataDir:       cfg.DataDir,
		ListWidth:     max(cfg.ListWidth, 0),
		ListCollapsed: cfg.ListCollapsed,
		DetailLayout:  detailLayoutName(cfg.DetailLayout),
		Keys:          keys,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("imprimir la configuración: %w", err)
	}

	return nil
}

// detailLayoutName reports the effective layout of the Hosts tab.
func detailLayoutName(raw string) string {
	if raw == "single" {
		return "single"
	}

	return "split"
}

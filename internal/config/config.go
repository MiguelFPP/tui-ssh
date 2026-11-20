// Package config parses flags, environment defaults and the optional JSON file.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Config is the validated runtime configuration of the TUI. ConfigPath and Keys
// come from the optional JSON file and Theme may stay empty for main to apply its
// default. ListWidth and ListCollapsed hold the persisted layout, while SavePath
// carries the resolved path where it can be written even before the file exists.
// ShowVersion, ListThemes and ListKeys ask main to print a catalog and exit;
// InitConfig writes a template and PrintConfig dumps the resolved values, so no
// ssh validation runs for them.
type Config struct {
	ShowVersion   bool
	ListThemes    bool
	ListKeys      bool
	InitConfig    bool
	Force         bool
	PrintConfig   bool
	SSHConfig     string
	SSHBinary     string
	Editor        string
	RuntimeDir    string
	DebugLog      string
	ConfigPath    string
	Theme         string
	Keys          map[string][]string
	ListWidth     int
	ListCollapsed bool
	DetailLayout  string
	SavePath      string
	DataDir       string
}

// Usage is the help text shown when -h or --help is requested.
const Usage = `usage: tui-ssh [flags]

flags:
  --version          print the version and exit
  --list-themes      lista los temas disponibles y sale
  --list-keys        lista las acciones y sus teclas por defecto y sale
  --ssh-config FILE  archivo de configuración de ssh, default: ~/.ssh/config
  --ssh CMD          binario de ssh, default: $SSH o ssh
  --editor CMD       editor para abrir el config, default: $EDITOR o vim
  --runtime-dir DIR  directorio de estado de los túneles, default: $XDG_RUNTIME_DIR/tui-ssh
  --debug-log FILE   debug log file, default: disabled
  --config FILE      archivo de configuración JSON, default: $XDG_CONFIG_HOME/tui-ssh/config.json
  --theme NAME       tema de color, default: tokyonight-night
  --init-config      escribe un archivo de configuración base y sale
  --force            sobrescribe el archivo existente con --init-config
  --print-config     imprime la configuración resuelta y sale`

// ErrHelp reports that -h or --help was requested.
var ErrHelp = flag.ErrHelp

// Errors returned by Parse.
var (
	ErrConfigFile     = errors.New("no se pudo leer el archivo de configuración")
	ErrConfigFormat   = errors.New("el archivo de configuración no es válido")
	ErrSSHConfigPath  = errors.New("--ssh-config debe ser una ruta de archivo")
	ErrEditorRequired = errors.New("--editor no puede estar vacío")
	ErrSSHRequired    = errors.New("--ssh no puede estar vacío")
)

const (
	defaultEditor  = "vim"
	defaultSSH     = "ssh"
	layoutSplit    = "split"
	layoutSingle   = "single"
	dataFilePerm   = 0o600
	configDirPerm  = 0o755
	maxConfigBytes = 1 << 20
)

// Parse validates args, the argument list without the program name, together
// with the environment read through getenv. It returns the resulting
// configuration and never writes to stdout or stderr.
func Parse(args []string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	flags := flag.NewFlagSet("tui-ssh", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	showVersion := flags.Bool("version", false, "muestra la versión y sale")
	listThemes := flags.Bool("list-themes", false, "lista los temas disponibles y sale")
	listKeys := flags.Bool("list-keys", false, "lista las acciones y sus teclas y sale")
	initConfig := flags.Bool("init-config", false, "escribe un archivo de configuración base y sale")
	force := flags.Bool("force", false, "sobrescribe el archivo existente con --init-config")
	printConfig := flags.Bool("print-config", false, "imprime la configuración resuelta y sale")
	sshConfig := flags.String("ssh-config", strings.TrimSpace(getenv("TUI_SSH_SSHCONFIG")), "archivo de configuración de ssh")
	sshBinary := flags.String("ssh", sshDefault(getenv), "binario de ssh")
	editor := flags.String("editor", editorDefault(getenv), "comando del editor externo")
	runtimeDir := flags.String("runtime-dir", strings.TrimSpace(getenv("TUI_SSH_RUNTIME_DIR")), "directorio de estado de los túneles")
	debugLog := flags.String("debug-log", "", "archivo de log de depuración")
	configPath := flags.String("config", strings.TrimSpace(getenv("TUI_SSH_CONFIG")), "archivo de configuración JSON")
	theme := flags.String("theme", strings.TrimSpace(getenv("TUI_SSH_THEME")), "tema de color")

	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if *showVersion {
		return Config{ShowVersion: true}, nil
	}
	// The catalogs are static, so neither the file nor the other flags matter.
	if *listThemes || *listKeys {
		return Config{ListThemes: *listThemes, ListKeys: *listKeys}, nil
	}
	if flags.NArg() > 0 {
		return Config{}, fmt.Errorf("argumento no esperado %q", flags.Arg(0))
	}

	// --init-config needs no existing file and wins over --print-config.
	if *initConfig {
		path, err := resolveInitPath(*configPath)
		if err != nil {
			return Config{}, err
		}

		return Config{InitConfig: true, Force: *force, ConfigPath: path}, nil
	}
	if *printConfig {
		cfg, err := loadConfig(*configPath, *theme)
		if err != nil {
			return Config{}, err
		}
		cfg.PrintConfig = true
		cfg.SSHBinary = strings.TrimSpace(*sshBinary)
		cfg.SSHConfig = resolveSSHConfig(*sshConfig)

		return cfg, nil
	}

	// The file is resolved before the ssh checks so the dump of --print-config and
	// the TUI share the very same loading rules.
	cfg, err := loadConfig(*configPath, *theme)
	if err != nil {
		return Config{}, err
	}

	editorCmd := strings.TrimSpace(*editor)
	if editorCmd == "" {
		return Config{}, ErrEditorRequired
	}

	binary := strings.TrimSpace(*sshBinary)
	if binary == "" {
		return Config{}, ErrSSHRequired
	}

	sshPath := resolveSSHConfig(*sshConfig)
	if sshPath == "" {
		return Config{}, fmt.Errorf("%w: no se pudo resolver la ruta", ErrSSHConfigPath)
	}

	cfg.SSHConfig = sshPath
	cfg.SSHBinary = binary
	cfg.Editor = editorCmd
	cfg.RuntimeDir = strings.TrimSpace(*runtimeDir)
	cfg.DebugLog = strings.TrimSpace(*debugLog)
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir()
	}

	return cfg, nil
}

// loadConfig resolves the path, reads the JSON file and applies the theme
// precedence: flag, then env (already folded into the flag default), then the
// file. ConfigPath is only set when a file was actually loaded, while SavePath
// always carries the resolved path so the UI can persist the layout.
func loadConfig(configPath, theme string) (Config, error) {
	path, explicit := resolveConfigPath(configPath)
	fileCfg, loaded, err := loadFile(path, explicit)
	if err != nil {
		return Config{}, err
	}
	if fileCfg.ListWidth < 0 {
		return Config{}, fmt.Errorf("%w: list_width no puede ser negativo", ErrConfigFormat)
	}
	if err := validateDetailLayout(fileCfg.DetailLayout); err != nil {
		return Config{}, err
	}

	effectiveTheme := strings.TrimSpace(theme)
	if effectiveTheme == "" {
		effectiveTheme = strings.TrimSpace(fileCfg.Theme)
	}

	cfg := Config{
		Theme:         effectiveTheme,
		Keys:          fileCfg.keyOverrides(),
		ListWidth:     fileCfg.ListWidth,
		ListCollapsed: fileCfg.ListCollapsed,
		DetailLayout:  strings.TrimSpace(fileCfg.DetailLayout),
	}
	if loaded {
		cfg.ConfigPath = path
	}
	if path != "" {
		cfg.SavePath = path
	}
	if dir := filepath.Dir(path); path != "" {
		cfg.DataDir = dir
	}

	return cfg, nil
}

// validateDetailLayout accepts the empty default plus the two persisted names.
func validateDetailLayout(layout string) error {
	switch strings.TrimSpace(layout) {
	case "", layoutSplit, layoutSingle:
		return nil
	default:
		return fmt.Errorf("%w: detail_layout %q no es válido; usa %q o %q", ErrConfigFormat, layout, layoutSplit, layoutSingle)
	}
}

// resolveInitPath returns the destination of --init-config. Unlike loadConfig it
// never touches the filesystem, because the file is expected to be missing.
func resolveInitPath(fromFlag string) (string, error) {
	path, _ := resolveConfigPath(fromFlag)
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: no se pudo resolver la ruta de destino; usa --config", ErrConfigFile)
	}

	return path, nil
}

// resolveSSHConfig prefers the explicit path, then ~/.ssh/config, then any existing
// system-wide configuration. An explicit path that does not exist is still returned
// so the TUI can report it, instead of silently falling back.
func resolveSSHConfig(raw string) string {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		path, err := expandHome(trimmed)
		if err != nil {
			return trimmed
		}

		return path
	}

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidate := filepath.Join(home, ".ssh", "config")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}

		return candidate
	}

	return ""
}

// expandHome resolves a leading ~/ against the home directory and makes the path absolute.
func expandHome(raw string) (string, error) {
	path := raw
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolver el home: %w", err)
		}

		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), string(filepath.Separator)))
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolver la ruta: %w", err)
	}

	return abs, nil
}

func editorDefault(getenv func(string) string) string {
	if fromEnv := strings.TrimSpace(getenv("EDITOR")); fromEnv != "" {
		return fromEnv
	}

	return defaultEditor
}

func sshDefault(getenv func(string) string) string {
	if fromEnv := strings.TrimSpace(getenv("SSH")); fromEnv != "" {
		return fromEnv
	}

	return defaultSSH
}

func defaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}

	return filepath.Join(dir, "tui-ssh")
}

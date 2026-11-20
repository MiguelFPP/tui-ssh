package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BaseFile is the template --init-config writes: a valid document with the
// default theme, an unset list layout, the split detail layout and no key
// overrides. It always ends with a newline.
const BaseFile = `{
  "theme": "tokyonight-night",
  "list_width": 0,
  "list_collapsed": false,
  "detail_layout": "split",
  "keys": {}
}
`

// ErrConfigExists reports that the destination exists and --force was not given.
var ErrConfigExists = errors.New("el archivo de configuración ya existe; usa --force")

// WriteDefault writes BaseFile to path. It creates the parent directories, never
// overwrites an existing file unless force is set and writes atomically, so an
// interrupted run never leaves a partial file behind.
func WriteDefault(path string, force bool) error {
	target := strings.TrimSpace(path)
	if target == "" {
		return fmt.Errorf("%w: ruta vacía", ErrConfigFile)
	}

	_, err := os.Stat(target)
	switch {
	case err == nil:
		if !force {
			return fmt.Errorf("%w: %s", ErrConfigExists, target)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s: %w", ErrConfigFile, target, err)
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, configDirPerm); err != nil {
		return fmt.Errorf("%w: no se pudo crear %s: %w", ErrConfigFile, dir, err)
	}

	return writeFileAtomic(target, BaseFile, force)
}

// SaveLayout persists the list layout, preserving the theme and the key overrides
// of an existing file and creating the parent directories when needed.
func SaveLayout(path string, width int, collapsed bool) error {
	target := strings.TrimSpace(path)
	if target == "" {
		return fmt.Errorf("%w: ruta vacía", ErrConfigFile)
	}
	if width < 0 {
		return fmt.Errorf("%w: list_width no puede ser negativo", ErrConfigFormat)
	}

	// A missing file yields an empty config; an unreadable or invalid one aborts
	// the save before anything is written.
	cfg, _, err := loadFile(target, false)
	if err != nil {
		return err
	}

	cfg.ListWidth = width
	cfg.ListCollapsed = collapsed

	return writeJSON(target, cfg)
}

// SaveDetailLayout persists the detail layout of the Hosts tab.
func SaveDetailLayout(path, layout string) error {
	target := strings.TrimSpace(path)
	if target == "" {
		return fmt.Errorf("%w: ruta vacía", ErrConfigFile)
	}
	if err := validateDetailLayout(layout); err != nil {
		return err
	}

	cfg, _, err := loadFile(target, false)
	if err != nil {
		return err
	}

	cfg.DetailLayout = strings.TrimSpace(layout)

	return writeJSON(target, cfg)
}

// writeJSON serialises cfg and writes it atomically, creating the directory.
func writeJSON(target string, cfg fileConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrConfigFormat, target, err)
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, configDirPerm); err != nil {
		return fmt.Errorf("%w: no se pudo crear %s: %w", ErrConfigFile, dir, err)
	}

	return writeFileAtomic(target, string(data)+"\n", true)
}

// writeFileAtomic writes data through a temporary file in the target directory:
// the rename is atomic, so readers never see a half-written file.
func writeFileAtomic(path, data string, replace bool) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrConfigFile, path, err)
	}

	name := tmp.Name()
	// After a successful rename the temporary file no longer exists, so this
	// cleanup only removes leftovers from a failed attempt.
	defer func() { _ = os.Remove(name) }()

	if _, err := tmp.WriteString(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("%w: %s: %w", ErrConfigFile, name, err)
	}
	if err := tmp.Chmod(dataFilePerm); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("%w: %s: %w", ErrConfigFile, name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrConfigFile, name, err)
	}

	if err := os.Rename(name, path); err != nil {
		if !replace {
			return fmt.Errorf("%w: %s: %w", ErrConfigFile, path, err)
		}

		// Windows refuses to rename over an existing file: drop the destination
		// and retry once.
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s: %w", ErrConfigFile, path, err)
		}
		if err := os.Rename(name, path); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrConfigFile, path, err)
		}
	}

	return nil
}

// fileConfig mirrors the JSON file; names and action IDs are validated later.
// The layout fields are always serialized, while an empty theme or set of keys
// is omitted.
type fileConfig struct {
	Theme         string             `json:"theme,omitempty"`
	ListWidth     int                `json:"list_width"`
	ListCollapsed bool               `json:"list_collapsed"`
	DetailLayout  string             `json:"detail_layout,omitempty"`
	Keys          map[string]KeyList `json:"keys,omitempty"`
}

// KeyList is an action's binding: a string or a list of strings.
type KeyList []string

// UnmarshalJSON accepts a string or a string array and rejects null.
func (k *KeyList) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("la lista de teclas no admite null")
	}

	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*k = KeyList{single}

		return nil
	}

	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return errors.New("se esperaba una cadena o una lista de cadenas")
	}

	*k = KeyList(list)

	return nil
}

// keyOverrides returns the raw bindings, or nil when there are none.
func (f fileConfig) keyOverrides() map[string][]string {
	if len(f.Keys) == 0 {
		return nil
	}

	keys := make(map[string][]string, len(f.Keys))
	for action, list := range f.Keys {
		keys[action] = []string(list)
	}

	return keys
}

// resolveConfigPath prefers an explicit flag/env path over the XDG default.
func resolveConfigPath(fromFlag string) (path string, explicit bool) {
	if trimmed := strings.TrimSpace(fromFlag); trimmed != "" {
		return trimmed, true
	}

	return defaultConfigPath(), false
}

// defaultConfigPath returns the XDG config file, or an empty string.
func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}

	return filepath.Join(dir, "tui-ssh", "config.json")
}

// loadFile reads the file; a missing default file is ignored.
func loadFile(path string, explicit bool) (fileConfig, bool, error) {
	if strings.TrimSpace(path) == "" {
		return fileConfig{}, false, nil
	}

	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if explicit {
			return fileConfig{}, false, fmt.Errorf("%w: %s no existe", ErrConfigFile, path)
		}

		return fileConfig{}, false, nil
	case err != nil:
		return fileConfig{}, false, fmt.Errorf("%w: %s: %w", ErrConfigFile, path, err)
	case info.IsDir():
		return fileConfig{}, false, fmt.Errorf("%w: %s es un directorio", ErrConfigFile, path)
	case info.Size() > maxConfigBytes:
		return fileConfig{}, false, fmt.Errorf("%w: %s supera %d bytes", ErrConfigFile, path, maxConfigBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, false, fmt.Errorf("%w: %s: %w", ErrConfigFile, path, err)
	}

	cfg, err := decodeFile(data, path)
	if err != nil {
		return fileConfig{}, false, err
	}

	return cfg, true, nil
}

// decodeFile rejects unknown fields and trailing data.
func decodeFile(data []byte, path string) (fileConfig, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var cfg fileConfig
	if err := dec.Decode(&cfg); err != nil {
		return fileConfig{}, fmt.Errorf("%w: %s: %w", ErrConfigFormat, path, err)
	}

	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return fileConfig{}, fmt.Errorf("%w: %s: datos extra tras el objeto JSON", ErrConfigFormat, path)
	}

	return cfg, nil
}

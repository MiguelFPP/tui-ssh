// Package tags persists the two pieces of state that deliberately live outside
// ~/.ssh/config: the tags of each alias and the forward specifications the user
// declared. Both files are strict JSON written atomically with 0600 permissions.
package tags

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/ports"
)

const (
	filePerm = 0o600
	dirPerm  = 0o755

	// maxFileBytes bounds both stores; a tag index never needs more.
	maxFileBytes = 1 << 20
)

// Files are the two documents the store manages.
type Files struct {
	Tags     string
	Forwards string
}

// Store implements ports.TagService over the XDG configuration directory.
type Store struct {
	files Files

	readFile func(string) ([]byte, error)
	stat     func(string) (fs.FileInfo, error)
}

var _ ports.TagService = (*Store)(nil)

// New builds the store for the given files.
func New(files Files) *Store {
	return &Store{
		files:    files,
		readFile: os.ReadFile,
		stat:     os.Stat,
	}
}

// Tags reads the tag index. A missing file is an empty index, never an error.
func (s *Store) Tags(ctx context.Context) (domain.TagIndex, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tags: Tags: %w", err)
	}

	index := domain.TagIndex{}

	data, err := s.read(s.files.Tags)
	if err != nil {
		if errors.Is(err, domain.ErrConfigMissing) {
			return index, nil
		}

		return nil, err
	}

	raw := make(map[string][]string)
	if err := decodeStrict(data, &raw, s.files.Tags); err != nil {
		return nil, err
	}

	for alias, list := range raw {
		// tags.json is external text: another local user or a synced dotfile bundle
		// may have written it, and the alias is matched against the live hosts and
		// its tags are rendered as chips in the list.
		if domain.ValidateAlias(alias) != nil {
			continue
		}

		tags, err := domain.NormalizeTags(list)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %s: %w", domain.ErrConfigInvalid, s.files.Tags, alias, err)
		}

		if len(tags) == 0 {
			continue
		}

		index[alias] = tags
	}

	return index, nil
}

// SetTags replaces the tags of alias, removing the entry when the list is empty.
func (s *Store) SetTags(ctx context.Context, alias string, list []string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("tags: SetTags: %w", err)
	}
	if err := domain.ValidateAlias(alias); err != nil {
		return err
	}

	tags, err := domain.NormalizeTags(list)
	if err != nil {
		return err
	}

	raw, err := s.readMap(s.files.Tags)
	if err != nil {
		return err
	}

	if len(tags) == 0 {
		delete(raw, alias)
	} else {
		raw[alias] = tags
	}

	return s.writeMap(s.files.Tags, raw)
}

// Forwards reads the declared forward specs, keyed by alias.
func (s *Store) Forwards(ctx context.Context) (map[string][]domain.ForwardSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tags: Forwards: %w", err)
	}

	out := make(map[string][]domain.ForwardSpec)

	data, err := s.read(s.files.Forwards)
	if err != nil {
		if errors.Is(err, domain.ErrConfigMissing) {
			return out, nil
		}

		return nil, err
	}

	raw := make(map[string][]forwardJSON)
	if err := decodeStrict(data, &raw, s.files.Forwards); err != nil {
		return nil, err
	}

	for alias, list := range raw {
		specs := make([]domain.ForwardSpec, 0, len(list))

		for _, item := range list {
			spec, err := item.toDomain()
			if err != nil {
				return nil, fmt.Errorf("%w: %s: %s: %w", domain.ErrConfigInvalid, s.files.Forwards, alias, err)
			}

			specs = append(specs, spec)
		}

		if err := domain.ValidateForwardSpecs(specs); err != nil {
			return nil, fmt.Errorf("%w: %s: %s: %w", domain.ErrConfigInvalid, s.files.Forwards, alias, err)
		}

		if len(specs) > 0 {
			out[alias] = specs
		}
	}

	return out, nil
}

// SetForwards replaces the declared forward specs of alias.
func (s *Store) SetForwards(ctx context.Context, alias string, specs []domain.ForwardSpec) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("tags: SetForwards: %w", err)
	}
	if err := domain.ValidateAlias(alias); err != nil {
		return err
	}
	if err := domain.ValidateForwardSpecs(specs); err != nil {
		return err
	}

	raw := make(map[string][]forwardJSON)
	data, err := s.read(s.files.Forwards)
	switch {
	case err != nil && !errors.Is(err, domain.ErrConfigMissing):
		return err
	case err == nil:
		if err := decodeStrict(data, &raw, s.files.Forwards); err != nil {
			return err
		}
	}

	if len(specs) == 0 {
		delete(raw, alias)
	} else {
		list := make([]forwardJSON, 0, len(specs))
		for _, spec := range specs {
			list = append(list, fromDomain(spec))
		}
		raw[alias] = list
	}

	return s.writeMap(s.files.Forwards, raw)
}

// readMap reads a JSON object of lists; a missing file is an empty object.
func (s *Store) readMap(path string) (map[string][]string, error) {
	out := make(map[string][]string)

	data, err := s.read(path)
	if err != nil {
		if errors.Is(err, domain.ErrConfigMissing) {
			return out, nil
		}

		return nil, err
	}

	if err := decodeStrict(data, &out, path); err != nil {
		return nil, err
	}

	return out, nil
}

// writeMap serialises the object with sorted keys, so the file diffs cleanly.
// The JSON encoder already sorts the keys of a map, so no extra pass is needed.
func (s *Store) writeMap(path string, raw any) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %s: %w", domain.ErrConfigInvalid, path, err)
	}

	return writeAtomic(path, string(data)+"\n")
}

// read reads one file, translating the failures into domain errors.
func (s *Store) read(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: ruta vacía", domain.ErrInvalidPath)
	}

	info, err := s.stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%w: %s", domain.ErrConfigMissing, path)
	case err != nil:
		return nil, fmt.Errorf("%w: %s: %w", domain.ErrConfigInvalid, path, err)
	case info.IsDir():
		return nil, fmt.Errorf("%w: %s es un directorio", domain.ErrConfigInvalid, path)
	case info.Size() > maxFileBytes:
		return nil, fmt.Errorf("%w: %s supera %d bytes", domain.ErrTooLarge, path, maxFileBytes)
	}

	data, err := s.readFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, fmt.Errorf("%w: %s", domain.ErrPermissionDenied, path)
		}

		return nil, fmt.Errorf("%w: %s: %w", domain.ErrConfigInvalid, path, err)
	}

	return data, nil
}

// decodeStrict rejects unknown fields, nulls and trailing data.
func decodeStrict(data []byte, out any, path string) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("%w: %s: el documento no admite null", domain.ErrConfigInvalid, path)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: %s: %w", domain.ErrConfigInvalid, path, err)
	}

	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %s: datos extra tras el documento JSON", domain.ErrConfigInvalid, path)
	}

	return nil
}

// writeAtomic writes through a temporary sibling and renames, so a reader never
// sees a half-written file. The parent directory is created when missing.
func writeAtomic(path, data string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("%w: no se pudo crear %s: %w", domain.ErrPermissionDenied, dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tui-ssh-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, path, err)
	}

	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if _, err := tmp.WriteString(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("%w: %s: %w", domain.ErrConfigInvalid, name, err)
	}
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: %s: %w", domain.ErrConfigInvalid, name, err)
	}

	if err := os.Rename(name, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, path, err)
		}
		if err := os.Rename(name, path); err != nil {
			return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, path, err)
		}
	}

	return nil
}

// forwardJSON is the on-disk shape of one forward spec.
type forwardJSON struct {
	Name       string `json:"name,omitempty"`
	Direction  string `json:"direction"`
	BindAddr   string `json:"bind_addr"`
	BindPort   int    `json:"bind_port"`
	TargetHost string `json:"target_host,omitempty"`
	TargetPort int    `json:"target_port,omitempty"`
}

func fromDomain(spec domain.ForwardSpec) forwardJSON {
	normalized := spec.Normalize()

	return forwardJSON{
		Name:       normalized.Name,
		Direction:  normalized.Direction.String(),
		BindAddr:   normalized.Bind.Addr,
		BindPort:   normalized.Bind.Port,
		TargetHost: normalized.Target.Host,
		TargetPort: normalized.Target.Port,
	}
}

func (f forwardJSON) toDomain() (domain.ForwardSpec, error) {
	direction, err := domain.ParseDirection(f.Direction)
	if err != nil {
		return domain.ForwardSpec{}, err
	}

	spec := domain.ForwardSpec{
		Name:      f.Name,
		Direction: direction,
		Bind:      domain.BindAddr{Addr: f.BindAddr, Port: f.BindPort},
		Target:    domain.TargetAddr{Host: f.TargetHost, Port: f.TargetPort},
	}.Normalize()

	if err := spec.Validate(); err != nil {
		return domain.ForwardSpec{}, err
	}

	return spec, nil
}

// SortAliases returns the aliases of the index in a deterministic order.
func SortAliases(index domain.TagIndex) []string {
	aliases := make([]string, 0, len(index))
	for alias := range index {
		aliases = append(aliases, alias)
	}

	sort.Strings(aliases)

	return aliases
}

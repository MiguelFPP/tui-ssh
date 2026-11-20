package domain

import (
	"fmt"
	"strings"
)

// ConfigFile is one file the loader read, with its size in lines.
type ConfigFile struct {
	Path     string
	Lines    int
	Included bool
	Err      string
}

// ConfigReport summarises what the loader found: the files it read and everything
// it had to skip. It is what the Config tab shows next to the raw text.
type ConfigReport struct {
	Main     string
	Files    []ConfigFile
	Hosts    int
	Warnings []string
	Err      error
}

// OK reports whether the report describes a readable configuration.
func (r ConfigReport) OK() bool {
	return len(r.Files) > 0 && (r.Err == nil || r.Hosts > 0)
}

// AddWarning records a warning once, keeping the report deterministic. Warnings
// embed paths and values read from the configuration, so the control characters are
// stripped before the panel renders them.
func (r *ConfigReport) AddWarning(format string, args ...any) {
	message := StripControlChars(fmt.Sprintf(format, args...))
	for _, existing := range r.Warnings {
		if existing == message {
			return
		}
	}

	if len(r.Warnings) >= MaxDirectives {
		return
	}

	r.Warnings = append(r.Warnings, message)
}

// ConfigSource is the raw text of the configuration, already bounded, for the
// read-only viewer of the Config tab.
type ConfigSource struct {
	Path       string
	Lines      []string
	Truncated  bool
	TotalLines int
}

// SSHCommand is the binary and the arguments of one ssh invocation, never a shell
// command line. A zero value is not runnable.
type SSHCommand struct {
	Binary string
	Args   []string
}

// IsZero reports whether the command has no binary to run.
func (c SSHCommand) IsZero() bool {
	return strings.TrimSpace(c.Binary) == ""
}

// String joins the command for logging and debugging only. It is never executed.
func (c SSHCommand) String() string {
	if c.IsZero() {
		return ""
	}

	return strings.Join(append([]string{c.Binary}, c.Args...), " ")
}

// TagIndex maps an alias to its tags, as persisted outside the ssh config.
type TagIndex map[string][]string

// All returns every distinct tag in stable order of first appearance.
func (i TagIndex) All() []string {
	seen := make(map[string]struct{}, len(i))
	all := make([]string, 0, len(i))

	// Iterating the map is unordered by design; the callers that need a stable
	// order sort the result.
	for _, tags := range i {
		for _, tag := range tags {
			if _, ok := seen[tag]; ok {
				continue
			}

			seen[tag] = struct{}{}
			all = append(all, tag)
		}
	}

	return all
}

// NormalizeTag lowercases and trims a tag, rejecting the ones the UI cannot render
// as a single word.
func NormalizeTag(raw string) (string, error) {
	tag := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case tag == "":
		return "", fmt.Errorf("%w: la etiqueta está vacía", ErrInvalidArgument)
	case len(tag) > MaxTagBytes:
		return "", fmt.Errorf("%w: la etiqueta %q supera %d bytes", ErrInvalidArgument, tag, MaxTagBytes)
	}

	for _, r := range tag {
		if isControl(r) {
			return "", fmt.Errorf("%w: la etiqueta contiene caracteres de control", ErrInvalidArgument)
		}
	}

	return tag, nil
}

// NormalizeTags trims, lowercases and de-duplicates the list, preserving order.
func NormalizeTags(raw []string) ([]string, error) {
	if len(raw) > MaxTagsPerHost {
		return nil, fmt.Errorf("%w: más de %d etiquetas", ErrInvalidArgument, MaxTagsPerHost)
	}

	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))

	for _, item := range raw {
		fields := strings.FieldsFunc(item, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})

		for _, field := range fields {
			tag, err := NormalizeTag(field)
			if err != nil {
				return nil, err
			}

			if _, ok := seen[tag]; ok {
				continue
			}

			seen[tag] = struct{}{}
			out = append(out, tag)
		}
	}

	return out, nil
}

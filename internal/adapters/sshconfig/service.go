// Package sshconfig enumerates the hosts of the OpenSSH client configuration.
// It deliberately parses only the block structure (Host, Include and Match):
// resolving the effective options of an alias is the job of `ssh -G`, so this
// adapter never tries to reimplement the ssh option semantics.
package sshconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/ports"
)

// Service implements ports.SSHConfigService over the local filesystem.
type Service struct {
	path   string
	home   string
	lookup func(string) ([]string, error)

	// readFile and stat are seams for the tests; production uses the os package.
	readFile func(string) ([]byte, error)
	stat     func(string) (fs.FileInfo, error)
}

var _ ports.SSHConfigService = (*Service)(nil)

// New builds the adapter for one configuration file. An empty path is a valid
// state: Hosts reports ErrConfigMissing with the path the user should create.
func New(path string) *Service {
	home, _ := os.UserHomeDir()

	return &Service{
		path:     strings.TrimSpace(path),
		home:     home,
		lookup:   filepath.Glob,
		readFile: os.ReadFile,
		stat:     os.Stat,
	}
}

// Path returns the main configuration file the service reads.
func (s *Service) Path() string {
	return s.path
}

// Hosts returns every connectable alias in source order. Wildcard patterns and
// negations describe defaults, so they never become connectable hosts.
func (s *Service) Hosts(ctx context.Context) ([]domain.Host, error) {
	hosts, _, err := s.scan(ctx)
	if err != nil {
		return nil, err
	}

	return hosts, nil
}

// Report describes what the scan read, including the warnings it collected.
func (s *Service) Report(ctx context.Context) (domain.ConfigReport, error) {
	_, report, err := s.scan(ctx)

	return report, err
}

// Source returns the raw text of the main file, bounded to MaxTunnelLogLines so
// the read-only viewer never keeps an unbounded slice in memory.
func (s *Service) Source(ctx context.Context) (domain.ConfigSource, error) {
	if err := ctx.Err(); err != nil {
		return domain.ConfigSource{}, fmt.Errorf("sshconfig: Source: %w", err)
	}

	source := domain.ConfigSource{Path: s.path}
	if s.path == "" {
		return source, fmt.Errorf("%w: no hay ruta de configuración", domain.ErrConfigMissing)
	}

	data, err := s.read(s.path)
	if err != nil {
		return source, err
	}

	lines := strings.Split(string(data), "\n")
	source.TotalLines = len(lines)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		source.TotalLines--
		lines = lines[:len(lines)-1]
	}

	if len(lines) > domain.MaxTunnelLogLines {
		lines = lines[:domain.MaxTunnelLogLines]
		source.Truncated = true
	}

	source.Lines = lines

	return source, nil
}

// scan reads the main file and every include, returning the hosts in source order.
func (s *Service) scan(ctx context.Context) ([]domain.Host, domain.ConfigReport, error) {
	report := domain.ConfigReport{Main: s.path}

	if s.path == "" {
		err := fmt.Errorf("%w: no hay ruta de configuración", domain.ErrConfigMissing)

		return nil, report, err
	}

	if err := ctx.Err(); err != nil {
		return nil, report, fmt.Errorf("sshconfig: scan: %w", err)
	}

	hosts := make([]domain.Host, 0, 16)
	visited := make(map[string]bool, 4)
	stack := make(map[string]bool, 4)

	// Any failure of the main file is fatal: without it there is nothing to show.
	if err := s.scanFile(ctx, s.path, false, 0, visited, stack, &hosts, &report); err != nil {
		report.Err = err

		return nil, report, err
	}

	report.Hosts = len(hosts)

	return hosts, report, nil
}

// scanFile parses one file, recursing into its Include directives before the Host
// blocks that follow them, which is how ssh splices included files in place.
func (s *Service) scanFile(
	ctx context.Context,
	path string,
	included bool,
	depth int,
	visited map[string]bool,
	stack map[string]bool,
	hosts *[]domain.Host,
	report *domain.ConfigReport,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sshconfig: scanFile: %w", err)
	}

	if depth > domain.MaxIncludeDepth {
		report.AddWarning("include: se superó la profundidad máxima (%d) en %s", domain.MaxIncludeDepth, path)

		return nil
	}
	if stack[path] {
		report.AddWarning("include circular detectado en %s", path)

		return nil
	}

	data, err := s.read(path)
	if err != nil {
		// A broken main file is fatal: there is nothing to show. A broken include is
		// a warning, so the rest of the configuration still loads.
		if !included || errors.Is(err, domain.ErrConfigMissing) {
			return err
		}

		report.Files = append(report.Files, domain.ConfigFile{Path: path, Included: true, Err: err.Error()})
		report.AddWarning("%s: %v", path, err)

		return nil
	}

	lines := strings.Split(string(data), "\n")
	entry := domain.ConfigFile{Path: path, Lines: len(lines), Included: included}
	report.Files = append(report.Files, entry)

	visited[path] = true
	stack[path] = true
	defer delete(stack, path)

	// The current block receives the directives of every Host line until the next
	// Host, Match or Include resets it.
	var current *domain.Host
	// matchAll tracks an active `Match all` block, which applies to every host and
	// therefore cannot be attributed to one alias.
	matchAll := false

	flush := func() {
		if current == nil {
			return
		}
		if len(*hosts) < domain.MaxHosts {
			*hosts = append(*hosts, *current)
		}
		current = nil
	}

	for index, raw := range lines {
		lineNumber := index + 1

		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) > domain.MaxConfigLineBytes {
			report.AddWarning("%s:%d: línea demasiado larga, ignorada", path, lineNumber)
			continue
		}

		key, value := splitDirective(line)
		if key == "" {
			continue
		}

		switch key {
		case "host":
			flush()

			matchAll = false
			patterns := parsePatterns(value)
			// A trailing `Host *` or a block of negations carries no connectable alias.
			if !hasConnectable(patterns) {
				current = nil

				continue
			}
			current = newHost(patterns, path, lineNumber)
		case "match":
			flush()

			current = nil
			matchAll = strings.EqualFold(strings.TrimSpace(value), "all")
			if !matchAll {
				report.AddWarning("%s:%d: bloque Match no soportado, se ignora", path, lineNumber)
			}
		case "include":
			flush()

			current = nil
			matchAll = false
			s.scanIncludes(ctx, value, path, depth, visited, stack, hosts, report)
		default:
			if current != nil {
				if len(current.Declared) < domain.MaxDirectives {
					current.Declared = append(current.Declared, domain.Directive{Key: key, Value: value, Line: lineNumber})
				}
			} else if matchAll {
				// Directives of a Match all block are shared by every host: they are
				// not attributed, but their presence is worth a warning.
				report.AddWarning("%s:%d: directivas de Match all no se atribuyen a un host", path, lineNumber)
			}
		}
	}

	flush()

	return nil
}

// scanIncludes expands one Include value: several patterns, globs, ~ and paths
// relative to ~/.ssh, which is where ssh resolves them from.
func (s *Service) scanIncludes(
	ctx context.Context,
	value, from string,
	depth int,
	visited map[string]bool,
	stack map[string]bool,
	hosts *[]domain.Host,
	report *domain.ConfigReport,
) {
	for _, pattern := range strings.Fields(value) {
		expanded, err := s.expandPattern(pattern, from)
		if err != nil {
			report.AddWarning("%s: include %q: %v", from, pattern, err)

			continue
		}

		matches := s.glob(expanded)
		if len(matches) == 0 {
			report.AddWarning("%s: include %q no encontró archivos", from, pattern)

			continue
		}

		for _, match := range matches {
			abs, err := filepath.Abs(match)
			if err != nil {
				report.AddWarning("include %s: %v", match, err)

				continue
			}

			// A file already on the current stack is a cycle, and one already read is
			// a repeat: ssh reads the first occurrence only, so both are skipped.
			if stack[abs] {
				report.AddWarning("include circular detectado en %s", abs)

				continue
			}
			if visited[abs] {
				continue
			}

			if err := s.scanFile(ctx, abs, true, depth+1, visited, stack, hosts, report); err != nil &&
				!errors.Is(err, domain.ErrConfigMissing) {
				report.AddWarning("%s: %v", abs, err)
			}
		}
	}
}

// expandPattern rewrites ~ and resolves a relative pattern against ~/.ssh first and
// the directory of the including file second, which covers both conventions.
func (s *Service) expandPattern(pattern, from string) (string, error) {
	value := strings.TrimSpace(pattern)
	if value == "" {
		return "", fmt.Errorf("%w: patrón vacío", domain.ErrInvalidArgument)
	}

	if value == "~" || strings.HasPrefix(value, "~/") {
		if s.home == "" {
			return "", fmt.Errorf("%w: no se pudo resolver el home", domain.ErrInvalidPath)
		}

		rest := strings.TrimPrefix(strings.TrimPrefix(value, "~"), string(filepath.Separator))

		return filepath.Join(s.home, rest), nil
	}

	if filepath.IsAbs(value) {
		return value, nil
	}

	// ssh resolves a relative Include against ~/.ssh, not against the file that
	// declares it, and falls back to the declaring directory when nothing matches.
	if s.home != "" {
		candidate := filepath.Join(s.home, ".ssh", value)
		if len(s.glob(candidate)) > 0 {
			return candidate, nil
		}
	}

	return filepath.Join(filepath.Dir(from), value), nil
}

// glob expands a pattern, returning the matches sorted for a deterministic report.
func (s *Service) glob(pattern string) []string {
	if s.lookup == nil {
		return nil
	}

	matches, err := s.lookup(pattern)
	if err != nil {
		return nil
	}

	sort.Strings(matches)

	return matches
}

// read reads one file, translating the failures into domain errors.
func (s *Service) read(path string) ([]byte, error) {
	info, err := s.stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%w: %s", domain.ErrConfigMissing, path)
	case err != nil:
		return nil, fmt.Errorf("%w: %s: %w", domain.ErrConfigInvalid, path, err)
	case info.IsDir():
		return nil, fmt.Errorf("%w: %s es un directorio", domain.ErrConfigInvalid, path)
	case info.Size() > domain.MaxConfigBytes:
		return nil, fmt.Errorf("%w: %s supera %d bytes", domain.ErrTooLarge, path, domain.MaxConfigBytes)
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

// newHost builds the Host for one Host line, keeping the first literal alias as
// the connectable name and dropping the wildcard and negated patterns.
func newHost(patterns []domain.Pattern, file string, line int) *domain.Host {
	host := &domain.Host{SourceFile: file, SourceLine: line, Patterns: patterns}
	host.Alias = firstLiteral(patterns)

	return host
}

// firstLiteral returns the first pattern that can be connected to. A block whose
// only patterns are wildcards has no alias and is skipped by the caller.
func firstLiteral(patterns []domain.Pattern) string {
	for _, pattern := range patterns {
		if pattern.Wild || pattern.Negated || pattern.Value == "" {
			continue
		}

		return pattern.Value
	}

	return ""
}

// hasConnectable reports whether at least one pattern of the block names a host.
// A block of negations or wildcards alone describes defaults, not an alias.
func hasConnectable(patterns []domain.Pattern) bool {
	return firstLiteral(patterns) != ""
}

// parsePatterns splits the value of a Host line into its patterns.
func parsePatterns(value string) []domain.Pattern {
	fields := strings.Fields(value)
	patterns := make([]domain.Pattern, 0, len(fields))

	for _, field := range fields {
		pattern := domain.NewPattern(field)
		if pattern.Value == "" {
			continue
		}

		patterns = append(patterns, pattern)
	}

	return patterns
}

// splitDirective splits one line into its lowercased key and its value. OpenSSH
// accepts both "Key value" and "Key=value".
func splitDirective(line string) (string, string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", ""
	}

	index := strings.IndexAny(trimmed, " \t=")
	if index < 0 {
		return strings.ToLower(trimmed), ""
	}

	key := strings.ToLower(strings.TrimSpace(trimmed[:index]))
	value := strings.TrimSpace(trimmed[index+1:])

	return key, unquote(value)
}

// stripComment removes a trailing comment that is not inside quotes.
func stripComment(line string) string {
	var quote rune

	for index, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#':
			return line[:index]
		}
	}

	return line
}

// unquote removes one pair of matching surrounding quotes.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}

	first, last := value[0], value[len(value)-1]
	if first != last || (first != '"' && first != '\'') {
		return value
	}

	return value[1 : len(value)-1]
}

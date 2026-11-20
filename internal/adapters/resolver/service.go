// Package resolver resolves the effective options of an ssh alias by running
// `ssh -G`. Delegating to the real binary keeps the TUI honest: it never has to
// reimplement the option precedence of OpenSSH.
package resolver

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/ports"
)

// Service implements ports.ResolverService with a bounded per-alias cache.
type Service struct {
	binary     string
	configPath string

	mu    sync.Mutex
	cache map[string]domain.Resolved

	// run is the seam the tests replace; production executes the binary.
	run func(ctx context.Context, binary string, args []string) ([]byte, error)
}

var _ ports.ResolverService = (*Service)(nil)

// New builds the resolver for one ssh binary and the configuration file it must
// resolve against. The path is passed as `-F`: OpenSSH resolves `~` from the
// password database, not from $HOME, so without it the answer could come from a
// different file than the one the TUI shows.
func New(binary, configPath string) *Service {
	return &Service{
		binary:     strings.TrimSpace(binary),
		configPath: strings.TrimSpace(configPath),
		cache:      make(map[string]domain.Resolved, 32),
		run:        runCommand,
	}
}

// Resolve returns the effective configuration of alias, cached for the session.
func (s *Service) Resolve(ctx context.Context, alias string) (domain.Resolved, error) {
	if err := domain.ValidateAlias(alias); err != nil {
		return domain.Resolved{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Resolved{}, fmt.Errorf("resolver: Resolve: %w", err)
	}

	if cached, ok := s.cached(alias); ok {
		return cached, nil
	}

	binary := s.binary
	if binary == "" {
		binary = "ssh"
	}

	ctx, cancel := context.WithTimeout(ctx, domain.ResolveTimeout)
	defer cancel()

	out, err := s.run(ctx, binary, s.args(alias))
	if err != nil {
		return domain.Resolved{}, fmt.Errorf("%w: %s", domain.ErrSSHUnavailable, err)
	}

	resolved := domain.Resolved{Host: domain.ParseResolved(alias, splitLines(out))}
	s.store(alias, resolved)

	return resolved, nil
}

// args builds the `ssh -G` invocation. The config path is passed with -F so the
// resolution matches the file the TUI is showing.
func (s *Service) args(alias string) []string {
	args := make([]string, 0, 4)
	if s.configPath != "" {
		args = append(args, "-F", s.configPath)
	}

	return append(args, "-G", alias)
}

// Invalidate drops the cache entry of alias, or the whole cache when alias is empty.
func (s *Service) Invalidate(alias string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(alias) == "" {
		s.cache = make(map[string]domain.Resolved, 32)

		return
	}

	delete(s.cache, alias)
}

func (s *Service) cached(alias string) (domain.Resolved, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resolved, ok := s.cache[alias]

	return resolved, ok
}

func (s *Service) store(alias string, resolved domain.Resolved) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache == nil {
		s.cache = make(map[string]domain.Resolved, 32)
	}
	s.cache[alias] = resolved
}

// splitLines trims the trailing empty line of the command output.
func splitLines(out []byte) []string {
	text := strings.ReplaceAll(string(out), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}

	return strings.Split(text, "\n")
}

// runCommand executes the binary without a shell and returns its stdout.
func runCommand(ctx context.Context, binary string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return out, nil
}

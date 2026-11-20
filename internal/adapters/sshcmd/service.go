// Package sshcmd resolves the binaries the TUI hands the terminal over to: the
// interactive ssh session and the external editor for the ssh configuration.
package sshcmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/ports"
)

// Session resolves an interactive `ssh <alias>` invocation.
type Session struct {
	binary   string
	lookPath func(string) (string, error)
}

var _ ports.SessionService = (*Session)(nil)

// New builds the session resolver. An empty binary falls back to "ssh".
func New(binary string) *Session {
	name := strings.TrimSpace(binary)
	if name == "" {
		name = "ssh"
	}

	return &Session{binary: name, lookPath: exec.LookPath}
}

// Command resolves the absolute binary and the single alias argument. The caller
// runs it with tea.ExecProcess, so the TUI restores itself when ssh exits.
func (s *Session) Command(ctx context.Context, alias string) (domain.SSHCommand, error) {
	if err := ctx.Err(); err != nil {
		return domain.SSHCommand{}, fmt.Errorf("sshcmd: Command: %w", err)
	}
	if err := domain.ValidateAlias(alias); err != nil {
		return domain.SSHCommand{}, err
	}

	binary, err := s.lookup()
	if err != nil {
		return domain.SSHCommand{}, fmt.Errorf("%w: %q", domain.ErrSSHUnavailable, s.binary)
	}

	return domain.SSHCommand{Binary: binary, Args: []string{alias}}, nil
}

func (s *Session) lookup() (string, error) {
	if s.lookPath == nil {
		return exec.LookPath(s.binary)
	}

	return s.lookPath(s.binary)
}

// ConfigEditor resolves the command that opens the ssh configuration in the user
// editor. The TUI never writes ~/.ssh/config itself.
type ConfigEditor struct {
	editor   domain.SSHCommand
	lookPath func(string) (string, error)
}

var _ ports.ConfigEditorService = (*ConfigEditor)(nil)

// NewConfigEditor parses the editor command once at boot.
func NewConfigEditor(command string) (*ConfigEditor, error) {
	parsed, err := parseCommand(command)
	if err != nil {
		return nil, err
	}

	return &ConfigEditor{editor: parsed, lookPath: exec.LookPath}, nil
}

// Resolve returns the binary plus the absolute config path, never a shell line.
func (e *ConfigEditor) Resolve(ctx context.Context, path string) (domain.SSHCommand, error) {
	if err := ctx.Err(); err != nil {
		return domain.SSHCommand{}, fmt.Errorf("sshcmd: Resolve: %w", err)
	}

	target := strings.TrimSpace(path)
	if target == "" {
		return domain.SSHCommand{}, fmt.Errorf("%w: no hay archivo que abrir", domain.ErrInvalidPath)
	}

	binary, err := e.lookup(e.editor.Binary)
	if err != nil {
		return domain.SSHCommand{}, fmt.Errorf("%w: %q", domain.ErrEditorUnavailable, e.editor.Binary)
	}

	args := make([]string, 0, len(e.editor.Args)+1)
	args = append(args, e.editor.Args...)
	args = append(args, target)

	return domain.SSHCommand{Binary: binary, Args: args}, nil
}

func (e *ConfigEditor) lookup(binary string) (string, error) {
	if e.lookPath == nil {
		return exec.LookPath(binary)
	}

	return e.lookPath(binary)
}

// parseCommand splits the configured editor into binary plus arguments. Quotes
// are not interpreted, matching how the rest of the tooling parses commands.
func parseCommand(raw string) (domain.SSHCommand, error) {
	if strings.ContainsRune(raw, 0) {
		return domain.SSHCommand{}, fmt.Errorf("%w: el comando contiene un byte NUL", domain.ErrInvalidArgument)
	}

	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return domain.SSHCommand{}, fmt.Errorf("%w: el comando del editor está vacío", domain.ErrInvalidArgument)
	}

	return domain.SSHCommand{Binary: fields[0], Args: fields[1:]}, nil
}

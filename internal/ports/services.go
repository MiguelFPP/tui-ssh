package ports

import (
	"context"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

// SSHConfigService enumerates the hosts of the ssh configuration and reports what
// it read. It never writes: ~/.ssh/config belongs to the user.
type SSHConfigService interface {
	Hosts(ctx context.Context) ([]domain.Host, error)
	Report(ctx context.Context) (domain.ConfigReport, error)
	// Source returns the raw text of the main file, already bounded.
	Source(ctx context.Context) (domain.ConfigSource, error)
}

// ResolverService resolves the effective options of one alias. The adapter runs
// `ssh -G`, so the answer is the same one ssh itself would use.
type ResolverService interface {
	Resolve(ctx context.Context, alias string) (domain.Resolved, error)
	// Invalidate drops the cached resolution of alias, or all of them when empty.
	Invalidate(alias string)
}

// TagService persists the tags and the declared forward specs, which live outside
// the ssh configuration on purpose.
type TagService interface {
	Tags(ctx context.Context) (domain.TagIndex, error)
	SetTags(ctx context.Context, alias string, tags []string) error
	Forwards(ctx context.Context) (map[string][]domain.ForwardSpec, error)
	SetForwards(ctx context.Context, alias string, specs []domain.ForwardSpec) error
}

// SessionService resolves the command that opens an interactive session. It never
// runs it: the TUI hands the terminal over with tea.ExecProcess.
type SessionService interface {
	Command(ctx context.Context, alias string) (domain.SSHCommand, error)
}

// TunnelService owns the background forwarding masters.
type TunnelService interface {
	// List returns the known tunnels, re-adopting the ones a previous session left
	// running and dropping the ones whose master is gone.
	List(ctx context.Context) ([]domain.Tunnel, error)
	Start(ctx context.Context, host string, spec domain.ForwardSpec) (domain.Tunnel, error)
	Stop(ctx context.Context, id string) error
	Check(ctx context.Context, id string) (domain.TunnelState, error)
	Logs(ctx context.Context, id string) (domain.TunnelLog, error)
	// StopAll stops every tracked tunnel; the TUI asks before calling it on quit.
	StopAll(ctx context.Context) error
}

// ConfigEditorService resolves the command that opens the ssh config in the user
// editor, so the TUI never writes to it.
type ConfigEditorService interface {
	Resolve(ctx context.Context, path string) (domain.SSHCommand, error)
}

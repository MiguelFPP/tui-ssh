package app

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/tui/components/configview"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

const (
	hostsTimeout    = 5 * time.Second
	servicesTimeout = 5 * time.Second
	persistTimeout  = 3 * time.Second
)

// hostsLoadedMsg carries one read of the ssh configuration merged with the tags.
type hostsLoadedMsg struct {
	hosts []domain.Host
	err   error
}

// resolvedMsg carries the effective resolution of one alias.
type resolvedMsg struct {
	alias    string
	resolved domain.Resolved
	err      error
}

// sessionDoneMsg reports that the interactive ssh session ended.
type sessionDoneMsg struct {
	alias string
	err   error
}

// tagsSavedMsg reports the result of persisting the tags of one alias.
type tagsSavedMsg struct {
	alias string
	tags  []string
	err   error
}

// forwardsSavedMsg reports the result of persisting the forwards of one alias.
type forwardsSavedMsg struct {
	alias string
	specs []domain.ForwardSpec
	err   error
}

// externalDoneMsg reports that the external editor closed.
type externalDoneMsg struct {
	err error
}

// loadHosts reads the configuration and merges the tags and forward specs.
func (m Model) loadHosts() tea.Cmd {
	ssh, tags := m.ssh, m.tags

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), hostsTimeout)
		defer cancel()

		hosts, err := ssh.Hosts(ctx)
		if err != nil {
			return hostsLoadedMsg{err: err}
		}

		index, err := tags.Tags(ctx)
		if err != nil {
			return hostsLoadedMsg{err: err}
		}

		forwards, err := tags.Forwards(ctx)
		if err != nil {
			return hostsLoadedMsg{err: err}
		}

		for i := range hosts {
			if list, ok := index[hosts[i].Alias]; ok {
				hosts[i].Tags = list
			}
			if specs, ok := forwards[hosts[i].Alias]; ok {
				hosts[i].Forwards = specs
			}
		}

		return hostsLoadedMsg{hosts: hosts}
	}
}

// applyHosts stores one configuration read and reloads the detail of the selection.
func (m *Model) applyHosts(msg hostsLoadedMsg) tea.Cmd {
	if msg.err != nil {
		m.list.SetHosts(nil)
		m.detail.Clear()

		return m.notify(describeHostError(msg.err))
	}

	m.list.SetHosts(msg.hosts)

	host, ok := m.list.Selected()
	if !ok {
		m.detail.Clear()

		return nil
	}
	m.detail.SetHost(host)

	return m.resolveHost(host)
}

// resolveHost asks the resolver for the effective options of the alias.
func (m Model) resolveHost(host domain.Host) tea.Cmd {
	resolver := m.resolve
	if resolver == nil {
		return nil
	}

	alias := host.Alias

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), domain.ResolveTimeout+time.Second)
		defer cancel()

		resolved, err := resolver.Resolve(ctx, alias)

		return resolvedMsg{alias: alias, resolved: resolved, err: err}
	}
}

// applyResolved stores the resolution if it still belongs to the selection.
func (m *Model) applyResolved(msg resolvedMsg) tea.Cmd {
	host, ok := m.detail.Host()
	if !ok || host.Alias != msg.alias {
		return nil
	}

	if msg.err != nil {
		// Without ssh the panel falls back to the literal directives, flagged as approximate.
		m.detail.SetResolved(domain.Resolved{
			Host:        domain.ApproximateResolved(host),
			Approximate: true,
		})

		return nil
	}

	m.detail.SetResolved(msg.resolved)

	return nil
}

// connect hands the terminal over to an interactive ssh session.
func (m Model) connect(host domain.Host) tea.Cmd {
	session := m.session
	if session == nil {
		return m.notify("servicio de sesión no configurado")
	}

	alias := host.Alias
	m.resumed = false

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), servicesTimeout)
		defer cancel()

		command, err := session.Command(ctx, alias)
		if err != nil {
			return sessionDoneMsg{alias: alias, err: err}
		}

		return execMsg{command: command, alias: alias}
	}
}

// execMsg asks the root to hand the terminal to command until it exits.
type execMsg struct {
	command  domain.SSHCommand
	alias    string
	external bool
}

// quitMsg asks the program to exit once the pending work finished.
type quitMsg struct{}

// runningCount is the number of tunnels believed to be alive.
func (m Model) runningCount() int {
	return len(m.tunnels.RunningTunnels())
}

// reconnectTunnels reloads the tunnel list after an external action.
func (m *Model) reconnectTunnels() tea.Cmd {
	m.tunnels.InvalidateHostState()

	return m.tunnels.Refresh()
}

// applySessionDone refreshes the state after an interactive session.
func (m *Model) applySessionDone(msg sessionDoneMsg) tea.Cmd {
	m.resumed = true
	if msg.err != nil {
		return m.notify(describeHostError(msg.err))
	}

	return tea.Batch(m.reconnectTunnels(), m.notify("sesión terminada · "+msg.alias))
}

// saveTags persists the tags of one alias.
func (m Model) saveTags(alias string, tags []string) tea.Cmd {
	store := m.tags
	if store == nil {
		return m.notify("servicio de etiquetas no configurado")
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
		defer cancel()

		return tagsSavedMsg{alias: alias, tags: tags, err: store.SetTags(ctx, alias, tags)}
	}
}

// applyTagsSaved applies the persisted tags to the loaded host.
func (m *Model) applyTagsSaved(msg tagsSavedMsg) tea.Cmd {
	if msg.err != nil {
		return m.notify("guardar etiquetas: " + msg.err.Error())
	}

	hosts := m.list.Hosts()
	for index := range hosts {
		if hosts[index].Alias == msg.alias {
			hosts[index].Tags = msg.tags
		}
	}
	m.list.SetHosts(hosts)

	if host, ok := m.list.Selected(); ok {
		m.detail.SetHost(host)
	}

	return m.notify("etiquetas guardadas · " + msg.alias)
}

// saveForwards persists the declared specs of one alias.
func (m Model) saveForwards(alias string, specs []domain.ForwardSpec) tea.Cmd {
	store := m.tags
	if store == nil {
		return m.notify("servicio de etiquetas no configurado")
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
		defer cancel()

		return forwardsSavedMsg{alias: alias, specs: specs, err: store.SetForwards(ctx, alias, specs)}
	}
}

// applyForwardsSaved applies the persisted specs to the loaded host.
func (m *Model) applyForwardsSaved(msg forwardsSavedMsg) tea.Cmd {
	if msg.err != nil {
		return m.notify("guardar forwards: " + msg.err.Error())
	}

	hosts := m.list.Hosts()
	for index := range hosts {
		if hosts[index].Alias == msg.alias {
			hosts[index].Forwards = msg.specs
		}
	}
	m.list.SetHosts(hosts)

	host, ok := m.list.Selected()
	if !ok {
		return nil
	}
	m.detail.SetHost(host)

	// Declaring a forward immediately offers it as a running tunnel.
	if spec, ok := lastSpec(msg.specs); ok {
		return m.tunnels.Start(msg.alias, spec)
	}

	return m.notify("forward guardado · " + msg.alias)
}

// openConfigEditor hands the terminal over to $EDITOR with the ssh config.
func (m Model) openConfigEditor() tea.Cmd {
	editor := m.editor
	path := m.cfg.SSHConfig

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), servicesTimeout)
		defer cancel()

		command, err := editor.Resolve(ctx, path)
		if err != nil {
			return externalDoneMsg{err: err}
		}

		return execMsg{command: command, external: true}
	}
}

// applyExternalDone refreshes the configuration after the editor closed.
func (m *Model) applyExternalDone(msg externalDoneMsg) tea.Cmd {
	m.resumed = true

	if msg.err != nil {
		return m.notify(msg.err.Error())
	}

	if m.resolve != nil {
		m.resolve.Invalidate("")
	}
	m.config = configview.New(m.ssh, m.keys)
	m.resize()

	return tea.Batch(m.loadHosts(), m.config.Reload())
}

// describeHostError turns a configuration failure into one actionable line.
func describeHostError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrConfigMissing):
		return "no hay ~/.ssh/config · crea el archivo o usa --ssh-config"
	case errors.Is(err, domain.ErrPermissionDenied):
		return "permiso denegado al leer la configuración de ssh"
	case errors.Is(err, domain.ErrTooLarge):
		return "la configuración de ssh es demasiado grande"
	case errors.Is(err, domain.ErrConfigInvalid):
		return "la configuración de ssh no es válida"
	case errors.Is(err, domain.ErrSSHUnavailable):
		return "ssh no está disponible en el PATH"
	case errors.Is(err, domain.ErrAuthRequired):
		return "el host pide autenticación interactiva"
	default:
		return err.Error()
	}
}

// lastSpec returns the last element of specs, which is the one just declared.
func lastSpec(specs []domain.ForwardSpec) (domain.ForwardSpec, bool) {
	if len(specs) == 0 {
		return domain.ForwardSpec{}, false
	}

	return specs[len(specs)-1], true
}

// notify shows a toast that does not come from a panel error.
func (m *Model) notify(text string) tea.Cmd {
	m.toast = toastPrefix + text
	m.toastSeq++
	seq := m.toastSeq

	schedule := m.expire
	if schedule == nil {
		return nil
	}

	return schedule(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{seq: seq} })
}

// keyLabel is the effective label of an action, or an empty string when the keymap
// does not know it.
func (m Model) keyLabel(action keymap.Action) string {
	return m.keys.Label(action)
}

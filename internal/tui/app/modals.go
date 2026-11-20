package app

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/tui/common"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

// promptState is the tag editor of one alias.
type promptState struct {
	prompt common.Prompt
	alias  string
}

// Visible reports whether the tag prompt owns the keyboard.
func (p promptState) Visible() bool {
	return p.prompt.Visible()
}

// View renders the tag prompt.
func (p promptState) View(width int) []string {
	return p.prompt.View(width)
}

// forwardState is the forward specification editor of one alias.
type forwardState struct {
	prompt common.Prompt
	alias  string
	specs  []domain.ForwardSpec
}

// Visible reports whether the forward prompt owns the keyboard.
func (f forwardState) Visible() bool {
	return f.prompt.Visible()
}

// View renders the forward prompt.
func (f forwardState) View(width int) []string {
	return f.prompt.View(width)
}

// confirmState is the pending yes/no question.
type confirmState struct {
	confirm common.Confirm
	kind    confirmKind
}

// Visible reports whether the confirmation owns the keyboard.
func (c confirmState) Visible() bool {
	return c.confirm.Visible()
}

// View renders the confirmation.
func (c confirmState) View(width int) []string {
	return c.confirm.View(width)
}

// confirmKind distinguishes the questions the UI can ask before quitting.
type confirmKind uint8

const (
	confirmNone confirmKind = iota
	confirmQuit
)

// openTagPrompt shows the tag editor for host.
func (m *Model) openTagPrompt(host domain.Host) {
	prompt := common.NewPrompt(m.keys, keymap.ScopeHostsTags,
		"etiquetas · "+host.Label(),
		"separa con comas · vacío para borrarlas",
		strings.Join(host.Tags, ", "))
	prompt.Hint = "ejemplo: prod, db, cliente-x"
	prompt.Open()

	m.tagPrompt = promptState{prompt: prompt, alias: host.Alias}
}

// applyTagPrompt validates and persists the tags typed in the prompt.
func (m *Model) applyTagPrompt() tea.Cmd {
	alias := m.tagPrompt.alias
	value := m.tagPrompt.prompt.Value()

	tags, err := domain.NormalizeTags([]string{value})
	if err != nil {
		m.tagPrompt.prompt.SetError(err.Error())

		return nil
	}

	m.tagPrompt.prompt.Close()
	m.tagPrompt.alias = ""

	return m.saveTags(alias, tags)
}

// openForwardPrompt shows the forward editor for host.
func (m *Model) openForwardPrompt(host domain.Host) {
	prompt := common.NewPrompt(m.keys, keymap.ScopeHostsForward,
		"forward · "+host.Label(),
		"nombre=L 8080:db.internal:5432 · R 8080:localhost:80 · D 1080",
		"")
	prompt.Hint = "se guarda en forwards.json y se levanta al confirmar"
	prompt.Open()

	m.forwardPrompt = forwardState{prompt: prompt, alias: host.Alias, specs: host.Forwards}
}

// applyForwardPrompt validates the spec, appends it to the declared list and
// persists the result.
func (m *Model) applyForwardPrompt() tea.Cmd {
	alias := m.forwardPrompt.alias
	specs := m.forwardPrompt.specs

	spec, err := domain.ParseForwardSpec(m.forwardPrompt.prompt.Value())
	if err != nil {
		m.forwardPrompt.prompt.SetError(err.Error())

		return nil
	}

	// Re-declaring the same spec replaces it instead of duplicating it.
	merged := make([]domain.ForwardSpec, 0, len(specs)+1)
	id := spec.ID(alias)
	for _, existing := range specs {
		if existing.ID(alias) == id {
			continue
		}

		merged = append(merged, existing)
	}
	merged = append(merged, spec)

	if err := domain.ValidateForwardSpecs(merged); err != nil {
		m.forwardPrompt.prompt.SetError(err.Error())

		return nil
	}

	m.forwardPrompt.prompt.Close()
	m.forwardPrompt.alias = ""
	m.forwardPrompt.specs = nil

	return m.saveForwards(alias, merged)
}

// openQuitConfirmation asks whether the running tunnels must be stopped on exit.
func (c *confirmState) OpenForQuit(running int) {
	c.confirm = common.NewConfirm(keymap.Default(), keymap.ScopeTunnelsConfirm)
	c.confirm.Open(
		"¿detener los túneles antes de salir?",
		plural(running, "túnel activo", "túneles activos")+" · si no, siguen en segundo plano",
	)
	c.kind = confirmQuit
}

// stopTunnelsOnQuit stops every tunnel and then exits.
func (m *Model) stopTunnelsOnQuit() tea.Cmd {
	service := m.tunnels.Service()
	if service == nil {
		return func() tea.Msg { return quitMsg{} }
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
		defer cancel()

		_ = service.StopAll(ctx)

		return quitMsg{}
	}
}

// plural renders "1 túnel" / "3 túneles".
func plural(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}

	return itoa(count) + " " + many
}

func itoa(value int) string {
	if value <= 0 {
		return "0"
	}

	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	return string(digits)
}

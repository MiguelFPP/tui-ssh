package common

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
	"github.com/MiguelFPP/tui-ssh/internal/tui/styles"
)

// Prompt is a single-line modal that asks for one value. The owner opens it,
// routes the keys through HandleKey and applies the result; the prompt itself
// never performs I/O.
type Prompt struct {
	Title string
	Label string
	Hint  string
	Input textinput.Model
	Err   string
	open  bool
	mode  keymap.Scope
	keys  keymap.Registry
}

// NewPrompt builds a closed prompt with the given title, label and initial value.
func NewPrompt(keys keymap.Registry, mode keymap.Scope, title, label, value string) Prompt {
	input := textinput.New()
	input.SetValue(value)
	input.CharLimit = 256
	input.Prompt = ""

	return Prompt{Title: title, Label: label, Input: input, mode: mode, keys: keys}
}

// Open shows the prompt with a clean error and focuses the input.
func (p *Prompt) Open() {
	p.open = true
	p.Err = ""
	p.Input.Focus()
}

// Close hides the prompt and clears its error.
func (p *Prompt) Close() {
	p.open = false
	p.Err = ""
	p.Input.Blur()
}

// Visible reports whether the prompt owns the keyboard.
func (p Prompt) Visible() bool {
	return p.open
}

// Value returns the trimmed content of the input.
func (p Prompt) Value() string {
	return strings.TrimSpace(p.Input.Value())
}

// SetValue replaces the content of the input and moves the cursor to the end.
func (p *Prompt) SetValue(value string) {
	p.Input.SetValue(value)
	p.Input.CursorEnd()
}

// SetError records the validation failure shown under the input.
func (p *Prompt) SetError(message string) {
	p.Err = message
}

// HandleKey applies the modal keys and returns the resolved action, or an empty
// string when the key only edited the text.
func (p Prompt) HandleKey(msg tea.KeyPressMsg) (Prompt, keymap.Action, tea.Cmd) {
	if !p.open {
		return p, "", nil
	}

	if action, ok := p.keys.Match(p.mode, msg.String()); ok {
		if action == keymap.ActionForwardSave || action == keymap.ActionTagsSave {
			return p, action, nil
		}
		if action == keymap.ActionForwardCancel || action == keymap.ActionTagsCancel {
			p.Close()

			return p, action, nil
		}
	}

	updated, cmd := p.Input.Update(msg)
	p.Input = updated

	return p, "", cmd
}

// View renders the prompt: title, label, input and the hint or error row.
func (p Prompt) View(width int) []string {
	if !p.open {
		return nil
	}

	lines := []string{styles.HelpTitle.Render(Truncate(p.Title, width))}
	if p.Label != "" {
		lines = append(lines, styles.MetaPlain.Render(Truncate(p.Label, width)))
	}

	lines = append(lines, Truncate(p.Input.View(), width))

	switch {
	case p.Err != "":
		lines = append(lines, styles.FailPlain.Render(Truncate(p.Err, width)))
	case p.Hint != "":
		lines = append(lines, styles.MetaPlain.Render(Truncate(p.Hint, width)))
	}

	lines = append(lines, styles.MetaPlain.Render(Truncate(p.keys.Label(p.saveAction())+": guardar · "+
		p.keys.Label(p.cancelAction())+": cancelar", width)))

	return lines
}

// saveAction is the action that confirms this prompt.
func (p Prompt) saveAction() keymap.Action {
	if p.mode == keymap.ScopeHostsTags {
		return keymap.ActionTagsSave
	}

	return keymap.ActionForwardSave
}

// cancelAction is the action that dismisses this prompt.
func (p Prompt) cancelAction() keymap.Action {
	if p.mode == keymap.ScopeHostsTags {
		return keymap.ActionTagsCancel
	}

	return keymap.ActionForwardCancel
}

// Decision is the answer a confirmation produced.
type Decision uint8

const (
	DecisionNone Decision = iota
	DecisionYes
	DecisionNo
)

// Confirm is a yes/no modal for the actions that stop or discard something.
type Confirm struct {
	Question string
	Detail   string
	open     bool
	keys     keymap.Registry
	mode     keymap.Scope
}

// NewConfirm builds a closed confirmation for the given scope.
func NewConfirm(keys keymap.Registry, mode keymap.Scope) Confirm {
	return Confirm{keys: keys, mode: mode}
}

// Open shows the confirmation with the question and its detail line.
func (c *Confirm) Open(question, detail string) {
	c.Question = question
	c.Detail = detail
	c.open = true
}

// Close hides the confirmation.
func (c *Confirm) Close() {
	c.open = false
	c.Question = ""
	c.Detail = ""
}

// Visible reports whether the confirmation owns the keyboard.
func (c Confirm) Visible() bool {
	return c.open
}

// HandleKey resolves the pending decision.
func (c Confirm) HandleKey(msg tea.KeyPressMsg) (Confirm, Decision) {
	if !c.open {
		return c, DecisionNone
	}

	action, ok := c.keys.Match(c.mode, msg.String())
	if !ok {
		return c, DecisionNone
	}

	switch action {
	case keymap.ActionHostConfirmYes, keymap.ActionTunnelConfirmYes:
		c.Close()

		return c, DecisionYes
	case keymap.ActionHostConfirmNo, keymap.ActionTunnelConfirmNo:
		c.Close()

		return c, DecisionNo
	default:
		return c, DecisionNone
	}
}

// View renders the confirmation: the question, the detail and the key hints.
func (c Confirm) View(width int) []string {
	if !c.open {
		return nil
	}

	lines := []string{styles.HelpTitle.Render(Truncate(c.Question, width))}
	if c.Detail != "" {
		lines = append(lines, styles.MetaPlain.Render(Truncate(c.Detail, width)))
	}

	yes, no := keymap.ActionHostConfirmYes, keymap.ActionHostConfirmNo
	if c.mode == keymap.ScopeTunnelsConfirm {
		yes, no = keymap.ActionTunnelConfirmYes, keymap.ActionTunnelConfirmNo
	}

	lines = append(lines, styles.MetaPlain.Render(Truncate(
		c.keys.Label(yes)+": confirmar · "+c.keys.Label(no)+": cancelar", width)))

	return lines
}

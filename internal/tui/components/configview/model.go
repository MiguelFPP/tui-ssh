// Package configview renders the read-only Config tab: the files the loader read,
// the warnings it collected and the raw text of the main file with line numbers.
package configview

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/ports"
	"github.com/MiguelFPP/tui-ssh/internal/tui/common"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
	"github.com/MiguelFPP/tui-ssh/internal/tui/styles"
)

const (
	defaultWidth  = 80
	defaultHeight = 20

	headerRows = 2
	footerRows = 1

	loadTimeout = 3 * time.Second

	wheelStep = 3

	gutterWidth = 5
)

// reportMsg carries one read of the configuration report.
type reportMsg struct {
	report domain.ConfigReport
	source domain.ConfigSource
	err    error
}

// Model is the read-only configuration viewer.
type Model struct {
	config ports.SSHConfigService
	keys   keymap.Registry

	report domain.ConfigReport
	source domain.ConfigSource
	err    error

	loading bool
	loaded  bool

	viewport viewport.Model

	width  int
	height int
}

// New builds the viewer; nothing is read until Init or Reload runs.
func New(config ports.SSHConfigService, keys keymap.Registry) Model {
	if len(keys.All()) == 0 {
		keys = keymap.Default()
	}

	m := Model{config: config, keys: keys, viewport: viewport.New()}
	m.viewport.FillHeight = true
	m.viewport.MouseWheelEnabled = true
	m.SetBounds(defaultWidth, defaultHeight)

	return m
}

// SetBounds stores the size and resizes the viewport.
func (m *Model) SetBounds(width, height int) {
	m.width, m.height = max(width, 0), max(height, 0)
	m.viewport.SetWidth(max(m.width, 1))
	m.viewport.SetHeight(max(m.height-headerRows-footerRows, 1))
}

// Loading reports whether a read is still in flight.
func (m Model) Loading() bool {
	return m.loading
}

// Err returns the failure of the last read.
func (m Model) Err() error {
	return m.err
}

// Init loads the report the first time the tab is shown.
func (m Model) Init() tea.Cmd {
	if m.loaded {
		return nil
	}

	return m.Reload()
}

// Reload reads the report and the raw text again.
func (m *Model) Reload() tea.Cmd {
	if m.config == nil {
		return nil
	}

	m.loading = true
	service := m.config

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()

		report, err := service.Report(ctx)
		if err != nil {
			return reportMsg{report: report, err: err}
		}

		source, err := service.Source(ctx)

		return reportMsg{report: report, source: source, err: err}
	}
}

// Update applies one message: the report, the keys or the wheel.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case reportMsg:
		m.loading = false
		m.loaded = true

		if msg.err != nil {
			m.err = msg.err

			return m, nil
		}

		m.err = nil
		m.report = msg.report
		m.source = msg.source
		m.viewport.SetContent(m.bodyText())

		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseWheelMsg:
		m.viewport, _ = m.viewport.Update(msg)

		return m, nil
	default:
		return m, nil
	}
}

// handleKey applies the Config scope.
func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	action, ok := m.keys.Match(keymap.ScopeConfigView, msg.String())
	if !ok {
		return m, nil
	}

	switch action {
	case keymap.ActionConfigRefresh:
		return m, m.Reload()
	case keymap.ActionConfigUp:
		m.viewport.ScrollUp(1)
	case keymap.ActionConfigDown:
		m.viewport.ScrollDown(1)
	case keymap.ActionConfigPageUp:
		m.viewport.ScrollUp(m.viewport.Height())
	case keymap.ActionConfigPageDown:
		m.viewport.ScrollDown(m.viewport.Height())
	case keymap.ActionConfigHome:
		m.viewport.GotoTop()
	case keymap.ActionConfigEnd:
		m.viewport.GotoBottom()
	}

	return m, nil
}

// Path returns the main configuration file the tab documents.
func (m Model) Path() string {
	if m.source.Path != "" {
		return m.source.Path
	}

	return m.report.Main
}

// View composes the summary header, the text and the footer.
func (m Model) View() string {
	lines := make([]string, 0, m.height)
	lines = append(lines, styles.TitlePlain.Render(common.Truncate(m.titleText(), m.width)))
	lines = append(lines, styles.Header.Render(common.Truncate(m.subtitleText(), m.width)))

	switch {
	case m.err != nil && len(m.source.Lines) == 0:
		lines = append(lines, styles.FailPlain.Render(common.Truncate("error: "+m.err.Error(), m.width)))
	case m.loading && len(m.source.Lines) == 0:
		lines = append(lines, styles.MetaPlain.Render("cargando…"))
	default:
		lines = append(lines, m.viewport.View())
	}

	lines = append(lines, styles.MetaPlain.Render(common.Truncate(m.footerText(), m.width)))

	return strings.Join(lines, "\n")
}

// titleText names the file and its size.
func (m Model) titleText() string {
	parts := []string{"config", orDash(m.Path())}

	if m.source.TotalLines > 0 {
		parts = append(parts, fmt.Sprintf("%d líneas", m.source.TotalLines))
	}
	if m.source.Truncated {
		parts = append(parts, "vista truncada")
	}

	return strings.Join(parts, common.ColumnSeparator)
}

// subtitleText summarises the loaded files, the host count and the warnings.
func (m Model) subtitleText() string {
	parts := []string{fmt.Sprintf("%d hosts", m.report.Hosts), fmt.Sprintf("%d archivos", len(m.report.Files))}

	if len(m.report.Warnings) > 0 {
		parts = append(parts, fmt.Sprintf("%d avisos", len(m.report.Warnings)))
	}

	return strings.Join(parts, common.ColumnSeparator)
}

// footerText lists the keys of the tab.
func (m Model) footerText() string {
	return strings.Join([]string{
		m.keys.Label(keymap.ActionConfigUp) + "/" + m.keys.Label(keymap.ActionConfigDown) + ": mover",
		m.keys.Label(keymap.ActionConfigRefresh) + ": recargar",
		m.keys.Label(keymap.ActionConfigEdit) + ": abrir en $EDITOR",
	}, common.ColumnSeparator)
}

// bodyText builds the content of the viewport: the file list, the warnings and the
// numbered lines.
func (m Model) bodyText() string {
	lines := make([]string, 0, len(m.source.Lines)+16)

	if len(m.report.Files) > 0 {
		lines = append(lines, styles.Header.Render("archivos"))
		for _, file := range m.report.Files {
			marker := "  "
			if file.Included {
				marker = "↳ "
			}

			entry := marker + file.Path
			if file.Err != "" {
				entry += "  (" + file.Err + ")"
			} else {
				entry += fmt.Sprintf("  %d líneas", file.Lines)
			}

			lines = append(lines, entry)
		}
		lines = append(lines, "")
	}

	if len(m.report.Warnings) > 0 {
		lines = append(lines, styles.WarnPlain.Render("avisos"))
		for _, warning := range m.report.Warnings {
			lines = append(lines, styles.WarnPlain.Render("  · "+warning))
		}
		lines = append(lines, "")
	}

	lines = append(lines, styles.Header.Render("contenido"))

	for index, line := range m.source.Lines {
		lines = append(lines, styles.Gutter.Render(fmt.Sprintf("%*d ", gutterWidth-1, index+1))+line)
	}

	return strings.Join(lines, "\n")
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}

	return value
}

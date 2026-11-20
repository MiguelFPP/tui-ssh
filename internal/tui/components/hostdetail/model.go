// Package hostdetail renders the right column of the Hosts tab: what the ssh
// configuration says about the selected alias, the effective resolution and the
// forward specifications the user declared.
package hostdetail

import (
	"strings"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/tui/common"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
	"github.com/MiguelFPP/tui-ssh/internal/tui/styles"
)

const (
	defaultWidth  = 60
	defaultHeight = 20

	// labelColumn is the width reserved for the key of each row.
	labelColumn = 15

	// maxIdentityFiles bounds the identities listed before the rest is summarised.
	maxIdentityFiles = 3
)

// Model is the detail column of the Hosts tab.
type Model struct {
	keys keymap.Registry

	host     domain.Host
	hasHost  bool
	resolved domain.Resolved
	resolve  bool

	width  int
	height int
}

// New builds an empty detail with the default size.
func New(keys keymap.Registry) Model {
	if len(keys.All()) == 0 {
		keys = keymap.Default()
	}

	return Model{keys: keys, width: defaultWidth, height: defaultHeight}
}

// SetBounds stores the size the panel is drawn in.
func (m *Model) SetBounds(width, height int) {
	m.width, m.height = max(width, 0), max(height, 0)
}

// SetHost replaces the selected host and drops the stale resolution. The values are
// stripped of control characters here: the panel renders them verbatim and they come
// from the ssh configuration, which is external text.
func (m *Model) SetHost(host domain.Host) {
	m.host = sanitizeHost(host)
	m.hasHost = true
	m.resolved = domain.Resolved{}
	m.resolve = false
}

// Clear empties the panel; it is what the tab shows with no selection.
func (m *Model) Clear() {
	m.host = domain.Host{}
	m.hasHost = false
	m.resolved = domain.Resolved{}
	m.resolve = false
}

// SetResolved stores the effective resolution of the selected host. Its fields come
// from `ssh -G`, which is external text too, so they are stripped before rendering.
func (m *Model) SetResolved(resolved domain.Resolved) {
	resolved.Host = sanitizeResolved(resolved.Host)
	m.resolved = resolved
	m.resolve = true
}

// sanitizeHost strips the control characters from every value the panel renders.
func sanitizeHost(host domain.Host) domain.Host {
	host.Alias = domain.StripControlChars(host.Alias)

	tags := make([]string, len(host.Tags))
	for i, tag := range host.Tags {
		tags[i] = domain.StripControlChars(tag)
	}
	host.Tags = tags

	declared := make([]domain.Directive, len(host.Declared))
	for i, directive := range host.Declared {
		declared[i] = domain.Directive{
			Key:   domain.StripControlChars(directive.Key),
			Value: domain.StripControlChars(directive.Value),
			Line:  directive.Line,
		}
	}
	host.Declared = declared

	return host
}

// sanitizeResolved strips the control characters from the resolved fields.
func sanitizeResolved(resolved domain.ResolvedHost) domain.ResolvedHost {
	resolved.Alias = domain.StripControlChars(resolved.Alias)
	resolved.HostName = domain.StripControlChars(resolved.HostName)
	resolved.User = domain.StripControlChars(resolved.User)
	resolved.ProxyJump = domain.StripControlChars(resolved.ProxyJump)

	for index, file := range resolved.IdentityFiles {
		resolved.IdentityFiles[index] = domain.StripControlChars(file)
	}

	return resolved
}

// Host returns the selected host.
func (m Model) Host() (domain.Host, bool) {
	return m.host, m.hasHost
}

// View renders the detail of the selection.
func (m Model) View() string {
	if !m.hasHost || m.height <= 0 {
		return strings.Join(common.PadLines(
			[]string{styles.Placeholder.Render("selecciona un host")}, m.height), "\n")
	}

	lines := make([]string, 0, m.height)
	lines = append(lines, m.titleLine(), "")

	for _, row := range m.rows() {
		lines = append(lines, m.rowLine(row))
	}

	if forwards := m.forwardLines(); len(forwards) > 0 {
		lines = append(lines, "", styles.Header.Render(common.Pad("forwards", m.width)))
		lines = append(lines, forwards...)
	}

	lines = append(lines, "", styles.MetaPlain.Render(common.Pad(m.hint(), m.width)))

	return strings.Join(common.PadLines(lines, m.height), "\n")
}

// titleLine names the host and its tags.
func (m Model) titleLine() string {
	parts := []string{styles.TitlePlain.Render(m.host.Label())}
	if len(m.host.Tags) > 0 {
		parts = append(parts, styles.TagPlain.Render("#"+strings.Join(m.host.Tags, " #")))
	}
	if m.resolve && m.resolved.Approximate {
		parts = append(parts, styles.WarnPlain.Render("resolución aproximada"))
	}

	return common.Clip(strings.Join(parts, " "), m.width)
}

// rows is the key/value block: effective resolution first, then provenance.
func (m Model) rows() []domain.KeyValue {
	if m.resolve {
		rows := m.resolved.Host.SummaryRows()

		return append(rows, domain.KeyValue{Key: "archivo", Value: m.sourceLabel()})
	}

	rows := []domain.KeyValue{
		{Key: "alias", Value: m.host.Label()},
	}

	if hostname, ok := m.host.DeclaredValue("hostname"); ok {
		rows = append(rows, domain.KeyValue{Key: "hostname", Value: hostname})
	}
	if user, ok := m.host.DeclaredValue("user"); ok {
		rows = append(rows, domain.KeyValue{Key: "user", Value: user})
	}
	if port, ok := m.host.DeclaredValue("port"); ok {
		rows = append(rows, domain.KeyValue{Key: "port", Value: port})
	}

	return append(rows, domain.KeyValue{Key: "archivo", Value: m.sourceLabel()})
}

// rowLine renders one key/value row with the label in a fixed column.
func (m Model) rowLine(row domain.KeyValue) string {
	label := common.PadRight(row.Key, labelColumn)

	room := max(m.width-labelColumn, 1)

	return styles.ValuePlain.Render(label) + common.Truncate(row.Value, room)
}

// forwardLines renders the declared forwards of the host.
func (m Model) forwardLines() []string {
	if len(m.host.Forwards) == 0 {
		return nil
	}

	lines := make([]string, 0, len(m.host.Forwards))
	for _, spec := range m.host.Forwards {
		lines = append(lines, "  "+common.Truncate(spec.Summary(), max(m.width-2, 1)))
	}

	return lines
}

// hint names the keys that act on the selection.
func (m Model) hint() string {
	parts := []string{
		m.keys.Label(keymap.ActionHostConnect) + " conectar",
		m.keys.Label(keymap.ActionHostTunnel) + " túnel",
		m.keys.Label(keymap.ActionHostTags) + " etiquetas",
	}

	return strings.Join(parts, common.ColumnSeparator)
}

// sourceLabel renders where the block was declared.
func (m Model) sourceLabel() string {
	if m.host.SourceFile == "" {
		return "-"
	}

	file := shortPath(m.host.SourceFile)

	return file + ":" + itoa(m.host.SourceLine)
}

// shortPath keeps the last two components of a path, which is what identifies the
// file without spending the whole row.
func shortPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}

	return ".../" + strings.Join(parts[len(parts)-2:], "/")
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	return string(digits)
}

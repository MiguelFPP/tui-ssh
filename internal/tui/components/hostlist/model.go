// Package hostlist renders the flat list of hosts of the Hosts tab: the rows, the
// incremental filter, the tag chips and the window arithmetic. It never performs I/O.
package hostlist

import (
	"image"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/tui/common"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
	"github.com/MiguelFPP/tui-ssh/internal/tui/styles"
)

const (
	defaultWidth  = 40
	defaultHeight = 20

	// headerRows is the filter row plus the column header.
	headerRows = 2
	// footerRows is the count row at the bottom of the list.
	footerRows = 1

	wheelStep = 3

	// rowIndent separates a host from its tag headers.
	rowIndent = "  "
)

// RowKind distinguishes a host row from a tag header.
type RowKind uint8

const (
	RowHost RowKind = iota
	RowTagHeader
)

// Row is one visible line of the list.
type Row struct {
	Kind RowKind
	Tag  string
	Host domain.Host
}

// Model is the host list of the Hosts tab.
type Model struct {
	keys keymap.Registry

	hosts []domain.Host
	rows  []Row

	filter    textinput.Model
	filtering bool
	tagFilter string
	tags      []string

	cursor int
	offset int

	width  int
	height int
	bounds image.Rectangle

	rowRects []image.Rectangle
}

// New builds an empty list with a collapsed filter input.
func New(keys keymap.Registry) Model {
	if len(keys.All()) == 0 {
		keys = keymap.Default()
	}

	filter := textinput.New()
	filter.Placeholder = "filtrar por alias, etiqueta o hostname"
	filter.Prompt = "/ "
	filter.CharLimit = 128
	filter.Blur()

	m := Model{keys: keys, filter: filter, width: defaultWidth, height: defaultHeight}
	m.SetBounds(0, 0, defaultWidth, defaultHeight)

	return m
}

// SetHosts replaces the hosts and rebuilds the rows, keeping the selection on the
// same alias when it survives the refresh.
func (m *Model) SetHosts(hosts []domain.Host) {
	anchor := ""
	if host, ok := m.Selected(); ok {
		anchor = host.Alias
	}

	m.hosts = hosts
	m.tags = collectTags(hosts)
	if m.tagFilter != "" && !containsTag(m.tags, m.tagFilter) {
		m.tagFilter = ""
	}
	m.rebuild()

	if anchor != "" {
		m.SelectAlias(anchor)
	}
	m.clamp()
}

// SetBounds places the list in the terminal and recomputes the hit rectangles.
func (m *Model) SetBounds(x, y, width, height int) {
	m.width, m.height = max(width, 0), max(height, 0)
	m.bounds = image.Rect(x, y, x+m.width, y+m.height)

	m.filter.SetWidth(max(m.width-len("/ "), 1))

	m.markRows()
}

// SetFilterVisible focuses or blurs the filter input.
func (m *Model) SetFilterVisible(visible bool) {
	m.filtering = visible
	if visible {
		m.filter.Focus()

		return
	}

	m.filter.Blur()
}

// Filtering reports whether the input owns the keyboard.
func (m Model) Filtering() bool {
	return m.filtering
}

// SetFilter replaces the free-text filter and rebuilds the rows.
func (m *Model) SetFilter(value string) {
	m.filter.SetValue(value)
	m.rebuild()
	m.cursor, m.offset = 0, 0
	m.clamp()
}

// Filter returns the current free-text filter.
func (m Model) Filter() string {
	return strings.TrimSpace(m.filter.Value())
}

// SetTagFilter narrows the list to one tag; an empty value shows every host.
func (m *Model) SetTagFilter(tag string) {
	m.tagFilter = strings.TrimSpace(tag)
	m.rebuild()
	m.cursor, m.offset = 0, 0
	m.clamp()
}

// TagFilter returns the active tag, or an empty string.
func (m Model) TagFilter() string {
	return m.tagFilter
}

// Tags returns every tag present in the loaded hosts, sorted.
func (m Model) Tags() []string {
	out := make([]string, len(m.tags))
	copy(out, m.tags)

	return out
}

// CycleTag moves the tag filter forward or backward through the catalog.
func (m *Model) CycleTag(delta int) {
	catalog := append([]string{""}, m.tags...)
	if len(catalog) == 0 {
		return
	}

	index := 0
	for i, tag := range catalog {
		if tag == m.tagFilter {
			index = i

			break
		}
	}

	index = (index + delta + len(catalog)) % len(catalog)
	m.SetTagFilter(catalog[index])
}

// Count returns the number of host rows currently visible.
func (m Model) Count() int {
	count := 0
	for _, row := range m.rows {
		if row.Kind == RowHost {
			count++
		}
	}

	return count
}

// Total returns the number of hosts before filtering.
func (m Model) Total() int {
	return len(m.hosts)
}

// Hosts returns a copy of the loaded hosts, in configuration order.
func (m Model) Hosts() []domain.Host {
	out := make([]domain.Host, len(m.hosts))
	copy(out, m.hosts)

	return out
}

// Selected returns the host under the cursor, skipping the tag headers.
func (m Model) Selected() (domain.Host, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return domain.Host{}, false
	}

	row := m.rows[m.cursor]
	if row.Kind != RowHost {
		return domain.Host{}, false
	}

	return row.Host, true
}

// SelectAlias moves the cursor to the row of alias, if it is visible.
func (m *Model) SelectAlias(alias string) bool {
	for index, row := range m.rows {
		if row.Kind == RowHost && row.Host.Alias == alias {
			m.cursor = index
			m.scrollToCursor()

			return true
		}
	}

	return false
}

// Up moves the cursor one host up.
func (m *Model) Up() {
	m.moveBy(-1)
}

// Down moves the cursor one host down.
func (m *Model) Down() {
	m.moveBy(1)
}

// PageUp moves the cursor one page up.
func (m *Model) PageUp() {
	m.moveBy(-m.bodyHeight())
}

// PageDown moves the cursor one page down.
func (m *Model) PageDown() {
	m.moveBy(m.bodyHeight())
}

// Home moves the cursor to the first row.
func (m *Model) Home() {
	m.moveTo(0)
}

// End moves the cursor to the last row.
func (m *Model) End() {
	m.moveTo(len(m.rows) - 1)
}

// Wheel scrolls the list without moving the cursor past the edges.
func (m *Model) Wheel(delta int) {
	if delta == 0 {
		return
	}

	step := -wheelStep
	if delta > 0 {
		step = wheelStep
	}

	m.moveBy(step)
}

// Update forwards a key to the filter input while it is visible.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.filtering {
		return m, nil
	}

	switch msg.(type) {
	case tea.KeyPressMsg:
		updated, cmd := m.filter.Update(msg)
		m.filter = updated
		m.rebuild()
		m.clamp()

		return m, cmd
	default:
		return m, nil
	}
}

// RowAt maps terminal coordinates to the host row under them.
func (m Model) RowAt(x, y int) (domain.Host, bool) {
	point := image.Pt(x, y)
	for index, rect := range m.rowRects {
		if !point.In(rect) {
			continue
		}

		row := m.offset + index
		if row < 0 || row >= len(m.rows) || m.rows[row].Kind != RowHost {
			return domain.Host{}, false
		}

		return m.rows[row].Host, true
	}

	return domain.Host{}, false
}

// View renders the filter row, the column header, the visible rows and the count.
func (m Model) View() string {
	lines := make([]string, 0, m.height)
	lines = append(lines, m.filterRow())

	header := "host"
	if m.tagFilter != "" {
		header = "#" + m.tagFilter
	}
	lines = append(lines, styles.Header.Render(common.Pad(header, m.width)))

	if len(m.rows) == 0 {
		lines = append(lines, styles.Placeholder.Render(common.Pad(m.emptyText(), m.width)))
	} else {
		for index, row := range common.Window(m.rows, m.offset, m.bodyHeight()) {
			lines = append(lines, m.rowLine(row, m.offset+index == m.cursor))
		}
	}

	lines = lines[:min(len(lines), m.height-1)]
	lines = append(lines, styles.MetaPlain.Render(common.Pad(m.countText(), m.width)))

	return strings.Join(lines, "\n")
}

// filterRow renders the input or the active filter summary.
func (m Model) filterRow() string {
	if m.filtering {
		return common.Clip(m.filter.View(), m.width)
	}

	summary := m.Filter()
	if summary == "" {
		summary = m.keys.Label(keymap.ActionHostFilter) + " filtrar"
	} else {
		summary = "/ " + summary
	}

	return styles.MetaPlain.Render(common.Pad(summary, m.width))
}

// rowLine renders one row, with the cursor token when it is selected.
func (m Model) rowLine(row Row, selected bool) string {
	line := m.renderRow(row, m.width)
	if selected {
		return styles.CursorPlain.Render(line)
	}

	return line
}

// renderRow builds the plain text of one row before the cursor token is applied.
func (m Model) renderRow(row Row, width int) string {
	switch row.Kind {
	case RowTagHeader:
		label := "── " + row.Tag + " ──"

		return common.Pad(styles.TagPlain.Render(common.Truncate(label, width)), width)
	}

	host := row.Host
	badge := hostBadges(host)

	name := rowIndent + host.Label()
	if m.tagFilter == "" && len(host.Tags) > 0 {
		name += "  " + strings.Join(host.Tags, " ")
	}

	if badge == "" {
		return common.Pad(common.Truncate(name, width), width)
	}

	// The badge is right-aligned so the aliases stay column-aligned.
	room := max(width-len([]rune(badge))-1, 1)
	left := common.PadRight(common.Truncate(name, room), room)

	return styles.MetaPlain.Render(left + " " + badge)
}

// bodyHeight is the number of rows the list can show.
func (m Model) bodyHeight() int {
	return max(m.height-headerRows-footerRows, 1)
}

// clamp keeps the cursor and the offset inside the rows.
func (m *Model) clamp() {
	if len(m.rows) == 0 {
		m.cursor, m.offset = 0, 0
		m.markRows()

		return
	}

	m.cursor = common.Clamp(m.cursor, 0, len(m.rows)-1)
	m.offset = common.Clamp(m.offset, 0, max(len(m.rows)-m.bodyHeight(), 0))

	// The rebuilt rows may start with a tag header: the cursor always lands on a host.
	if m.rows[m.cursor].Kind != RowHost {
		m.snapToHost()
	}

	m.scrollToCursor()
	m.markRows()
}

// snapToHost moves the cursor to the nearest host row in the current direction.
func (m *Model) snapToHost() {
	for next := m.cursor + 1; next < len(m.rows); next++ {
		if m.rows[next].Kind == RowHost {
			m.cursor = next

			return
		}
	}
	for next := m.cursor - 1; next >= 0; next-- {
		if m.rows[next].Kind == RowHost {
			m.cursor = next

			return
		}
	}
}

// scrollToCursor moves the offset so the cursor stays visible.
func (m *Model) scrollToCursor() {
	height := m.bodyHeight()
	if height <= 0 {
		return
	}

	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+height {
		m.offset = m.cursor - height + 1
	}
}

// markRows stores the rectangle of every visible row for the hit-testing.
func (m *Model) markRows() {
	m.rowRects = m.rowRects[:0]

	count := min(m.bodyHeight(), len(m.rows))
	if count <= 0 || m.width <= 0 {
		return
	}

	top := m.bounds.Min.Y + headerRows
	for row := 0; row < count; row++ {
		m.rowRects = append(m.rowRects, image.Rect(m.bounds.Min.X, top+row, m.bounds.Max.X, top+row+1))
	}
}

// moveBy moves the cursor delta rows, landing on a host and never on a tag header.
func (m *Model) moveBy(delta int) {
	if len(m.rows) == 0 {
		return
	}

	step := 1
	if delta < 0 {
		step = -1
	}

	steps := delta
	if steps < 0 {
		steps = -steps
	}

	for i := 0; i < steps; i++ {
		next := m.cursor + step
		for next >= 0 && next < len(m.rows) && m.rows[next].Kind != RowHost {
			next += step
		}

		if next < 0 || next >= len(m.rows) {
			break
		}

		m.cursor = next
	}

	m.scrollToCursor()
}

// moveTo moves the cursor to index, landing on the nearest host row.
func (m *Model) moveTo(index int) {
	if len(m.rows) == 0 {
		return
	}

	index = common.Clamp(index, 0, len(m.rows)-1)

	// A header is skipped towards the end of the list first, so End lands on the
	// last host instead of the last header.
	for next := index; next < len(m.rows); next++ {
		if m.rows[next].Kind == RowHost {
			m.cursor = next
			m.scrollToCursor()

			return
		}
	}
	for next := index; next >= 0; next-- {
		if m.rows[next].Kind == RowHost {
			m.cursor = next
			m.scrollToCursor()

			return
		}
	}
}

// rebuild filters the hosts and flattens them into rows, grouped by tag when no
// host already answers the filter.
func (m *Model) rebuild() {
	filter := m.Filter()
	previous, hadSelection := m.Selected()
	_ = previous
	_ = hadSelection

	visible := make([]domain.Host, 0, len(m.hosts))
	for _, host := range m.hosts {
		if !host.Matches(filter) {
			continue
		}
		if m.tagFilter != "" && !host.HasTag(m.tagFilter) {
			continue
		}

		visible = append(visible, host)
	}

	rows := make([]Row, 0, len(visible)+8)

	// Grouping by tag is only meaningful when the whole catalog is shown; a filtered
	// list reads better as a plain list.
	grouped := m.tagFilter != "" || (filter == "" && len(m.tags) > 0 && len(visible) > 8)
	if !grouped {
		for _, host := range visible {
			rows = append(rows, Row{Kind: RowHost, Host: host})
		}
		m.rows = rows

		return
	}

	for _, tag := range append([]string{"sin etiqueta"}, m.tags...) {
		members := make([]domain.Host, 0, len(visible))

		for _, host := range visible {
			switch {
			case tag == "sin etiqueta":
				if len(host.Tags) == 0 {
					members = append(members, host)
				}
			case host.HasTag(tag):
				members = append(members, host)
			}
		}

		if len(members) == 0 {
			continue
		}

		rows = append(rows, Row{Kind: RowTagHeader, Tag: tag})
		for _, host := range members {
			rows = append(rows, Row{Kind: RowHost, Host: host})
		}
	}

	m.rows = rows
}

// emptyText explains an empty list, naming what narrowed it.
func (m Model) emptyText() string {
	switch {
	case len(m.hosts) == 0:
		return "sin hosts en la configuración"
	case m.Filter() != "":
		return "sin hosts para «" + m.Filter() + "»"
	case m.tagFilter != "":
		return "sin hosts con la etiqueta #" + m.tagFilter
	default:
		return "sin hosts"
	}
}

// countText summarises the list: visible hosts, total and active filter.
func (m Model) countText() string {
	parts := []string{
		plural(m.Count(), "host", "hosts"),
	}

	if visible := m.Count(); visible != len(m.hosts) {
		parts = append(parts, "de "+plural(len(m.hosts), "host", "hosts"))
	}
	if m.tagFilter != "" {
		parts = append(parts, "#"+m.tagFilter)
	}

	return strings.Join(parts, common.ColumnSeparator)
}

// hostBadges is the short state suffix of a row: the declared forwards.
func hostBadges(host domain.Host) string {
	if len(host.Forwards) == 0 {
		return ""
	}

	return "⇄" + itoa(len(host.Forwards))
}

func collectTags(hosts []domain.Host) []string {
	seen := make(map[string]struct{}, 8)
	tags := make([]string, 0, 8)

	for _, host := range hosts {
		for _, tag := range host.Tags {
			if _, ok := seen[tag]; ok {
				continue
			}

			seen[tag] = struct{}{}
			tags = append(tags, tag)
		}
	}

	sort.Strings(tags)

	return tags
}

func containsTag(tags []string, tag string) bool {
	for _, candidate := range tags {
		if candidate == tag {
			return true
		}
	}

	return false
}

func plural(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}

	return itoa(count) + " " + many
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

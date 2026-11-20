package app

import (
	"fmt"
	"image"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/MiguelFPP/tui-ssh/internal/tui/common"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
	"github.com/MiguelFPP/tui-ssh/internal/tui/styles"
)

// toastPrefix marks the statusbar toast, distinct from the panel error lines.
const toastPrefix = "aviso: "

// tabSeparator joins the top labels; the mouse hit-testing relies on its width.
const tabSeparator = "│"

// overlayBoxChrome is the cells the shared overlay box spends: a frame plus the
// padding on both sides. Long lines are clipped to what is left of the canvas.
const overlayBoxChrome = 6

// resize recomputes the canvas and the panel rectangles; the root owns hit-testing.
func (m *Model) resize() {
	width, height := m.canvas()
	body := bodyHeight(height)
	listW := m.listWidthNow(width)
	detailW := max(width-listW, 0)

	// The frames are chrome: every rectangle is the inner area of its box, one cell inside.
	listInner := image.Rectangle{}
	if listW > 0 {
		listInner = innerRect(0, headerRows, listW, body)
	}
	// The hidden list keeps the geometry of its visible form: its cursor must not be clamped.
	listArea := listInner
	if listW == 0 {
		listArea = innerRect(0, headerRows, splitWidth(width, m.listWidth), body)
	}
	detailInner := innerRect(listW, headerRows, detailW, body)
	panelInner := innerRect(0, headerRows, width, body)

	m.list.SetBounds(listArea.Min.X, listArea.Min.Y, listArea.Dx(), listArea.Dy())
	m.detail.SetBounds(detailInner.Dx(), detailInner.Dy())
	m.tunnels.SetBounds(panelInner.Min.X, panelInner.Min.Y, panelInner.Dx(), panelInner.Dy())
	m.config.SetBounds(panelInner.Dx(), panelInner.Dy())

	m.listRect, m.detailRect = listInner, detailInner
	m.panelRect = panelInner
	m.splitterRect = m.splitterBounds(listW, body)
	m.collapseRect = m.collapseBounds(width)
	m.markTabs(width)
}

// splitterBounds is the two shared border cells of the list and the detail, or an
// empty rectangle.
func (m Model) splitterBounds(listW, body int) image.Rectangle {
	if !m.canSplit() {
		return image.Rectangle{}
	}

	return image.Rect(listW-1, headerRows, listW+1, headerRows+body)
}

// collapseBounds is the right-aligned list button of the tab row, or empty when it
// does not fit.
func (m Model) collapseBounds(width int) image.Rectangle {
	if m.tab != HostsTab {
		return image.Rectangle{}
	}

	size := lipgloss.Width(m.collapseLabel())
	if start := width - size; start >= m.tabLabelsWidth() {
		return image.Rect(start, headerRows-1, start+size, headerRows)
	}

	return image.Rectangle{}
}

// collapseLabel is the list button: it points left while the list is shown and
// right while hidden.
func (m Model) collapseLabel() string {
	if m.listCollapsed {
		return "[>]"
	}

	return "[<]"
}

// tabLabelsWidth is the rendered width of the tab labels and their separators.
func (m Model) tabLabelsWidth() int {
	total := 0
	for tab := Tab(0); tab < tabCount; tab++ {
		total += lipgloss.Width(tabLabels[tab])
		if tab > 0 {
			total += lipgloss.Width(tabSeparator)
		}
	}

	return total
}

// innerRect is the area left inside the frame of a box at x, y with the given outer size.
func innerRect(x, y, width, height int) image.Rectangle {
	x, y = x+1, y+1

	return image.Rect(x, y, x+common.InnerCells(width), y+common.InnerCells(height))
}

// markTabs stores the rectangle of every top tab, on the row between the title and
// the body.
func (m *Model) markTabs(width int) {
	m.tabRects = [tabCount]image.Rectangle{}

	column := 0
	for tab := Tab(0); tab < tabCount; tab++ {
		size := lipgloss.Width(tabLabels[tab])
		if column+size > width {
			return // the labels a narrow row cannot show are not clickable
		}

		m.tabRects[tab] = image.Rect(column, headerRows-1, column+size, headerRows)
		column += size + lipgloss.Width(tabSeparator)
	}
}

func (m Model) canvas() (int, int) {
	return max(m.width, minWidth), max(m.height, minHeight)
}

func bodyHeight(height int) int {
	return max(height-headerRows-footerRows, 0)
}

// splitWidth reserves the left column for the list: the explicit width when given,
// a percentage otherwise, always capped so the detail keeps a minimum.
func splitWidth(total, explicit int) int {
	reserve := explicit
	if reserve == 0 {
		reserve = total * listPercent / 100
	}

	list := common.Clamp(reserve, minList, max(total-minDetailBox, minList))

	return common.Clamp(list, 0, total)
}

// render composes the header, the active tab and the statusbar.
func (m Model) render() string {
	width, height := m.canvas()

	lines := make([]string, 0, height)
	lines = append(lines, styles.Title.Render(common.Clip(" tui-ssh ", width)))
	lines = append(lines, m.tabsLine(width))
	lines = append(lines, m.body(width, height)...)
	lines = append(lines, m.statusLine(width))

	content := strings.Join(lines, "\n")
	switch {
	case m.tagPrompt.Visible():
		return m.promptOverlay(content, m.tagPrompt.View(width-overlayBoxChrome), width, height)
	case m.forwardPrompt.Visible():
		return m.promptOverlay(content, m.forwardPrompt.View(width-overlayBoxChrome), width, height)
	case m.confirm.Visible():
		return m.confirmOverlay(content, width, height)
	case m.help:
		return m.helpOverlay(content, width, height)
	case m.settings:
		return m.settingsOverlay(content, width, height)
	}

	return content
}

// body renders the framed panels of the active tab, always exactly bodyHeight rows high.
func (m Model) body(width, height int) []string {
	rows := bodyHeight(height)

	switch m.tab {
	case TunnelsTab:
		return common.Fit(m.box(m.tunnels.View(), width, rows, m.focus == FocusTunnels), width, rows)
	case ConfigTab:
		return common.Fit(m.box(m.config.View(), width, rows, m.focus == FocusConfig), width, rows)
	}

	listW := m.listWidthNow(width)
	detailW := max(width-listW, 0)

	detailLines := common.Fit(m.box(m.detail.View(), detailW, rows, m.focus == FocusDetail), detailW, rows)
	if listW == 0 {
		return detailLines // a collapsed list leaves the whole width to the detail
	}

	listLines := common.Fit(m.box(m.list.View(), listW, rows, m.focus == FocusList), listW, rows)

	out := make([]string, 0, rows)
	for i := range listLines {
		out = append(out, listLines[i]+detailLines[i])
	}

	if m.dragging {
		out = m.paintSplitter(out, listW)
	}

	return out
}

// box draws content inside a framed box of exactly width x height cells; focused
// picks the token.
func (m Model) box(content string, width, height int, focused bool) string {
	innerW, innerH := common.InnerCells(width), common.InnerCells(height)
	if innerW < 1 || innerH < 1 {
		// No room for a frame: the bare content keeps the layout tiling exactly.
		return strings.Join(common.Fit(content, width, height), "\n")
	}

	lines := common.Fit(content, innerW, innerH)
	for i, line := range lines {
		lines[i] = line + common.ResetStyle // an attribute left open must not reach the frame
	}

	style := styles.Border
	if focused {
		style = styles.BorderFocused
	}

	return style.Render(strings.Join(lines, "\n"))
}

// tabsLine renders the top labels plus, in the Hosts tab, the list button on the right.
func (m Model) tabsLine(width int) string {
	row := m.tabLabelsLine()
	if rect := m.collapseBounds(width); rect.Dx() > 0 {
		row = common.Pad(row, rect.Min.X) + styles.Tab.Render(m.collapseLabel())
	}

	return common.Pad(row, width)
}

// tabLabelsLine renders the labels of every tab with their separators.
func (m Model) tabLabelsLine() string {
	parts := make([]string, 0, tabCount*2-1)

	for tab := Tab(0); tab < tabCount; tab++ {
		if tab > 0 {
			parts = append(parts, styles.TabSeparator.Render(tabSeparator))
		}

		style := styles.Tab
		if tab == m.tab {
			style = styles.TabActive
		}

		parts = append(parts, style.Render(tabLabels[tab]))
	}

	return strings.Join(parts, "")
}

// statusLine shows the config path, the tab, the focus, the hints, the spinner and
// the toast.
func (m Model) statusLine(width int) string {
	parts := []string{
		"config: " + m.configLabel(),
		"tab: " + m.tab.String(),
		"foco: " + m.focusLabel(),
		m.tabsHint(),
		m.keys.Label(keymap.ActionFocusNext) + " foco",
		m.keys.Label(keymap.ActionHelp) + " ayuda",
		m.keys.Label(keymap.ActionQuit) + " salir",
	}

	text := strings.Join(parts, common.ColumnSeparator)
	if m.toast != "" {
		text = m.toast + common.ColumnSeparator + text
	}
	if m.loading() {
		text = m.spinner.View() + " " + text
	}

	return styles.Status.Render(common.Clip(text, width))
}

// tabsHint names the keys that switch the top tabs, in tab order.
func (m Model) tabsHint() string {
	actions := [...]keymap.Action{keymap.ActionTabHosts, keymap.ActionTabTunnels, keymap.ActionTabConfig}

	labels := make([]string, 0, len(actions))
	for _, action := range actions {
		labels = append(labels, m.keys.Label(action))
	}

	return strings.Join(labels, "/") + " tabs"
}

// helpOverlay centers the help box over the layout without changing its size.
func (m Model) helpOverlay(base string, width, height int) string {
	box := styles.Help.Render(strings.Join(m.helpLines(width-overlayBoxChrome), "\n"))

	return overlayBox(base, box, width, height)
}

// settingsOverlay centers the read-only settings box over the layout.
func (m Model) settingsOverlay(base string, width, height int) string {
	box := styles.Help.Render(strings.Join(m.settingsLines(width-overlayBoxChrome), "\n"))

	return overlayBox(base, box, width, height)
}

// promptOverlay centers an already rendered prompt over the layout.
func (m Model) promptOverlay(base string, lines []string, width, height int) string {
	box := styles.Help.Render(strings.Join(lines, "\n"))

	return overlayBox(base, box, width, height)
}

// confirmOverlay centers the confirmation over the layout, with its own frame.
func (m Model) confirmOverlay(base string, width, height int) string {
	lines := append(m.confirm.View(width-overlayBoxChrome), "", m.confirmHint())
	box := styles.Help.Render(strings.Join(lines, "\n"))

	return overlayBox(base, box, width, height)
}

// confirmHint is the esc line every confirmation documents.
func (m Model) confirmHint() string {
	return styles.MetaPlain.Render("esc también cancela")
}

// helpLines renders the full keymap shown by the help overlay.
func (m Model) helpLines(width int) []string {
	lines := make([]string, 0, 24)
	lines = append(lines, styles.HelpTitle.Render("tui-ssh · atajos"), "")

	for _, binding := range m.keys.Bindings(keymap.ScopeRoot) {
		lines = append(lines, m.helpLine(binding))
	}

	lines = append(lines, "", styles.HelpTitle.Render("hosts"))
	for _, binding := range m.keys.Bindings(keymap.ScopeHosts) {
		lines = append(lines, m.helpLine(binding))
	}

	lines = append(lines, "", styles.HelpTitle.Render("túneles"))
	for _, binding := range m.keys.Bindings(keymap.ScopeTunnels) {
		lines = append(lines, m.helpLine(binding))
	}

	lines = append(lines, "", styles.HelpTitle.Render("config"))
	for _, binding := range m.keys.Bindings(keymap.ScopeConfigView) {
		lines = append(lines, m.helpLine(binding))
	}

	lines = append(lines, "", styles.HelpKey.Render("esc")+"  cerrar la ayuda o reenviarla al panel")

	limit := max(width, 1)
	for index, line := range lines {
		lines[index] = common.Clip(line, limit)
	}

	return lines
}

// helpLine renders one binding: its keys and its description.
func (m Model) helpLine(binding keymap.Binding) string {
	return styles.HelpKey.Render(common.PadRight(m.keys.Label(binding.Action), 12)) + " " + binding.Desc
}

// settingsLines renders the settings summary: the file, the theme, the ssh config
// and the key overrides.
func (m Model) settingsLines(width int) []string {
	actions := m.overrideActions()

	lines := make([]string, 0, len(actions)+10)
	lines = append(lines, styles.HelpTitle.Render("configuración"))
	lines = append(lines, "archivo: "+m.settingsFileLabel())
	lines = append(lines, "tema: "+styles.CurrentTheme().Name)
	lines = append(lines, "ssh: "+m.cfg.SSHConfig)
	lines = append(lines, "binario ssh: "+m.cfg.SSHBinary)
	lines = append(lines, "runtime: "+m.runtimeLabel())
	lines = append(lines, "detalle: "+m.detailLayoutName())
	lines = append(lines, fmt.Sprintf("overrides: %d", len(actions)))

	if len(actions) == 0 {
		lines = append(lines, "sin overrides")
	} else {
		for _, action := range actions {
			lines = append(lines, "  "+action+": "+strings.Join(m.cfg.Keys[action], " / "))
		}
	}

	lines = append(lines, "", "edita el archivo y reinicia para aplicar", "")
	lines = append(lines, m.keys.Label(keymap.ActionSettings)+" / esc: cerrar")

	limit := max(width, 1)
	for index, line := range lines {
		lines[index] = common.Clip(line, limit)
	}

	return lines
}

// settingsFileLabel names the loaded file, or reports the built-in defaults.
func (m Model) settingsFileLabel() string {
	if strings.TrimSpace(m.cfg.ConfigPath) == "" {
		return "sin archivo (defaults)"
	}

	return m.cfg.ConfigPath
}

// overrideActions returns the actions with at least one override key, sorted.
func (m Model) overrideActions() []string {
	actions := make([]string, 0, len(m.cfg.Keys))
	for action, keys := range m.cfg.Keys {
		if len(keys) > 0 {
			actions = append(actions, action)
		}
	}

	sort.Strings(actions)

	return actions
}

// overlayBox centers an already rendered box over base without resizing base.
func overlayBox(base, box string, width, height int) string {
	boxLines := strings.Split(box, "\n")

	left := max((width-lipgloss.Width(box))/2, 0)
	top := max((height-len(boxLines))/2, 0)

	lines := strings.Split(base, "\n")
	for i, line := range boxLines {
		row := top + i
		if row < 0 || row >= len(lines) {
			continue
		}

		lines[row] = strings.Repeat(" ", left) + line
	}

	return strings.Join(lines, "\n")
}

// paintSplitter redraws the two cells of the shared border with the handle token.
func (m Model) paintSplitter(rows []string, listW int) []string {
	if !m.canSplit() {
		return rows
	}

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(styles.CurrentTheme().Border))
	if m.dragging {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(styles.CurrentTheme().Accent))
	}
	handle := style.Render("││")

	// The top and the bottom rows keep the corners of the two boxes.
	for i := 1; i < len(rows)-1; i++ {
		rows[i] = common.Clip(rows[i], listW-1) + handle + common.Tail(rows[i], listW+1)
	}

	return rows
}

// canSplit reports whether the Hosts tab shows a draggable splitter: a list beside
// the detail.
func (m Model) canSplit() bool {
	width, _ := m.canvas()
	if m.detailSingle || width < splitThreshold {
		return false
	}
	listW := m.listWidthNow(width)

	return m.tab == HostsTab && listW > 0 && width > listW
}

// listWidthNow returns the outer width of the list, or zero while it is collapsed.
func (m Model) listWidthNow(width int) int {
	if m.listCollapsed {
		return 0
	}
	if m.detailSingle || width < splitThreshold {
		return 0
	}

	return splitWidth(width, m.listWidth)
}

// configLabel is the ssh config the TUI reads, shortened for the status bar.
func (m Model) configLabel() string {
	if strings.TrimSpace(m.cfg.SSHConfig) == "" {
		return "-"
	}

	return shortPath(m.cfg.SSHConfig)
}

// runtimeLabel names the directory of the tunnel state.
func (m Model) runtimeLabel() string {
	if strings.TrimSpace(m.cfg.RuntimeDir) == "" {
		return "(temporal)"
	}

	return m.cfg.RuntimeDir
}

// detailLayoutName reports the effective layout of the Hosts tab.
func (m Model) detailLayoutName() string {
	if m.detailSingle {
		return "single"
	}

	return "split"
}

// focusLabel names the panel that owns the keyboard.
func (m Model) focusLabel() string {
	switch m.focus {
	case FocusDetail:
		return "detalle"
	case FocusTunnels:
		return "túneles"
	case FocusConfig:
		return "config"
	default:
		return "lista"
	}
}

// shortPath keeps the last two components of a path.
func shortPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}

	return ".../" + strings.Join(parts[len(parts)-2:], "/")
}

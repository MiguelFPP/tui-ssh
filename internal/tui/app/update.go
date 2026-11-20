package app

import (
	"image"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/tui/common"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

// handleKey applies the root keymap; stop means the returned command quits the program.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()

	// A modal owns every key but ctrl+c, which still quits after a confirmation.
	if m.modalVisible() {
		if key != "ctrl+c" {
			return m.handleModalKey(msg), false
		}

		// ctrl+c on a pending confirmation discards it and keeps the tunnels.
		m.tagPrompt.prompt.Close()
		m.forwardPrompt.prompt.Close()
		m.confirm.confirm.Close()
		m.confirm.kind = confirmNone

		return m.quit()
	}

	// The help overlay only answers to quit, to its own toggle and to esc.
	if m.help {
		switch {
		case m.isQuit(key):
			return m.quit()
		case isAction(m.keys, keymap.ScopeRoot, key, keymap.ActionHelp), key == "esc":
			m.help = false
		}

		return nil, false
	}

	// The settings overlay only answers to quit, to its own toggle and to esc.
	if m.settings {
		switch {
		case m.isQuit(key):
			return m.quit()
		case isAction(m.keys, keymap.ScopeRoot, key, keymap.ActionSettings), key == "esc":
			m.settings = false
		}

		return nil, false
	}

	// The filter input of the Hosts tab owns every key but the quit shortcut.
	if m.tab == HostsTab && m.focus == FocusList && m.list.Filtering() {
		if key == "ctrl+c" {
			return m.quit()
		}

		return m.forwardToList(msg), false
	}

	// A modal inside a panel owns every key but the quit shortcut.
	if m.tab == TunnelsTab && m.tunnels.CapturesInput() {
		if key == "ctrl+c" {
			return m.quit()
		}

		return m.forwardToTunnels(msg), false
	}

	action, ok := m.keys.Match(keymap.ScopeRoot, key)
	if !ok {
		return m.forwardKey(msg), false
	}

	switch action {
	case keymap.ActionQuit:
		return m.quit()
	case keymap.ActionHelp:
		m.help = true

		return nil, false
	case keymap.ActionSettings:
		m.settings = true

		return nil, false
	case keymap.ActionTabHosts:
		return m.setTab(HostsTab), false
	case keymap.ActionTabTunnels:
		return m.setTab(TunnelsTab), false
	case keymap.ActionTabConfig:
		return m.setTab(ConfigTab), false
	case keymap.ActionToggleList:
		if m.tab == HostsTab {
			return m.toggleList(), false
		}
	case keymap.ActionListShrink:
		if m.tab == HostsTab {
			return m.resizeList(-listStep), false
		}
	case keymap.ActionListGrow:
		if m.tab == HostsTab {
			return m.resizeList(listStep), false
		}
	case keymap.ActionDetailLayout:
		if m.tab == HostsTab {
			return m.toggleDetailLayout(), false
		}
	case keymap.ActionFocusNext, keymap.ActionFocusPrev:
		// Outside the Hosts tab the key belongs to the panel, which uses it for its own sections.
		if m.tab == HostsTab {
			m.toggleFocus()

			return nil, false
		}
	}

	return m.forwardKey(msg), false
}

// handleModalKey routes a key to the open modal.
func (m *Model) handleModalKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.tagPrompt.Visible() {
		next, action, cmd := m.tagPrompt.prompt.HandleKey(msg)
		m.tagPrompt.prompt = next

		if action == keymap.ActionTagsSave {
			return m.applyTagPrompt()
		}

		return cmd
	}

	if m.forwardPrompt.Visible() {
		next, action, cmd := m.forwardPrompt.prompt.HandleKey(msg)
		m.forwardPrompt.prompt = next

		if action == keymap.ActionForwardSave {
			return m.applyForwardPrompt()
		}

		return cmd
	}

	if m.confirm.Visible() {
		next, decision := m.confirm.confirm.HandleKey(msg)
		m.confirm.confirm = next

		switch decision {
		case common.DecisionYes:
			return m.applyConfirm()
		case common.DecisionNo:
			m.confirm.kind = confirmNone

			// Declining keeps the tunnels alive and leaves the program right away.
			return tea.Quit
		}

		return nil
	}

	return nil
}

// applyConfirm runs the action the confirmation approved.
func (m *Model) applyConfirm() tea.Cmd {
	kind := m.confirm.kind
	m.confirm.kind = confirmNone

	if kind == confirmQuit {
		return m.stopTunnelsOnQuit()
	}

	return nil
}

// forwardKey sends a key to the panel that owns the focus.
func (m *Model) forwardKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.focus {
	case FocusDetail:
		return m.handleListKey(msg)
	case FocusTunnels:
		return m.forwardToTunnels(msg)
	case FocusConfig:
		return m.forwardToConfig(msg)
	default:
		return m.handleListKey(msg)
	}
}

// forwardToTunnels sends a key to the tunnel panel.
func (m *Model) forwardToTunnels(msg tea.KeyPressMsg) tea.Cmd {
	next, cmd := m.tunnels.Update(msg)
	m.tunnels = next

	return cmd
}

// forwardToConfig sends a key to the configuration viewer, or hands the terminal
// over to the editor when the edit key is pressed.
func (m *Model) forwardToConfig(msg tea.KeyPressMsg) tea.Cmd {
	if isAction(m.keys, keymap.ScopeConfigView, msg.String(), keymap.ActionConfigEdit) {
		return m.openConfigEditor()
	}

	next, cmd := m.config.Update(msg)
	m.config = next

	return cmd
}

// forwardToList sends a key to the filter input. Esc and enter end the filter
// without clearing it: the filter stays applied and the list takes the keyboard
// back, which is what makes "/texto" then "enter" a usable gesture.
func (m *Model) forwardToList(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "enter":
		m.list.SetFilterVisible(false)

		return nil
	}

	next, cmd := m.list.Update(msg)
	m.list = next

	return cmd
}

// handleListKey applies the Hosts scope to the list and the detail panels.
func (m *Model) handleListKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.list.Filtering() {
		return m.forwardToList(msg)
	}

	action, ok := m.keys.Match(keymap.ScopeHosts, msg.String())
	if !ok {
		return nil
	}

	switch action {
	case keymap.ActionHostUp:
		m.list.Up()
	case keymap.ActionHostDown:
		m.list.Down()
	case keymap.ActionHostPageUp:
		m.list.PageUp()
	case keymap.ActionHostPageDown:
		m.list.PageDown()
	case keymap.ActionHostHome:
		m.list.Home()
	case keymap.ActionHostEnd:
		m.list.End()
	case keymap.ActionHostFilter:
		m.list.SetFilterVisible(true)

		return nil
	case keymap.ActionHostClearFilter:
		m.list.SetFilter("")
		m.list.SetTagFilter("")

		return m.afterSelection()
	case keymap.ActionTagNext:
		m.list.CycleTag(1)

		return m.afterSelection()
	case keymap.ActionTagPrev:
		m.list.CycleTag(-1)

		return m.afterSelection()
	case keymap.ActionHostConnect:
		host, ok := m.list.Selected()
		if !ok {
			return nil
		}

		return m.connect(host)
	case keymap.ActionHostTunnel:
		host, ok := m.list.Selected()
		if !ok {
			return nil
		}

		return m.quickTunnel(host)
	case keymap.ActionHostTags:
		host, ok := m.list.Selected()
		if !ok {
			return nil
		}
		m.openTagPrompt(host)

		return nil
	case keymap.ActionHostForward:
		host, ok := m.list.Selected()
		if !ok {
			return nil
		}
		m.openForwardPrompt(host)

		return nil
	case keymap.ActionHostCopy:
		host, ok := m.list.Selected()
		if !ok {
			return nil
		}

		return m.notify("comando ssh · " + host.Alias)
	case keymap.ActionHostExternal:
		return m.openConfigEditor()
	case keymap.ActionHostRefresh:
		if m.resolve != nil {
			m.resolve.Invalidate("")
		}

		return m.loadHosts()
	}

	return m.afterSelection()
}

// afterSelection reloads the detail of the newly selected host.
func (m *Model) afterSelection() tea.Cmd {
	host, ok := m.list.Selected()
	if !ok {
		m.detail.Clear()

		return nil
	}

	m.detail.SetHost(host)

	return m.resolveHost(host)
}

// quickTunnel starts the first declared forward of the host, or opens the prompt
// when the host has none.
func (m *Model) quickTunnel(host domain.Host) tea.Cmd {
	if len(host.Forwards) == 0 {
		m.openForwardPrompt(host)

		return nil
	}

	return m.tunnels.Start(host.Alias, host.Forwards[0])
}

// setTab switches the active tab and hands the focus to its main panel.
func (m *Model) setTab(tab Tab) tea.Cmd {
	if tab == m.tab {
		return nil
	}

	if m.tab == TunnelsTab {
		m.tunnels.SetActive(false)
	}

	m.tab = tab
	var cmd tea.Cmd

	switch tab {
	case TunnelsTab:
		m.focus = FocusTunnels
		if host, ok := m.list.Selected(); ok {
			m.tunnels.SetHostFilter(host.Alias)
		}
		cmd = m.tunnels.SetActive(true)
	case ConfigTab:
		m.focus = FocusConfig
		cmd = m.config.Init()
	default:
		m.focus = m.listFocus()
	}
	m.resize() // the list button of the tab row belongs to the Hosts tab only

	return cmd
}

// listFocus is the panel that owns the keyboard inside the Hosts tab.
func (m Model) listFocus() Focus {
	if m.listCollapsed {
		return FocusDetail
	}

	return FocusList
}

// toggleFocus alternates between the list and the detail inside the Hosts tab.
func (m *Model) toggleFocus() {
	if m.focus == FocusDetail {
		m.focus = m.listFocus()

		return
	}

	m.focus = FocusDetail
}

// toggleList hides or shows the list, leaving the keyboard in the detail either way.
func (m *Model) toggleList() tea.Cmd {
	if m.tab != HostsTab {
		return nil
	}

	m.listCollapsed = !m.listCollapsed
	if m.listCollapsed {
		m.dragging = false // the handle disappears together with the list
	}
	m.focus = FocusDetail
	m.resize()

	save := m.saveLayout
	if save == nil {
		return nil
	}

	width, collapsed := m.listWidth, m.listCollapsed

	return func() tea.Msg { return layoutSavedMsg{err: save(width, collapsed)} }
}

// resizeList moves the shared border by delta cells and persists it once.
func (m *Model) resizeList(delta int) tea.Cmd {
	if m.tab != HostsTab || m.listCollapsed || m.detailSingle {
		return nil
	}

	width, _ := m.canvas()
	current := splitWidth(width, m.listWidth)
	next := common.Clamp(current+delta, minList, max(width-minDetailBox, minList))
	if next == current {
		return nil
	}

	m.listWidth = next
	m.resize()

	save := m.saveLayout
	if save == nil {
		return nil
	}

	persisted, collapsed := m.listWidth, m.listCollapsed

	return func() tea.Msg { return layoutSavedMsg{err: save(persisted, collapsed)} }
}

// toggleDetailLayout alternates between the split and the single column layout.
func (m *Model) toggleDetailLayout() tea.Cmd {
	m.detailSingle = !m.detailSingle
	m.resize()

	save := m.saveDetail
	if save == nil {
		return nil
	}

	layout := m.detailLayoutName()

	return func() tea.Msg { return layoutSavedMsg{err: save(layout)} }
}

// handleClick routes a click to the tabs, the list button, the splitter or the panel.
func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if m.help || m.settings || m.modalVisible() {
		return nil
	}

	splitter := m.onSplitter(msg.X, msg.Y)
	if !splitter {
		m.dragging = false // a click away from the handle never keeps a drag alive
	}

	if tab, ok := m.tabAt(msg.X, msg.Y); ok {
		return m.setTab(tab) // an active tab is a no-op: it never steals the focus
	}

	if m.onCollapse(msg.X, msg.Y) {
		return m.toggleList() // the button mirrors the toggle key
	}

	if splitter {
		m.dragging, m.dragDirty = true, false

		return nil // resizing never moves the keyboard
	}

	panel, ok := m.panelAt(msg.X, msg.Y)
	if !ok {
		return nil
	}
	m.focus = panel

	if panel == FocusList {
		host, ok := m.list.RowAt(msg.X, msg.Y)
		if !ok {
			return nil
		}
		m.list.SelectAlias(host.Alias)

		return m.afterSelection()
	}

	return m.dispatchMouse(msg, panel)
}

// handleMotion resizes the list while the border is dragged.
func (m *Model) handleMotion(msg tea.MouseMotionMsg) tea.Cmd {
	if !m.dragging {
		return nil
	}

	// The handle only exists in the Hosts tab with the list visible.
	if m.tab != HostsTab || m.listCollapsed || m.detailSingle {
		m.dragging = false

		return nil
	}

	width, _ := m.canvas()
	next := common.Clamp(msg.X+1, minList, max(width-minDetailBox, minList))
	if next == splitWidth(width, m.listWidth) {
		return nil
	}

	m.listWidth = next
	m.dragDirty = true
	m.resize()

	return nil
}

// handleRelease ends the list drag and persists the new width once.
func (m *Model) handleRelease() tea.Cmd {
	if !m.dragging {
		return nil
	}

	m.dragging = false
	if !m.dragDirty {
		return nil
	}

	m.dragDirty = false
	save := m.saveLayout
	if save == nil {
		return nil
	}

	width, collapsed := m.listWidth, m.listCollapsed

	return func() tea.Msg { return layoutSavedMsg{err: save(width, collapsed)} }
}

// onSplitter reports whether the point falls on the draggable border.
func (m Model) onSplitter(x, y int) bool {
	return m.tab == HostsTab && m.canSplit() && image.Pt(x, y).In(m.splitterRect)
}

// onCollapse reports whether the point falls on the list button of the tab row.
func (m Model) onCollapse(x, y int) bool {
	return m.tab == HostsTab && image.Pt(x, y).In(m.collapseRect)
}

// handleWheel sends the wheel to the panel under the cursor, not to the focused one.
func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.help || m.settings || m.modalVisible() {
		return nil
	}

	panel, ok := m.panelAt(msg.X, msg.Y)
	if !ok {
		return nil
	}
	m.focus = panel

	switch panel {
	case FocusList:
		m.list.Wheel(wheelDelta(msg))

		return m.afterSelection()
	case FocusTunnels:
		next, cmd := m.tunnels.Update(msg)
		m.tunnels = next

		return cmd
	case FocusConfig:
		next, cmd := m.config.Update(msg)
		m.config = next

		return cmd
	default:
		return nil
	}
}

// dispatchMouse routes a non-selection click to the panel under the pointer.
func (m *Model) dispatchMouse(msg tea.MouseClickMsg, panel Focus) tea.Cmd {
	switch panel {
	case FocusTunnels:
		next, cmd := m.tunnels.Update(msg)
		m.tunnels = next

		return cmd
	case FocusConfig:
		next, cmd := m.config.Update(msg)
		m.config = next

		return cmd
	default:
		return nil
	}
}

// tabAt maps terminal coordinates to the top tab that owns them.
func (m Model) tabAt(x, y int) (Tab, bool) {
	point := image.Pt(x, y)

	for tab := Tab(0); tab < tabCount; tab++ {
		if m.tabRects[tab].Dx() > 0 && point.In(m.tabRects[tab]) {
			return tab, true
		}
	}

	return m.tab, false
}

// panelAt maps terminal coordinates to the panel that owns them.
func (m Model) panelAt(x, y int) (Focus, bool) {
	point := image.Pt(x, y)

	switch m.tab {
	case TunnelsTab:
		return FocusTunnels, point.In(m.panelRect)
	case ConfigTab:
		return FocusConfig, point.In(m.panelRect)
	}

	switch {
	case point.In(m.listRect):
		return FocusList, true
	case point.In(m.detailRect):
		return FocusDetail, true
	default:
		return m.focus, false
	}
}

// fanOut delivers an async message to every child; each one ignores what it does not own.
func (m *Model) fanOut(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, 0, 3)

	var cmd tea.Cmd

	m.tunnels, cmd = m.tunnels.Update(msg)
	cmds = append(cmds, cmd)

	m.config, cmd = m.config.Update(msg)
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

// afterUpdate reconciles the toast and keeps exactly one spinner tick in flight.
func (m *Model) afterUpdate() tea.Cmd {
	cmds := make([]tea.Cmd, 0, 2)

	if cmd := m.syncToast(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.loading() && !m.spinning {
		m.spinning = true
		cmds = append(cmds, m.spinner.Tick)
	}

	return tea.Batch(cmds...)
}

// syncToast shows a toast when the first error changes, and schedules its expiry.
func (m *Model) syncToast() tea.Cmd {
	err := m.currentError()
	if sameError(err, m.lastErr) {
		return nil
	}

	m.lastErr = err
	if err == nil {
		return nil
	}

	m.toast = toastPrefix + err.Error()
	m.toastSeq++
	seq := m.toastSeq

	schedule := m.expire
	if schedule == nil {
		return nil
	}

	return schedule(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{seq: seq} })
}

// currentError returns the first active error of the panels.
func (m Model) currentError() error {
	if err := m.tunnels.Err(); err != nil {
		return err
	}

	return m.config.Err()
}

// loading reports whether any panel is still reading.
func (m Model) loading() bool {
	return m.tunnels.Loading() || m.config.Loading()
}

// quit leaves the program, asking first when there are running tunnels.
func (m *Model) quit() (tea.Cmd, bool) {
	if m.tunnels.AnyRunning() {
		m.confirm.OpenForQuit(m.runningCount())

		return nil, false
	}

	return tea.Quit, true
}

// isQuit reports whether key maps to quitting in the root scope.
func (m Model) isQuit(key string) bool {
	return isAction(m.keys, keymap.ScopeRoot, key, keymap.ActionQuit)
}

// isAction reports whether key maps to action inside scope.
func isAction(keys keymap.Registry, scope keymap.Scope, key string, action keymap.Action) bool {
	match, ok := keys.Match(scope, key)

	return ok && match == action
}

// modalVisible reports whether any modal owns the keyboard.
func (m Model) modalVisible() bool {
	return m.tagPrompt.Visible() || m.forwardPrompt.Visible() || m.confirm.Visible()
}

// execProcess turns a resolved command into the tea command that hands the
// terminal over until the program exits.
func execProcess(msg execMsg) tea.Cmd {
	command := exec.Command(msg.command.Binary, msg.command.Args...)

	return tea.ExecProcess(command, func(err error) tea.Msg {
		if msg.external {
			return externalDoneMsg{err: err}
		}

		return sessionDoneMsg{alias: msg.alias, err: err}
	})
}

func sameError(a, b error) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Error() == b.Error()
	}
}

// wheelDelta turns a wheel message into the signed step the panels expect: a
// positive value scrolls down.
func wheelDelta(msg tea.MouseWheelMsg) int {
	if strings.Contains(msg.String(), "up") {
		return -1
	}

	return 1
}

// Package tunnelpanel renders the Tunnels tab: one table of the background
// forwardings, the confirmation before stopping one and the paginated logs of the
// selected master. The ticker only runs while the tab is visible.
package tunnelpanel

import (
	"context"
	"errors"
	"fmt"
	"image"
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

	headerRows = 2 // the title row and the column headers
	footerRows = 1

	// defaultTickInterval refreshes the state of the masters; it only exists while
	// the tab is visible.
	defaultTickInterval = time.Second

	checkTimeout = 2 * time.Second
	logsTimeout  = 2 * time.Second

	wheelStep = 3

	hostColumn      = 22
	directionColumn = 4
	bindColumn      = 22
	targetColumn    = 26
	stateColumn     = 10
	columnGap       = "  "

	columnSeparator = " · "

	// scope labels name the active filter of the table.
	scopeAll = "alcance: todos"
)

// tunnelsMsg carries one refresh of the tunnel list.
type tunnelsMsg struct {
	seq     int
	tunnels []domain.Tunnel
	err     error
}

// logsMsg carries one bounded read of the master stderr.
type logsMsg struct {
	seq  int
	log  domain.TunnelLog
	name string
	err  error
}

// actionMsg carries the outcome of start, stop or restart.
type actionMsg struct {
	seq  int
	name string
	op   string
	err  error
}

// tickMsg re-arms the conditional ticker; the generation invalidates the ticks of
// a hidden tab.
type tickMsg struct {
	gen int
}

// confirmKind selects which action a confirmation would run.
type confirmKind uint8

const (
	confirmNone confirmKind = iota
	confirmStop
	confirmStopAll
)

// Model is the Tunnels panel; it never performs I/O outside a command.
type Model struct {
	tunnels ports.TunnelService
	keys    keymap.Registry

	all    []domain.Tunnel
	rows   []string
	cursor int
	offset int

	hostFilter string
	scopeLabel string

	logs     domain.TunnelLog
	logsOpen bool
	logsErr  error
	logsName string
	logLines []string
	viewport viewport.Model

	active  bool
	tickGen int
	// interval is the polling period; the tests shorten it so no batch waits.
	interval time.Duration

	loading bool
	writing bool
	op      string

	err       error
	actionErr error
	notice    string

	confirm     confirmKind
	confirmName string

	tunnelsSeq int
	logsSeq    int
	actionSeq  int

	width  int
	height int
	bounds image.Rectangle

	rowRects []image.Rectangle
}

// New builds the panel. Nothing is read until the tab becomes active. An empty
// keymap falls back to the built-in defaults.
func New(tunnels ports.TunnelService, keys keymap.Registry) Model {
	if len(keys.All()) == 0 {
		keys = keymap.Default()
	}

	m := Model{
		tunnels:    tunnels,
		keys:       keys,
		viewport:   viewport.New(),
		interval:   defaultTickInterval,
		scopeLabel: scopeAll,
	}
	m.viewport.FillHeight = true
	m.viewport.MouseWheelEnabled = true
	m.SetBounds(0, 0, defaultWidth, defaultHeight)

	return m
}

// SetInterval shortens or lengthens the polling period; the tests use it.
func (m *Model) SetInterval(d time.Duration) {
	if d > 0 {
		m.interval = d
	}
}

// DisableTicker stops scheduling refreshes. The tests use it so no command ever
// waits on a timer, which would block the test until the interval elapses.
func (m *Model) DisableTicker() {
	m.interval = 0
}

// SetBounds places the panel and resizes the logs viewport.
func (m *Model) SetBounds(x, y, width, height int) {
	m.width, m.height = max(width, 0), max(height, 0)
	m.bounds = image.Rect(x, y, x+m.width, y+m.height)
	m.viewport.SetWidth(max(m.width, 1))
	m.viewport.SetHeight(m.logViewportHeight())
	m.markRows()
}

// SetHostFilter narrows the table to the tunnels of one alias.
func (m *Model) SetHostFilter(alias string) {
	m.hostFilter = strings.TrimSpace(alias)
	m.scopeLabel = m.hostFilter
	if m.scopeLabel == "" {
		m.scopeLabel = scopeAll
	}
	m.clamp()
}

// ToggleScope drops or restores the host filter.
func (m *Model) ToggleScope() {
	if m.hostFilter == "" {
		return
	}

	if m.scopeLabel == m.hostFilter {
		m.scopeLabel = scopeAll
	} else {
		m.scopeLabel = m.hostFilter
	}
	m.clamp()
}

// SetActive starts or stops the ticker. A hidden tab never polls the masters.
func (m *Model) SetActive(active bool) tea.Cmd {
	if m.active == active {
		return nil
	}

	m.active = active
	if !active {
		m.tickGen++

		return nil
	}

	if !m.loading {
		m.loading = true

		return tea.Batch(m.load(), m.tick())
	}

	return m.tick()
}

// Active reports whether the ticker is running.
func (m Model) Active() bool {
	return m.active
}

// Loading reports whether a read or an action is still in flight.
func (m Model) Loading() bool {
	return m.loading || m.writing
}

// CapturesInput reports that a confirmation or the logs own the keyboard.
func (m Model) CapturesInput() bool {
	return m.confirm != confirmNone || m.logsOpen
}

// Err returns the first active failure of the panel.
func (m Model) Err() error {
	switch {
	case m.actionErr != nil:
		return m.actionErr
	case m.err != nil:
		return m.err
	case m.logsOpen && m.logsErr != nil:
		return m.logsErr
	default:
		return nil
	}
}

// Selected returns the tunnel under the cursor.
func (m Model) Selected() (domain.Tunnel, bool) {
	rows := m.visible()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return domain.Tunnel{}, false
	}

	return rows[m.cursor], true
}

// SelectID moves the cursor to the tunnel with the given id, if it is visible.
func (m *Model) SelectID(id string) bool {
	rows := m.visible()
	for index, tunnel := range rows {
		if tunnel.ID == id {
			m.cursor = index
			m.scrollToCursor()

			return true
		}
	}

	return false
}

// Refresh reloads the tunnels.
func (m *Model) Refresh() tea.Cmd {
	m.loading = true

	return m.load()
}

// InvalidateHostState clears the cached status of the masters, so the next tick
// re-checks them instead of trusting a list that an external action changed.
func (m *Model) InvalidateHostState() {
	m.loading = false
	m.actionErr = nil
}

// Service returns the tunnel service the panel is bound to, or nil.
func (m Model) Service() ports.TunnelService {
	return m.tunnels
}

// AnyRunning reports whether at least one tunnel is believed to be alive.
func (m Model) AnyRunning() bool {
	for _, tunnel := range m.all {
		if tunnel.Running() {
			return true
		}
	}

	return false
}

// RunningTunnels returns the tunnels believed to be alive.
func (m Model) RunningTunnels() []domain.Tunnel {
	running := make([]domain.Tunnel, 0, len(m.all))
	for _, tunnel := range m.all {
		if tunnel.Running() {
			running = append(running, tunnel)
		}
	}

	return running
}

// Start launches a spec for host and records the outcome.
func (m *Model) Start(host string, spec domain.ForwardSpec) tea.Cmd {
	if m.tunnels == nil {
		m.actionErr = errors.New("panel de túneles sin servicio")

		return nil
	}

	m.writing = true
	m.op = "start"
	m.actionErr = nil
	seq := m.nextActionSeq()

	service := m.tunnels

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), domain.TunnelStartTimeout+time.Second)
		defer cancel()

		tunnel, err := service.Start(ctx, host, spec)

		return actionMsg{seq: seq, name: tunnel.Host, op: opStart, err: err}
	}
}

// Stop asks for confirmation before ending the selected tunnel.
func (m *Model) Stop() tea.Cmd {
	tunnel, ok := m.Selected()
	if !ok {
		return nil
	}

	m.confirm = confirmStop
	m.confirmName = tunnel.ID

	return nil
}

// Restart stops and starts the selected tunnel again.
func (m *Model) Restart() tea.Cmd {
	tunnel, ok := m.Selected()
	if !ok {
		return nil
	}

	if m.tunnels == nil {
		m.actionErr = errors.New("panel de túneles sin servicio")

		return nil
	}

	m.writing = true
	m.op = "restart"
	m.actionErr = nil
	seq := m.nextActionSeq()

	service := m.tunnels

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), domain.TunnelStartTimeout+domain.TunnelStopTimeout)
		defer cancel()

		if tunnel.Running() {
			if err := service.Stop(ctx, tunnel.ID); err != nil {
				return actionMsg{seq: seq, name: tunnel.Host, op: opRestart, err: err}
			}
		}

		_, err := service.Start(ctx, tunnel.Host, tunnel.Spec)

		return actionMsg{seq: seq, name: tunnel.Host, op: opRestart, err: err}
	}
}

// OpenLogs loads the stderr of the selected tunnel and shows the viewport.
func (m *Model) OpenLogs() tea.Cmd {
	tunnel, ok := m.Selected()
	if !ok {
		return nil
	}

	if m.tunnels == nil {
		m.logsErr = errors.New("panel de túneles sin servicio")
		m.logsOpen = true

		return nil
	}

	m.logsOpen = true
	m.logsName = tunnel.Host
	m.logsErr = nil
	seq := m.nextLogsSeq()

	service := m.tunnels

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), logsTimeout)
		defer cancel()

		log, err := service.Logs(ctx, tunnel.ID)

		return logsMsg{seq: seq, log: log, name: tunnel.Host, err: err}
	}
}

// CloseLogs hides the logs viewport.
func (m *Model) CloseLogs() {
	m.logsOpen = false
	m.logsErr = nil
	m.logLines = nil
	m.viewport.SetContent("")
}

// Up moves the cursor up.
func (m *Model) Up() {
	m.moveBy(-1)
}

// Down moves the cursor down.
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
	m.moveTo(len(m.visible()) - 1)
}

// Wheel scrolls the logs while they are open, or the table otherwise.
func (m *Model) Wheel(delta int) {
	if delta == 0 {
		return
	}

	if m.logsOpen {
		if delta > 0 {
			m.viewport.ScrollDown(wheelStep)
		} else {
			m.viewport.ScrollUp(wheelStep)
		}

		return
	}

	step := -wheelStep
	if delta > 0 {
		step = wheelStep
	}
	m.moveBy(step)
}

// LogLines returns the visible lines of the open log.
func (m Model) LogLines() []string {
	return m.logLines
}

// Update applies one message and returns the follow-up command.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tunnelsMsg:
		return m.applyTunnels(msg)
	case logsMsg:
		return m.applyLogs(msg)
	case actionMsg:
		return m.applyAction(msg)
	case tickMsg:
		return m.applyTick(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseWheelMsg:
		if m.logsOpen {
			m.viewport, _ = m.viewport.Update(msg)
		}

		return m, nil
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	default:
		return m, nil
	}
}

// Init loads the tunnels for the first time.
func (m Model) Init() tea.Cmd {
	if !m.active {
		return nil
	}

	return m.load()
}

// applyTunnels stores one refresh.
func (m Model) applyTunnels(msg tunnelsMsg) (Model, tea.Cmd) {
	if msg.seq != m.tunnelsSeq {
		return m, nil
	}

	m.loading = false
	if msg.err != nil {
		m.err = msg.err

		return m, nil
	}

	anchor := ""
	if tunnel, ok := m.Selected(); ok {
		anchor = tunnel.ID
	}

	m.err = nil
	m.all = msg.tunnels
	m.clamp()

	if anchor != "" {
		m.SelectID(anchor)
	}

	return m, nil
}

// applyLogs stores one log read.
func (m Model) applyLogs(msg logsMsg) (Model, tea.Cmd) {
	if msg.seq != m.logsSeq {
		return m, nil
	}

	if msg.err != nil {
		m.logsErr = msg.err

		return m, nil
	}

	m.logsErr = nil
	m.logs = msg.log
	m.logLines = msg.log.Lines
	m.viewport.SetContent(strings.Join(msg.log.Lines, "\n"))
	m.viewport.GotoBottom()

	return m, nil
}

// applyAction stores the outcome of start, stop or restart and refreshes the list.
func (m Model) applyAction(msg actionMsg) (Model, tea.Cmd) {
	if msg.seq != m.actionSeq {
		return m, nil
	}

	m.writing = false
	m.op = ""

	if msg.err != nil {
		m.actionErr = msg.err

		return m, nil
	}

	m.actionErr = nil
	switch msg.op {
	case opStart:
		m.notice = "túnel levantado"
	case opStop:
		m.notice = "túnel detenido"
	case opRestart:
		m.notice = "túnel reiniciado"
	}

	m.loading = true

	return m, m.load()
}

// applyTick refreshes the state of the masters while the tab is visible.
func (m Model) applyTick(msg tickMsg) (Model, tea.Cmd) {
	if msg.gen != m.tickGen || !m.active {
		return m, nil
	}

	cmds := make([]tea.Cmd, 0, 2)
	if !m.loading {
		m.loading = true
		cmds = append(cmds, m.load())
	}
	cmds = append(cmds, m.tick())

	return m, tea.Batch(cmds...)
}

// handleKey routes the panel keys, including the modal scopes.
func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	key := msg.String()

	if m.confirm != confirmNone {
		action, ok := m.keys.Match(keymap.ScopeTunnelsConfirm, key)
		if !ok {
			return m, nil
		}

		if action == keymap.ActionTunnelConfirmNo {
			m.confirm = confirmNone
			m.confirmName = ""

			return m, nil
		}

		kind := m.confirm
		name := m.confirmName
		m.confirm = confirmNone
		m.confirmName = ""

		switch kind {
		case confirmStop:
			return m.stopConfirmed(name)
		case confirmStopAll:
			return m.stopAllConfirmed()
		}

		return m, nil
	}

	if m.logsOpen {
		action, ok := m.keys.Match(keymap.ScopeTunnelLogs, key)
		if !ok {
			m.viewport, _ = m.viewport.Update(msg)

			return m, nil
		}

		switch action {
		case keymap.ActionLogsClose:
			m.CloseLogs()
		case keymap.ActionLogsReload:
			return m, m.OpenLogs()
		case keymap.ActionLogsUp:
			m.viewport.ScrollUp(1)
		case keymap.ActionLogsDown:
			m.viewport.ScrollDown(1)
		case keymap.ActionLogsPageUp:
			m.viewport.ScrollUp(m.viewport.Height())
		case keymap.ActionLogsPageDown:
			m.viewport.ScrollDown(m.viewport.Height())
		case keymap.ActionLogsHome:
			m.viewport.GotoTop()
		case keymap.ActionLogsEnd:
			m.viewport.GotoBottom()
		}

		return m, nil
	}

	action, ok := m.keys.Match(keymap.ScopeTunnels, key)
	if !ok {
		return m, nil
	}

	return m.applyActionKey(action)
}

// applyActionKey runs the table action of the Tunnels scope.
func (m Model) applyActionKey(action keymap.Action) (Model, tea.Cmd) {
	switch action {
	case keymap.ActionTunnelUp:
		m.Up()
	case keymap.ActionTunnelDown:
		m.Down()
	case keymap.ActionTunnelPageUp:
		m.PageUp()
	case keymap.ActionTunnelPageDown:
		m.PageDown()
	case keymap.ActionTunnelHome:
		m.Home()
	case keymap.ActionTunnelEnd:
		m.End()
	case keymap.ActionTunnelScope:
		m.ToggleScope()
	case keymap.ActionTunnelRefresh:
		m.loading = true

		return m, m.load()
	case keymap.ActionTunnelLogs:
		return m, m.OpenLogs()
	case keymap.ActionTunnelStop:
		return m, m.Stop()
	case keymap.ActionTunnelRestart:
		return m, m.Restart()
	case keymap.ActionTunnelStart:
		return m.startSelected()
	}

	return m, nil
}

// startSelected restarts a stopped tunnel with its declared spec.
func (m Model) startSelected() (Model, tea.Cmd) {
	tunnel, ok := m.Selected()
	if !ok {
		return m, nil
	}

	if tunnel.Running() {
		m.notice = "el túnel ya está activo"

		return m, nil
	}

	return m, m.Start(tunnel.Host, tunnel.Spec)
}

// stopConfirmed ends the master of id.
func (m Model) stopConfirmed(id string) (Model, tea.Cmd) {
	if m.tunnels == nil {
		m.actionErr = errors.New("panel de túneles sin servicio")

		return m, nil
	}

	m.writing = true
	m.op = "stop"
	m.actionErr = nil
	seq := m.nextActionSeq()

	service := m.tunnels

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), domain.TunnelStopTimeout)
		defer cancel()

		return actionMsg{seq: seq, name: id, op: opStop, err: service.Stop(ctx, id)}
	}
}

// stopAllConfirmed stops every tunnel; it is used when the user quits.
func (m Model) stopAllConfirmed() (Model, tea.Cmd) {
	if m.tunnels == nil {
		return m, nil
	}

	m.writing = true
	m.op = "stop"
	service := m.tunnels
	seq := m.nextActionSeq()

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), domain.TunnelStopTimeout*4)
		defer cancel()

		return actionMsg{seq: seq, name: "todos", op: opStop, err: service.StopAll(ctx)}
	}
}

// handleClick selects the row under the pointer.
func (m Model) handleClick(msg tea.MouseClickMsg) (Model, tea.Cmd) {
	if m.logsOpen || m.confirm != confirmNone {
		return m, nil
	}

	point := image.Pt(msg.X, msg.Y)
	for index, rect := range m.rowRects {
		if !point.In(rect) {
			continue
		}

		m.cursor = m.offset + index
		m.scrollToCursor()

		return m, nil
	}

	return m, nil
}

// load asks the service for the current tunnels. It is a pointer method on
// purpose: the sequence counter lives on the model, and a value receiver would
// increment a copy and drop every result as stale.
func (m *Model) load() tea.Cmd {
	if m.tunnels == nil {
		return nil
	}

	seq := m.nextTunnelsSeq()
	service := m.tunnels

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout*2+time.Second)
		defer cancel()

		tunnels, err := service.List(ctx)

		return tunnelsMsg{seq: seq, tunnels: tunnels, err: err}
	}
}

// tick re-arms the ticker while the tab stays visible. A zero interval disables it.
func (m *Model) tick() tea.Cmd {
	if m.interval <= 0 {
		return nil
	}

	gen := m.tickGen

	return tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg{gen: gen} })
}

// visible returns the tunnels that pass the active scope.
func (m Model) visible() []domain.Tunnel {
	if m.scopeLabel == scopeAll || m.scopeLabel == "" {
		return m.all
	}

	rows := make([]domain.Tunnel, 0, len(m.all))
	for _, tunnel := range m.all {
		if tunnel.Host == m.scopeLabel {
			rows = append(rows, tunnel)
		}
	}

	return rows
}

// bodyHeight is the number of rows the table can show.
func (m Model) bodyHeight() int {
	return max(m.height-headerRows-footerRows, 1)
}

// logViewportHeight leaves one row for the logs title and one for its hints.
func (m Model) logViewportHeight() int {
	return max(m.height-2, 1)
}

// clamp keeps the cursor and the offset inside the visible rows.
func (m *Model) clamp() {
	rows := len(m.visible())
	if rows == 0 {
		m.cursor, m.offset = 0, 0
		m.markRows()

		return
	}

	m.cursor = common.Clamp(m.cursor, 0, rows-1)
	m.offset = common.Clamp(m.offset, 0, max(rows-m.bodyHeight(), 0))
	m.scrollToCursor()
	m.markRows()
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

// markRows stores the rectangle of every visible row.
func (m *Model) markRows() {
	m.rowRects = m.rowRects[:0]

	count := min(m.bodyHeight(), len(m.visible()))
	if count <= 0 || m.width <= 0 {
		return
	}

	top := m.bounds.Min.Y + headerRows
	for row := 0; row < count; row++ {
		m.rowRects = append(m.rowRects, image.Rect(m.bounds.Min.X, top+row, m.bounds.Max.X, top+row+1))
	}
}

func (m *Model) moveBy(delta int) {
	m.moveTo(m.cursor + delta)
}

func (m *Model) moveTo(index int) {
	if len(m.visible()) == 0 {
		return
	}

	m.cursor = common.Clamp(index, 0, len(m.visible())-1)
	m.scrollToCursor()
}

func (m *Model) nextTunnelsSeq() int {
	m.tunnelsSeq++

	return m.tunnelsSeq
}

func (m *Model) nextLogsSeq() int {
	m.logsSeq++

	return m.logsSeq
}

func (m *Model) nextActionSeq() int {
	m.actionSeq++

	return m.actionSeq
}

// View composes the header, the table or the logs, and the footer.
func (m Model) View() string {
	if m.logsOpen {
		return m.logsView()
	}

	lines := make([]string, 0, m.height)
	lines = append(lines, styles.TitlePlain.Render(common.Truncate(m.titleText(), m.width)))
	lines = append(lines, styles.Header.Render(common.Truncate(columnHeader(), m.width)))

	switch {
	case m.confirm != confirmNone:
		lines = append(lines, m.confirmLines()...)
	default:
		if state, ok := m.stateLine(); ok {
			lines = append(lines, state)
		} else {
			lines = append(lines, m.bodyRows()...)
		}
		lines = append(lines, styles.MetaPlain.Render(common.Truncate(m.footerText(), m.width)))
	}

	return strings.Join(lines, "\n")
}

// titleText is the header: identity, scope, state and the selection.
func (m Model) titleText() string {
	parts := []string{"túneles", m.scopeLabel}
	if state := m.stateLabel(); state != "" {
		parts = append(parts, state)
	}
	parts = append(parts, countLabel(len(m.visible())))

	if tunnel, ok := m.Selected(); ok {
		parts = append(parts, tunnel.Host)
	}

	return strings.Join(parts, columnSeparator)
}

// stateLabel reports the state of the panel in the header.
func (m Model) stateLabel() string {
	switch {
	case m.writing:
		if m.op == "" {
			return "actuando…"
		}

		return "actuando: " + m.op
	case m.loading && len(m.all) == 0:
		return "cargando…"
	default:
		return ""
	}
}

// stateLine replaces the rows while loading, on failure or when empty.
func (m Model) stateLine() (string, bool) {
	switch {
	case m.actionErr != nil:
		return styles.FailPlain.Render(common.Truncate("error al actuar: "+describeError(m.actionErr), m.width)), true
	case m.err != nil && len(m.all) == 0:
		return styles.FailPlain.Render(common.Truncate("error: "+describeError(m.err), m.width)), true
	case m.loading && len(m.all) == 0:
		return styles.MetaPlain.Render("cargando…"), true
	case len(m.all) == 0:
		return styles.MetaPlain.Render(common.Pad(m.emptyText(), m.width)), true
	}

	return "", false
}

// emptyText explains an empty table.
func (m Model) emptyText() string {
	return "sin túneles · t en Hosts para levantar uno"
}

// bodyRows renders the visible window of the table.
func (m Model) bodyRows() []string {
	rows := m.visible()

	height := m.bodyHeight()
	start := m.offset
	end := min(start+height, len(rows))

	window := make([]string, 0, height)
	for i := start; i < end; i++ {
		line := common.Truncate(tunnelRow(rows[i]), m.width)
		if i == m.cursor {
			line = styles.CursorPlain.Render(line)
		}

		window = append(window, line)
	}

	return common.PadLines(window, height)
}

// footerText lists the keys of the panel and the notice of the last action.
func (m Model) footerText() string {
	parts := make([]string, 0, 10)
	if m.notice != "" {
		parts = append(parts, m.notice)
	}
	if m.err != nil && len(m.all) > 0 {
		parts = append(parts, "error: "+describeError(m.err))
	}

	parts = append(parts,
		m.keys.Label(keymap.ActionTunnelUp)+"/"+m.keys.Label(keymap.ActionTunnelDown)+": mover",
		m.keys.Label(keymap.ActionTunnelStart)+": start",
		m.keys.Label(keymap.ActionTunnelStop)+": stop",
		m.keys.Label(keymap.ActionTunnelRestart)+": restart",
		m.keys.Label(keymap.ActionTunnelLogs)+": logs",
		m.keys.Label(keymap.ActionTunnelScope)+": alcance",
		m.keys.Label(keymap.ActionTunnelRefresh)+": refrescar",
	)

	return strings.Join(parts, columnSeparator)
}

// confirmLines renders the confirmation over the body of the panel.
func (m Model) confirmLines() []string {
	lines := make([]string, 0, m.bodyHeight()+footerRows)
	lines = append(lines, styles.TitlePlain.Render(common.Truncate("confirmar · "+m.confirm.question(), m.width)))

	if m.writing {
		lines = append(lines, styles.MetaPlain.Render("actuando…"))
	}

	lines = append(lines, styles.MetaPlain.Render(common.Truncate(
		m.keys.Label(keymap.ActionTunnelConfirmYes)+": confirmar · "+
			m.keys.Label(keymap.ActionTunnelConfirmNo)+": cancelar", m.width)))

	return common.PadLines(lines, m.bodyHeight()+footerRows)
}

// question is the text of the pending confirmation.
func (k confirmKind) question() string {
	switch k {
	case confirmStop:
		return "¿detener el túnel?"
	case confirmStopAll:
		return "¿detener todos los túneles al salir?"
	default:
		return ""
	}
}

// logsView renders the logs title, the scrollable viewport and its hints.
func (m Model) logsView() string {
	footer := m.keys.Label(keymap.ActionLogsClose) + ": cerrar · " +
		m.keys.Label(keymap.ActionLogsReload) + ": recargar · ↑/↓, rueda: desplazar"

	switch {
	case m.logsErr != nil:
		footer = "error: " + describeError(m.logsErr)
	case len(m.logLines) == 0:
		footer = "sin salida del master · " + m.keys.Label(keymap.ActionLogsClose) + ": cerrar"
	}

	return strings.Join([]string{
		styles.TitlePlain.Render(common.Truncate(m.logsTitle(), m.width)),
		m.viewport.View(),
		styles.MetaPlain.Render(common.Truncate(footer, m.width)),
	}, "\n")
}

// logsTitle names the tunnel of the open logs.
func (m Model) logsTitle() string {
	parts := []string{"logs", m.logsName, lineLabel(len(m.logLines))}
	if m.logs.Truncated {
		parts = append(parts, "truncado")
	}

	return strings.Join(parts, columnSeparator)
}

// tunnelRow renders one table row with fixed columns.
func tunnelRow(tunnel domain.Tunnel) string {
	spec := tunnel.Spec.Normalize()

	bind := formatHostPort(spec.Bind.Addr, tunnel.EffectivePort())
	target := "-"
	if spec.Direction != domain.ForwardDynamic {
		target = formatHostPort(spec.Target.Host, spec.Target.Port)
	}

	return strings.Join([]string{
		common.PadRight(common.Truncate(tunnel.Host, hostColumn), hostColumn),
		common.PadRight(spec.Direction.Flag(), directionColumn),
		common.PadRight(common.Truncate(bind, bindColumn), bindColumn),
		common.PadRight(common.Truncate(target, targetColumn), targetColumn),
		common.PadRight(common.Truncate(stateLabel(tunnel), stateColumn), stateColumn),
		tunnel.Spec.Label(),
	}, columnGap)
}

// columnHeader names the columns of the table.
func columnHeader() string {
	return strings.Join([]string{
		common.PadRight("host", hostColumn),
		common.PadRight("tipo", directionColumn),
		common.PadRight("escucha", bindColumn),
		common.PadRight("destino", targetColumn),
		common.PadRight("estado", stateColumn),
		"nombre",
	}, columnGap)
}

// stateLabel renders the state of one tunnel as text, never colour alone.
func stateLabel(tunnel domain.Tunnel) string {
	switch tunnel.State {
	case domain.TunnelActive:
		return "activo"
	case domain.TunnelStarting:
		return "iniciando"
	case domain.TunnelFailed:
		return "fallido"
	default:
		return "detenido"
	}
}

// formatHostPort renders host:port, bracketing IPv6 literals.
func formatHostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}

	return fmt.Sprintf("%s:%d", host, port)
}

func countLabel(count int) string {
	switch count {
	case 0:
		return "sin túneles"
	case 1:
		return "1 túnel"
	default:
		return fmt.Sprintf("%d túneles", count)
	}
}

func lineLabel(count int) string {
	if count == 1 {
		return "1 línea"
	}

	return fmt.Sprintf("%d líneas", count)
}

// describeError turns an adapter failure into the message the panel shows.
func describeError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrSSHUnavailable):
		return "ssh no está disponible"
	case errors.Is(err, domain.ErrTunnelExists):
		return "el túnel ya está activo"
	case errors.Is(err, domain.ErrTunnelNotFound):
		return "túnel no encontrado"
	case errors.Is(err, domain.ErrTunnelFailed), errors.Is(err, domain.ErrAuthRequired):
		return err.Error()
	case errors.Is(err, domain.ErrPermissionDenied):
		return "permiso denegado"
	case errors.Is(err, context.DeadlineExceeded):
		return "tiempo agotado"
	case errors.Is(err, context.Canceled):
		return "cancelado"
	default:
		return common.Truncate(err.Error(), 160)
	}
}

// The actions and the notice each one leaves behind.
const (
	opStart   = "start"
	opStop    = "stop"
	opRestart = "restart"
)

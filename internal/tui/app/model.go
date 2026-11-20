// Package app holds the root Bubble Tea model of the TUI.
package app

import (
	"image"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/config"
	"github.com/MiguelFPP/tui-ssh/internal/ports"
	"github.com/MiguelFPP/tui-ssh/internal/tui/components/configview"
	"github.com/MiguelFPP/tui-ssh/internal/tui/components/hostdetail"
	"github.com/MiguelFPP/tui-ssh/internal/tui/components/hostlist"
	"github.com/MiguelFPP/tui-ssh/internal/tui/components/tunnelpanel"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
	"github.com/MiguelFPP/tui-ssh/internal/tui/styles"
)

const (
	defaultWidth  = 90
	defaultHeight = 24

	minWidth  = 40
	minHeight = 10

	headerRows = 2
	footerRows = 1

	minList      = 24
	listPercent  = 38
	minDetailBox = 24

	// listStep is the cells one keyboard resize moves the shared border.
	listStep = 2

	toastTTL = 3 * time.Second

	// splitThreshold is the width under which the Hosts tab falls back to one column.
	splitThreshold = 72
)

// Tab is one top-level section of the TUI.
type Tab uint8

const (
	HostsTab Tab = iota
	TunnelsTab
	ConfigTab

	tabCount = 3
)

// tabLabels are the rendered labels, shared by the tab row and its hit-testing.
var tabLabels = [tabCount]string{HostsTab: " Hosts ", TunnelsTab: " Túneles ", ConfigTab: " Config "}

func (t Tab) String() string {
	if t >= tabCount {
		return strings.TrimSpace(tabLabels[HostsTab])
	}

	return strings.TrimSpace(tabLabels[t])
}

// Focus is the panel that receives the keyboard of the active tab.
type Focus uint8

const (
	FocusList Focus = iota
	FocusDetail
	FocusTunnels
	FocusConfig
)

// Deps are the collaborators injected by the composition root. Keys carries the
// user keymap; an empty registry falls back to the built-in defaults.
type Deps struct {
	Config  config.Config
	SSH     ports.SSHConfigService
	Tags    ports.TagService
	Resolve ports.ResolverService
	Session ports.SessionService
	Tunnels ports.TunnelService
	Editor  ports.ConfigEditorService
	Keys    keymap.Registry

	// SaveLayout persists the list layout; nil means it is not persisted.
	SaveLayout func(width int, collapsed bool) error
	// SaveDetailLayout persists the one/two column preference; nil means it is not persisted.
	SaveDetailLayout func(layout string) error
	// Expire schedules the toast expiry. It defaults to tea.Tick; the tests inject a
	// no-op so no command waits on a timer.
	Expire func(d time.Duration, msg func(time.Time) tea.Msg) tea.Cmd
}

// Model is the root model: it owns the tabs, the layout, the focus and the toasts.
type Model struct {
	cfg     config.Config
	ssh     ports.SSHConfigService
	tags    ports.TagService
	resolve ports.ResolverService
	session ports.SessionService
	editor  ports.ConfigEditorService
	keys    keymap.Registry

	list    hostlist.Model
	detail  hostdetail.Model
	tunnels tunnelpanel.Model
	config  configview.Model

	tagPrompt     promptState
	forwardPrompt forwardState
	confirm       confirmState

	tab   Tab
	focus Focus

	width  int
	height int

	tabRects     [tabCount]image.Rectangle
	listRect     image.Rectangle
	detailRect   image.Rectangle
	panelRect    image.Rectangle
	splitterRect image.Rectangle
	collapseRect image.Rectangle

	// Layout of the Hosts tab: the explicit list width, the handle state and the button.
	listWidth     int
	listCollapsed bool
	detailSingle  bool
	dragging      bool
	dragDirty     bool
	saveLayout    func(int, bool) error
	saveDetail    func(string) error

	help     bool
	settings bool
	spinning bool
	spinner  spinner.Model

	// expire schedules the toast expiry; the default is tea.Tick.
	expire func(time.Duration, func(time.Time) tea.Msg) tea.Cmd

	toast    string
	toastSeq int
	lastErr  error

	// resumed is set after an external program handed the terminal back.
	resumed bool
}

// toastExpiredMsg clears the toast unless a newer one has taken its place.
type toastExpiredMsg struct {
	seq int
}

// layoutSavedMsg carries the result of persisting the layout.
type layoutSavedMsg struct {
	err error
}

// New builds the root model for the given dependencies.
func New(deps Deps) Model {
	keys := deps.Keys
	if len(keys.All()) == 0 {
		keys = keymap.Default()
	}

	expire := deps.Expire
	if expire == nil {
		expire = tea.Tick
	}

	configSingle := strings.TrimSpace(deps.Config.DetailLayout) == "single"

	m := Model{
		cfg:           deps.Config,
		ssh:           deps.SSH,
		tags:          deps.Tags,
		resolve:       deps.Resolve,
		session:       deps.Session,
		editor:        deps.Editor,
		keys:          keys,
		list:          hostlist.New(keys),
		detail:        hostdetail.New(keys),
		tunnels:       tunnelpanel.New(deps.Tunnels, keys),
		config:        configview.New(deps.SSH, keys),
		tab:           HostsTab,
		width:         defaultWidth,
		height:        defaultHeight,
		listWidth:     deps.Config.ListWidth,
		listCollapsed: deps.Config.ListCollapsed,
		detailSingle:  configSingle,
		saveLayout:    deps.SaveLayout,
		saveDetail:    deps.SaveDetailLayout,
		expire:        expire,
		spinner:       spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styles.Spinner)),
		spinning:      true,
	}
	m.focus = m.listFocus()
	m.resize()

	return m
}

// Init loads the hosts and starts the spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadHosts(), m.spinner.Tick)
}

// Update routes one message: keys and mouse to the focused panel, the rest by fan-out.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 4)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
	case tea.KeyPressMsg:
		cmd, stop := m.handleKey(msg)
		if stop {
			return m, cmd
		}

		cmds = append(cmds, cmd)
	case tea.MouseClickMsg:
		cmds = append(cmds, m.handleClick(msg))
	case tea.MouseMotionMsg:
		cmds = append(cmds, m.handleMotion(msg))
	case tea.MouseReleaseMsg:
		cmds = append(cmds, m.handleRelease())
	case tea.MouseWheelMsg:
		cmds = append(cmds, m.handleWheel(msg))
	case spinner.TickMsg:
		var tick tea.Cmd
		m.spinner, tick = m.spinner.Update(msg)
		m.spinning = false
		if m.loading() {
			m.spinning = true
			cmds = append(cmds, tick)
		}
	case toastExpiredMsg:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
	case layoutSavedMsg:
		if msg.err != nil {
			cmds = append(cmds, m.notify(msg.err.Error()))
		}
	case hostsLoadedMsg:
		cmds = append(cmds, m.applyHosts(msg))
	case resolvedMsg:
		cmds = append(cmds, m.applyResolved(msg))
	case sessionDoneMsg:
		cmds = append(cmds, m.applySessionDone(msg))
	case tagsSavedMsg:
		cmds = append(cmds, m.applyTagsSaved(msg))
	case forwardsSavedMsg:
		cmds = append(cmds, m.applyForwardsSaved(msg))
	case execMsg:
		// The terminal belongs to the program until it exits; the TUI pauses here.
		cmds = append(cmds, execProcess(msg))
	case quitMsg:
		return m, tea.Quit
	case externalDoneMsg:
		cmds = append(cmds, m.applyExternalDone(msg))
	default:
		cmds = append(cmds, m.fanOut(msg))
	}

	cmds = append(cmds, m.afterUpdate())

	return m, tea.Batch(cmds...)
}

// View renders the layout; the alternate screen and the mouse are set here.
func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	return view
}

// Tab returns the active tab.
func (m Model) Tab() Tab {
	return m.tab
}

// Focus returns the panel that owns the keyboard.
func (m Model) Focus() Focus {
	return m.focus
}

// HelpVisible reports whether the help overlay is open.
func (m Model) HelpVisible() bool {
	return m.help
}

// SettingsVisible reports whether the read-only settings overlay is open.
func (m Model) SettingsVisible() bool {
	return m.settings
}

// List returns the host list, for the tests of the root model.
func (m Model) List() hostlist.Model {
	return m.list
}

// Tunnels returns the tunnel panel, for the tests of the root model.
func (m Model) Tunnels() tunnelpanel.Model {
	return m.tunnels
}

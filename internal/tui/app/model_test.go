package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/config"
	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/testutil/fakes"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

// harness bundles the root model with the fakes its commands reach.
type harness struct {
	model   Model
	ssh     *fakes.SSHConfig
	tags    *fakes.Tags
	resolve *fakes.Resolver
	session *fakes.Session
	tunnels *fakes.Tunnels
	editor  *fakes.ConfigEditor
}

func newHarness(t *testing.T, hosts ...domain.Host) *harness {
	t.Helper()

	h := &harness{
		ssh:     &fakes.SSHConfig{HostsValue: hosts},
		tags:    &fakes.Tags{},
		resolve: &fakes.Resolver{Value: domain.Resolved{Host: domain.ResolvedHost{HostName: "10.0.0.1", User: "deploy", Port: 22}}},
		session: &fakes.Session{},
		tunnels: &fakes.Tunnels{},
		editor:  &fakes.ConfigEditor{},
	}

	deps := Deps{
		Config: config.Config{
			SSHConfig: "/home/u/.ssh/config",
			SSHBinary: "ssh",
			DataDir:   t.TempDir(),
		},
		SSH:     h.ssh,
		Tags:    h.tags,
		Resolve: h.resolve,
		Session: h.session,
		Tunnels: h.tunnels,
		Editor:  h.editor,
		Keys:    keymap.Default(),
		// A no-op expiry keeps every toast visible without scheduling a timer.
		Expire: func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil },
	}

	h.model = New(deps)
	// The ticker is a timer: disabling it keeps a test from waiting on it.
	h.model.tunnels.DisableTicker()
	h.model, _ = update(t, h.model, tea.WindowSizeMsg{Width: 120, Height: 32})

	return h
}

// update applies one message and returns the resulting model, ignoring the command.
func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(msg)

	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", next)
	}

	return updated, cmd
}

// run applies a message and then processes the resulting command tree, so the
// async reads and writes of the model land in the same step.
func run(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	next, cmd := update(t, m, msg)

	return applyCmd(t, next, cmd)
}

// applyCmd processes a command tree in breadth, applying every data message and
// expanding batches. The harness injects a no-op expiry scheduler, so no command
// here blocks on a timer.
func applyCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()

	queue := []tea.Cmd{cmd}

	for steps := 0; len(queue) > 0 && steps < 64; steps++ {
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}

		msg := current()
		if msg == nil {
			continue
		}

		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				queue = append(queue, sub)
			}

			continue
		}

		next, followUp := update(t, m, msg)
		m = next

		if followUp != nil {
			queue = append(queue, followUp)
		}
	}

	return m
}

// key builds a printable key press, which is what update routes.
func key(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func hosts() []domain.Host {
	return []domain.Host{
		{
			Alias:      "gd-vps",
			Tags:       []string{"prod", "vps"},
			SourceFile: "/home/u/.ssh/config",
			SourceLine: 7,
			Declared:   []domain.Directive{{Key: "hostname", Value: "207.244.252.156"}},
		},
		{Alias: "github.com", SourceFile: "/home/u/.ssh/config", SourceLine: 1},
		{Alias: "gd-vps-root", SourceFile: "/home/u/.ssh/config", SourceLine: 14},
	}
}

func TestInitAndLoad(t *testing.T) {
	h := newHarness(t, hosts()...)

	next, _ := update(t, h.model, tea.WindowSizeMsg{Width: 120, Height: 32})
	if next.Tab() != HostsTab {
		t.Errorf("Tab = %v, want Hosts", next.Tab())
	}

	h.model = applyCmd(t, h.model, h.model.Init())

	if h.ssh.HostsCalls == 0 {
		t.Fatal("Hosts was never called")
	}
	if got := h.model.List().Total(); got != 3 {
		t.Errorf("hosts = %d, want 3", got)
	}
	if h.resolve.Calls == 0 {
		t.Error("the selected host was never resolved")
	}
}

func TestUpdateLoadsHostsOnInit(t *testing.T) {
	h := newHarness(t, hosts()...)

	cmd := h.model.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}

	h.model = applyCmd(t, h.model, cmd)

	if h.ssh.HostsCalls == 0 {
		t.Fatal("Init did not read the hosts")
	}
	if h.model.List().Total() != 3 {
		t.Errorf("hosts = %d, want 3", h.model.List().Total())
	}
}

func TestTabSwitching(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	h.model = run(t, h.model, key('2'))
	if h.model.Tab() != TunnelsTab {
		t.Fatalf("Tab after 2 = %v, want Tunnels", h.model.Tab())
	}
	if h.model.Focus() != FocusTunnels {
		t.Errorf("Focus = %v, want the tunnel panel", h.model.Focus())
	}

	h.model = run(t, h.model, key('3'))
	if h.model.Tab() != ConfigTab {
		t.Fatalf("Tab after 3 = %v, want Config", h.model.Tab())
	}

	h.model = run(t, h.model, key('1'))
	if h.model.Tab() != HostsTab {
		t.Errorf("Tab after 1 = %v, want Hosts", h.model.Tab())
	}
}

func TestHelpAndSettingsOverlays(t *testing.T) {
	h := newHarness(t, hosts()...)

	h.model = run(t, h.model, key('?'))
	if !h.model.HelpVisible() {
		t.Fatal("? should open the help")
	}
	view := h.model.View().Content
	if !strings.Contains(view, "atajos") {
		t.Errorf("help view does not render the keymap:\n%s", view)
	}

	h.model = run(t, h.model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if h.model.HelpVisible() {
		t.Error("esc should close the help")
	}

	h.model = run(t, h.model, key(','))
	if !h.model.SettingsVisible() {
		t.Fatal(", should open the settings")
	}
	if view := h.model.View().Content; !strings.Contains(view, "configuración") {
		t.Errorf("settings view = %q", view)
	}
}

func TestFilterFlow(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	// "/" focuses the filter and the text narrows the list.
	h.model = run(t, h.model, key('/'))
	if !h.model.List().Filtering() {
		t.Fatal("/ should focus the filter input")
	}

	for _, r := range "gd" {
		h.model = run(t, h.model, key(r))
	}

	if got := h.model.List().Count(); got != 2 {
		t.Errorf("count after filtering = %d, want the two gd hosts", got)
	}

	// enter ends the filter but keeps it applied.
	h.model = run(t, h.model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if h.model.List().Filtering() {
		t.Error("enter should end the filter input")
	}
	if got := h.model.List().Filter(); got != "gd" {
		t.Errorf("filter = %q, want it kept after enter", got)
	}

	// esc on the list clears the filter and shows every host again.
	h.model = run(t, h.model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := h.model.List().Filter(); got != "" {
		t.Errorf("filter = %q, want esc to clear it", got)
	}
	if got := h.model.List().Count(); got != 3 {
		t.Errorf("count = %d, want every host back", got)
	}
}

func TestTagPromptPersistsTags(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	// T opens the prompt on the selected host.
	h.model = run(t, h.model, key('T'))
	if !h.model.modalVisible() {
		t.Fatal("T should open the tag prompt")
	}

	// Replace the content with a new list and save.
	h.model.tagPrompt.prompt.SetValue("prod, db")
	h.model = run(t, h.model, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	h.model = applyCmd(t, h.model, nil)

	if h.tags.TagsWrites != 1 {
		t.Fatalf("TagsWrites = %d, want 1", h.tags.TagsWrites)
	}
	if got := h.tags.Index["gd-vps"]; len(got) != 2 || got[0] != "prod" || got[1] != "db" {
		t.Errorf("stored tags = %v, want [prod db]", got)
	}
	if h.model.modalVisible() {
		t.Error("the prompt should close after saving")
	}
}

func TestForwardPromptSavesAndStarts(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	h.model = run(t, h.model, key('n'))
	if !h.model.modalVisible() {
		t.Fatal("n should open the forward prompt")
	}

	h.model.forwardPrompt.prompt.SetValue("pg=L 5432:db.internal:5432")
	h.model = run(t, h.model, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	h.model = applyCmd(t, h.model, nil)

	if h.tags.ForwardsWrites != 1 {
		t.Fatalf("ForwardsWrites = %d, want 1", h.tags.ForwardsWrites)
	}
	specs := h.tags.Specs["gd-vps"]
	if len(specs) != 1 || specs[0].Name != "pg" || specs[0].Bind.Port != 5432 {
		t.Errorf("stored specs = %+v", specs)
	}
	if h.tunnels.StartCalls != 1 {
		t.Errorf("StartCalls = %d, want the declared forward to start", h.tunnels.StartCalls)
	}
}

func TestForwardPromptRejectsInvalidSpec(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	h.model = run(t, h.model, key('n'))
	h.model.forwardPrompt.prompt.SetValue("L not-a-spec")
	h.model = run(t, h.model, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	if !h.model.forwardPrompt.Visible() {
		t.Error("an invalid spec must keep the prompt open")
	}
	if h.tags.ForwardsWrites != 0 {
		t.Errorf("ForwardsWrites = %d, want 0", h.tags.ForwardsWrites)
	}
	if h.model.forwardPrompt.prompt.Err == "" {
		t.Error("the prompt should show the validation error")
	}
}

func TestConnectResolvesTheCommand(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	_, cmd := update(t, h.model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}

	msg := cmd()
	if _, ok := msg.(execMsg); !ok {
		t.Fatalf("enter produced %T, want execMsg", msg)
	}
	if h.session.Calls != 1 {
		t.Errorf("Session.Calls = %d, want 1", h.session.Calls)
	}

	exec, _ := msg.(execMsg)
	if exec.command.Binary == "" || len(exec.command.Args) != 1 || exec.command.Args[0] != "gd-vps" {
		t.Errorf("command = %+v, want ssh gd-vps", exec.command)
	}
}

func TestConnectErrorIsReported(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())
	h.session.Err = errors.New("ssh no está disponible")

	h.model = run(t, h.model, tea.KeyPressMsg{Code: tea.KeyEnter})

	if h.model.toast == "" {
		t.Error("a failed connect should raise a toast")
	}
	if !strings.Contains(h.model.toast, "ssh") {
		t.Errorf("toast = %q, want the ssh hint", h.model.toast)
	}
}

func TestCopyAndExternal(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	h.model = run(t, h.model, key('y'))
	if !strings.Contains(h.model.toast, "gd-vps") {
		t.Errorf("toast = %q, want the ssh command", h.model.toast)
	}

	_, cmd := update(t, h.model, key('E'))
	if cmd == nil {
		t.Fatal("E returned no command")
	}
	msg := cmd()
	if _, ok := msg.(execMsg); !ok {
		t.Fatalf("E produced %T, want execMsg", msg)
	}
	if h.editor.Calls != 1 {
		t.Errorf("Editor.Calls = %d, want 1", h.editor.Calls)
	}
}

func TestQuitWithoutTunnels(t *testing.T) {
	h := newHarness(t, hosts()...)

	_, cmd := update(t, h.model, key('q'))
	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("q produced %T, want tea.QuitMsg", cmd())
	}
}

func TestQuitWithTunnelsAsksFirst(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	// Declare the running tunnel through a start, then open the Tunnels tab.
	h.model = applyCmd(t, h.model, h.model.tunnels.Start("gd-vps", domain.ForwardSpec{
		Direction: domain.ForwardLocal,
		Bind:      domain.BindAddr{Port: 8080},
		Target:    domain.TargetAddr{Host: "localhost", Port: 80},
	}))
	h.model = run(t, h.model, key('2'))
	h.model = applyCmd(t, h.model, h.model.tunnels.Refresh())

	if !h.model.Tunnels().AnyRunning() {
		t.Skip("the fake did not report a running tunnel")
	}

	// q asks instead of exiting.
	next, cmd := update(t, h.model, key('q'))
	h.model = next

	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q exited without asking")
		}
	}
	if !h.model.confirm.Visible() {
		t.Fatal("q should open the quit confirmation")
	}

	// Declining keeps the tunnels and exits.
	_, cmd = update(t, h.model, key('n'))
	if cmd == nil {
		t.Fatal("declining returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("declining produced %T, want tea.QuitMsg", cmd())
	}
}

func TestQuitConfirmationStopsTunnels(t *testing.T) {
	h := newHarness(t, hosts()...)

	h.model.confirm.OpenForQuit(1)

	next, cmd := update(t, h.model, key('y'))
	h.model = next

	if cmd == nil {
		t.Fatal("confirming returned no command")
	}
	msg := cmd()
	if _, ok := msg.(quitMsg); !ok {
		t.Fatalf("confirming produced %T, want quitMsg", msg)
	}
	if h.tunnels.StopAllCalls != 1 {
		t.Errorf("StopAllCalls = %d, want 1", h.tunnels.StopAllCalls)
	}
}

func TestLayoutPersistence(t *testing.T) {
	saved := 0
	var lastWidth int

	h := newHarness(t, hosts()...)
	h.model.saveLayout = func(width int, collapsed bool) error {
		saved++
		lastWidth = width

		return nil
	}

	h.model = run(t, h.model, tea.KeyPressMsg{Code: tea.KeyF2})
	if !h.model.listCollapsed {
		t.Fatal("F2 should collapse the list")
	}
	if saved != 1 || !h.model.listCollapsed {
		t.Errorf("saved = %d after collapsing", saved)
	}

	h.model = run(t, h.model, tea.KeyPressMsg{Code: tea.KeyF2})
	if h.model.listCollapsed {
		t.Error("F2 again should show the list")
	}

	// shift+right grows the list and persists the new width.
	h.model = run(t, h.model, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
	if saved != 3 {
		t.Errorf("saved = %d, want the resize persisted too", saved)
	}
	if lastWidth == 0 {
		t.Errorf("persisted width = %d, want the explicit width", lastWidth)
	}
}

func TestDetailLayoutToggle(t *testing.T) {
	saved := ""

	h := newHarness(t, hosts()...)
	h.model.saveDetail = func(layout string) error {
		saved = layout

		return nil
	}

	h.model = run(t, h.model, key('L'))
	if !h.model.detailSingle {
		t.Fatal("L should switch to the single column layout")
	}
	if saved != "single" {
		t.Errorf("saved = %q, want single", saved)
	}

	h.model = run(t, h.model, key('L'))
	if saved != "split" {
		t.Errorf("saved = %q, want split", saved)
	}
}

func TestViewRendersTheLayout(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model = applyCmd(t, h.model, h.model.Init())

	view := h.model.View().Content
	for _, want := range []string{"tui-ssh", "Hosts", "Túneles", "Config", "gd-vps", "config:"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestNarrowTerminalFallsBackToSingleColumn(t *testing.T) {
	h := newHarness(t, hosts()...)
	h.model, _ = update(t, h.model, tea.WindowSizeMsg{Width: 50, Height: 20})

	if h.model.canSplit() {
		t.Error("a narrow terminal must not split the list and the detail")
	}
	if got := h.model.listWidthNow(50); got != 0 {
		t.Errorf("listWidthNow = %d, want 0 under the split threshold", got)
	}
}

func TestEmptyConfigIsReported(t *testing.T) {
	h := newHarness(t)
	h.ssh.HostsErr = domain.ErrConfigMissing

	h.model = applyCmd(t, h.model, h.model.Init())

	if h.model.toast == "" {
		t.Fatal("a missing config should raise a toast")
	}
	if !strings.Contains(h.model.toast, "ssh") {
		t.Errorf("toast = %q, want the actionable hint", h.model.toast)
	}
}

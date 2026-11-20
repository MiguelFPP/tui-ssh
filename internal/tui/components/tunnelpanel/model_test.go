package tunnelpanel

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/testutil/fakes"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

func spec() domain.ForwardSpec {
	return domain.ForwardSpec{
		Name:      "pg",
		Direction: domain.ForwardLocal,
		Bind:      domain.BindAddr{Addr: "127.0.0.1", Port: 5432},
		Target:    domain.TargetAddr{Host: "db.internal", Port: 5432},
	}
}

func tunnel(id, host string, state domain.TunnelState) domain.Tunnel {
	return domain.Tunnel{
		ID:        id,
		Host:      host,
		Spec:      spec(),
		State:     state,
		StartedAt: fakes.Epoch,
	}
}

func newPanel(tunnels *fakes.Tunnels) Model {
	m := New(tunnels, keymap.Default())
	m.SetBounds(0, 0, 100, 20)

	return m
}

// The tests never start the real ticker: tea.Tick blocks until its deadline, so a
// test that ran it would sleep for the interval. `load` provokes the same first
// read directly and applies its message, which is what the ticker would deliver.

// load marks the panel active and applies the first read, skipping the ticker.
func load(t *testing.T, m Model) Model {
	t.Helper()

	m.active = true

	return apply(t, m, m.Refresh())
}

// apply runs one data command and folds its message into the model. It is used for
// the reads the tests trigger by hand; the ticker is never run.
func apply(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()

	if cmd == nil {
		return m
	}

	msg := cmd()
	if msg == nil {
		return m
	}

	next, _ := m.Update(msg)

	return next
}

func TestInactiveTickDoesNotPoll(t *testing.T) {
	service := &fakes.Tunnels{}
	m := newPanel(service)

	// The panel starts inactive: the ticker must not exist and no read may happen.
	if cmd := m.SetActive(false); cmd != nil {
		t.Errorf("SetActive(false) on an inactive panel = %v, want nil", cmd)
	}
	if service.ListCalls != 0 {
		t.Errorf("ListCalls = %d, want 0 while the tab is hidden", service.ListCalls)
	}

	// Even a tick message delivered while hidden must not reach the service.
	next, cmd := m.Update(tickMsg{gen: 0})
	if cmd != nil {
		t.Error("a tick on a hidden panel returned a command")
	}
	if next.Active() {
		t.Error("the panel became active from a tick")
	}
	if service.ListCalls != 0 {
		t.Errorf("ListCalls = %d after a hidden tick, want 0", service.ListCalls)
	}
}

func BenchmarkInactiveTickNoPoll(b *testing.B) {
	service := &fakes.Tunnels{}
	m := New(service, keymap.Default())
	m.SetBounds(0, 0, 100, 20)
	m.SetActive(false)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		next, cmd := m.Update(tickMsg{gen: 0})
		if cmd != nil {
			b.Fatal("a hidden tick returned a command")
		}
		m = next
	}
}

func TestSetActiveStartsPolling(t *testing.T) {
	service := &fakes.Tunnels{Value: []domain.Tunnel{tunnel("web-L1-2", "web", domain.TunnelActive)}}
	m := newPanel(service)

	cmd := m.SetActive(true)
	if cmd == nil {
		t.Fatal("SetActive(true) returned no command")
	}
	if !m.Active() {
		t.Fatal("the panel should be active right away")
	}
	if !m.loading {
		t.Fatal("activating the panel should start a read")
	}

	// The returned batch is [load, tick]; the tick is inspected, never run.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("SetActive(true) should return a batch")
	}
	if len(batch) != 2 {
		t.Fatalf("batch = %d commands, want the load and the tick", len(batch))
	}

	// Running the load command directly proves the read reaches the service.
	m = apply(t, m, batch[0])

	if service.ListCalls == 0 {
		t.Error("ListCalls = 0, want the first read")
	}
	if len(m.all) != 1 {
		t.Errorf("tunnels = %d, want the one loaded", len(m.all))
	}
}

func TestNavigationAndSelection(t *testing.T) {
	service := &fakes.Tunnels{Value: []domain.Tunnel{
		tunnel("a-L1-2", "a", domain.TunnelActive),
		tunnel("b-L1-2", "b", domain.TunnelStopped),
		tunnel("c-L1-2", "c", domain.TunnelActive),
	}}
	m := newPanel(service)
	m = load(t, m)

	if sel, ok := m.Selected(); !ok || sel.ID != "a-L1-2" {
		t.Fatalf("Selected = %q, want the first tunnel", sel.ID)
	}

	m.Down()
	if sel, _ := m.Selected(); sel.ID != "b-L1-2" {
		t.Errorf("after Down = %q", sel.ID)
	}

	m.End()
	if sel, _ := m.Selected(); sel.ID != "c-L1-2" {
		t.Errorf("after End = %q", sel.ID)
	}

	m.Home()
	if sel, _ := m.Selected(); sel.ID != "a-L1-2" {
		t.Errorf("after Home = %q", sel.ID)
	}
}

func TestStopRequiresConfirmation(t *testing.T) {
	service := &fakes.Tunnels{Value: []domain.Tunnel{tunnel("web-L1-2", "web", domain.TunnelActive)}}
	m := newPanel(service)
	m = load(t, m)

	// The stop key only opens the confirmation.
	next, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = next

	if !m.CapturesInput() {
		t.Fatal("CapturesInput should be true while the confirmation is open")
	}
	if service.StopCalls != 0 {
		t.Errorf("StopCalls = %d, want 0 before confirming", service.StopCalls)
	}

	// Cancelling leaves the tunnel alone.
	next, _ = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = next
	if m.CapturesInput() {
		t.Error("the confirmation should be closed after cancelling")
	}
	if service.StopCalls != 0 {
		t.Errorf("StopCalls after cancel = %d, want 0", service.StopCalls)
	}

	// Confirming stops it.
	next, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = next
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = apply(t, next, cmd)

	if service.StopCalls != 1 {
		t.Errorf("StopCalls = %d, want 1 after confirming", service.StopCalls)
	}
}

func TestLogsOpenAndClose(t *testing.T) {
	service := &fakes.Tunnels{
		Value: []domain.Tunnel{tunnel("web-L1-2", "web", domain.TunnelActive)},
		Log:   domain.TunnelLog{Path: "/tmp/x.log", Lines: []string{"line one", "line two"}},
	}
	m := newPanel(service)
	m = load(t, m)

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = apply(t, next, cmd)

	if !m.CapturesInput() {
		t.Fatal("the logs should own the keyboard")
	}
	if len(m.LogLines()) != 2 {
		t.Errorf("LogLines = %v, want the two lines", m.LogLines())
	}
	if view := m.View(); !strings.Contains(view, "logs") {
		t.Errorf("logs view = %q", view)
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next
	if m.CapturesInput() {
		t.Error("esc should close the logs")
	}
}

func TestLogsErrorIsReported(t *testing.T) {
	service := &fakes.Tunnels{
		Value:   []domain.Tunnel{tunnel("web-L1-2", "web", domain.TunnelActive)},
		LogsErr: errors.New("permiso denegado"),
	}
	m := newPanel(service)
	m = load(t, m)

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = apply(t, next, cmd)

	if err := m.Err(); err == nil {
		t.Fatal("Err should report the failed read")
	}
}

func TestStartAndRestart(t *testing.T) {
	service := &fakes.Tunnels{}
	m := newPanel(service)

	cmd := m.Start("web", spec())
	m = apply(t, m, cmd)

	if service.StartCalls != 1 {
		t.Errorf("StartCalls = %d, want 1", service.StartCalls)
	}
	if len(service.StartedWith) != 1 || service.StartedWith[0] != "web" {
		t.Errorf("StartedWith = %v", service.StartedWith)
	}

	// Restarting stops and starts again.
	m = load(t, m)
	m.cursor = 0

	cmd = m.Restart()
	m = apply(t, m, cmd)

	if service.StopCalls != 1 {
		t.Errorf("StopCalls = %d, want 1 from the restart", service.StopCalls)
	}
	if service.StartCalls != 2 {
		t.Errorf("StartCalls = %d, want 2 after the restart", service.StartCalls)
	}
}

func TestStartErrorIsReported(t *testing.T) {
	service := &fakes.Tunnels{Err: errors.New("el host pide autenticación interactiva")}
	m := newPanel(service)

	m = apply(t, m, m.Start("web", spec()))

	if err := m.Err(); err == nil {
		t.Fatal("Err should report the failed start")
	}
	if strings.Contains(m.View(), "autenticación") == false {
		t.Errorf("View does not surface the error:\n%s", m.View())
	}
}

func TestScopeFilter(t *testing.T) {
	service := &fakes.Tunnels{Value: []domain.Tunnel{
		tunnel("a-L1-2", "web", domain.TunnelActive),
		tunnel("b-L1-2", "db", domain.TunnelActive),
	}}
	m := newPanel(service)
	m = load(t, m)

	m.SetHostFilter("web")
	if got := len(m.visible()); got != 1 {
		t.Fatalf("visible = %d, want the web tunnel only", got)
	}

	m.ToggleScope()
	if got := len(m.visible()); got != 2 {
		t.Errorf("visible after ToggleScope = %d, want both", got)
	}

	m.ToggleScope()
	if got := len(m.visible()); got != 1 {
		t.Errorf("visible after a second toggle = %d, want web only", got)
	}
}

func TestAnyRunning(t *testing.T) {
	service := &fakes.Tunnels{Value: []domain.Tunnel{
		tunnel("a-L1-2", "web", domain.TunnelActive),
		tunnel("b-L1-2", "db", domain.TunnelStopped),
	}}
	m := newPanel(service)
	m = load(t, m)

	if !m.AnyRunning() {
		t.Error("AnyRunning should be true")
	}
	if got := len(m.RunningTunnels()); got != 1 {
		t.Errorf("RunningTunnels = %d, want 1", got)
	}
}

func TestViewStates(t *testing.T) {
	loading := newPanel(&fakes.Tunnels{})
	if !strings.Contains(loading.View(), "sin túneles") {
		t.Errorf("empty view = %q", loading.View())
	}

	service := &fakes.Tunnels{Value: []domain.Tunnel{tunnel("web-L1-2", "web", domain.TunnelActive)}}
	m := newPanel(service)
	m = load(t, m)

	view := m.View()
	for _, want := range []string{"túneles", "web", "activo", "127.0.0.1:5432", "db.internal:5432"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestTickRearmsWhileActive(t *testing.T) {
	m := newPanel(&fakes.Tunnels{})
	m.SetInterval(time.Millisecond)
	m.SetActive(true)

	m.loading = false
	next, cmd := m.Update(tickMsg{gen: m.tickGen})

	if cmd == nil {
		t.Fatal("an active tick must re-arm the ticker")
	}
	if !next.loading {
		t.Error("an active tick should start a read")
	}
}

package configview

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/testutil/fakes"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

func newView(service *fakes.SSHConfig) Model {
	m := New(service, keymap.Default())
	m.SetBounds(80, 20)

	return m
}

// apply runs one data command and folds its message into the model.
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

func TestInitLoadsReportAndSource(t *testing.T) {
	service := &fakes.SSHConfig{
		ReportValue: domain.ConfigReport{
			Main:  "/home/u/.ssh/config",
			Hosts: 2,
			Files: []domain.ConfigFile{
				{Path: "/home/u/.ssh/config", Lines: 12},
				{Path: "/home/u/.ssh/conf.d/a.conf", Lines: 4, Included: true},
			},
			Warnings: []string{"include circular detectado"},
		},
		SourceValue: domain.ConfigSource{
			Path:       "/home/u/.ssh/config",
			Lines:      []string{"Host web", "    HostName 10.0.0.1"},
			TotalLines: 12,
		},
	}

	m := newView(service)
	m = apply(t, m, m.Init())

	if !m.loaded {
		t.Fatal("Init did not mark the view as loaded")
	}
	if service.ReportCalls != 1 || service.SourceCalls != 1 {
		t.Errorf("calls = %d reports, %d sources", service.ReportCalls, service.SourceCalls)
	}

	view := m.View()
	for _, want := range []string{"config", "2 hosts", "2 archivos", "avisos", "conf.d", "Host web"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestInitLoadsOnlyOnce(t *testing.T) {
	service := &fakes.SSHConfig{}
	m := newView(service)

	m = apply(t, m, m.Init())
	m = apply(t, m, m.Init())

	if service.ReportCalls != 1 {
		t.Errorf("ReportCalls = %d, want the report read once", service.ReportCalls)
	}
}

func TestReloadReadsAgain(t *testing.T) {
	service := &fakes.SSHConfig{}
	m := newView(service)
	m = apply(t, m, m.Init())

	m = apply(t, m, m.Reload())
	if service.ReportCalls != 2 {
		t.Errorf("ReportCalls = %d, want the reload to read again", service.ReportCalls)
	}
}

func TestErrorIsShown(t *testing.T) {
	service := &fakes.SSHConfig{ReportErr: errors.New("permiso denegado")}
	m := newView(service)

	m = apply(t, m, m.Init())

	if m.Err() == nil {
		t.Fatal("Err should report the failure")
	}
	if !strings.Contains(m.View(), "error") {
		t.Errorf("view does not show the error:\n%s", m.View())
	}
}

func TestRefreshKeyReloads(t *testing.T) {
	service := &fakes.SSHConfig{}
	m := newView(service)
	m = apply(t, m, m.Init())

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = apply(t, next, cmd)

	if service.ReportCalls != 2 {
		t.Errorf("ReportCalls = %d, want the r key to reload", service.ReportCalls)
	}
}

func TestNavigationKeysScroll(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "Host h" + string(rune('a'+i%26))
	}

	service := &fakes.SSHConfig{
		SourceValue: domain.ConfigSource{Path: "/x", Lines: lines, TotalLines: 100},
	}
	m := newView(service)
	m = apply(t, m, m.Init())

	m, _ = m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})

	m, _ = m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyHome})

	// The keys must move the viewport without panicking and stay in range.
	if m.viewport.YOffset() < 0 {
		t.Errorf("YOffset = %d", m.viewport.YOffset())
	}
}

func TestPathFallsBackToTheReport(t *testing.T) {
	service := &fakes.SSHConfig{
		ReportValue: domain.ConfigReport{Main: "/from/report"},
	}
	m := newView(service)
	m = apply(t, m, m.Init())

	if got := m.Path(); got != "/from/report" {
		t.Errorf("Path = %q, want the report path", got)
	}
}

func TestTruncatedSourceIsFlagged(t *testing.T) {
	service := &fakes.SSHConfig{
		SourceValue: domain.ConfigSource{
			Path:       "/x",
			Lines:      []string{"Host a"},
			Truncated:  true,
			TotalLines: 5000,
		},
	}
	m := newView(service)
	m = apply(t, m, m.Init())

	if !strings.Contains(m.View(), "truncada") {
		t.Errorf("view does not flag the truncation:\n%s", m.View())
	}
}

func TestNilServiceIsSafe(t *testing.T) {
	m := New(nil, keymap.Default())
	m.SetBounds(80, 20)

	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init with no service = %v, want nil", cmd)
	}
	if view := m.View(); view == "" {
		t.Error("the view must render even without a service")
	}
}

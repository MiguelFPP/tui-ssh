package hostdetail

import (
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

func newDetail() Model {
	m := New(keymap.Default())
	m.SetBounds(60, 20)

	return m
}

func sampleHost() domain.Host {
	return domain.Host{
		Alias:      "gd-vps",
		Tags:       []string{"prod", "vps"},
		SourceFile: "/home/u/.ssh/config",
		SourceLine: 7,
		Declared: []domain.Directive{
			{Key: "hostname", Value: "207.244.252.156"},
			{Key: "user", Value: "deployer"},
		},
		Forwards: []domain.ForwardSpec{{
			Name:      "pg",
			Direction: domain.ForwardLocal,
			Bind:      domain.BindAddr{Addr: "127.0.0.1", Port: 5432},
			Target:    domain.TargetAddr{Host: "db.internal", Port: 5432},
		}},
	}
}

func TestEmptyView(t *testing.T) {
	m := newDetail()

	if !strings.Contains(m.View(), "selecciona un host") {
		t.Errorf("empty view = %q", m.View())
	}
	if _, ok := m.Host(); ok {
		t.Error("Host reported a selection on an empty panel")
	}
}

func TestDeclaredFallback(t *testing.T) {
	m := newDetail()
	m.SetHost(sampleHost())

	view := m.View()
	for _, want := range []string{"gd-vps", "#prod", "207.244.252.156", "deployer", "config:7"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "forwards") || !strings.Contains(view, "pg") {
		t.Errorf("view does not list the declared forwards:\n%s", view)
	}
}

func TestResolvedView(t *testing.T) {
	m := newDetail()
	m.SetHost(sampleHost())
	m.SetResolved(domain.Resolved{
		Host: domain.ResolvedHost{
			Alias:         "gd-vps",
			HostName:      "207.244.252.156",
			User:          "deployer",
			Port:          2222,
			IdentityFiles: []string{"~/.ssh/id_ed25519_gd_vps"},
		},
	})

	view := m.View()
	for _, want := range []string{"2222", "identity", "id_ed25519"} {
		if !strings.Contains(view, want) {
			t.Errorf("resolved view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "aproximada") {
		t.Error("a real resolution must not be flagged as approximate")
	}
}

func TestApproximateResolutionIsFlagged(t *testing.T) {
	host := sampleHost()

	m := newDetail()
	m.SetHost(host)
	m.SetResolved(domain.Resolved{Host: domain.ApproximateResolved(host), Approximate: true})

	if !strings.Contains(m.View(), "aproximada") {
		t.Errorf("view does not flag an approximate resolution:\n%s", m.View())
	}
}

func TestSetHostDropsStaleResolution(t *testing.T) {
	m := newDetail()
	m.SetHost(sampleHost())
	m.SetResolved(domain.Resolved{Host: domain.ResolvedHost{HostName: "old", User: "u", Port: 22}})

	m.SetHost(domain.Host{Alias: "other"})

	if strings.Contains(m.View(), "old") {
		t.Errorf("the new host kept the old resolution:\n%s", m.View())
	}
}

func TestClear(t *testing.T) {
	m := newDetail()
	m.SetHost(sampleHost())
	m.Clear()

	if _, ok := m.Host(); ok {
		t.Error("Clear kept the selection")
	}
	if !strings.Contains(m.View(), "selecciona") {
		t.Errorf("view after Clear = %q", m.View())
	}
}

func TestHintsNameTheKeys(t *testing.T) {
	m := newDetail()
	m.SetHost(sampleHost())

	view := m.View()
	for _, want := range []string{"conectar", "túnel", "etiquetas"} {
		if !strings.Contains(view, want) {
			t.Errorf("hints missing %q:\n%s", want, view)
		}
	}
}

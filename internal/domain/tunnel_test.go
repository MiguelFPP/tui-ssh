package domain

import (
	"testing"
	"time"
)

func TestTunnelState(t *testing.T) {
	cases := map[TunnelState]string{
		TunnelStopped:  "stopped",
		TunnelStarting: "starting",
		TunnelActive:   "active",
		TunnelFailed:   "failed",
	}
	for state, want := range cases {
		if got := state.String(); got != want {
			t.Errorf("TunnelState(%d).String() = %q, want %q", state, got, want)
		}
	}
}

func TestTunnelRunningAndUptime(t *testing.T) {
	now := time.Unix(1_700_000_100, 0)

	active := Tunnel{State: TunnelActive, StartedAt: time.Unix(1_700_000_000, 0)}
	if !active.Running() {
		t.Error("an active tunnel should be running")
	}
	if got := active.Uptime(now); got != 100*time.Second {
		t.Errorf("Uptime = %v, want 100s", got)
	}

	stopped := Tunnel{State: TunnelStopped, StartedAt: time.Unix(1_700_000_000, 0)}
	if stopped.Running() {
		t.Error("a stopped tunnel should not be running")
	}
	if got := stopped.Uptime(now); got != 0 {
		t.Errorf("Uptime of a stopped tunnel = %v, want 0", got)
	}

	future := Tunnel{State: TunnelActive, StartedAt: now.Add(time.Hour)}
	if got := future.Uptime(now); got != 0 {
		t.Errorf("Uptime with a future start = %v, want 0", got)
	}
}

func TestTunnelEffectivePort(t *testing.T) {
	ephemeral := Tunnel{Spec: ForwardSpec{Bind: BindAddr{Port: 0}}, AllocatedPort: 43218}
	if got := ephemeral.EffectivePort(); got != 43218 {
		t.Errorf("EffectivePort() = %d, want the allocated 43218", got)
	}

	fixed := Tunnel{Spec: ForwardSpec{Bind: BindAddr{Port: 5432}}}
	if got := fixed.EffectivePort(); got != 5432 {
		t.Errorf("EffectivePort() = %d, want 5432", got)
	}
}

func TestParseAllocatedPort(t *testing.T) {
	cases := []struct {
		line string
		port int
		ok   bool
	}{
		{line: "Allocated port 43218 for forward", port: 43218, ok: true},
		{line: "debug1: Allocated port 22 for remote forward", port: 22, ok: true},
		{line: "Allocated port 70000 for forward", ok: false},
		{line: "Allocated port abc for forward", ok: false},
		{line: "no allocation here", ok: false},
		{line: "", ok: false},
	}

	for _, tc := range cases {
		port, ok := ParseAllocatedPort(tc.line)
		if ok != tc.ok || port != tc.port {
			t.Errorf("ParseAllocatedPort(%q) = %d, %v; want %d, %v", tc.line, port, ok, tc.port, tc.ok)
		}
	}
}

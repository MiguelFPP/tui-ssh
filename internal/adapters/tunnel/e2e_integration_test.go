//go:build integration

package tunnel

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

// TestE2EAgainstLocalSSHD drives the real adapter against a local sshd. It is
// skipped unless TUI_SSH_E2E_CONFIG points to a working config for such a server,
// so a machine without one never fails.
func TestE2EAgainstLocalSSHD(t *testing.T) {
	config := os.Getenv("TUI_SSH_E2E_CONFIG")
	if config == "" {
		t.Skip("TUI_SSH_E2E_CONFIG is not set")
	}

	alias := os.Getenv("TUI_SSH_E2E_ALIAS")
	if alias == "" {
		alias = "lab"
	}

	runtimeDir := t.TempDir()
	service := New("ssh", config, runtimeDir)

	spec := domain.ForwardSpec{
		Direction: domain.ForwardLocal,
		Bind:      domain.BindAddr{Addr: "127.0.0.1", Port: 18081},
		Target:    domain.TargetAddr{Host: "127.0.0.1", Port: 2222},
	}

	tunnel, err := service.Start(context.Background(), alias, spec)
	if err != nil {
		t.Fatalf("Start: %v (error: %s)", err, tunnel.Error)
	}
	if tunnel.State != domain.TunnelActive {
		t.Fatalf("State = %v, want active", tunnel.State)
	}

	// The forwarded port must really accept a connection.
	dial := func() error {
		conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:18081", 2*time.Second)
		if dialErr != nil {
			return dialErr
		}

		return conn.Close()
	}
	if err := dial(); err != nil {
		t.Fatalf("the forwarded port is not reachable: %v", err)
	}

	// A second service must adopt the running master from the manifest.
	adopted := New("ssh", config, runtimeDir)
	tunnels, err := adopted.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tunnels) != 1 || tunnels[0].State != domain.TunnelActive {
		t.Fatalf("adopted = %+v, want the active tunnel", tunnels)
	}

	// Stop must end the master and release the port.
	if err := service.Stop(context.Background(), tunnel.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if dialErr := dial(); dialErr != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("the port is still reachable after Stop")
}

// TestE2ELogsAfterStop proves the log survives the master so the panel can always
// explain what happened.
func TestE2ELogsAfterStop(t *testing.T) {
	config := os.Getenv("TUI_SSH_E2E_CONFIG")
	if config == "" {
		t.Skip("TUI_SSH_E2E_CONFIG is not set")
	}

	alias := os.Getenv("TUI_SSH_E2E_ALIAS")
	if alias == "" {
		alias = "lab"
	}

	service := New("ssh", config, t.TempDir())

	spec := domain.ForwardSpec{
		Direction: domain.ForwardLocal,
		Bind:      domain.BindAddr{Addr: "127.0.0.1", Port: 18082},
		Target:    domain.TargetAddr{Host: "127.0.0.1", Port: 2222},
	}

	tunnel, err := service.Start(context.Background(), alias, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	log, err := service.Logs(context.Background(), tunnel.ID)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if log.Path == "" {
		t.Error("Logs.Path is empty")
	}
	if _, statErr := os.Stat(log.Path); statErr != nil {
		t.Errorf("the log file does not exist: %v", statErr)
	}

	if err := service.Stop(context.Background(), tunnel.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// The socket goes away with the master; the log stays so the panel can still
	// explain it after a restart.
	if _, sockErr := os.Stat(tunnel.Socket); sockErr == nil {
		t.Error("the socket was not removed on stop")
	}
	if _, logErr := os.Stat(log.Path); logErr != nil {
		t.Errorf("the log was removed on stop: %v", logErr)
	}
}

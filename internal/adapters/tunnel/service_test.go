package tunnel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

// serviceFor builds a service rooted at a temporary directory, with the ssh calls
// replaced by the given fakes.
func serviceFor(t *testing.T, exec execFunc, spawn spawnFunc) *Service {
	t.Helper()

	service := New("ssh", "", t.TempDir())
	service.exec = exec
	service.spawn = spawn

	return service
}

type execFunc = func(ctx context.Context, binary string, args []string, stdout, stderr *strings.Builder) error
type spawnFunc = func(ctx context.Context, binary string, args []string, logPath string) (int, error)

// fakeSpawn writes the given stderr to the log file and records the args.
func fakeSpawn(stderr string, pid int, err error, got *[]string) spawnFunc {
	return func(_ context.Context, _ string, args []string, logPath string) (int, error) {
		if got != nil {
			*got = append([]string{}, args...)
		}
		if stderr != "" {
			if writeErr := os.WriteFile(logPath, []byte(stderr), 0o600); writeErr != nil {
				return 0, writeErr
			}
		}

		return pid, err
	}
}

func localSpec() domain.ForwardSpec {
	return domain.ForwardSpec{
		Name:      "pg",
		Direction: domain.ForwardLocal,
		Bind:      domain.BindAddr{Addr: "127.0.0.1", Port: 5432},
		Target:    domain.TargetAddr{Host: "db.internal", Port: 5432},
	}
}

func TestStartBuildsMasterArgs(t *testing.T) {
	var got []string
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		fakeSpawn("", 4242, nil, &got))
	// The socket has to exist for the check to report active.
	service.spawn = func(ctx context.Context, binary string, args []string, logPath string) (int, error) {
		got = append([]string{}, args...)
		if err := os.MkdirAll(filepath.Dir(args[2]), 0o700); err != nil {
			return 0, err
		}
		if err := os.WriteFile(args[2], nil, 0o600); err != nil {
			return 0, err
		}

		return 4242, nil
	}

	tunnel, err := service.Start(context.Background(), "gd-vps", localSpec())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if tunnel.State != domain.TunnelActive {
		t.Errorf("State = %v, want active", tunnel.State)
	}
	if tunnel.PID != 4242 {
		t.Errorf("PID = %d", tunnel.PID)
	}

	joined := strings.Join(got, " ")
	for _, want := range []string{"-M", "-S", "-f", "-N", "ExitOnForwardFailure=yes", "-L 127.0.0.1:5432:db.internal:5432", "gd-vps"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestStartReadsAllocatedPort(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		fakeSpawn("Allocated port 43218 for remote forward to localhost:3000\n", 10, nil, nil))

	// Only a remote forward may leave the port to OpenSSH.
	spec := domain.ForwardSpec{Direction: domain.ForwardRemote, Bind: domain.BindAddr{Port: 0}, Target: domain.TargetAddr{Host: "localhost", Port: 3000}}

	tunnel, err := service.Start(context.Background(), "web", spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if tunnel.AllocatedPort != 43218 {
		t.Errorf("AllocatedPort = %d, want 43218", tunnel.AllocatedPort)
	}
	if got := tunnel.EffectivePort(); got != 43218 {
		t.Errorf("EffectivePort = %d, want the allocated port", got)
	}
}

func TestStartRejectsLocalEphemeralPort(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error {
			t.Error("ssh must not run for a spec the domain already rejected")

			return nil
		},
		fakeSpawn("", 1, nil, nil))

	// ssh rejects `-L addr:0:...`, so the adapter refuses it before spawning.
	spec := domain.ForwardSpec{Direction: domain.ForwardLocal, Bind: domain.BindAddr{Port: 0}, Target: domain.TargetAddr{Host: "db", Port: 5432}}

	if _, err := service.Start(context.Background(), "web", spec); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("Start = %v, want ErrInvalidArgument", err)
	}
}

func TestStartFailureIsRecorded(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		fakeSpawn("Warning: Permanently added host\nPermission denied (publickey).\n", 0, errors.New("exit status 255"), nil))

	tunnel, err := service.Start(context.Background(), "web", localSpec())
	if !errors.Is(err, domain.ErrTunnelFailed) {
		t.Fatalf("Start = %v, want ErrTunnelFailed", err)
	}

	if tunnel.State != domain.TunnelFailed {
		t.Errorf("State = %v, want failed", tunnel.State)
	}
	if !strings.Contains(tunnel.Error, "autenticación interactiva") {
		t.Errorf("Error = %q, want the authentication hint", tunnel.Error)
	}

	tunnels, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tunnels) != 1 {
		t.Fatalf("List = %d tunnels, want the failed one kept", len(tunnels))
	}
}

func TestStartRejectsDuplicate(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		func(_ context.Context, _ string, args []string, _ string) (int, error) {
			if err := os.MkdirAll(filepath.Dir(args[2]), 0o700); err != nil {
				return 0, err
			}

			return 1, os.WriteFile(args[2], nil, 0o600)
		})

	if _, err := service.Start(context.Background(), "web", localSpec()); err != nil {
		t.Fatalf("First Start: %v", err)
	}

	if _, err := service.Start(context.Background(), "web", localSpec()); !errors.Is(err, domain.ErrTunnelExists) {
		t.Fatalf("Second Start = %v, want ErrTunnelExists", err)
	}
}

func TestStartRejectsInvalidSpec(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		fakeSpawn("", 1, nil, nil))

	invalid := domain.ForwardSpec{Direction: domain.ForwardLocal, Bind: domain.BindAddr{Port: 80}}
	if _, err := service.Start(context.Background(), "web", invalid); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("Start = %v, want ErrInvalidArgument", err)
	}
}

func TestCheckUsesTheControlSocket(t *testing.T) {
	var checked []string
	service := serviceFor(t,
		func(_ context.Context, _ string, args []string, _, _ *strings.Builder) error {
			checked = append(checked, strings.Join(args, " "))

			return nil
		},
		func(_ context.Context, _ string, args []string, _ string) (int, error) {
			if err := os.MkdirAll(filepath.Dir(args[2]), 0o700); err != nil {
				return 0, err
			}

			return 7, os.WriteFile(args[2], nil, 0o600)
		})

	tunnel, err := service.Start(context.Background(), "web", localSpec())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	state, err := service.Check(context.Background(), tunnel.ID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if state != domain.TunnelActive {
		t.Errorf("Check = %v, want active", state)
	}

	if len(checked) == 0 || !strings.Contains(checked[0], "-O check") {
		t.Errorf("exec args = %v, want a -O check first", checked)
	}
}

func TestCheckWithoutSocketReportsStopped(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error {
			t.Error("exec must not run without a socket")

			return nil
		},
		fakeSpawn("", 9, nil, nil))

	tunnel, err := service.Start(context.Background(), "web", localSpec())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// No socket file exists, so the master is considered gone.
	state, err := service.Check(context.Background(), tunnel.ID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if state != domain.TunnelStopped {
		t.Errorf("Check = %v, want stopped", state)
	}
}

func TestStopEndsTheMaster(t *testing.T) {
	var exited []string
	service := serviceFor(t,
		func(_ context.Context, _ string, args []string, _, _ *strings.Builder) error {
			exited = append(exited, strings.Join(args, " "))

			return nil
		},
		func(_ context.Context, _ string, args []string, _ string) (int, error) {
			if err := os.MkdirAll(filepath.Dir(args[2]), 0o700); err != nil {
				return 0, err
			}

			return 3, os.WriteFile(args[2], nil, 0o600)
		})

	tunnel, err := service.Start(context.Background(), "web", localSpec())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := service.Stop(context.Background(), tunnel.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Start already probes the socket once; Stop appends the -O exit.
	var exitedOne bool
	for _, call := range exited {
		if strings.Contains(call, "-O exit") {
			exitedOne = true
		}
	}
	if !exitedOne {
		t.Errorf("exec args = %v, want one -O exit", exited)
	}

	tunnels, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tunnels) != 1 || tunnels[0].State != domain.TunnelStopped {
		t.Errorf("List = %+v, want the stopped tunnel", tunnels)
	}
}

func TestStopUnknownTunnel(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		fakeSpawn("", 1, nil, nil))

	if err := service.Stop(context.Background(), "nope"); !errors.Is(err, domain.ErrTunnelNotFound) {
		t.Fatalf("Stop = %v, want ErrTunnelNotFound", err)
	}
}

func TestLogsReadsTheTail(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		fakeSpawn("line one\nline two\nAllocated port 2222 for forward\n", 5, nil, nil))

	tunnel, err := service.Start(context.Background(), "web", localSpec())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	log, err := service.Logs(context.Background(), tunnel.ID)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}

	if len(log.Lines) != 3 {
		t.Fatalf("Lines = %v, want 3", log.Lines)
	}
	if log.Lines[0] != "line one" {
		t.Errorf("Lines[0] = %q", log.Lines[0])
	}
	if log.Truncated {
		t.Error("a short log must not be flagged as truncated")
	}
}

func TestLogsMissingTunnel(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		fakeSpawn("", 1, nil, nil))

	if _, err := service.Logs(context.Background(), "nope"); !errors.Is(err, domain.ErrTunnelNotFound) {
		t.Fatalf("Logs = %v, want ErrTunnelNotFound", err)
	}
}

func TestManifestReAdoption(t *testing.T) {
	runtimeDir := t.TempDir()

	// The first service starts a tunnel and records it in the manifest.
	first := New("ssh", "", runtimeDir)
	first.exec = func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil }
	first.spawn = func(_ context.Context, _ string, args []string, _ string) (int, error) {
		if err := os.MkdirAll(filepath.Dir(args[2]), 0o700); err != nil {
			return 0, err
		}

		return 11, os.WriteFile(args[2], nil, 0o600)
	}

	tunnel, err := first.Start(context.Background(), "web", localSpec())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// A second service, as if the TUI had been restarted, must see the live tunnel.
	second := New("ssh", "", runtimeDir)
	second.exec = func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil }

	tunnels, err := second.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(tunnels) != 1 {
		t.Fatalf("List = %d tunnels, want the adopted one", len(tunnels))
	}
	if tunnels[0].ID != tunnel.ID || tunnels[0].Host != "web" {
		t.Errorf("adopted = %+v, want %s from web", tunnels[0], tunnel.ID)
	}
	if tunnels[0].State != domain.TunnelActive {
		t.Errorf("State = %v, want active after the check", tunnels[0].State)
	}
}

func TestManifestIsRemovedWhenEmpty(t *testing.T) {
	runtimeDir := t.TempDir()

	service := New("ssh", "", runtimeDir)
	service.exec = func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil }
	service.spawn = func(_ context.Context, _ string, args []string, _ string) (int, error) {
		if err := os.MkdirAll(filepath.Dir(args[2]), 0o700); err != nil {
			return 0, err
		}

		return 1, os.WriteFile(args[2], nil, 0o600)
	}

	tunnel, err := service.Start(context.Background(), "web", localSpec())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, manifestName)); err != nil {
		t.Fatalf("manifest missing after Start: %v", err)
	}

	if err := service.Stop(context.Background(), tunnel.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := os.Stat(filepath.Join(runtimeDir, manifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("manifest still exists after stopping everything: %v", err)
	}
}

func TestStopAll(t *testing.T) {
	service := serviceFor(t,
		func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil },
		func(_ context.Context, _ string, args []string, _ string) (int, error) {
			if err := os.MkdirAll(filepath.Dir(args[2]), 0o700); err != nil {
				return 0, err
			}

			return 1, os.WriteFile(args[2], nil, 0o600)
		})

	remote := domain.ForwardSpec{Direction: domain.ForwardRemote, Bind: domain.BindAddr{Port: 9000}, Target: domain.TargetAddr{Host: "localhost", Port: 3000}}

	if _, err := service.Start(context.Background(), "web", localSpec()); err != nil {
		t.Fatalf("Start local: %v", err)
	}
	if _, err := service.Start(context.Background(), "db", remote); err != nil {
		t.Fatalf("Start remote: %v", err)
	}

	if err := service.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	tunnels, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, tunnel := range tunnels {
		if tunnel.Running() {
			t.Errorf("tunnel %s still running after StopAll", tunnel.ID)
		}
	}
}

func TestSocketPathIsShortened(t *testing.T) {
	service := New("ssh", "", "/"+strings.Repeat("very-long-directory/", 6))

	short, err := service.socketPath("web-L5432-5432")
	if err != nil {
		t.Fatalf("socketPath: %v", err)
	}

	if len(short) > maxControlPathBytes {
		t.Errorf("socketPath = %q (%d bytes), want at most %d", short, len(short), maxControlPathBytes)
	}
	if !strings.HasSuffix(short, ".sock") {
		t.Errorf("socketPath = %q, want a .sock suffix", short)
	}
}

func TestSocketPathWithoutRuntimeDir(t *testing.T) {
	service := New("ssh", "", "")

	if _, err := service.socketPath("web-L1-2"); !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("socketPath = %v, want ErrInvalidPath", err)
	}
}

func TestDescribeFailure(t *testing.T) {
	cases := map[string]string{
		"Permission denied (publickey)":         "autenticación interactiva",
		"bind: Address already in use":          "el puerto ya está en uso",
		"ssh: Could not resolve hostname db":    "no se pudo resolver el host",
		"connect to host: Connection refused":   "no se pudo conectar con el host",
		"something entirely different happened": "no arrancó",
	}

	for message, want := range cases {
		got := describeFailure(errors.New(message), "/tmp/log")
		if !strings.Contains(got, want) {
			t.Errorf("describeFailure(%q) = %q, want it to contain %q", message, got, want)
		}
	}
}

func TestMasterArgsKeepArgumentsSeparate(t *testing.T) {
	args := masterArgs("", "/run/x.sock", localSpec(), "web")

	// No argument may smuggle a shell line: each option is its own argument.
	for _, arg := range args {
		if strings.ContainsAny(arg, "|&;`$") {
			t.Errorf("argument %q carries shell syntax", arg)
		}
	}

	last := args[len(args)-1]
	if last != "web" {
		t.Errorf("last argument = %q, want the alias", last)
	}
}

func TestMasterArgsPassTheConfigPath(t *testing.T) {
	args := masterArgs("/home/u/.ssh/config", "/run/x.sock", localSpec(), "web")

	if len(args) < 2 || args[0] != "-F" || args[1] != "/home/u/.ssh/config" {
		t.Fatalf("args = %v, want -F with the config path first", args)
	}

	// Without a config path the flag is omitted instead of passing an empty value.
	bare := masterArgs("", "/run/x.sock", localSpec(), "web")
	for _, arg := range bare {
		if arg == "-F" {
			t.Errorf("args = %v, want no -F without a config path", bare)
		}
	}
}

func TestControlArgsPassTheConfigPath(t *testing.T) {
	service := New("ssh", "/home/u/.ssh/config", t.TempDir())

	args := service.controlArgs("/run/x.sock", "check", "web")
	want := []string{"-F", "/home/u/.ssh/config", "-S", "/run/x.sock", "-O", "check", "web"}

	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i, arg := range want {
		if args[i] != arg {
			t.Errorf("args[%d] = %q, want %q", i, args[i], arg)
		}
	}
}

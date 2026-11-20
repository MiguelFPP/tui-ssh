package resolver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

func TestResolveParsesOutput(t *testing.T) {
	service := New("ssh", "/home/u/.ssh/config")
	service.run = func(_ context.Context, binary string, args []string) ([]byte, error) {
		if binary != "ssh" {
			t.Errorf("binary = %q, want ssh", binary)
		}
		// The config path must be passed with -F: OpenSSH resolves ~ from the
		// password database, so omitting it could resolve against another file.
		want := []string{"-F", "/home/u/.ssh/config", "-G", "gd-vps"}
		if len(args) != len(want) {
			t.Fatalf("args = %v, want %v", args, want)
		}
		for i, arg := range want {
			if args[i] != arg {
				t.Errorf("args[%d] = %q, want %q", i, args[i], arg)
			}
		}

		return []byte("hostname 207.244.252.156\nuser deployer\nport 22\n"), nil
	}

	resolved, err := service.Resolve(context.Background(), "gd-vps")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if resolved.Approximate {
		t.Error("a successful ssh -G is authoritative, not approximate")
	}
	if resolved.Host.HostName != "207.244.252.156" || resolved.Host.User != "deployer" {
		t.Errorf("resolved = %+v", resolved.Host)
	}
	if resolved.Host.Port != 22 {
		t.Errorf("Port = %d", resolved.Host.Port)
	}
}

func TestResolveWithoutConfigOmitsTheFlag(t *testing.T) {
	service := New("ssh", "")
	service.run = func(_ context.Context, _ string, args []string) ([]byte, error) {
		if len(args) != 2 || args[0] != "-G" {
			t.Errorf("args = %v, want the bare -G invocation", args)
		}

		return []byte("hostname host\n"), nil
	}

	if _, err := service.Resolve(context.Background(), "web"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

func TestResolveCaches(t *testing.T) {
	calls := 0
	service := New("ssh", "")
	service.run = func(context.Context, string, []string) ([]byte, error) {
		calls++

		return []byte("hostname host\n"), nil
	}

	for i := 0; i < 3; i++ {
		if _, err := service.Resolve(context.Background(), "web"); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
	}

	if calls != 1 {
		t.Errorf("run called %d times, want 1 from the cache", calls)
	}

	service.Invalidate("web")
	if _, err := service.Resolve(context.Background(), "web"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if calls != 2 {
		t.Errorf("run called %d times after Invalidate, want 2", calls)
	}

	service.Invalidate("")
	if _, err := service.Resolve(context.Background(), "web"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if calls != 3 {
		t.Errorf("run called %d times after Invalidate(\"\"), want 3", calls)
	}
}

func TestResolveRejectsInvalidAlias(t *testing.T) {
	service := New("ssh", "")
	service.run = func(context.Context, string, []string) ([]byte, error) {
		t.Fatal("run must not be called for an invalid alias")

		return nil, nil
	}

	if _, err := service.Resolve(context.Background(), "-oProxyCommand=evil"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("Resolve = %v, want ErrInvalidArgument", err)
	}
}

func TestResolveReportsSSHUnavailable(t *testing.T) {
	service := New("ssh", "")
	service.run = func(context.Context, string, []string) ([]byte, error) {
		return nil, errors.New("exec: not found")
	}

	_, err := service.Resolve(context.Background(), "web")
	if !errors.Is(err, domain.ErrSSHUnavailable) {
		t.Fatalf("Resolve = %v, want ErrSSHUnavailable", err)
	}
}

func TestResolveRespectsCancelledContext(t *testing.T) {
	service := New("ssh", "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.Resolve(ctx, "web"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve = %v, want context.Canceled", err)
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{name: "empty", in: "", want: 0},
		{name: "trailing newline", in: "a 1\nb 2\n", want: 2},
		{name: "crlf", in: "a 1\r\nb 2\r\n", want: 2},
		{name: "no newline", in: "a 1", want: 1},
	}

	for _, tc := range cases {
		if got := splitLines([]byte(tc.in)); len(got) != tc.want {
			t.Errorf("%s: splitLines(%q) = %v, want %d lines", tc.name, tc.in, got, tc.want)
		}
	}

	if got := splitLines([]byte("a 1\r\nb 2\r\n")); strings.Contains(got[0], "\r") {
		t.Errorf("splitLines left a carriage return: %q", got[0])
	}
}

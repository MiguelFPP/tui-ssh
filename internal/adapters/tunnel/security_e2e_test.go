package tunnel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTamperedManifestNeverReachesSSH is the end-to-end proof of the fix: a manifest
// entry carrying an ssh option as its host and another escaping the state directory
// through its ID must be ignored, and `ssh` must never be invoked with those values.
func TestTamperedManifestNeverReachesSSH(t *testing.T) {
	dir := t.TempDir()

	payload := `[
  {"id":"escape","host":"-oProxyCommand=/tmp/pwn.sh","direction":"local","bind_addr":"127.0.0.1","bind_port":5432,"target_host":"db","target_port":5432,"pid":1,"started_at":1},
  {"id":"../outside","host":"gd-vps","direction":"local","bind_addr":"127.0.0.1","bind_port":5433,"target_host":"db","target_port":5432,"pid":2,"started_at":1},
  {"id":"good","host":"gd-vps","direction":"local","bind_addr":"127.0.0.1","bind_port":5434,"target_host":"db","target_port":5432,"pid":3,"started_at":1}
]`
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(payload), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var seen [][]string
	service := New("ssh", "", dir)
	service.exec = func(_ context.Context, _ string, args []string, _ *strings.Builder, _ *strings.Builder) error {
		seen = append(seen, append([]string{}, args...))

		return nil
	}

	tunnels, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Only the valid entry survives.
	if len(tunnels) != 1 || tunnels[0].Host != "gd-vps" || tunnels[0].ID != "good" {
		t.Fatalf("adopted = %+v, want only the valid entry", tunnels)
	}

	for _, args := range seen {
		for _, arg := range args {
			if strings.HasPrefix(arg, "-o") {
				t.Fatalf("ssh was invoked with an injected option: %v", args)
			}
		}
	}
}

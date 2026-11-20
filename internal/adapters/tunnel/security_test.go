package tunnel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

// TestLoadRejectsUnsafeManifestEntries covers the persistence trust boundary: the
// manifest lives in a runtime directory that another local user may be able to
// write (a shared /tmp, a stale XDG fallback). Its contents name the socket of a
// master and the alias handed to ssh, so an entry that does not validate must be
// skipped instead of being adopted.
func TestLoadRejectsUnsafeManifestEntries(t *testing.T) {
	dir := t.TempDir()

	// An entry whose host would become an ssh argument starting with a dash, and
	// one whose ID would escape the state directory through a path separator.
	payload := `[
  {"id":"../escape","host":"-oProxyCommand=touch /tmp/pwned","direction":"local","bind_addr":"127.0.0.1","bind_port":5432,"target_host":"db","target_port":5432,"pid":1,"started_at":1},
  {"id":"ok","host":"","direction":"local","bind_addr":"127.0.0.1","bind_port":5433,"target_host":"db","target_port":5432,"pid":2,"started_at":1},
  {"id":"ok2","host":"gd-vps","direction":"bogus","bind_addr":"127.0.0.1","bind_port":5434,"target_host":"db","target_port":5432,"pid":3,"started_at":1}
]`
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(payload), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	service := New("ssh", "", dir)
	service.exec = func(context.Context, string, []string, *strings.Builder, *strings.Builder) error { return nil }

	tunnels, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, tunnel := range tunnels {
		if err := domain.ValidateAlias(tunnel.Host); err != nil {
			t.Errorf("adopted a tunnel with an unsafe host %q: %v", tunnel.Host, err)
		}
		if tunnel.ID != filepath.Base(tunnel.ID) {
			t.Errorf("adopted a tunnel whose ID escapes the state directory: %q", tunnel.ID)
		}
	}
}

package tunnel

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSocketFallbackRefusesWorldWritableDirectory covers the long-path fallback: when
// the runtime directory is too deep, the socket moves under the system temp directory,
// which is shared by every local user. The subdirectory is derived from a hash, so it
// is predictable: adopting one that somebody else already created would hand them
// control of the master socket. The fallback must never place the socket in a
// directory other local users can write to.
func TestSocketFallbackRefusesWorldWritableDirectory(t *testing.T) {
	deep := filepath.Join(t.TempDir(), strings.Repeat("d", 120))

	service := New("ssh", "", deep)

	// Pre-create the predictable fallback directory the way another local user on a
	// shared /tmp would.
	dirSum := sha256.Sum256([]byte(deep))
	foreign := filepath.Join(os.TempDir(), "tui-ssh-"+hex.EncodeToString(dirSum[:4]))
	if err := os.MkdirAll(foreign, 0o777); err != nil {
		t.Fatalf("create fallback dir: %v", err)
	}
	if err := os.Chmod(foreign, 0o777); err != nil {
		t.Fatalf("chmod fallback dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(foreign) })

	socket, err := service.socketPath("gd-vps-L5432-5432")
	if err != nil {
		// Rejecting the fallback outright is the other acceptable answer.
		return
	}

	info, statErr := os.Stat(filepath.Dir(socket))
	if statErr != nil {
		t.Fatalf("stat fallback dir: %v", statErr)
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Fatalf("socket would live in a group/world-writable directory: %s (%04o)",
			filepath.Dir(socket), info.Mode().Perm())
	}
}

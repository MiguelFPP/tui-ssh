package tags

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

func storeFor(t *testing.T) *Store {
	t.Helper()

	dir := t.TempDir()

	return New(Files{
		Tags:     filepath.Join(dir, "tags.json"),
		Forwards: filepath.Join(dir, "forwards.json"),
	})
}

func TestTagsMissingFileIsEmpty(t *testing.T) {
	store := storeFor(t)

	index, err := store.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}

	if len(index) != 0 {
		t.Errorf("Tags = %v, want an empty index", index)
	}
}

func TestSetTagsRoundTrip(t *testing.T) {
	store := storeFor(t)

	if err := store.SetTags(context.Background(), "gd-vps", []string{"prod", "VPS"}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}

	index, err := store.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}

	got := index["gd-vps"]
	if len(got) != 2 || got[0] != "prod" || got[1] != "vps" {
		t.Fatalf("tags = %v, want [prod vps] normalized", got)
	}

	// The file must be private: it is user state next to the configuration.
	info, err := os.Stat(store.files.Tags)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("permissions = %o, want 600", mode)
	}
}

func TestSetTagsEmptyRemovesEntry(t *testing.T) {
	store := storeFor(t)

	if err := store.SetTags(context.Background(), "web", []string{"prod"}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	if err := store.SetTags(context.Background(), "web", nil); err != nil {
		t.Fatalf("SetTags(nil): %v", err)
	}

	index, err := store.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}

	if _, ok := index["web"]; ok {
		t.Errorf("tags = %v, want the entry removed", index)
	}
}

func TestSetTagsRejectsInvalidAlias(t *testing.T) {
	store := storeFor(t)

	if err := store.SetTags(context.Background(), "-oProxyCommand=x", []string{"a"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("SetTags = %v, want ErrInvalidArgument", err)
	}
}

func TestTagsRejectsInvalidJSON(t *testing.T) {
	store := storeFor(t)

	if err := os.MkdirAll(filepath.Dir(store.files.Tags), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.files.Tags, []byte(`{"web": "not-a-list"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := store.Tags(context.Background()); !errors.Is(err, domain.ErrConfigInvalid) {
		t.Fatalf("Tags = %v, want ErrConfigInvalid", err)
	}
}

func TestTagsRejectsUnknownFields(t *testing.T) {
	store := storeFor(t)

	if err := os.MkdirAll(filepath.Dir(store.files.Tags), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The tag index is a map of lists, so an unknown field can only appear as a
	// value shape the decoder rejects.
	if err := os.WriteFile(store.files.Tags, []byte(`{"web": [{"oops": 1}]}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := store.Tags(context.Background()); !errors.Is(err, domain.ErrConfigInvalid) {
		t.Fatalf("Tags = %v, want ErrConfigInvalid", err)
	}
}

func TestTagsRejectsNull(t *testing.T) {
	store := storeFor(t)

	if err := os.MkdirAll(filepath.Dir(store.files.Tags), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.files.Tags, []byte("null"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := store.Tags(context.Background()); !errors.Is(err, domain.ErrConfigInvalid) {
		t.Fatalf("Tags = %v, want ErrConfigInvalid for null", err)
	}
}

func TestForwardsRoundTrip(t *testing.T) {
	store := storeFor(t)

	spec := domain.ForwardSpec{
		Name:      "pg",
		Direction: domain.ForwardLocal,
		Bind:      domain.BindAddr{Addr: "127.0.0.1", Port: 5432},
		Target:    domain.TargetAddr{Host: "db.internal", Port: 5432},
	}

	if err := store.SetForwards(context.Background(), "gd-vps", []domain.ForwardSpec{spec}); err != nil {
		t.Fatalf("SetForwards: %v", err)
	}

	forwards, err := store.Forwards(context.Background())
	if err != nil {
		t.Fatalf("Forwards: %v", err)
	}

	list := forwards["gd-vps"]
	if len(list) != 1 {
		t.Fatalf("forwards = %v, want one spec", list)
	}

	got := list[0]
	if got.Name != "pg" || got.Direction != domain.ForwardLocal ||
		got.Bind.Port != 5432 || got.Target.Host != "db.internal" || got.Target.Port != 5432 {
		t.Errorf("round trip = %+v", got)
	}
}

func TestForwardsDynamicRoundTrip(t *testing.T) {
	store := storeFor(t)

	spec := domain.ForwardSpec{Direction: domain.ForwardDynamic, Bind: domain.BindAddr{Addr: "127.0.0.1", Port: 1080}}
	if err := store.SetForwards(context.Background(), "web", []domain.ForwardSpec{spec}); err != nil {
		t.Fatalf("SetForwards: %v", err)
	}

	forwards, err := store.Forwards(context.Background())
	if err != nil {
		t.Fatalf("Forwards: %v", err)
	}

	if got := forwards["web"]; len(got) != 1 || got[0].Direction != domain.ForwardDynamic {
		t.Fatalf("forwards = %v", got)
	}
}

func TestSetForwardsEmptyRemovesEntry(t *testing.T) {
	store := storeFor(t)

	spec := domain.ForwardSpec{Direction: domain.ForwardDynamic, Bind: domain.BindAddr{Port: 1080}}
	if err := store.SetForwards(context.Background(), "web", []domain.ForwardSpec{spec}); err != nil {
		t.Fatalf("SetForwards: %v", err)
	}
	if err := store.SetForwards(context.Background(), "web", nil); err != nil {
		t.Fatalf("SetForwards(nil): %v", err)
	}

	forwards, err := store.Forwards(context.Background())
	if err != nil {
		t.Fatalf("Forwards: %v", err)
	}

	if _, ok := forwards["web"]; ok {
		t.Errorf("forwards = %v, want the entry removed", forwards)
	}
}

func TestForwardsMissingFileIsEmpty(t *testing.T) {
	store := storeFor(t)

	forwards, err := store.Forwards(context.Background())
	if err != nil {
		t.Fatalf("Forwards: %v", err)
	}

	if len(forwards) != 0 {
		t.Errorf("Forwards = %v, want an empty map", forwards)
	}
}

func TestForwardsRejectsInvalidDirection(t *testing.T) {
	store := storeFor(t)

	if err := os.MkdirAll(filepath.Dir(store.files.Forwards), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"web": [{"direction": "sideways", "bind_addr": "127.0.0.1", "bind_port": 1080}]}`
	if err := os.WriteFile(store.files.Forwards, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := store.Forwards(context.Background()); !errors.Is(err, domain.ErrConfigInvalid) {
		t.Fatalf("Forwards = %v, want ErrConfigInvalid", err)
	}
}

func TestWriteAtomicReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.json")

	if err := writeAtomic(path, "first"); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeAtomic(path, "second"); err != nil {
		t.Fatalf("writeAtomic overwrite: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "second" {
		t.Errorf("content = %q, want second", data)
	}
}

func TestSortAliases(t *testing.T) {
	index := domain.TagIndex{"b": {"x"}, "a": {"y"}}

	aliases := SortAliases(index)
	if len(aliases) != 2 || aliases[0] != "a" || aliases[1] != "b" {
		t.Errorf("SortAliases = %v, want [a b]", aliases)
	}
}

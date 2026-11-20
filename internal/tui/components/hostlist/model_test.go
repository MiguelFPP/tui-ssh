package hostlist

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/tui/keymap"
)

func host(alias string, tags ...string) domain.Host {
	return domain.Host{
		Alias:    alias,
		Tags:     tags,
		Declared: []domain.Directive{{Key: "hostname", Value: alias + ".example.com"}},
	}
}

func newList(hosts ...domain.Host) Model {
	m := New(keymap.Default())
	m.SetBounds(0, 0, 40, 12)
	m.SetHosts(hosts)

	return m
}

func TestSetHostsBuildsRows(t *testing.T) {
	m := newList(host("web"), host("db"), host("cache"))

	if m.Total() != 3 {
		t.Errorf("Total = %d, want 3", m.Total())
	}
	if m.Count() != 3 {
		t.Errorf("Count = %d, want 3", m.Count())
	}

	selected, ok := m.Selected()
	if !ok || selected.Alias != "web" {
		t.Errorf("Selected = %q, %v; want the first host", selected.Alias, ok)
	}
}

func TestNavigationSkipsTagHeaders(t *testing.T) {
	hosts := make([]domain.Host, 0, 10)
	for i := 0; i < 10; i++ {
		hosts = append(hosts, host("h"+string(rune('a'+i)), "prod"))
	}
	m := newList(hosts...)

	// With a grouped list the cursor starts on the tag header, so Down must land on
	// a host, never on a header.
	m.Down()
	for i := 0; i < 20; i++ {
		if _, ok := m.Selected(); !ok {
			t.Fatalf("cursor %d sits on a non-host row", i)
		}
		m.Down()
	}
	for i := 0; i < 20; i++ {
		if _, ok := m.Selected(); !ok {
			t.Fatalf("cursor %d sits on a non-host row on the way up", i)
		}
		m.Up()
	}
}

func TestFilterNarrowsRows(t *testing.T) {
	m := newList(host("web-prod"), host("db-prod"), host("bastion"))

	m.SetFilter("web")
	if m.Count() != 1 {
		t.Fatalf("Count = %d, want 1 for the filter", m.Count())
	}

	selected, ok := m.Selected()
	if !ok || selected.Alias != "web-prod" {
		t.Errorf("Selected = %q", selected.Alias)
	}

	m.SetFilter("")
	if m.Count() != 3 {
		t.Errorf("Count = %d, want 3 after clearing the filter", m.Count())
	}
}

func TestFilterMatchesTagsAndHostname(t *testing.T) {
	m := newList(host("web", "prod"), host("db", "staging"))

	m.SetFilter("staging")
	if m.Count() != 1 {
		t.Errorf("filter by tag: Count = %d, want 1", m.Count())
	}

	m.SetFilter("db.example")
	if m.Count() != 1 {
		t.Errorf("filter by hostname: Count = %d, want 1", m.Count())
	}
}

func TestTagFilterAndCycle(t *testing.T) {
	m := newList(host("web", "prod"), host("db", "prod"), host("cache", "staging"), host("other"))

	tags := m.Tags()
	if len(tags) != 2 || tags[0] != "prod" || tags[1] != "staging" {
		t.Fatalf("Tags = %v, want [prod staging]", tags)
	}

	m.SetTagFilter("prod")
	if m.Count() != 2 {
		t.Errorf("Count = %d, want the two prod hosts", m.Count())
	}

	// Cycling goes through the empty filter and every tag.
	m.CycleTag(1)
	if m.TagFilter() != "staging" {
		t.Errorf("TagFilter after one cycle = %q, want staging", m.TagFilter())
	}
	m.CycleTag(1)
	if m.TagFilter() != "" {
		t.Errorf("TagFilter after wrapping = %q, want empty", m.TagFilter())
	}
	m.CycleTag(-1)
	if m.TagFilter() != "staging" {
		t.Errorf("TagFilter after cycling back = %q, want staging", m.TagFilter())
	}
}

func TestSetHostsKeepsSelection(t *testing.T) {
	m := newList(host("web"), host("db"), host("cache"))
	m.Down()

	selected, _ := m.Selected()
	if selected.Alias != "db" {
		t.Fatalf("Selected = %q, want db", selected.Alias)
	}

	// A refresh that keeps the alias must keep the cursor on it.
	m.SetHosts([]domain.Host{host("cache"), host("db"), host("web")})

	selected, ok := m.Selected()
	if !ok || selected.Alias != "db" {
		t.Errorf("Selected after refresh = %q, %v; want db", selected.Alias, ok)
	}
}

func TestSetHostsDropsUnknownSelection(t *testing.T) {
	m := newList(host("web"), host("db"))
	m.Down()

	m.SetHosts([]domain.Host{host("cache")})

	selected, ok := m.Selected()
	if !ok || selected.Alias != "cache" {
		t.Errorf("Selected = %q, %v; want the only remaining host", selected.Alias, ok)
	}
}

func TestHomeAndEnd(t *testing.T) {
	m := newList(host("a"), host("b"), host("c"))

	m.End()
	if selected, _ := m.Selected(); selected.Alias != "c" {
		t.Errorf("End selected %q, want c", selected.Alias)
	}

	m.Home()
	if selected, _ := m.Selected(); selected.Alias != "a" {
		t.Errorf("Home selected %q, want a", selected.Alias)
	}
}

func TestViewRendersHeaderAndCount(t *testing.T) {
	m := newList(host("web", "prod"), host("db", "prod"))
	view := m.View()

	if !strings.Contains(view, "host") {
		t.Errorf("View does not render the column header:\n%s", view)
	}
	if !strings.Contains(view, "web") || !strings.Contains(view, "db") {
		t.Errorf("View does not render the hosts:\n%s", view)
	}
	if !strings.Contains(view, "2 hosts") {
		t.Errorf("View does not render the count:\n%s", view)
	}
}

func TestViewEmptyStates(t *testing.T) {
	empty := New(keymap.Default())
	empty.SetBounds(0, 0, 40, 12)
	if !strings.Contains(empty.View(), "sin hosts") {
		t.Errorf("empty list view = %q", empty.View())
	}

	filtered := newList(host("web"))
	filtered.SetFilter("nope")
	if !strings.Contains(filtered.View(), "sin hosts para") {
		t.Errorf("filtered empty view = %q", filtered.View())
	}
}

func TestRowAtMapsCoordinates(t *testing.T) {
	m := newList(host("web"), host("db"), host("cache"))

	// headerRows is 2: the filter row and the column header.
	got, ok := m.RowAt(0, 2)
	if !ok || got.Alias != "web" {
		t.Errorf("RowAt(0, 2) = %q, %v; want web", got.Alias, ok)
	}

	if _, ok := m.RowAt(0, 0); ok {
		t.Error("a click on the header row must not select a host")
	}
}

func TestFilterInputCapturesKeys(t *testing.T) {
	m := newList(host("web"), host("db"))

	m.SetFilterVisible(true)
	if !m.Filtering() {
		t.Fatal("Filtering should be true after SetFilterVisible(true)")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.Filter() != "d" {
		t.Errorf("Filter = %q, want the typed d", m.Filter())
	}

	m.SetFilterVisible(false)
	if m.Filtering() {
		t.Error("Filtering should be false after blurring")
	}
}

func TestWheelMovesSelection(t *testing.T) {
	hosts := make([]domain.Host, 0, 20)
	for i := 0; i < 20; i++ {
		hosts = append(hosts, host("h"+string(rune('a'+i))))
	}
	m := newList(hosts...)

	before, _ := m.Selected()
	m.Wheel(1)
	after, _ := m.Selected()

	if before.Alias == after.Alias {
		t.Errorf("Wheel did not move the selection from %q", before.Alias)
	}
}

func TestEmptyListIsSafe(t *testing.T) {
	m := New(keymap.Default())
	m.SetBounds(0, 0, 40, 12)

	// Every navigation on an empty list must be a no-op instead of a panic.
	m.Up()
	m.Down()
	m.Home()
	m.End()
	m.PageUp()
	m.PageDown()
	m.Wheel(1)

	if _, ok := m.Selected(); ok {
		t.Error("Selected reported a host in an empty list")
	}
	if _, ok := m.RowAt(0, 2); ok {
		t.Error("RowAt reported a host in an empty list")
	}
}

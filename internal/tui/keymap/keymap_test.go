package keymap

import (
	"strings"
	"testing"
)

func TestDefaultCatalogIsConsistent(t *testing.T) {
	registry := Default()

	bindings := registry.All()
	if len(bindings) == 0 {
		t.Fatal("Default() returned an empty catalog")
	}

	seen := make(map[Action]bool, len(bindings))
	for _, binding := range bindings {
		if seen[binding.Action] {
			t.Errorf("action %q appears twice", binding.Action)
		}
		seen[binding.Action] = true

		if len(binding.Keys) == 0 && len(binding.FixedKeys) == 0 {
			t.Errorf("action %q has no keys", binding.Action)
		}
		if binding.Desc == "" {
			t.Errorf("action %q has no description", binding.Action)
		}
	}
}

func TestDefaultDoesNotShareSlices(t *testing.T) {
	first := Default()
	second := Default()

	// Mutating the slice returned by one registry must never reach the other. The
	// test lives in the package, so it touches the internal slice on purpose.
	first.bindings[0].Keys[0] = "sabotage"

	if second.Keys(ActionQuit)[0] == "sabotage" {
		t.Fatal("Default() shares the key slices between registries")
	}
	if catalog[0].Keys[0] == "sabotage" {
		t.Fatal("Default() shares the key slices with the static catalog")
	}
}

func TestMatchResolvesPerScope(t *testing.T) {
	registry := Default()

	cases := []struct {
		scope  Scope
		key    string
		action Action
	}{
		{scope: ScopeRoot, key: "q", action: ActionQuit},
		{scope: ScopeRoot, key: "ctrl+c", action: ActionQuit},
		{scope: ScopeRoot, key: "1", action: ActionTabHosts},
		{scope: ScopeRoot, key: "2", action: ActionTabTunnels},
		{scope: ScopeRoot, key: "3", action: ActionTabConfig},
		{scope: ScopeHosts, key: "enter", action: ActionHostConnect},
		{scope: ScopeHosts, key: "t", action: ActionHostTunnel},
		{scope: ScopeHosts, key: "T", action: ActionHostTags},
		{scope: ScopeHosts, key: "/", action: ActionHostFilter},
		{scope: ScopeTunnels, key: "x", action: ActionTunnelStop},
		{scope: ScopeTunnelLogs, key: "esc", action: ActionLogsClose},
		{scope: ScopeConfigView, key: "r", action: ActionConfigRefresh},
	}

	for _, tc := range cases {
		action, ok := registry.Match(tc.scope, tc.key)
		if !ok || action != tc.action {
			t.Errorf("Match(%q, %q) = %q, %v; want %q", tc.scope, tc.key, action, ok, tc.action)
		}
	}
}

func TestMatchIsScoped(t *testing.T) {
	registry := Default()

	// The same key means different things in different scopes.
	rootAction, rootOK := registry.Match(ScopeRoot, "q")
	tunnelAction, tunnelOK := registry.Match(ScopeTunnels, "q")

	if !rootOK || rootAction != ActionQuit {
		t.Errorf("root q = %q, %v", rootAction, rootOK)
	}
	if tunnelOK {
		t.Errorf("tunnels q = %q, want no binding", tunnelAction)
	}
}

func TestApplyOverrides(t *testing.T) {
	registry, err := Default().Apply(map[string][]string{
		"quit":        {"Q"},
		"host_filter": {"f", "ctrl+f"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := registry.Label(ActionQuit); got != "Q / ctrl+c" {
		t.Errorf("quit label = %q, want the override plus the fixed key", got)
	}
	if got := registry.Label(ActionHostFilter); got != "f / ctrl+f" {
		t.Errorf("host_filter label = %q", got)
	}

	if _, ok := registry.Match(ScopeHosts, "f"); !ok {
		t.Error("the overridden key should match")
	}
	if _, ok := registry.Match(ScopeHosts, "/"); ok {
		t.Error("the replaced key should no longer match")
	}

	// The default registry is untouched.
	if got := Default().Label(ActionQuit); got != "q / ctrl+c" {
		t.Errorf("Default() was mutated: quit label = %q", got)
	}
}

func TestApplyRejectsUnknownAction(t *testing.T) {
	if _, err := Default().Apply(map[string][]string{"nope": {"x"}}); err == nil {
		t.Fatal("Apply accepted an unknown action")
	}
}

func TestApplyRejectsFixedAction(t *testing.T) {
	if _, err := Default().Apply(map[string][]string{"logs_close": {"x"}}); err == nil {
		t.Fatal("Apply overrode a fixed action")
	}
}

func TestApplyRejectsReservedKeys(t *testing.T) {
	for _, key := range []string{"esc", "enter", "ctrl+c"} {
		if _, err := Default().Apply(map[string][]string{"host_filter": {key}}); err == nil {
			t.Errorf("Apply accepted the reserved key %q", key)
		}
	}
}

func TestApplyRejectsEmptyAndInvalid(t *testing.T) {
	if _, err := Default().Apply(map[string][]string{"quit": {}}); err == nil {
		t.Error("Apply accepted an empty binding")
	}
	if _, err := Default().Apply(map[string][]string{"quit": {"not a key"}}); err == nil {
		t.Error("Apply accepted an invalid key")
	}
}

func TestApplyDeduplicates(t *testing.T) {
	registry, err := Default().Apply(map[string][]string{"quit": {"q", "q"}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := registry.Keys(ActionQuit); len(got) != 2 || got[0] != "q" {
		t.Errorf("quit keys = %v, want [q ctrl+c]", got)
	}
}

func TestCanonicalKey(t *testing.T) {
	cases := map[string]string{
		" ":        "space",
		"esc":      "esc",
		"escape":   "esc",
		"return":   "enter",
		"pageup":   "pgup",
		"E":        "E",
		"shift+UP": "shift+up",
		"Ctrl+S":   "ctrl+s",
		"  q  ":    "q",
		"":         "",
	}

	for raw, want := range cases {
		if got := CanonicalKey(raw); got != want {
			t.Errorf("CanonicalKey(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseKey(t *testing.T) {
	valid := []string{"q", "Q", "ctrl+s", "alt+x", "tab", "shift+tab", "shift+left", "f2", "space", "up"}
	for _, raw := range valid {
		if _, err := ParseKey(raw); err != nil {
			t.Errorf("ParseKey(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []string{"", "  ", "shift+x", "ctrl+ab", "ctrl+", "not a key", "shift+down"}
	for _, raw := range invalid {
		if _, err := ParseKey(raw); err == nil {
			t.Errorf("ParseKey(%q) accepted an invalid key", raw)
		}
	}
}

func TestBindingsAreScopedAndCopied(t *testing.T) {
	registry := Default()

	root := registry.Bindings(ScopeRoot)
	if len(root) == 0 {
		t.Fatal("Bindings(root) is empty")
	}
	for _, binding := range root {
		if binding.Scope != ScopeRoot {
			t.Errorf("Bindings(root) returned %q from scope %q", binding.Action, binding.Scope)
		}
	}

	root[0].Keys[0] = "sabotage"
	if Default().Bindings(ScopeRoot)[0].Keys[0] == "sabotage" {
		t.Error("Bindings shares its slices with the registry")
	}
}

func TestConfigurableExcludesFixed(t *testing.T) {
	registry := Default()

	for _, binding := range registry.Configurable() {
		if !binding.Configurable {
			t.Errorf("%q is not configurable but was returned", binding.Action)
		}
	}
}

func TestHelpCoversEveryScope(t *testing.T) {
	// The help overlay documents these scopes; each one must expose bindings.
	scopes := []Scope{ScopeRoot, ScopeHosts, ScopeTunnels, ScopeConfigView}
	for _, scope := range scopes {
		if len(Default().Bindings(scope)) == 0 {
			t.Errorf("scope %q has no bindings to document", scope)
		}
	}
}

func TestLabelsAreNotEmpty(t *testing.T) {
	for _, binding := range Default().All() {
		if strings.TrimSpace(Default().Label(binding.Action)) == "" {
			t.Errorf("action %q has an empty label", binding.Action)
		}
	}
}

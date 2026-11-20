// Package keymap centralizes the keyboard catalog of the TUI: the default
// bindings per scope, the canonical key parser, the validation of user
// overrides, the conflicts inside a scope and the reserved keys.
//
// Production code only depends on the standard library: the parser understands
// the canonical strings produced by Bubble Tea's KeyPressMsg.
package keymap

import (
	"fmt"
	"sort"
	"strings"
)

// Scope identifies a keyboard context: a screen, a panel or a modal.
type Scope string

// Scopes of the application.
const (
	ScopeRoot           Scope = "root"
	ScopeHosts          Scope = "hosts"
	ScopeHostsTags      Scope = "hosts_tags"
	ScopeHostsForward   Scope = "hosts_forward"
	ScopeHostsConfirm   Scope = "hosts_confirm"
	ScopeTunnels        Scope = "tunnels"
	ScopeTunnelsConfirm Scope = "tunnels_confirm"
	ScopeTunnelLogs     Scope = "tunnel_logs"
	ScopeConfigView     Scope = "config_view"
)

// Action is the stable identifier of one user-facing command.
type Action string

// Actions of the root scope.
const (
	ActionQuit         Action = "quit"
	ActionHelp         Action = "help"
	ActionSettings     Action = "settings"
	ActionTabHosts     Action = "tab_hosts"
	ActionTabTunnels   Action = "tab_tunnels"
	ActionTabConfig    Action = "tab_config"
	ActionFocusNext    Action = "focus_next"
	ActionFocusPrev    Action = "focus_prev"
	ActionToggleList   Action = "toggle_list"
	ActionListShrink   Action = "list_shrink"
	ActionListGrow     Action = "list_grow"
	ActionDetailLayout Action = "detail_layout_toggle"
)

// Actions of the Hosts scope.
const (
	ActionHostUp          Action = "host_up"
	ActionHostDown        Action = "host_down"
	ActionHostPageUp      Action = "host_page_up"
	ActionHostPageDown    Action = "host_page_down"
	ActionHostHome        Action = "host_home"
	ActionHostEnd         Action = "host_end"
	ActionHostFilter      Action = "host_filter"
	ActionHostClearFilter Action = "host_clear_filter"
	ActionHostConnect     Action = "host_connect"
	ActionHostTunnel      Action = "host_tunnel"
	ActionHostTags        Action = "host_tags"
	ActionHostForward     Action = "host_forward"
	ActionHostCopy        Action = "host_copy"
	ActionHostExternal    Action = "host_external"
	ActionHostRefresh     Action = "host_refresh"
	ActionHostHelp        Action = "host_help"
	ActionTagNext         Action = "tag_next"
	ActionTagPrev         Action = "tag_prev"
)

// Actions of the Hosts modal scopes.
const (
	ActionTagsSave       Action = "tags_save"
	ActionTagsCancel     Action = "tags_cancel"
	ActionForwardSave    Action = "forward_save"
	ActionForwardCancel  Action = "forward_cancel"
	ActionHostConfirmYes Action = "host_confirm_yes"
	ActionHostConfirmNo  Action = "host_confirm_no"
)

// Actions of the Tunnels scope.
const (
	ActionTunnelUp       Action = "tunnel_up"
	ActionTunnelDown     Action = "tunnel_down"
	ActionTunnelPageUp   Action = "tunnel_page_up"
	ActionTunnelPageDown Action = "tunnel_page_down"
	ActionTunnelHome     Action = "tunnel_home"
	ActionTunnelEnd      Action = "tunnel_end"
	ActionTunnelStart    Action = "tunnel_start"
	ActionTunnelStop     Action = "tunnel_stop"
	ActionTunnelRestart  Action = "tunnel_restart"
	ActionTunnelLogs     Action = "tunnel_logs"
	ActionTunnelScope    Action = "tunnel_scope"
	ActionTunnelRefresh  Action = "tunnel_refresh"
	ActionTunnelConnect  Action = "tunnel_connect"
	ActionTunnelHelp     Action = "tunnel_help"
)

// Actions of the Tunnels modal scopes.
const (
	ActionTunnelConfirmYes Action = "tunnel_confirm_yes"
	ActionTunnelConfirmNo  Action = "tunnel_confirm_no"
)

// Actions of the tunnel logs scope.
const (
	ActionLogsClose    Action = "logs_close"
	ActionLogsReload   Action = "logs_reload"
	ActionLogsUp       Action = "logs_up"
	ActionLogsDown     Action = "logs_down"
	ActionLogsPageUp   Action = "logs_page_up"
	ActionLogsPageDown Action = "logs_page_down"
	ActionLogsHome     Action = "logs_home"
	ActionLogsEnd      Action = "logs_end"
)

// Actions of the Config scope.
const (
	ActionConfigRefresh  Action = "config_refresh"
	ActionConfigUp       Action = "config_up"
	ActionConfigDown     Action = "config_down"
	ActionConfigPageUp   Action = "config_page_up"
	ActionConfigPageDown Action = "config_page_down"
	ActionConfigHome     Action = "config_home"
	ActionConfigEnd      Action = "config_end"
	ActionConfigEdit     Action = "config_edit"
)

// Binding is one action inside a scope together with its keys and description.
//
// Keys are the configurable keys; FixedKeys are always active and cannot be
// overridden. Configurable reports whether the user may replace Keys.
type Binding struct {
	Action       Action
	Scope        Scope
	Keys         []string
	FixedKeys    []string
	Desc         string
	Configurable bool
}

// Registry is an immutable catalog indexed by action and by scope. The zero
// value is not usable: build it with Default or Apply.
type Registry struct {
	bindings []Binding
	actions  map[Action]int
	scopes   map[Scope]map[string]Action
}

// Default returns the built-in registry. It is the only source of defaults and
// descriptions, and it never shares slices with the returned value.
func Default() Registry {
	return mustRegistry(cloneBindings(catalog))
}

// Apply returns a new registry with overrides applied on top of r. It never
// mutates r nor the defaults, so registries can be chained. Overrides are
// validated in action order to keep the reported error deterministic.
func (r Registry) Apply(overrides map[string][]string) (Registry, error) {
	bindings := cloneBindings(r.bindings)

	for _, name := range sortedActions(overrides) {
		action := Action(name)

		index, ok := r.actions[action]
		if !ok {
			return Registry{}, fmt.Errorf("keymap: acción desconocida %q", name)
		}

		binding := &bindings[index]
		if !binding.Configurable {
			return Registry{}, fmt.Errorf("keymap: la acción %q no es configurable", name)
		}

		keys, err := normalizeKeys(action, overrides[name])
		if err != nil {
			return Registry{}, err
		}

		binding.Keys = keys
	}

	return newRegistry(bindings)
}

// Match canonicalizes key and resolves it inside scope.
func (r Registry) Match(scope Scope, key string) (Action, bool) {
	canonical := CanonicalKey(key)
	if canonical == "" {
		return "", false
	}

	keys, ok := r.scopes[scope]
	if !ok {
		return "", false
	}

	action, ok := keys[canonical]

	return action, ok
}

// Keys returns the effective keys of action (configurable plus fixed) in order.
// The result is always a fresh slice: callers never see the internal one.
func (r Registry) Keys(action Action) []string {
	index, ok := r.actions[action]
	if !ok {
		return nil
	}

	return effectiveKeys(r.bindings[index])
}

// Label joins the effective keys of action with " / ".
func (r Registry) Label(action Action) string {
	return strings.Join(r.Keys(action), " / ")
}

// Bindings returns the bindings of scope in catalog order, with copied slices.
func (r Registry) Bindings(scope Scope) []Binding {
	bindings := make([]Binding, 0, len(r.bindings))
	for _, binding := range r.bindings {
		if binding.Scope == scope {
			bindings = append(bindings, cloneBinding(binding))
		}
	}

	return bindings
}

// Configurable returns only the bindings the user may override, in catalog order.
func (r Registry) Configurable() []Binding {
	bindings := make([]Binding, 0, len(r.bindings))
	for _, binding := range r.bindings {
		if binding.Configurable {
			bindings = append(bindings, cloneBinding(binding))
		}
	}

	return bindings
}

// All returns every binding in catalog order, with copied slices.
func (r Registry) All() []Binding {
	bindings := make([]Binding, 0, len(r.bindings))
	for _, binding := range r.bindings {
		bindings = append(bindings, cloneBinding(binding))
	}

	return bindings
}

// reservedKeys cannot be used by user overrides; the defaults may still use them.
var reservedKeys = map[string]struct{}{
	"ctrl+c": {},
	"esc":    {},
	"enter":  {},
}

// catalog is the single source of the default bindings, in display order.
var catalog = []Binding{
	// root
	{Action: ActionQuit, Scope: ScopeRoot, Keys: []string{"q"}, FixedKeys: []string{"ctrl+c"}, Desc: "salir de la aplicación", Configurable: true},
	{Action: ActionHelp, Scope: ScopeRoot, Keys: []string{"?"}, Desc: "abrir la ayuda", Configurable: true},
	{Action: ActionSettings, Scope: ScopeRoot, Keys: []string{","}, Desc: "ver la configuración", Configurable: true},
	{Action: ActionTabHosts, Scope: ScopeRoot, Keys: []string{"1"}, Desc: "pestaña de hosts", Configurable: true},
	{Action: ActionTabTunnels, Scope: ScopeRoot, Keys: []string{"2"}, Desc: "pestaña de túneles", Configurable: true},
	{Action: ActionTabConfig, Scope: ScopeRoot, Keys: []string{"3"}, Desc: "pestaña del config", Configurable: true},
	{Action: ActionFocusNext, Scope: ScopeRoot, Keys: []string{"tab"}, Desc: "siguiente panel", Configurable: true},
	{Action: ActionFocusPrev, Scope: ScopeRoot, Keys: []string{"shift+tab"}, Desc: "panel anterior", Configurable: true},
	{Action: ActionToggleList, Scope: ScopeRoot, Keys: []string{"f2"}, Desc: "minimizar o mostrar la lista", Configurable: true},
	{Action: ActionListShrink, Scope: ScopeRoot, Keys: []string{"shift+left"}, Desc: "reducir el ancho de la lista", Configurable: true},
	{Action: ActionListGrow, Scope: ScopeRoot, Keys: []string{"shift+right"}, Desc: "ampliar el ancho de la lista", Configurable: true},
	{Action: ActionDetailLayout, Scope: ScopeRoot, Keys: []string{"L"}, Desc: "alternar una o dos columnas", Configurable: true},

	// hosts
	{Action: ActionHostUp, Scope: ScopeHosts, Keys: []string{"up", "k"}, Desc: "subir", Configurable: true},
	{Action: ActionHostDown, Scope: ScopeHosts, Keys: []string{"down", "j"}, Desc: "bajar", Configurable: true},
	{Action: ActionHostPageUp, Scope: ScopeHosts, Keys: []string{"pgup"}, Desc: "página arriba", Configurable: true},
	{Action: ActionHostPageDown, Scope: ScopeHosts, Keys: []string{"pgdown"}, Desc: "página abajo", Configurable: true},
	{Action: ActionHostHome, Scope: ScopeHosts, Keys: []string{"home"}, Desc: "inicio de la lista", Configurable: true},
	{Action: ActionHostEnd, Scope: ScopeHosts, Keys: []string{"end"}, Desc: "fin de la lista", Configurable: true},
	{Action: ActionHostFilter, Scope: ScopeHosts, Keys: []string{"/"}, Desc: "filtrar hosts", Configurable: true},
	{Action: ActionHostClearFilter, Scope: ScopeHosts, Keys: []string{"esc"}, Desc: "limpiar el filtro", Configurable: true},
	{Action: ActionHostConnect, Scope: ScopeHosts, Keys: []string{"enter"}, Desc: "abrir sesión ssh", Configurable: true},
	{Action: ActionHostTunnel, Scope: ScopeHosts, Keys: []string{"t"}, Desc: "levantar un túnel", Configurable: true},
	{Action: ActionHostTags, Scope: ScopeHosts, Keys: []string{"T"}, Desc: "editar etiquetas", Configurable: true},
	{Action: ActionHostForward, Scope: ScopeHosts, Keys: []string{"n"}, Desc: "declarar un forward", Configurable: true},
	{Action: ActionHostCopy, Scope: ScopeHosts, Keys: []string{"y"}, Desc: "copiar el comando ssh", Configurable: true},
	{Action: ActionHostExternal, Scope: ScopeHosts, Keys: []string{"E"}, Desc: "abrir el config en $EDITOR", Configurable: true},
	{Action: ActionHostRefresh, Scope: ScopeHosts, Keys: []string{"r"}, Desc: "recargar hosts", Configurable: true},
	{Action: ActionHostHelp, Scope: ScopeHosts, Keys: []string{"?"}, Desc: "ayuda de hosts", Configurable: true},
	{Action: ActionTagNext, Scope: ScopeHosts, Keys: []string{"]"}, Desc: "siguiente etiqueta", Configurable: true},
	{Action: ActionTagPrev, Scope: ScopeHosts, Keys: []string{"["}, Desc: "etiqueta anterior", Configurable: true},

	// hosts tags modal
	{Action: ActionTagsSave, Scope: ScopeHostsTags, Keys: []string{"ctrl+s"}, Desc: "guardar etiquetas", Configurable: false},
	{Action: ActionTagsCancel, Scope: ScopeHostsTags, Keys: []string{"esc"}, Desc: "cancelar etiquetas", Configurable: false},

	// hosts forward modal
	{Action: ActionForwardSave, Scope: ScopeHostsForward, Keys: []string{"ctrl+s"}, Desc: "guardar forward", Configurable: false},
	{Action: ActionForwardCancel, Scope: ScopeHostsForward, Keys: []string{"esc"}, Desc: "cancelar forward", Configurable: false},

	// hosts confirm
	{Action: ActionHostConfirmYes, Scope: ScopeHostsConfirm, Keys: []string{"y", "enter"}, Desc: "confirmar", Configurable: false},
	{Action: ActionHostConfirmNo, Scope: ScopeHostsConfirm, Keys: []string{"n", "esc"}, Desc: "cancelar", Configurable: false},

	// tunnels
	{Action: ActionTunnelUp, Scope: ScopeTunnels, Keys: []string{"k", "up"}, Desc: "subir", Configurable: true},
	{Action: ActionTunnelDown, Scope: ScopeTunnels, Keys: []string{"j", "down"}, Desc: "bajar", Configurable: true},
	{Action: ActionTunnelPageUp, Scope: ScopeTunnels, Keys: []string{"pgup"}, Desc: "página arriba", Configurable: true},
	{Action: ActionTunnelPageDown, Scope: ScopeTunnels, Keys: []string{"pgdown"}, Desc: "página abajo", Configurable: true},
	{Action: ActionTunnelHome, Scope: ScopeTunnels, Keys: []string{"home"}, Desc: "inicio de la lista", Configurable: true},
	{Action: ActionTunnelEnd, Scope: ScopeTunnels, Keys: []string{"end"}, Desc: "fin de la lista", Configurable: true},
	{Action: ActionTunnelStart, Scope: ScopeTunnels, Keys: []string{"s"}, Desc: "levantar el túnel", Configurable: true},
	{Action: ActionTunnelStop, Scope: ScopeTunnels, Keys: []string{"x"}, Desc: "detener el túnel", Configurable: true},
	{Action: ActionTunnelRestart, Scope: ScopeTunnels, Keys: []string{"R"}, Desc: "reiniciar el túnel", Configurable: true},
	{Action: ActionTunnelLogs, Scope: ScopeTunnels, Keys: []string{"l"}, Desc: "ver los logs del túnel", Configurable: true},
	{Action: ActionTunnelScope, Scope: ScopeTunnels, Keys: []string{"a"}, Desc: "alternar alcance", Configurable: true},
	{Action: ActionTunnelRefresh, Scope: ScopeTunnels, Keys: []string{"r"}, Desc: "refrescar", Configurable: true},
	{Action: ActionTunnelConnect, Scope: ScopeTunnels, Keys: []string{"enter"}, Desc: "abrir sesión ssh", Configurable: true},
	{Action: ActionTunnelHelp, Scope: ScopeTunnels, Keys: []string{"?"}, Desc: "ayuda de túneles", Configurable: true},

	// tunnels confirm
	{Action: ActionTunnelConfirmYes, Scope: ScopeTunnelsConfirm, Keys: []string{"y", "enter"}, Desc: "confirmar", Configurable: false},
	{Action: ActionTunnelConfirmNo, Scope: ScopeTunnelsConfirm, Keys: []string{"n", "esc"}, Desc: "cancelar", Configurable: false},

	// tunnel logs
	{Action: ActionLogsClose, Scope: ScopeTunnelLogs, Keys: []string{"esc"}, Desc: "cerrar los logs", Configurable: false},
	{Action: ActionLogsReload, Scope: ScopeTunnelLogs, Keys: []string{"r"}, Desc: "recargar los logs", Configurable: false},
	{Action: ActionLogsUp, Scope: ScopeTunnelLogs, Keys: []string{"k", "up"}, Desc: "subir", Configurable: false},
	{Action: ActionLogsDown, Scope: ScopeTunnelLogs, Keys: []string{"j", "down"}, Desc: "bajar", Configurable: false},
	{Action: ActionLogsPageUp, Scope: ScopeTunnelLogs, Keys: []string{"pgup"}, Desc: "página arriba", Configurable: false},
	{Action: ActionLogsPageDown, Scope: ScopeTunnelLogs, Keys: []string{"pgdown"}, Desc: "página abajo", Configurable: false},
	{Action: ActionLogsHome, Scope: ScopeTunnelLogs, Keys: []string{"home"}, Desc: "inicio de los logs", Configurable: false},
	{Action: ActionLogsEnd, Scope: ScopeTunnelLogs, Keys: []string{"end"}, Desc: "fin de los logs", Configurable: false},

	// config view
	{Action: ActionConfigRefresh, Scope: ScopeConfigView, Keys: []string{"r"}, Desc: "recargar el config", Configurable: true},
	{Action: ActionConfigUp, Scope: ScopeConfigView, Keys: []string{"k", "up"}, Desc: "subir", Configurable: true},
	{Action: ActionConfigDown, Scope: ScopeConfigView, Keys: []string{"j", "down"}, Desc: "bajar", Configurable: true},
	{Action: ActionConfigPageUp, Scope: ScopeConfigView, Keys: []string{"pgup"}, Desc: "página arriba", Configurable: true},
	{Action: ActionConfigPageDown, Scope: ScopeConfigView, Keys: []string{"pgdown"}, Desc: "página abajo", Configurable: true},
	{Action: ActionConfigHome, Scope: ScopeConfigView, Keys: []string{"home"}, Desc: "inicio del archivo", Configurable: true},
	{Action: ActionConfigEnd, Scope: ScopeConfigView, Keys: []string{"end"}, Desc: "fin del archivo", Configurable: true},
	{Action: ActionConfigEdit, Scope: ScopeConfigView, Keys: []string{"E"}, Desc: "abrir el config en $EDITOR", Configurable: true},
}

// newRegistry indexes bindings by action and by scope, rejecting duplicates.
func newRegistry(bindings []Binding) (Registry, error) {
	registry := Registry{
		bindings: bindings,
		actions:  make(map[Action]int, len(bindings)),
	}

	for index, binding := range bindings {
		if _, ok := registry.actions[binding.Action]; ok {
			return Registry{}, fmt.Errorf("keymap: acción duplicada %q", binding.Action)
		}

		registry.actions[binding.Action] = index
	}

	scopes, err := indexScopes(bindings)
	if err != nil {
		return Registry{}, err
	}

	registry.scopes = scopes

	return registry, nil
}

// mustRegistry builds a registry and panics only when the static catalog is
// inconsistent, which is a programming error.
func mustRegistry(bindings []Binding) Registry {
	registry, err := newRegistry(bindings)
	if err != nil {
		panic("keymap: catálogo de defaults inválido: " + err.Error())
	}

	return registry
}

// indexScopes maps every effective key of every scope to its action and rejects
// two actions sharing a key inside the same scope.
func indexScopes(bindings []Binding) (map[Scope]map[string]Action, error) {
	scopes := make(map[Scope]map[string]Action, len(bindings))

	for _, binding := range bindings {
		keys := scopes[binding.Scope]
		if keys == nil {
			keys = make(map[string]Action, len(binding.Keys)+len(binding.FixedKeys))
			scopes[binding.Scope] = keys
		}

		for _, key := range effectiveKeys(binding) {
			if previous, ok := keys[key]; ok {
				return nil, fmt.Errorf(
					"keymap: conflicto en el scope %q: la tecla %q está asignada a %q y a %q",
					binding.Scope, key, previous, binding.Action,
				)
			}

			keys[key] = binding.Action
		}
	}

	return scopes, nil
}

// normalizeKeys validates one override list and returns the canonical, unique
// keys in order.
func normalizeKeys(action Action, raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("keymap: la acción %q necesita al menos una tecla", action)
	}

	keys := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))

	for _, item := range raw {
		key, err := ParseKey(item)
		if err != nil {
			return nil, fmt.Errorf("keymap: la acción %q: tecla inválida %q", action, item)
		}

		if _, reserved := reservedKeys[key]; reserved {
			return nil, fmt.Errorf("keymap: la acción %q: la tecla %q está reservada", action, key)
		}

		if _, duplicated := seen[key]; duplicated {
			continue
		}

		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	return keys, nil
}

// effectiveKeys returns the configurable keys followed by the fixed ones. The
// result is always a fresh slice.
func effectiveKeys(binding Binding) []string {
	keys := make([]string, 0, len(binding.Keys)+len(binding.FixedKeys))
	keys = append(keys, binding.Keys...)
	keys = append(keys, binding.FixedKeys...)

	return keys
}

// sortedActions returns the override keys in deterministic order.
func sortedActions(overrides map[string][]string) []string {
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func cloneBindings(bindings []Binding) []Binding {
	if bindings == nil {
		return nil
	}

	cloned := make([]Binding, len(bindings))
	for index, binding := range bindings {
		cloned[index] = cloneBinding(binding)
	}

	return cloned
}

func cloneBinding(binding Binding) Binding {
	binding.Keys = cloneStrings(binding.Keys)
	binding.FixedKeys = cloneStrings(binding.FixedKeys)

	return binding
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	cloned := make([]string, len(values))
	copy(cloned, values)

	return cloned
}

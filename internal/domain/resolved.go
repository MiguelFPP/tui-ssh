package domain

import (
	"fmt"
	"strings"
)

// ResolvedHost is the effective configuration of an alias as `ssh -G` reports it.
// It is authoritative: the TUI never reimplements the option resolution of ssh.
type ResolvedHost struct {
	Alias          string
	HostName       string
	User           string
	Port           int
	IdentityFiles  []string
	ProxyJump      string
	IdentitiesOnly bool
	Options        map[string][]string
}

// Approximate marks a ResolvedHost built from the literal directives of the block
// because the ssh binary could not be run.
type Resolved struct {
	Host        ResolvedHost
	Approximate bool
}

// ParseResolved turns the flat `key value` output of `ssh -G` into a ResolvedHost.
// Keys repeat: ssh prints every candidate value, most specific first, so the first
// occurrence of each key is the effective one.
func ParseResolved(alias string, lines []string) ResolvedHost {
	resolved := ResolvedHost{Alias: alias, Options: make(map[string][]string, len(lines))}

	for _, line := range lines {
		key, value := splitDirective(line)
		if key == "" {
			continue
		}

		if len(resolved.Options[key]) < MaxDirectives {
			resolved.Options[key] = append(resolved.Options[key], value)
		}

		switch key {
		case "hostname":
			if resolved.HostName == "" {
				resolved.HostName = value
			}
		case "user":
			if resolved.User == "" {
				resolved.User = value
			}
		case "port":
			if resolved.Port == 0 {
				resolved.Port = parsePort(value)
			}
		case "identityfile":
			resolved.IdentityFiles = append(resolved.IdentityFiles, value)
		case "proxyjump":
			if resolved.ProxyJump == "" {
				resolved.ProxyJump = value
			}
		case "identitiesonly":
			if value == "yes" {
				resolved.IdentitiesOnly = true
			}
		}
	}

	return resolved
}

// ApproximateResolved builds the best-effort resolution from the literal directives
// of a Host block. The UI flags it as approximate so nobody mistakes it for ssh's word.
func ApproximateResolved(host Host) ResolvedHost {
	resolved := ResolvedHost{Alias: host.Alias, HostName: host.Alias, Options: map[string][]string{}}

	for _, directive := range host.Declared {
		if len(resolved.Options[directive.Key]) < MaxDirectives {
			resolved.Options[directive.Key] = append(resolved.Options[directive.Key], directive.Value)
		}

		switch directive.Key {
		case "hostname":
			if resolved.HostName == host.Alias {
				resolved.HostName = directive.Value
			}
		case "user":
			resolved.User = directive.Value
		case "port":
			if port := parsePort(directive.Value); port > 0 {
				resolved.Port = port
			}
		case "identityfile":
			resolved.IdentityFiles = append(resolved.IdentityFiles, directive.Value)
		case "proxyjump":
			resolved.ProxyJump = directive.Value
		case "identitiesonly":
			resolved.IdentitiesOnly = directive.Value == "yes"
		}
	}

	if resolved.Port == 0 {
		resolved.Port = 22
	}
	if resolved.User == "" {
		resolved.User = "(el usuario del sistema)"
	}

	return resolved
}

// Destination renders user@host:port for the detail panel.
func (r ResolvedHost) Destination() string {
	host := r.HostName
	if host == "" {
		host = r.Alias
	}

	if r.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, r.Port)
	}
	if r.User != "" {
		return r.User + "@" + host
	}

	return host
}

// KeyValue is one row of the detail panel.
type KeyValue struct {
	Key   string
	Value string
}

// SummaryRows flattens the most useful fields of the resolution, in display order.
func (r ResolvedHost) SummaryRows() []KeyValue {
	rows := []KeyValue{
		{Key: "alias", Value: r.Alias},
		{Key: "hostname", Value: orDash(r.HostName)},
		{Key: "user", Value: orDash(r.User)},
	}

	if r.Port > 0 {
		rows = append(rows, KeyValue{Key: "port", Value: fmt.Sprintf("%d", r.Port)})
	} else {
		rows = append(rows, KeyValue{Key: "port", Value: "-"})
	}

	identity := "-"
	if len(r.IdentityFiles) > 0 {
		identity = strings.Join(r.IdentityFiles, ", ")
	}
	rows = append(rows, KeyValue{Key: "identity", Value: identity})

	rows = append(rows,
		KeyValue{Key: "proxyjump", Value: orDash(r.ProxyJump)},
		KeyValue{Key: "identitiesonly", Value: boolLabel(r.IdentitiesOnly)},
	)

	return rows
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}

	return value
}

func boolLabel(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}

// splitDirective splits one `ssh -G` line on the first run of whitespace.
func splitDirective(line string) (key, value string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", ""
	}

	index := strings.IndexAny(trimmed, " \t")
	if index < 0 {
		return strings.ToLower(trimmed), ""
	}

	return strings.ToLower(trimmed[:index]), strings.TrimSpace(trimmed[index+1:])
}

// parsePort reads a port, returning zero for anything that is not one.
func parsePort(raw string) int {
	port := 0
	digits := 0

	for _, r := range strings.TrimSpace(raw) {
		if r < '0' || r > '9' {
			return 0
		}

		port = port*10 + int(r-'0')
		digits++

		if port > 65535 {
			return 0
		}
	}

	if digits == 0 {
		return 0
	}

	return port
}

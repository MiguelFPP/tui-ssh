package domain

import (
	"fmt"
	"strings"
	"unicode"
)

// Pattern is one entry of a Host line: a literal alias or a glob, optionally negated.
// Wild reports whether the value carries `*` or `?`; a wild pattern never becomes a
// connectable host, it only documents defaults.
type Pattern struct {
	Value   string
	Wild    bool
	Negated bool
}

// NewPattern parses one token of a Host line. It never fails: a leading `!` marks
// negation and the presence of a glob character marks the pattern as wild.
func NewPattern(raw string) Pattern {
	value := strings.TrimSpace(raw)

	pattern := Pattern{}
	if strings.HasPrefix(value, "!") {
		pattern.Negated = true
		value = value[1:]
	}

	pattern.Value = value
	pattern.Wild = strings.ContainsAny(value, "*?")

	return pattern
}

// Match reports whether alias matches the pattern, ignoring the negation flag.
func (p Pattern) Match(alias string) bool {
	if p.Value == "" {
		return false
	}
	if !p.Wild {
		return p.Value == alias
	}

	return globMatch(p.Value, alias)
}

func (p Pattern) String() string {
	if p.Negated {
		return "!" + p.Value
	}

	return p.Value
}

// Directive is one literal option of a Host block, kept in source order.
// It is what the user wrote, not how ssh resolves it.
type Directive struct {
	Key   string
	Value string
	Line  int
}

// Host is one connectable alias of the ssh configuration, merged with the tags and
// the forward specifications stored outside it.
type Host struct {
	Alias      string
	Patterns   []Pattern
	SourceFile string
	SourceLine int
	Declared   []Directive
	Tags       []string
	Forwards   []ForwardSpec
}

// DeclaredValue returns the first literal value of key in the block, if any.
func (h Host) DeclaredValue(key string) (string, bool) {
	want := strings.ToLower(strings.TrimSpace(key))

	for _, directive := range h.Declared {
		if directive.Key == want {
			return directive.Value, true
		}
	}

	return "", false
}

// HasTag reports whether the alias carries tag, comparing case-insensitively.
func (h Host) HasTag(tag string) bool {
	want := strings.ToLower(strings.TrimSpace(tag))
	if want == "" {
		return false
	}

	for _, candidate := range h.Tags {
		if candidate == want {
			return true
		}
	}

	return false
}

// Matches reports whether the host answers a free-text filter: the alias, any tag
// or the literal HostName of the block. An empty filter matches everything.
func (h Host) Matches(filter string) bool {
	needle := strings.ToLower(strings.TrimSpace(filter))
	if needle == "" {
		return true
	}

	if strings.Contains(strings.ToLower(h.Alias), needle) {
		return true
	}
	for _, tag := range h.Tags {
		if strings.Contains(tag, needle) {
			return true
		}
	}
	if hostname, ok := h.DeclaredValue("hostname"); ok {
		return strings.Contains(strings.ToLower(hostname), needle)
	}

	return false
}

// Label names the host in the UI, falling back to the alias.
func (h Host) Label() string {
	if h.Alias != "" {
		return h.Alias
	}

	return "(sin alias)"
}

// ValidateAlias rejects aliases that could not be passed to ssh as one argument.
func ValidateAlias(alias string) error {
	trimmed := strings.TrimSpace(alias)
	switch {
	case trimmed == "":
		return fmt.Errorf("%w: el alias está vacío", ErrInvalidArgument)
	case len(trimmed) > MaxAliasBytes:
		return fmt.Errorf("%w: el alias supera %d bytes", ErrInvalidArgument, MaxAliasBytes)
	case strings.HasPrefix(trimmed, "-"):
		return fmt.Errorf("%w: el alias no puede empezar por guion", ErrInvalidArgument)
	}

	for _, r := range trimmed {
		if unicode.IsSpace(r) || r < ' ' || r == 0x7f {
			return fmt.Errorf("%w: el alias contiene espacios o caracteres de control", ErrInvalidArgument)
		}
	}

	return nil
}

// globMatch compares value against pattern supporting `*` and `?`, which is the
// subset of the ssh Host syntax that names an alias.
func globMatch(pattern, value string) bool {
	patternRunes := []rune(pattern)
	valueRunes := []rune(value)

	pi, vi := 0, 0
	star, mark := -1, 0

	for vi < len(valueRunes) {
		switch {
		case pi < len(patternRunes) && (patternRunes[pi] == '?' || patternRunes[pi] == valueRunes[vi]):
			pi++
			vi++
		case pi < len(patternRunes) && patternRunes[pi] == '*':
			star = pi
			mark = vi
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			vi = mark
		default:
			return false
		}
	}

	for pi < len(patternRunes) && patternRunes[pi] == '*' {
		pi++
	}

	return pi == len(patternRunes)
}

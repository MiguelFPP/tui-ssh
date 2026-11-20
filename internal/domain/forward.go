package domain

import (
	"fmt"
	"strings"
	"unicode"
)

// ForwardDirection is the kind of port forwarding a spec describes.
type ForwardDirection uint8

const (
	ForwardLocal ForwardDirection = iota
	ForwardRemote
	ForwardDynamic
)

// Flag returns the ssh flag of the direction: L, R or D.
func (d ForwardDirection) Flag() string {
	switch d {
	case ForwardRemote:
		return "R"
	case ForwardDynamic:
		return "D"
	default:
		return "L"
	}
}

// String returns the technical name of the direction, not a UI label.
func (d ForwardDirection) String() string {
	switch d {
	case ForwardRemote:
		return "remote"
	case ForwardDynamic:
		return "dynamic"
	default:
		return "local"
	}
}

// ParseDirection accepts the flag, the technical name and their common aliases.
func ParseDirection(raw string) (ForwardDirection, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "l", "local":
		return ForwardLocal, nil
	case "r", "remote":
		return ForwardRemote, nil
	case "d", "dynamic", "socks":
		return ForwardDynamic, nil
	default:
		return ForwardLocal, fmt.Errorf("%w: dirección %q no válida; usa L, R o D", ErrInvalidArgument, raw)
	}
}

// ForwardSpec is one declared port forwarding: where it listens and where it points.
// Bind.Port zero asks ssh for an ephemeral port, which is only meaningful for a
// local or a remote forward.
type ForwardSpec struct {
	Name      string
	Direction ForwardDirection
	Bind      BindAddr
	Target    TargetAddr
}

// BindAddr is the local side of a forward: address plus port.
type BindAddr struct {
	Addr string
	Port int
}

// TargetAddr is the destination of a forward; it stays empty for a dynamic one.
type TargetAddr struct {
	Host string
	Port int
}

// Validate checks the invariants the adapter relies on before building the ssh args.
func (s ForwardSpec) Validate() error {
	if len(s.Name) > MaxForwardNameBytes {
		return fmt.Errorf("%w: el nombre del forward supera %d bytes", ErrInvalidArgument, MaxForwardNameBytes)
	}
	for _, r := range s.Name {
		if unicode.IsSpace(r) || isControl(r) {
			return fmt.Errorf("%w: el nombre del forward contiene espacios o caracteres de control", ErrInvalidArgument)
		}
	}

	if s.Bind.Port < 0 || s.Bind.Port > 65535 {
		return fmt.Errorf("%w: el puerto de escucha debe estar entre 0 y 65535", ErrInvalidArgument)
	}
	if HasControlChars(s.Bind.Addr) || strings.ContainsAny(s.Bind.Addr, " \t") {
		return fmt.Errorf("%w: la dirección de escucha contiene espacios o caracteres de control", ErrInvalidArgument)
	}

	if s.Direction == ForwardDynamic {
		if s.Bind.Port == 0 {
			return fmt.Errorf("%w: un forward dinámico necesita un puerto de escucha", ErrInvalidArgument)
		}
		if s.Target.Host != "" || s.Target.Port != 0 {
			return fmt.Errorf("%w: un forward dinámico no lleva destino", ErrInvalidArgument)
		}

		return nil
	}

	// OpenSSH allocates the port itself only on the remote side: `-R 0:...` is
	// valid, while `-L addr:0:...` is rejected as an invalid specification.
	if s.Bind.Port == 0 && s.Direction == ForwardLocal {
		return fmt.Errorf("%w: un forward local necesita un puerto de escucha; el 0 solo vale en uno remoto", ErrInvalidArgument)
	}

	target := strings.TrimSpace(s.Target.Host)
	switch {
	case target == "":
		return fmt.Errorf("%w: falta el host de destino", ErrInvalidArgument)
	case HasControlChars(target), strings.ContainsAny(target, " \t"):
		return fmt.Errorf("%w: el host de destino contiene espacios o caracteres de control", ErrInvalidArgument)
	case s.Target.Port < 1 || s.Target.Port > 65535:
		return fmt.Errorf("%w: el puerto de destino debe estar entre 1 y 65535", ErrInvalidArgument)
	}

	return nil
}

// Normalize fills the defaults and lowercases the direction so two specs that mean
// the same thing compare equal.
func (s ForwardSpec) Normalize() ForwardSpec {
	s.Name = strings.TrimSpace(s.Name)
	s.Bind.Addr = strings.TrimSpace(s.Bind.Addr)
	s.Target.Host = strings.TrimSpace(s.Target.Host)

	if s.Bind.Addr == "" {
		if s.Direction == ForwardRemote {
			s.Bind.Addr = DefaultRemoteBind
		} else {
			s.Bind.Addr = DefaultLocalBind
		}
	}

	return s
}

// Spec renders the argument of the -L, -R or -D flag. A zero bind port is only
// valid for a remote forward, where it asks OpenSSH to allocate one.
func (s ForwardSpec) Spec() string {
	spec := s.Normalize()
	bind := formatHostPort(spec.Bind.Addr, spec.Bind.Port)

	if spec.Direction == ForwardDynamic {
		return bind
	}

	return bind + ":" + formatHostPort(spec.Target.Host, spec.Target.Port)
}

// Args returns the ssh flag and its value, always as two separate arguments.
func (s ForwardSpec) Args() []string {
	return []string{"-" + s.Direction.Flag(), s.Spec()}
}

// Label names the forward in the UI: the declared name, or the rendered spec.
func (s ForwardSpec) Label() string {
	if name := strings.TrimSpace(s.Name); name != "" {
		return name
	}

	return s.Spec()
}

// Summary describes the forward as one line: direction, spec and name.
func (s ForwardSpec) Summary() string {
	if name := strings.TrimSpace(s.Name); name != "" {
		return fmt.Sprintf("%s %s (%s)", s.Direction.Flag(), s.Spec(), name)
	}

	return s.Direction.Flag() + " " + s.Spec()
}

// ID builds the stable identifier of a tunnel for this host and spec. It is
// filesystem-safe, because it names the control socket of the master.
func (s ForwardSpec) ID(host string) string {
	spec := s.Normalize()

	return fmt.Sprintf("%s-%s%d-%d", sanitizeID(host), s.Direction.Flag(), spec.Bind.Port, spec.Target.Port)
}

// formatHostPort brackets IPv6 literals, which ssh requires inside a forward spec.
func formatHostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}

	return fmt.Sprintf("%s:%d", host, port)
}

// sanitizeID keeps the characters that are safe in a filename and collapses the rest.
func sanitizeID(raw string) string {
	const maxLen = 40

	var b strings.Builder
	b.Grow(min(len(raw), maxLen))

	for _, r := range raw {
		if b.Len() >= maxLen {
			break
		}

		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}

	if b.Len() == 0 {
		return "tunnel"
	}

	return b.String()
}

// ValidateForwardSpecs checks every spec and rejects duplicated identifiers.
func ValidateForwardSpecs(specs []ForwardSpec) error {
	if len(specs) > MaxForwardSpecs {
		return fmt.Errorf("%w: más de %d forwards", ErrInvalidArgument, MaxForwardSpecs)
	}

	for _, spec := range specs {
		if err := spec.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// ParseForwardSpec reads the compact form the TUI prompt accepts:
//
//	[name=]L [bindAddr:]bindPort:targetHost:targetPort
//	[name=]R [bindAddr:]bindPort:targetHost:targetPort
//	[name=]D [bindAddr:]bindPort
//
// The direction may also be written as local, remote or dynamic.
func ParseForwardSpec(raw string) (ForwardSpec, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ForwardSpec{}, fmt.Errorf("%w: el forward está vacío", ErrInvalidArgument)
	}
	if len(value) > 512 {
		return ForwardSpec{}, fmt.Errorf("%w: el forward es demasiado largo", ErrInvalidArgument)
	}

	spec := ForwardSpec{}

	// A leading name followed by `=` gives the spec a label in the UI.
	if index := strings.IndexByte(value, '='); index > 0 {
		name := strings.TrimSpace(value[:index])
		if !strings.ContainsAny(name, " \t") {
			spec.Name = name
			value = strings.TrimSpace(value[index+1:])
		}
	}

	fields := strings.Fields(value)
	if len(fields) != 2 {
		return ForwardSpec{}, fmt.Errorf("%w: usa «L 8080:host:80», «R 8080:host:80» o «D 1080»", ErrInvalidArgument)
	}

	direction, err := ParseDirection(fields[0])
	if err != nil {
		return ForwardSpec{}, err
	}
	spec.Direction = direction

	parts, err := splitSpec(fields[1])
	if err != nil {
		return ForwardSpec{}, err
	}

	if direction == ForwardDynamic {
		if len(parts) != 1 {
			return ForwardSpec{}, fmt.Errorf("%w: un forward dinámico solo lleva el puerto de escucha", ErrInvalidArgument)
		}

		spec.Bind = BindAddr{Addr: DefaultLocalBind, Port: mustPort(parts[0])}

		return spec.Normalize(), spec.Validate()
	}

	switch len(parts) {
	case 3:
		// bindPort:targetHost:targetPort
		bindPort, err := parseSpecPort(parts[0])
		if err != nil {
			return ForwardSpec{}, err
		}
		targetPort, err := parseSpecPort(parts[2])
		if err != nil {
			return ForwardSpec{}, err
		}

		spec.Bind = BindAddr{Addr: defaultBindFor(direction), Port: bindPort}
		spec.Target = TargetAddr{Host: parts[1], Port: targetPort}
	case 4:
		// bindAddr:bindPort:targetHost:targetPort
		bindPort, err := parseSpecPort(parts[1])
		if err != nil {
			return ForwardSpec{}, err
		}
		targetPort, err := parseSpecPort(parts[3])
		if err != nil {
			return ForwardSpec{}, err
		}

		spec.Bind = BindAddr{Addr: parts[0], Port: bindPort}
		spec.Target = TargetAddr{Host: parts[2], Port: targetPort}
	default:
		return ForwardSpec{}, fmt.Errorf("%w: usa «L [addr:]puerto:host:puerto»", ErrInvalidArgument)
	}

	return spec.Normalize(), spec.Validate()
}

// defaultBindFor is the address ssh assumes when the spec omits one.
func defaultBindFor(direction ForwardDirection) string {
	if direction == ForwardRemote {
		return DefaultRemoteBind
	}

	return DefaultLocalBind
}

// splitSpec splits a forward spec on `:`, honouring the brackets of an IPv6 literal.
func splitSpec(raw string) ([]string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, fmt.Errorf("%w: falta la especificación del forward", ErrInvalidArgument)
	}

	parts := make([]string, 0, 4)
	current := strings.Builder{}
	depth := 0

	for _, r := range value {
		switch r {
		case '[':
			depth++
			current.WriteRune(r)
		case ']':
			if depth == 0 {
				return nil, fmt.Errorf("%w: corchetes desbalanceados", ErrInvalidArgument)
			}
			depth--
			current.WriteRune(r)
		case ':':
			if depth > 0 {
				current.WriteRune(r)

				continue
			}
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}

	if depth != 0 {
		return nil, fmt.Errorf("%w: corchetes desbalanceados", ErrInvalidArgument)
	}
	parts = append(parts, strings.TrimSpace(current.String()))

	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("%w: el forward tiene un campo vacío", ErrInvalidArgument)
		}
	}

	return parts, nil
}

// parseSpecPort reads a port from a spec field.
func parseSpecPort(raw string) (int, error) {
	if !isAllDigits(raw) {
		return 0, fmt.Errorf("%w: %q no es un puerto", ErrInvalidArgument, raw)
	}

	port := 0
	for _, r := range raw {
		port = port*10 + int(r-'0')
		if port > 65535 {
			return 0, fmt.Errorf("%w: el puerto %q está fuera de rango", ErrInvalidArgument, raw)
		}
	}

	return port, nil
}

// mustPort reads a port that was already validated by the caller's shape check.
func mustPort(raw string) int {
	port, err := parseSpecPort(raw)
	if err != nil {
		return 0
	}

	return port
}

// isAllDigits reports whether raw is a non-empty run of decimal digits.
func isAllDigits(raw string) bool {
	if raw == "" {
		return false
	}

	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

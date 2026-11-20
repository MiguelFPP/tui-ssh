package domain

import "time"

// TunnelState is the lifecycle of one background forwarding master.
type TunnelState uint8

const (
	TunnelStopped TunnelState = iota
	TunnelStarting
	TunnelActive
	TunnelFailed
)

// String returns the technical state name, not a UI label.
func (s TunnelState) String() string {
	switch s {
	case TunnelStarting:
		return "starting"
	case TunnelActive:
		return "active"
	case TunnelFailed:
		return "failed"
	default:
		return "stopped"
	}
}

// Tunnel is one ssh master forwarding ports in the background, addressed by the
// control socket it was started with.
type Tunnel struct {
	ID            string
	Host          string
	Spec          ForwardSpec
	State         TunnelState
	Socket        string
	PID           int
	StartedAt     time.Time
	LogPath       string
	AllocatedPort int
	Error         string
}

// Running reports whether the master is believed to be alive.
func (t Tunnel) Running() bool {
	return t.State == TunnelActive || t.State == TunnelStarting
}

// Uptime returns how long the tunnel has been up, or zero when it never started.
func (t Tunnel) Uptime(now time.Time) time.Duration {
	if t.StartedAt.IsZero() || !t.Running() {
		return 0
	}
	if now.Before(t.StartedAt) {
		return 0
	}

	return now.Sub(t.StartedAt)
}

// EffectivePort is the port the tunnel really listens on: the allocated one when the
// spec asked for an ephemeral port.
func (t Tunnel) EffectivePort() int {
	if t.AllocatedPort > 0 {
		return t.AllocatedPort
	}

	return t.Spec.Normalize().Bind.Port
}

// TunnelLog is one bounded read of the stderr of a master.
type TunnelLog struct {
	Path      string
	Lines     []string
	Truncated bool
}

// ParseAllocatedPort extracts the ephemeral port ssh reports as
// "Allocated port 43218 for forward", so the UI can show where it really listens.
func ParseAllocatedPort(line string) (int, bool) {
	const marker = "Allocated port "

	index := indexOf(line, marker)
	if index < 0 {
		return 0, false
	}

	rest := line[index+len(marker):]

	port := 0
	digits := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}

		port = port*10 + int(r-'0')
		digits++
	}

	if digits == 0 || port < 1 || port > 65535 {
		return 0, false
	}

	return port, true
}

func indexOf(haystack, needle string) int {
	if len(needle) == 0 {
		return 0
	}

	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}

	return -1
}

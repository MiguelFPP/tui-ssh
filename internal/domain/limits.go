package domain

import "time"

// Hard limits applied by adapters before returning data to the TUI.
const (
	MaxConfigBytes      = 1 << 20
	MaxIncludeDepth     = 8
	MaxHosts            = 5000
	MaxDirectives       = 200
	MaxTagsPerHost      = 32
	MaxTagBytes         = 32
	MaxAliasBytes       = 255
	MaxTunnels          = 128
	MaxTunnelLogLines   = 1000
	MaxResolvedOptions  = 200
	MaxForwardSpecs     = 32
	MaxForwardNameBytes = 32
	MaxConfigLineBytes  = 4096

	// Timeouts of the external calls the adapters make.
	TunnelStartTimeout = 6 * time.Second
	TunnelCheckTimeout = 2 * time.Second
	TunnelStopTimeout  = 3 * time.Second
	ResolveTimeout     = 3 * time.Second
)

// Default bind addresses, matching what ssh assumes when the address is omitted.
const (
	DefaultLocalBind  = "127.0.0.1"
	DefaultRemoteBind = "localhost"
)

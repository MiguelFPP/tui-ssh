// Package fakes provides in-memory doubles of the ports for the tests of the TUI
// adapters and components. They have no I/O and no concurrency of their own.
package fakes

import (
	"context"
	"sync"
	"time"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

// Epoch is the fixed start time the fakes use, so a tunnel uptime is deterministic.
var Epoch = time.Unix(1_700_000_000, 0)

// SSHConfig is a fake of ports.SSHConfigService.
type SSHConfig struct {
	mu sync.Mutex

	HostsValue  []domain.Host
	HostsErr    error
	ReportValue domain.ConfigReport
	ReportErr   error
	SourceValue domain.ConfigSource
	SourceErr   error

	HostsCalls  int
	ReportCalls int
	SourceCalls int
}

// Hosts returns the configured hosts or error.
func (f *SSHConfig) Hosts(context.Context) ([]domain.Host, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.HostsCalls++
	if f.HostsErr != nil {
		return nil, f.HostsErr
	}

	out := make([]domain.Host, len(f.HostsValue))
	copy(out, f.HostsValue)

	return out, nil
}

// Report returns the configured report or error.
func (f *SSHConfig) Report(context.Context) (domain.ConfigReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ReportCalls++
	if f.ReportErr != nil {
		return domain.ConfigReport{}, f.ReportErr
	}

	return f.ReportValue, nil
}

// Source returns the configured raw source or error.
func (f *SSHConfig) Source(context.Context) (domain.ConfigSource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.SourceCalls++
	if f.SourceErr != nil {
		return domain.ConfigSource{}, f.SourceErr
	}

	return f.SourceValue, nil
}

// Resolver is a fake of ports.ResolverService.
type Resolver struct {
	mu sync.Mutex

	Value domain.Resolved
	Err   error

	Calls       int
	Invalidated []string
}

// Resolve returns the configured resolution or error.
func (f *Resolver) Resolve(_ context.Context, alias string) (domain.Resolved, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Calls++
	if f.Err != nil {
		return domain.Resolved{}, f.Err
	}

	value := f.Value
	if value.Host.Alias == "" {
		value.Host.Alias = alias
	}

	return value, nil
}

// Invalidate records the aliases the model dropped.
func (f *Resolver) Invalidate(alias string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Invalidated = append(f.Invalidated, alias)
}

// Tags is a fake of ports.TagService. The values are exposed as fields the tests
// may seed; the method names match the port.
type Tags struct {
	mu sync.Mutex

	Index domain.TagIndex
	Specs map[string][]domain.ForwardSpec
	Err   error

	TagsWrites     int
	ForwardsWrites int
}

// Tags returns a copy of the tag index.
func (f *Tags) Tags(context.Context) (domain.TagIndex, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return nil, f.Err
	}

	out := domain.TagIndex{}
	for alias, list := range f.Index {
		out[alias] = append([]string{}, list...)
	}

	return out, nil
}

// SetTags records the new list of alias.
func (f *Tags) SetTags(_ context.Context, alias string, list []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return f.Err
	}

	f.TagsWrites++
	if f.Index == nil {
		f.Index = domain.TagIndex{}
	}
	if len(list) == 0 {
		delete(f.Index, alias)
	} else {
		f.Index[alias] = append([]string{}, list...)
	}

	return nil
}

// Forwards returns a copy of the declared specs.
func (f *Tags) Forwards(context.Context) (map[string][]domain.ForwardSpec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return nil, f.Err
	}

	out := make(map[string][]domain.ForwardSpec, len(f.Specs))
	for alias, specs := range f.Specs {
		out[alias] = append([]domain.ForwardSpec{}, specs...)
	}

	return out, nil
}

// SetForwards records the new specs of alias.
func (f *Tags) SetForwards(_ context.Context, alias string, specs []domain.ForwardSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return f.Err
	}

	f.ForwardsWrites++
	if f.Specs == nil {
		f.Specs = make(map[string][]domain.ForwardSpec)
	}
	if len(specs) == 0 {
		delete(f.Specs, alias)
	} else {
		f.Specs[alias] = append([]domain.ForwardSpec{}, specs...)
	}

	return nil
}

// Session is a fake of ports.SessionService.
type Session struct {
	Next domain.SSHCommand
	Err  error

	Calls int
}

// Command returns the configured command or error.
func (f *Session) Command(_ context.Context, alias string) (domain.SSHCommand, error) {
	f.Calls++
	if f.Err != nil {
		return domain.SSHCommand{}, f.Err
	}

	command := f.Next
	if command.IsZero() {
		command = domain.SSHCommand{Binary: "ssh", Args: []string{alias}}
	}

	return command, nil
}

// Tunnels is a fake of ports.TunnelService.
type Tunnels struct {
	mu sync.Mutex

	Value   []domain.Tunnel
	Started domain.Tunnel
	Log     domain.TunnelLog

	// Err affects every method; the per-method fields narrow it to one.
	Err     error
	ListErr error
	LogsErr error

	ListCalls    int
	StartCalls   int
	StopCalls    int
	CheckCalls   int
	LogsCalls    int
	StopAllCalls int
	StartedWith  []string
	Stopped      []string
}

// List returns a copy of the configured tunnels.
func (f *Tunnels) List(context.Context) ([]domain.Tunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ListCalls++
	if f.Err != nil {
		return nil, f.Err
	}
	if f.ListErr != nil {
		return nil, f.ListErr
	}

	out := make([]domain.Tunnel, len(f.Value))
	copy(out, f.Value)

	return out, nil
}

// Start records the request and returns the configured tunnel.
func (f *Tunnels) Start(_ context.Context, host string, spec domain.ForwardSpec) (domain.Tunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.StartCalls++
	f.StartedWith = append(f.StartedWith, host)

	if f.Err != nil {
		return domain.Tunnel{}, f.Err
	}

	tunnel := f.Started
	if tunnel.ID == "" {
		tunnel = domain.Tunnel{
			ID:        spec.ID(host),
			Host:      host,
			Spec:      spec,
			State:     domain.TunnelActive,
			StartedAt: Epoch,
		}
	}
	f.Value = append(f.Value, tunnel)

	return tunnel, nil
}

// Stop records the id and removes it from the fake list.
func (f *Tunnels) Stop(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.StopCalls++
	f.Stopped = append(f.Stopped, id)

	if f.Err != nil {
		return f.Err
	}

	kept := f.Value[:0]
	for _, tunnel := range f.Value {
		if tunnel.ID != id {
			kept = append(kept, tunnel)
		}
	}
	f.Value = kept

	return nil
}

// Check returns the state of the tunnel, active when it is in the fake list.
func (f *Tunnels) Check(_ context.Context, id string) (domain.TunnelState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.CheckCalls++
	if f.Err != nil {
		return domain.TunnelStopped, f.Err
	}

	for _, tunnel := range f.Value {
		if tunnel.ID == id {
			return tunnel.State, nil
		}
	}

	return domain.TunnelStopped, nil
}

// Logs returns the configured log.
func (f *Tunnels) Logs(context.Context, string) (domain.TunnelLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.LogsCalls++
	if f.Err != nil {
		return domain.TunnelLog{}, f.Err
	}
	if f.LogsErr != nil {
		return domain.TunnelLog{}, f.LogsErr
	}

	return f.Log, nil
}

// StopAll stops every tunnel in the fake list.
func (f *Tunnels) StopAll(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.StopAllCalls++
	f.Value = nil

	return f.Err
}

// ConfigEditor is a fake of ports.ConfigEditorService.
type ConfigEditor struct {
	Next domain.SSHCommand
	Err  error

	Calls int
}

// Resolve returns the configured command or error.
func (f *ConfigEditor) Resolve(_ context.Context, path string) (domain.SSHCommand, error) {
	f.Calls++
	if f.Err != nil {
		return domain.SSHCommand{}, f.Err
	}

	command := f.Next
	if command.IsZero() {
		command = domain.SSHCommand{Binary: "vim", Args: []string{path}}
	}

	return command, nil
}

// Package tunnel owns the background port forwardings. Each one is an OpenSSH
// master process left in the background with a control socket of its own, so its
// life can be queried and ended with `ssh -O check` / `ssh -O exit` instead of
// guessing with signals and PIDs.
package tunnel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
	"github.com/MiguelFPP/tui-ssh/internal/ports"
)

const (
	manifestName = "tunnels.json"
	socketDir    = "tunnels"

	// maxControlPathBytes is the kernel limit of a unix socket path on Linux
	// (108 bytes including the terminator), kept short for macOS too.
	maxControlPathBytes = 100

	// logTailBytes bounds the stderr kept for each tunnel.
	logTailBytes = 128 << 10
)

// Service implements ports.TunnelService.
type Service struct {
	binary     string
	configPath string
	runtimeDir string

	// exec is the seam the tests replace; production runs the ssh binary.
	exec func(ctx context.Context, binary string, args []string, stdout, stderr *strings.Builder) error
	// spawn is the seam for the detached master; it returns once ssh forked.
	spawn func(ctx context.Context, binary string, args []string, logPath string) (int, error)

	mu      sync.Mutex
	tunnels map[string]domain.Tunnel

	// ops serialises the operations that touch one tunnel.
	ops map[string]*sync.Mutex
}

var _ ports.TunnelService = (*Service)(nil)

// New builds the service rooted at runtimeDir: sockets, logs and the manifest
// live there. The config path is passed to ssh with -F so a tunnel uses the very
// same configuration the panel lists. An empty directory disables persistence and
// only keeps the state in memory, which is what the tests use.
func New(binary, configPath, runtimeDir string) *Service {
	name := strings.TrimSpace(binary)
	if name == "" {
		name = "ssh"
	}

	service := &Service{
		binary:     name,
		configPath: strings.TrimSpace(configPath),
		runtimeDir: strings.TrimSpace(runtimeDir),
		tunnels:    make(map[string]domain.Tunnel, 8),
		ops:        make(map[string]*sync.Mutex, 8),
	}
	service.exec = runSSH
	service.spawn = spawnMaster

	return service
}

// List returns the tracked tunnels, reconciling them with the manifest on disk so
// a tunnel started by a previous run shows up again, and one whose master died is
// marked as stopped.
func (s *Service) List(ctx context.Context) ([]domain.Tunnel, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tunnel: List: %w", err)
	}

	if err := s.load(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	ids := make([]string, 0, len(s.tunnels))
	tunnels := make([]domain.Tunnel, 0, len(s.tunnels))

	for id, tunnel := range s.tunnels {
		ids = append(ids, id)
		tunnels = append(tunnels, tunnel)
	}
	s.mu.Unlock()

	// A tunnel whose master disappeared is reported as stopped, not silently dropped:
	// the user still wants to see it and be able to restart it.
	for index, tunnel := range tunnels {
		if !tunnel.Running() {
			continue
		}

		state, err := s.Check(ctx, tunnel.ID)
		if err != nil && !errors.Is(err, domain.ErrTunnelNotFound) {
			continue
		}

		tunnels[index].State = state
	}

	sort.Slice(tunnels, func(i, j int) bool {
		if tunnels[i].Host != tunnels[j].Host {
			return tunnels[i].Host < tunnels[j].Host
		}

		return tunnels[i].ID < tunnels[j].ID
	})

	return tunnels, nil
}

// Start launches a detached master for the spec and records it in the manifest.
func (s *Service) Start(ctx context.Context, host string, spec domain.ForwardSpec) (domain.Tunnel, error) {
	if err := domain.ValidateAlias(host); err != nil {
		return domain.Tunnel{}, err
	}
	if err := spec.Validate(); err != nil {
		return domain.Tunnel{}, err
	}

	spec = spec.Normalize()
	id := spec.ID(host)

	unlock := s.lockOp(id)
	defer unlock()

	if existing, ok := s.get(id); ok && existing.Running() {
		return domain.Tunnel{}, fmt.Errorf("%w: %s", domain.ErrTunnelExists, id)
	}

	socket, err := s.socketPath(id)
	if err != nil {
		return domain.Tunnel{}, err
	}
	// A leftover socket of a dead master makes ssh refuse to start.
	_ = os.Remove(socket)

	logPath := s.logPath(id)
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return domain.Tunnel{}, fmt.Errorf("%w: no se pudo crear %s: %w", domain.ErrPermissionDenied, filepath.Dir(socket), err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return domain.Tunnel{}, fmt.Errorf("%w: no se pudo crear %s: %w", domain.ErrPermissionDenied, filepath.Dir(logPath), err)
	}

	args := masterArgs(s.configPath, socket, spec, host)

	startCtx, cancel := context.WithTimeout(ctx, domain.TunnelStartTimeout)
	defer cancel()

	pid, err := s.spawn(startCtx, s.binary, args, logPath)
	if err != nil {
		tunnel := domain.Tunnel{
			ID:        id,
			Host:      host,
			Spec:      spec,
			State:     domain.TunnelFailed,
			Socket:    socket,
			LogPath:   logPath,
			StartedAt: time.Now(),
			Error:     describeFailure(err, logPath),
		}
		s.put(tunnel)
		_ = s.save(ctx)

		return tunnel, fmt.Errorf("%w: %s", domain.ErrTunnelFailed, tunnel.Error)
	}

	tunnel := domain.Tunnel{
		ID:        id,
		Host:      host,
		Spec:      spec,
		State:     domain.TunnelActive,
		Socket:    socket,
		PID:       pid,
		LogPath:   logPath,
		StartedAt: time.Now(),
	}

	// The ephemeral port is printed to stderr once the forward is set up, so it is
	// read back instead of being guessed.
	if port, ok := s.readAllocatedPort(logPath); ok {
		tunnel.AllocatedPort = port
	}

	if state, err := s.checkSocket(ctx, socket, host); err == nil {
		tunnel.State = state
	}

	s.put(tunnel)
	if err := s.save(ctx); err != nil {
		return tunnel, err
	}

	return tunnel, nil
}

// Stop ends the master through its control socket and removes its records.
func (s *Service) Stop(ctx context.Context, id string) error {
	unlock := s.lockOp(id)
	defer unlock()

	tunnel, ok := s.get(id)
	if !ok {
		return fmt.Errorf("%w: %s", domain.ErrTunnelNotFound, id)
	}

	stopCtx, cancel := context.WithTimeout(ctx, domain.TunnelStopTimeout)
	defer cancel()

	var out, errOut strings.Builder
	// -O exit ends the master; a master that is already gone is not an error.
	_ = s.exec(stopCtx, s.binary, s.controlArgs(tunnel.Socket, "exit", tunnel.Host), &out, &errOut)

	_ = os.Remove(tunnel.Socket)

	tunnel.State = domain.TunnelStopped
	tunnel.PID = 0
	s.put(tunnel)

	if err := s.save(ctx); err != nil {
		return err
	}

	return nil
}

// StopAll stops every known tunnel, one by one, reporting the first failure.
func (s *Service) StopAll(ctx context.Context) error {
	s.mu.Lock()
	ids := make([]string, 0, len(s.tunnels))
	for id, tunnel := range s.tunnels {
		if tunnel.Running() {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()

	sort.Strings(ids)

	var firstErr error
	for _, id := range ids {
		if err := s.Stop(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// Check asks the master whether it is still alive.
func (s *Service) Check(ctx context.Context, id string) (domain.TunnelState, error) {
	tunnel, ok := s.get(id)
	if !ok {
		return domain.TunnelStopped, fmt.Errorf("%w: %s", domain.ErrTunnelNotFound, id)
	}

	return s.checkSocket(ctx, tunnel.Socket, tunnel.Host)
}

// Logs reads the bounded stderr of the master.
func (s *Service) Logs(ctx context.Context, id string) (domain.TunnelLog, error) {
	if err := ctx.Err(); err != nil {
		return domain.TunnelLog{}, fmt.Errorf("tunnel: Logs: %w", err)
	}

	tunnel, ok := s.get(id)
	if !ok {
		return domain.TunnelLog{}, fmt.Errorf("%w: %s", domain.ErrTunnelNotFound, id)
	}

	data, err := readTail(tunnel.LogPath, logTailBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.TunnelLog{Path: tunnel.LogPath}, nil
		}

		return domain.TunnelLog{}, fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, tunnel.LogPath, err)
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	log := domain.TunnelLog{Path: tunnel.LogPath}
	if len(lines) > domain.MaxTunnelLogLines {
		lines = lines[len(lines)-domain.MaxTunnelLogLines:]
		log.Truncated = true
	}

	if len(data) >= logTailBytes {
		log.Truncated = true
	}

	log.Lines = lines

	return log, nil
}

// checkSocket runs `ssh -O check` against the control socket of the master.
func (s *Service) checkSocket(ctx context.Context, socket, host string) (domain.TunnelState, error) {
	if strings.TrimSpace(socket) == "" {
		return domain.TunnelStopped, nil
	}
	if _, err := os.Stat(socket); err != nil {
		return domain.TunnelStopped, nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, domain.TunnelCheckTimeout)
	defer cancel()

	var out, errOut strings.Builder
	if err := s.exec(checkCtx, s.binary, s.controlArgs(socket, "check", host), &out, &errOut); err != nil {
		return domain.TunnelStopped, nil
	}

	return domain.TunnelActive, nil
}

// controlArgs builds one `ssh -O <op>` invocation against the control socket. The
// config path travels with -F so the master is addressed with the same settings it
// was created with.
func (s *Service) controlArgs(socket, op, host string) []string {
	args := make([]string, 0, 8)
	if s.configPath != "" {
		args = append(args, "-F", s.configPath)
	}

	return append(args, "-S", socket, "-O", op, host)
}

// masterArgs builds the detached master invocation. Every option is an argument of
// its own: no shell, no interpolation.
func masterArgs(configPath, socket string, spec domain.ForwardSpec, host string) []string {
	args := make([]string, 0, 20)
	if configPath != "" {
		args = append(args, "-F", configPath)
	}

	args = append(args,
		"-M",         // the process becomes a control master
		"-S", socket, // the socket that identifies it
		"-f", // fork to the background after authenticating
		"-N", // no remote command: forwarding only
		"-o", "ControlPersist=yes",
		"-o", "ExitOnForwardFailure=yes", // a busy port fails now, not silently later
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
	)
	args = append(args, spec.Args()...)
	args = append(args, host)

	return args
}

// socketPath is the control socket of a tunnel. Unix sockets have a short path
// limit (108 bytes on Linux), so a deep runtime directory falls back to a stable
// directory under the system temp: the hash keeps it deterministic, which is what
// lets the next run re-adopt the master.
func (s *Service) socketPath(id string) (string, error) {
	if s.runtimeDir == "" {
		return "", fmt.Errorf("%w: no hay directorio de estado", domain.ErrInvalidPath)
	}

	candidate := filepath.Join(s.runtimeDir, socketDir, id+".sock")
	if len(candidate) <= maxControlPathBytes {
		return candidate, nil
	}

	dirSum := sha256.Sum256([]byte(s.runtimeDir))
	idSum := sha256.Sum256([]byte(id))

	return filepath.Join(os.TempDir(),
		"tui-ssh-"+hex.EncodeToString(dirSum[:4]),
		hex.EncodeToString(idSum[:8])+".sock"), nil
}

// logPath is the file that receives the stderr of the master.
func (s *Service) logPath(id string) string {
	if s.runtimeDir == "" {
		return ""
	}

	return filepath.Join(s.runtimeDir, socketDir, id+".log")
}

// readAllocatedPort scans the log for the line ssh prints when the bind port was 0.
func (s *Service) readAllocatedPort(path string) (int, bool) {
	data, err := readTail(path, logTailBytes)
	if err != nil {
		return 0, false
	}

	lines := strings.Split(string(data), "\n")
	// The last message wins: a restart may allocate a different port.
	for index := len(lines) - 1; index >= 0; index-- {
		if port, ok := domain.ParseAllocatedPort(lines[index]); ok {
			return port, true
		}
	}

	return 0, false
}

// get returns one tunnel by id.
func (s *Service) get(id string) (domain.Tunnel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tunnel, ok := s.tunnels[id]

	return tunnel, ok
}

// put stores one tunnel.
func (s *Service) put(tunnel domain.Tunnel) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tunnels == nil {
		s.tunnels = make(map[string]domain.Tunnel, 8)
	}
	s.tunnels[tunnel.ID] = tunnel
}

// lockOp serialises the operations of one tunnel.
func (s *Service) lockOp(id string) func() {
	s.mu.Lock()
	if s.ops == nil {
		s.ops = make(map[string]*sync.Mutex, 8)
	}
	op, ok := s.ops[id]
	if !ok {
		op = &sync.Mutex{}
		s.ops[id] = op
	}
	s.mu.Unlock()

	op.Lock()

	return op.Unlock
}

// describeFailure turns a failed start into one actionable line. The message ssh
// prints lives in the log, so the raw error is only the first source: a bare
// "exit status 255" carries no hint on its own.
func describeFailure(err error, logPath string) string {
	message := err.Error()
	if hint, ok := failureHint(message); ok {
		return hint
	}

	if logPath != "" {
		if data, readErr := readTail(logPath, 4096); readErr == nil {
			if hint, ok := failureHint(string(data)); ok {
				return hint
			}
		}

		return "el túnel no arrancó; revisa los logs"
	}

	return "el túnel no arrancó"
}

// failureHint maps the messages ssh prints to the sentence the panel shows.
func failureHint(message string) (string, bool) {
	switch {
	case strings.Contains(message, "Permission denied"),
		strings.Contains(message, "publickey"),
		strings.Contains(message, "passphrase"),
		strings.Contains(message, "Host key verification failed"):
		return "el host pide autenticación interactiva: abre una sesión ssh para autenticarte y reintenta", true
	case strings.Contains(message, "Address already in use"):
		return "el puerto ya está en uso", true
	case strings.Contains(message, "Could not resolve hostname"):
		return "no se pudo resolver el host", true
	case strings.Contains(message, "Connection refused"), strings.Contains(message, "timed out"):
		return "no se pudo conectar con el host", true
	case strings.Contains(message, "No such file or directory"):
		return "no se encontró el binario o el socket de ssh", true
	default:
		return "", false
	}
}

// readTail returns at most limit bytes from the end of the file.
func readTail(path string, limit int64) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, os.ErrNotExist
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	offset := int64(0)
	if size > limit {
		offset = size - limit
	}

	if _, err := file.Seek(offset, 0); err != nil {
		return nil, err
	}

	limited := io.LimitReader(file, limit)

	return io.ReadAll(limited)
}

// runSSH executes one ssh control command and captures both streams.
func runSSH(ctx context.Context, binary string, args []string, stdout, stderr *strings.Builder) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if stderr != nil {
		cmd.Stderr = stderr
	}

	return cmd.Run()
}

// spawnMaster runs the detached master. `-f` makes ssh fork once it authenticated,
// so the command returns as soon as the forwarding is set up, and any failure is
// reported on stderr before that fork.
func spawnMaster(ctx context.Context, binary string, args []string, logPath string) (int, error) {
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = file
	cmd.Stderr = file

	if err := cmd.Run(); err != nil {
		// The failure reason lives in the log file: it is the message ssh printed
		// before giving up, and it is what the panel shows.
		reason, _ := readTail(logPath, 4096)
		if trimmed := strings.TrimSpace(string(reason)); trimmed != "" {
			return 0, errors.New(trimmed)
		}

		return 0, err
	}

	return cmd.Process.Pid, nil
}

// manifestEntry is the on-disk record of one tunnel.
type manifestEntry struct {
	ID            string `json:"id"`
	Host          string `json:"host"`
	Direction     string `json:"direction"`
	Name          string `json:"name,omitempty"`
	BindAddr      string `json:"bind_addr"`
	BindPort      int    `json:"bind_port"`
	TargetHost    string `json:"target_host,omitempty"`
	TargetPort    int    `json:"target_port,omitempty"`
	PID           int    `json:"pid"`
	StartedAt     int64  `json:"started_at"`
	AllocatedPort int    `json:"allocated_port,omitempty"`
}

// manifestPath is the file that lets the next run re-adopt the live tunnels.
func (s *Service) manifestPath() string {
	if s.runtimeDir == "" {
		return ""
	}

	return filepath.Join(s.runtimeDir, manifestName)
}

// load merges the manifest with the state already in memory.
func (s *Service) load(ctx context.Context) error {
	path := s.manifestPath()
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, path, err)
	}

	entries := make([]manifestEntry, 0, 8)
	if err := decodeManifest(data, &entries); err != nil {
		return err
	}

	for _, entry := range entries {
		if _, ok := s.get(entry.ID); ok {
			continue
		}

		direction, err := domain.ParseDirection(entry.Direction)
		if err != nil {
			continue
		}

		spec := domain.ForwardSpec{
			Name:      entry.Name,
			Direction: direction,
			Bind:      domain.BindAddr{Addr: entry.BindAddr, Port: entry.BindPort},
			Target:    domain.TargetAddr{Host: entry.TargetHost, Port: entry.TargetPort},
		}.Normalize()

		socket, err := s.socketPath(entry.ID)
		if err != nil {
			continue
		}

		s.put(domain.Tunnel{
			ID:            entry.ID,
			Host:          entry.Host,
			Spec:          spec,
			State:         domain.TunnelActive,
			Socket:        socket,
			PID:           entry.PID,
			StartedAt:     time.Unix(entry.StartedAt, 0),
			LogPath:       s.logPath(entry.ID),
			AllocatedPort: entry.AllocatedPort,
		})
	}

	return nil
}

// save writes the manifest with only the running tunnels.
func (s *Service) save(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("tunnel: save: %w", err)
	}

	path := s.manifestPath()
	if path == "" {
		return nil
	}

	s.mu.Lock()
	entries := make([]manifestEntry, 0, len(s.tunnels))
	for _, tunnel := range s.tunnels {
		if !tunnel.Running() {
			continue
		}

		entries = append(entries, manifestEntry{
			ID:            tunnel.ID,
			Host:          tunnel.Host,
			Direction:     tunnel.Spec.Direction.String(),
			Name:          tunnel.Spec.Name,
			BindAddr:      tunnel.Spec.Normalize().Bind.Addr,
			BindPort:      tunnel.Spec.Normalize().Bind.Port,
			TargetHost:    tunnel.Spec.Target.Host,
			TargetPort:    tunnel.Spec.Target.Port,
			PID:           tunnel.PID,
			StartedAt:     tunnel.StartedAt.Unix(),
			AllocatedPort: tunnel.AllocatedPort,
		})
	}
	s.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	if len(entries) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, path, err)
		}

		return nil
	}

	data, err := encodeManifest(entries)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("%w: no se pudo crear %s: %w", domain.ErrPermissionDenied, filepath.Dir(path), err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tunnels-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, path, err)
	}

	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, name, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, name, err)
	}

	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("%w: %s: %w", domain.ErrPermissionDenied, path, err)
	}

	return nil
}

// encodeManifest serialises the entries with an indented, deterministic layout.
func encodeManifest(entries []manifestEntry) ([]byte, error) {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: manifiesto de túneles: %w", domain.ErrInvalidArgument, err)
	}

	return append(data, '\n'), nil
}

// decodeManifest parses the manifest, rejecting unknown fields and trailing data.
func decodeManifest(data []byte, out *[]manifestEntry) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: manifiesto de túneles: %w", domain.ErrConfigInvalid, err)
	}

	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: manifiesto de túneles: datos extra tras el array", domain.ErrConfigInvalid)
	}

	return nil
}

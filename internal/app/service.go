package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"codetap/internal/adapter/relay"
	"codetap/internal/domain"
)

// Config holds resolved runtime configuration for a session.
type Config struct {
	Name      string
	Commit    string
	Arch      string
	Folder    string
	SocketDir string
}

// Service orchestrates the codetap lifecycle.
type Service struct {
	downloader domain.Downloader
	extractor  domain.Extractor
	provision  domain.Provisioner
	runner     domain.ServerRunner
	store      domain.MetadataStore
	tokenGen   domain.TokenGenerator
	logger     domain.Logger
}

// NewService creates the application service with all dependencies injected.
func NewService(
	dl domain.Downloader,
	ex domain.Extractor,
	pr domain.Provisioner,
	sr domain.ServerRunner,
	st domain.MetadataStore,
	tg domain.TokenGenerator,
	lg domain.Logger,
) *Service {
	return &Service{
		downloader: dl,
		extractor:  ex,
		provision:  pr,
		runner:     sr,
		store:      st,
		tokenGen:   tg,
		logger:     lg,
	}
}

// sessionState holds the mutable state of a running session.
type sessionState struct {
	mu                sync.Mutex
	name              string
	commit            string
	arch              string
	folder            string
	token             string
	pid               int
	startedAt         time.Time
	leases            map[string]net.Conn // client_id → control conn
	waitFn            func() error        // set by doRestart for lifecycle goroutine
	stopFn            func()              // set by doRestart for lifecycle goroutine
	restartInProgress bool                // true while a version switch is in flight
}

// restartReq signals the lifecycle goroutine to restart code-server.
type restartReq struct {
	commit string
	result chan error
}

// Run starts a codetap session with the CTAP1 control socket protocol.
// It provisions the server, starts code-server on <name>.sock, listens on
// <name>.ctl.sock for INFO and CONNECT commands, and blocks until the
// server process exits.
func (s *Service) Run(cfg Config) error {
	s.logger.Info("starting session", "name", cfg.Name, "commit", cfg.Commit, "arch", cfg.Arch)

	if err := s.store.EnsureDir(); err != nil {
		return fmt.Errorf("ensure socket dir: %w", err)
	}

	binPath, err := s.Provision(cfg.Commit, cfg.Arch)
	if err != nil {
		return err
	}

	socketPath := s.store.SocketPath(cfg.Name)
	ctlSocketPath := s.store.CtlSocketPath(cfg.Name)

	// Prevent clobbering if a same-named session is already running.
	if _, err := os.Stat(ctlSocketPath); err == nil {
		if isSocketAliveNow(ctlSocketPath) {
			return fmt.Errorf(
				"session %q already running on %s; use a different --name or stop the existing session first",
				cfg.Name,
				ctlSocketPath,
			)
		}
		_ = os.Remove(ctlSocketPath)
	}
	_ = os.Remove(socketPath)

	// Generate connection token (in-memory, no file)
	token, err := s.tokenGen.Generate()
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	state := &sessionState{
		name:      cfg.Name,
		commit:    cfg.Commit,
		arch:      cfg.Arch,
		folder:    cfg.Folder,
		token:     token,
		pid:       os.Getpid(),
		startedAt: time.Now(),
		leases:    make(map[string]net.Conn),
	}

	// Start code-server
	wait, stop, err := s.runner.Start(binPath, socketPath, token)
	if err != nil {
		return err
	}

	// Wait for code-server to create the data socket before accepting clients.
	if err := waitForSocket(socketPath); err != nil {
		stop()
		return fmt.Errorf("server failed to start: %w", err)
	}
	s.logger.Info("code-server ready", "socket", socketPath)

	// Listen on control socket
	ctlListener, err := net.Listen("unix", ctlSocketPath)
	if err != nil {
		stop()
		return fmt.Errorf("listen ctl socket: %w", err)
	}

	// Cleanup on exit
	defer func() {
		s.logger.Info("cleaning up session", "name", cfg.Name)
		_ = ctlListener.Close()
		if err := s.store.Remove(cfg.Name); err != nil {
			s.logger.Error("cleanup failed", "name", cfg.Name, "err", err)
		}
	}()

	restartCh := make(chan restartReq)
	done := make(chan error, 1)

	// Lifecycle goroutine: watches code-server, handles restart requests.
	go func() {
		for {
			serverDone := make(chan error, 1)
			go func() {
				serverDone <- wait()
			}()

			select {
			case sErr := <-serverDone:
				// Server exited — check for pending restart.
				select {
				case req := <-restartCh:
					if restartErr := s.doRestart(req, state, socketPath); restartErr != nil {
						req.result <- restartErr
						done <- fmt.Errorf("restart failed: %w", restartErr)
						return
					}
					state.mu.Lock()
					wait = state.waitFn
					stop = state.stopFn
					state.mu.Unlock()
					req.result <- nil
					// loop: wait on new server
				default:
					done <- sErr
					return
				}
			case req := <-restartCh:
				// Restart while server still running.
				stop()
				<-serverDone
				if restartErr := s.doRestart(req, state, socketPath); restartErr != nil {
					req.result <- restartErr
					done <- fmt.Errorf("restart failed: %w", restartErr)
					return
				}
				state.mu.Lock()
				wait = state.waitFn
				stop = state.stopFn
				state.mu.Unlock()
				req.result <- nil
			}
		}
	}()

	// Accept control connections.
	go func() {
		for {
			conn, acceptErr := ctlListener.Accept()
			if acceptErr != nil {
				return
			}
			go s.handleCtlConn(conn, state, restartCh)
		}
	}()

	return <-done
}

// waitFn and stopFn are stored on sessionState during restart so the
// lifecycle goroutine can pick them up under the lock.
// We embed them as unexported fields set by doRestart.

// doRestart provisions and starts a new code-server, updating state.
func (s *Service) doRestart(req restartReq, state *sessionState, socketPath string) error {
	state.mu.Lock()
	arch := state.arch
	state.mu.Unlock()

	newBin, err := s.Provision(req.commit, arch)
	if err != nil {
		return fmt.Errorf("provision: %w", err)
	}

	newToken, err := s.tokenGen.Generate()
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	_ = os.Remove(socketPath)

	newWait, newStop, err := s.runner.Start(newBin, socketPath, newToken)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}

	if err := waitForSocket(socketPath); err != nil {
		newStop()
		return fmt.Errorf("server failed to start after restart: %w", err)
	}

	state.mu.Lock()
	state.commit = req.commit
	state.token = newToken
	state.startedAt = time.Now()
	state.waitFn = newWait
	state.stopFn = newStop
	state.mu.Unlock()

	s.logger.Info("code-server restarted", "commit", req.commit)
	return nil
}

// handleCtlConn dispatches a single control socket connection.
func (s *Service) handleCtlConn(conn net.Conn, state *sessionState, restartCh chan restartReq) {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return
	}
	line = strings.TrimSpace(line)

	switch {
	case line == "CTAP1 INFO":
		s.handleInfo(conn, state)
	case strings.HasPrefix(line, "CTAP1 CONNECT "):
		s.handleConnect(conn, state, line, restartCh)
	default:
		_, _ = fmt.Fprintf(conn, "ERR unknown command\n")
		_ = conn.Close()
	}
}

// handleInfo responds with session metadata JSON and closes the connection.
func (s *Service) handleInfo(conn net.Conn, state *sessionState) {
	defer conn.Close()

	state.mu.Lock()
	info := infoResponse{
		Name:      state.name,
		Commit:    state.commit,
		Arch:      state.arch,
		Folder:    state.folder,
		PID:       state.pid,
		StartedAt: state.startedAt.Format(time.RFC3339),
	}
	state.mu.Unlock()

	data, _ := json.Marshal(info)
	data = append(data, '\n')
	_, _ = conn.Write(data)
}

// infoResponse is the JSON payload for CTAP1 INFO.
type infoResponse struct {
	Name      string `json:"name"`
	Commit    string `json:"commit"`
	Arch      string `json:"arch"`
	Folder    string `json:"folder"`
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

// handleConnect performs version negotiation and keeps the connection open as a lease.
func (s *Service) handleConnect(conn net.Conn, state *sessionState, line string, restartCh chan restartReq) {
	parts := strings.Fields(line)
	if len(parts) != 4 {
		_, _ = fmt.Fprintf(conn, "ERR invalid CONNECT syntax\n")
		_ = conn.Close()
		return
	}
	clientCommit := parts[2]
	clientID := parts[3]

	state.mu.Lock()

	// Replace existing lease for the same client_id (reconnect).
	if old, ok := state.leases[clientID]; ok {
		_ = old.Close()
		delete(state.leases, clientID)
	}

	if clientCommit == state.commit {
		// Same version — grant lease immediately.
		state.leases[clientID] = conn
		token := state.token
		state.mu.Unlock()

		_ = conn.SetReadDeadline(time.Time{})
		_, _ = fmt.Fprintf(conn, "OK %s\n", token)
		s.logger.Info("lease granted", "client", clientID, "commit", clientCommit)
		go s.monitorLease(conn, state, clientID)
		return
	}

	// Different version — check for conflicting leases from other clients.
	conflicting := 0
	for id := range state.leases {
		if id != clientID {
			conflicting++
		}
	}

	currentCommit := state.commit

	if conflicting > 0 {
		state.mu.Unlock()
		_, _ = fmt.Fprintf(conn, "ERR version mismatch: %s running, %d client(s) connected\n", currentCommit, conflicting)
		_ = conn.Close()
		return
	}

	if state.restartInProgress {
		state.mu.Unlock()
		_, _ = fmt.Fprintf(conn, "ERR restart already in progress\n")
		_ = conn.Close()
		return
	}
	state.restartInProgress = true
	state.mu.Unlock()

	// No conflicting leases — request restart with the new version.
	s.logger.Info("restart requested", "from", currentCommit, "to", clientCommit, "client", clientID)
	result := make(chan error, 1)
	restartCh <- restartReq{commit: clientCommit, result: result}

	restartErr := <-result

	state.mu.Lock()
	state.restartInProgress = false
	if restartErr != nil {
		state.mu.Unlock()
		_, _ = fmt.Fprintf(conn, "ERR restart failed: %v\n", restartErr)
		_ = conn.Close()
		return
	}
	state.leases[clientID] = conn
	token := state.token
	state.mu.Unlock()

	_ = conn.SetReadDeadline(time.Time{})
	_, _ = fmt.Fprintf(conn, "OK %s\n", token)
	s.logger.Info("lease granted after restart", "client", clientID, "commit", clientCommit)
	go s.monitorLease(conn, state, clientID)
}

// monitorLease blocks until the control connection closes, then removes the lease.
func (s *Service) monitorLease(conn net.Conn, state *sessionState, clientID string) {
	buf := make([]byte, 1)
	_, _ = conn.Read(buf) // blocks until EOF or error
	state.mu.Lock()
	if state.leases[clientID] == conn {
		delete(state.leases, clientID)
		s.logger.Info("lease released", "client", clientID)
	}
	state.mu.Unlock()
}

// List returns all discovered session entries by querying control sockets.
func (s *Service) List() ([]domain.SocketEntry, error) {
	names, err := s.store.ListSessionNames()
	if err != nil {
		return nil, err
	}

	var entries []domain.SocketEntry
	for _, name := range names {
		ctlPath := s.store.CtlSocketPath(name)
		meta, alive := QueryCtlInfo(ctlPath)
		if !alive {
			meta.Name = name
		}
		entries = append(entries, domain.SocketEntry{
			Name:     name,
			Path:     s.store.SocketPath(name),
			Metadata: meta,
			Alive:    alive,
		})
	}
	return entries, nil
}

// Clean removes stale session entries whose control sockets are no longer alive.
func (s *Service) Clean() error {
	names, err := s.store.ListSessionNames()
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	removed := 0
	for _, name := range names {
		ctlPath := s.store.CtlSocketPath(name)
		_, alive := QueryCtlInfo(ctlPath)
		if !alive {
			s.logger.Info("removing stale session", "name", name)
			if err := s.store.Remove(name); err != nil {
				s.logger.Error("remove stale session failed", "name", name, "err", err)
				continue
			}
			removed++
		}
	}
	s.logger.Info("cleanup complete", "removed", removed)
	return nil
}

// Provision ensures the VS Code Server is downloaded and extracted for the
// given commit and architecture. Returns the path to the server binary.
func (s *Service) Provision(commit, arch string) (string, error) {
	if !s.provision.IsProvisioned(commit) {
		tarball, err := s.downloader.Download(commit, arch)
		if err != nil {
			return "", fmt.Errorf("download: %w", err)
		}
		targetDir := s.provision.ServerDir(commit)
		if err := s.extractor.Extract(tarball, targetDir); err != nil {
			return "", fmt.Errorf("extract: %w", err)
		}
	}
	return s.provision.ServerBinPath(commit), nil
}

// QueryCtlInfo connects to a control socket, sends CTAP1 INFO, and parses
// the response. Returns the metadata and whether the session is alive.
func QueryCtlInfo(ctlPath string) (domain.Metadata, bool) {
	conn, err := net.DialTimeout("unix", ctlPath, time.Second)
	if err != nil {
		return domain.Metadata{}, false
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = fmt.Fprintf(conn, "CTAP1 INFO\n")

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return domain.Metadata{}, false
	}

	var info infoResponse
	if err := json.Unmarshal([]byte(line), &info); err != nil {
		return domain.Metadata{}, false
	}

	startedAt, _ := time.Parse(time.RFC3339, info.StartedAt)
	return domain.Metadata{
		Name:      info.Name,
		Commit:    info.Commit,
		Arch:      info.Arch,
		Folder:    info.Folder,
		PID:       info.PID,
		StartedAt: startedAt,
	}, true
}

// RunStdio starts VS Code Server on a temporary socket inside the container
// and relays all traffic over stdin/stdout using the mux frame protocol.
func (s *Service) RunStdio(cfg Config, stdin io.Reader, stdout io.Writer, resolveCommit func() (string, error)) error {
	commit := cfg.Commit
	initPhase := commit == ""

	if initPhase {
		s.logger.Info("waiting for init frame with commit hash")
		var err error
		commit, err = readInitCommit(stdin)
		if err != nil {
			return err
		}
		if commit != "" {
			s.logger.Info("received init frame", "commit", commit)
		} else {
			s.logger.Info("init frame had no commit, resolving locally")
			if resolveCommit != nil {
				commit, err = resolveCommit()
				if err != nil {
					return fmt.Errorf("resolve commit: %w", err)
				}
			}
			if commit == "" {
				return fmt.Errorf("no commit available from init frame or local resolution")
			}
			s.logger.Info("resolved commit locally", "commit", commit)
		}
	}

	s.logger.Info("starting stdio session", "commit", commit, "arch", cfg.Arch)

	binPath, err := s.Provision(commit, cfg.Arch)
	if err != nil {
		return err
	}

	tmpSocket := fmt.Sprintf("/tmp/codetap-%d.sock", os.Getpid())
	if err := os.Remove(tmpSocket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale temp socket: %w", err)
	}

	wait, stop, err := s.runner.Start(binPath, tmpSocket, "")
	if err != nil {
		return err
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- wait()
	}()

	if err := waitForSocket(tmpSocket); err != nil {
		stop()
		<-serverErr
		return fmt.Errorf("server failed to start: %w", err)
	}
	s.logger.Info("server ready, starting relay", "socket", tmpSocket)

	if initPhase {
		if err := relay.WriteFrame(stdout, relay.Frame{
			Type: relay.FrameInit, ConnID: 0, Data: []byte(commit),
		}); err != nil {
			stop()
			<-serverErr
			return fmt.Errorf("write init ack: %w", err)
		}
		s.logger.Info("init ack sent", "commit", commit)
	}

	relayErr := make(chan error, 1)
	go func() {
		relayErr <- relay.ContainerSide(stdin, stdout, tmpSocket, s.logger)
	}()

	select {
	case err := <-serverErr:
		return err
	case err := <-relayErr:
		s.logger.Info("relay ended, stopping code-server")
		stop()
		<-serverErr
		return err
	}
}

func readInitCommit(r io.Reader) (string, error) {
	frame, err := relay.ReadFrame(r)
	if err != nil {
		return "", fmt.Errorf("read init frame: %w", err)
	}
	if frame.Type != relay.FrameInit {
		return "", fmt.Errorf("expected FrameInit (0x%02x), got 0x%02x", relay.FrameInit, frame.Type)
	}
	return string(frame.Data), nil
}

func waitForSocket(path string) error {
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for socket %s", path)
}

func isSocketAliveNow(path string) bool {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

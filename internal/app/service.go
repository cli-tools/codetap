package app

import (
	"fmt"
	"io"
	"net"
	"os"
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

// Run starts a codetap session. It provisions the server if needed, writes
// metadata and token files, starts the server, and cleans up on exit.
// Run blocks until the server process exits.
func (s *Service) Run(cfg Config) error {
	s.logger.Info("starting session", "name", cfg.Name, "commit", cfg.Commit, "arch", cfg.Arch)

	binPath, err := s.Provision(cfg.Commit, cfg.Arch)
	if err != nil {
		return err
	}

	socketPath := s.store.SocketPath(cfg.Name)
	// Prevent token/metadata clobbering if a same-named session is already running.
	// This avoids auth mismatch errors where the .token file no longer matches the live server.
	if _, err := os.Stat(socketPath); err == nil {
		if isSocketAliveNow(socketPath) {
			return fmt.Errorf(
				"session %q already running on %s; use a different --name or stop the existing session first",
				cfg.Name,
				socketPath,
			)
		}
		// Stale socket from an unclean exit; remove it so startup can proceed.
		_ = os.Remove(socketPath)
	}

	// Generate connection token
	token, err := s.tokenGen.Generate()
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	// Write token file
	if err := s.store.WriteToken(cfg.Name, token); err != nil {
		return fmt.Errorf("write token: %w", err)
	}

	// Write metadata
	meta := domain.Metadata{
		Name:      cfg.Name,
		Commit:    cfg.Commit,
		Arch:      cfg.Arch,
		Folder:    cfg.Folder,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	if err := s.store.WriteMetadata(meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	// Clean up session files on exit
	defer func() {
		s.logger.Info("cleaning up session", "name", cfg.Name)
		if err := s.store.Remove(cfg.Name); err != nil {
			s.logger.Error("cleanup failed", "name", cfg.Name, "err", err)
		}
	}()

	// Start server
	wait, _, err := s.runner.Start(binPath, socketPath, token)
	if err != nil {
		return err
	}
	// Block until process exits
	return wait()
}

// List returns all discovered session entries.
func (s *Service) List() ([]domain.SocketEntry, error) {
	return s.store.ListEntries()
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

// RunStdio starts VS Code Server on a temporary socket inside the container
// and relays all traffic over stdin/stdout using the mux frame protocol.
// This mode does NOT require --ipc=host on the container.
//
// If cfg.Commit is empty, RunStdio waits for a FrameInit frame from the host
// relay containing the commit hash. If that is also empty, resolveCommit is
// called as a fallback (e.g. to fetch the latest stable version).
func (s *Service) RunStdio(cfg Config, stdin io.Reader, stdout io.Writer, resolveCommit func() (string, error)) error {
	commit := cfg.Commit
	initPhase := commit == ""

	// Init phase: wait for FrameInit from host relay with commit hash
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

	// Use a temp socket inside the container
	tmpSocket := fmt.Sprintf("/tmp/codetap-%d.sock", os.Getpid())
	if err := os.Remove(tmpSocket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale temp socket: %w", err)
	}

	// Start server — non-blocking
	// In stdio relay mode we do not use a connection token. The host side
	// tunnel already scopes access to the local Unix socket.
	wait, stop, err := s.runner.Start(binPath, tmpSocket, "")
	if err != nil {
		return err
	}

	// Collect server exit status in background
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- wait()
	}()

	// Wait for the server socket to appear
	if err := waitForSocket(tmpSocket); err != nil {
		stop()
		<-serverErr
		return fmt.Errorf("server failed to start: %w", err)
	}
	s.logger.Info("server ready, starting relay", "socket", tmpSocket)

	// Send init ack back to host so it knows provisioning succeeded
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

	// Relay mux frames between stdio and server socket
	relayErr := make(chan error, 1)
	go func() {
		relayErr <- relay.ContainerSide(stdin, stdout, tmpSocket, s.logger)
	}()

	// Wait for either server exit or relay end
	select {
	case err := <-serverErr:
		return err
	case err := <-relayErr:
		// Relay ended (e.g. host relay killed, SSH dropped) — kill code-server
		s.logger.Info("relay ended, stopping code-server")
		stop()
		<-serverErr
		return err
	}
}

// readInitCommit reads a FrameInit frame from r and returns the commit hash.
// An empty commit in the frame is valid — it signals the remote should use
// its own resolution chain (env, file, code CLI, latest stable).
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

// Clean removes stale session entries whose sockets are no longer alive.
func (s *Service) Clean() error {
	entries, err := s.store.ListEntries()
	if err != nil {
		return fmt.Errorf("list entries: %w", err)
	}

	removed := 0
	for _, e := range entries {
		if !e.Alive {
			s.logger.Info("removing stale session", "name", e.Name)
			if err := s.store.Remove(e.Name); err != nil {
				s.logger.Error("remove stale session failed", "name", e.Name, "err", err)
				continue
			}
			removed++
		}
	}
	s.logger.Info("cleanup complete", "removed", removed)
	return nil
}

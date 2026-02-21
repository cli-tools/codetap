package app

import (
	"fmt"
	"io"
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
		s.store.Remove(cfg.Name)
	}()

	// Start server — blocks until process exits
	socketPath := s.store.SocketPath(cfg.Name)
	return s.runner.Start(binPath, socketPath, token)
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
func (s *Service) RunStdio(cfg Config, stdin io.Reader, stdout io.Writer) error {
	s.logger.Info("starting stdio session", "commit", cfg.Commit, "arch", cfg.Arch)

	binPath, err := s.Provision(cfg.Commit, cfg.Arch)
	if err != nil {
		return err
	}

	// Generate token for the server
	token, err := s.tokenGen.Generate()
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	// Use a temp socket inside the container
	tmpSocket := fmt.Sprintf("/tmp/codetap-%d.sock", os.Getpid())
	os.Remove(tmpSocket)

	// Start server in background goroutine
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- s.runner.Start(binPath, tmpSocket, token)
	}()

	// Wait for the server socket to appear
	if err := waitForSocket(tmpSocket); err != nil {
		return fmt.Errorf("server failed to start: %w", err)
	}
	s.logger.Info("server ready, starting relay", "socket", tmpSocket)

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
		return err
	}
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
			s.store.Remove(e.Name)
			removed++
		}
	}
	s.logger.Info("cleanup complete", "removed", removed)
	return nil
}

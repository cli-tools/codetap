package server

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"codetap/internal/domain"
)

// ProcessRunner starts the VS Code Server as a child process.
type ProcessRunner struct {
	logger domain.Logger
}

// NewProcessRunner creates a runner that manages the server process lifecycle.
func NewProcessRunner(logger domain.Logger) *ProcessRunner {
	return &ProcessRunner{logger: logger}
}

// Start launches code-server on the given Unix socket with the given token.
// It blocks until the process exits. Signals (SIGINT, SIGTERM) are forwarded
// to the child process.
func (r *ProcessRunner) Start(binPath, socketPath, token string) error {
	args := []string{
		"--socket-path=" + socketPath,
		"--accept-server-license-terms",
	}
	if token == "" {
		args = append(args, "--without-connection-token")
	} else {
		args = append(args, "--connection-token="+token)
	}

	cmd := exec.Command(binPath, args...)
	// Stdout is reserved for relay frame traffic in `codetap run --stdio`.
	// Keep code-server logs on stderr to avoid corrupting the mux protocol.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	// Do not share stdin with code-server. In stdio relay mode stdin carries
	// framed transport data and must remain exclusive to the relay reader.
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start code-server: %w", err)
	}

	r.logger.Info("code-server started", "pid", cmd.Process.Pid, "socket", socketPath)

	// Forward signals to child
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		r.logger.Info("forwarding signal to code-server", "signal", sig)
		if err := cmd.Process.Signal(sig); err != nil {
			r.logger.Error("forward signal failed", "signal", sig, "err", err)
		}
	}()

	err := cmd.Wait()
	signal.Stop(sigCh)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("code-server exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("code-server: %w", err)
	}
	return nil
}

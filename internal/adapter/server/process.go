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
	cmd := exec.Command(binPath,
		"--socket-path="+socketPath,
		"--connection-token="+token,
		"--accept-server-license-terms",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

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
		cmd.Process.Signal(sig)
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

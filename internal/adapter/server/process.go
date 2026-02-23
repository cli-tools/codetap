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
// It returns a wait function that blocks until the process exits and a stop
// function that sends SIGTERM to the entire process group (sh + node).
// Signals (SIGINT, SIGTERM) received by codetap are forwarded to the process group.
func (r *ProcessRunner) Start(binPath, socketPath, token string) (func() error, func(), error) {
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
	// Put the child in its own process group so we can kill sh + node together.
	cmd.SysProcAttr = sysProcAttr()

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start code-server: %w", err)
	}

	pgid := cmd.Process.Pid
	r.logger.Info("code-server started", "pid", pgid, "socket", socketPath)

	// Forward signals to the process group
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		r.logger.Info("forwarding signal to code-server process group", "signal", sig, "pgid", pgid)
		// Negative pid signals the entire process group
		if err := syscall.Kill(-pgid, sig.(syscall.Signal)); err != nil {
			r.logger.Error("forward signal failed", "signal", sig, "err", err)
		}
	}()

	wait := func() error {
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

	stop := func() {
		r.logger.Info("stopping code-server process group", "pgid", pgid)
		if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
			r.logger.Error("kill process group failed", "pgid", pgid, "err", err)
		}
	}

	return wait, stop, nil
}

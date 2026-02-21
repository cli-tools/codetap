package relay

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"codetap/internal/domain"
)

// waitForCommitFile polls for the .commit sidecar file written by the VS Code
// extension. Blocks until the file appears.
func waitForCommitFile(path string, logger domain.Logger) string {
	logger.Info("waiting for VS Code client", "path", path)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			c := strings.TrimSpace(string(data))
			if c != "" {
				short := c
				if len(short) > 12 {
					short = short[:12]
				}
				logger.Info("commit file found", "commit", short)
				_ = os.Remove(path)
				return c
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// HostSide creates a Unix socket listener, spawns the remote command, and
// multiplexes accepted connections over the subprocess stdin/stdout.
//
// commitFilePath is the path to a sidecar file written by the VS Code
// extension containing the client's commit hash. HostSide creates the socket
// listener first (so the session is discoverable), then waits for the file,
// spawns the subprocess, and performs the FrameInit handshake.
func HostSide(socketPath string, command []string, commitFilePath string, onInit func(string), logger domain.Logger) error {
	// Create socket listener first so the session is discoverable by the
	// VS Code extension and isAlive checks succeed.
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
	}()
	defer func() {
		_ = os.Remove(socketPath)
	}()

	logger.Info("listening", "socket", socketPath)

	// Wait for the .commit file from the VS Code extension.
	commit := waitForCommitFile(commitFilePath, logger)

	// Spawn the subprocess
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stderr = os.Stderr

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	logger.Info("subprocess started", "pid", cmd.Process.Pid)

	// Forward signals to subprocess
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("forwarding signal", "signal", sig)
		if err := cmd.Process.Signal(sig); err != nil {
			logger.Error("forward signal failed", "signal", sig, "err", err)
		}
	}()

	fw := NewFrameWriter(stdinPipe)

	// Init phase: send commit to remote and wait for ack.
	logger.Info("sending init frame", "commit", commit)
	if err := fw.Write(Frame{Type: FrameInit, ConnID: 0, Data: []byte(commit)}); err != nil {
		return fmt.Errorf("write init frame: %w", err)
	}

	ackFrame, err := ReadFrame(stdoutPipe)
	if err != nil {
		return fmt.Errorf("read init ack: %w", err)
	}
	if ackFrame.Type != FrameInit {
		return fmt.Errorf("expected FrameInit ack, got 0x%02x", ackFrame.Type)
	}
	logger.Info("init ack received", "commit", string(ackFrame.Data))
	if onInit != nil {
		onInit(string(ackFrame.Data))
	}

	conns := &sync.Map{} // map[uint32]net.Conn
	var nextID atomic.Uint32

	// Read frames from subprocess stdout -> dispatch to connections
	done := make(chan error, 1)
	go func() {
		for {
			frame, err := ReadFrame(stdoutPipe)
			if err != nil {
				done <- err
				return
			}
			switch frame.Type {
			case FrameData:
				if v, ok := conns.Load(frame.ConnID); ok {
					conn := v.(net.Conn)
					if _, writeErr := conn.Write(frame.Data); writeErr != nil {
						logger.Error("write to host socket failed", "conn", frame.ConnID, "err", writeErr)
						_ = conn.Close()
						conns.Delete(frame.ConnID)
					}
				}
			case FrameClose:
				if v, ok := conns.LoadAndDelete(frame.ConnID); ok {
					_ = v.(net.Conn).Close()
				}
			}
		}
	}()

	// Accept connections on the listener
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return // listener closed
			}
			id := nextID.Add(1)
			conns.Store(id, conn)
			logger.Info("connection accepted", "conn", id)

			// Send OPEN frame to subprocess
			if writeErr := fw.Write(Frame{Type: FrameOpen, ConnID: id}); writeErr != nil {
				logger.Error("write OPEN frame failed", "conn", id, "err", writeErr)
				conns.Delete(id)
				_ = conn.Close()
				continue
			}

			// Read from connection -> write DATA frames to subprocess
			go func(cid uint32, c net.Conn) {
				buf := make([]byte, 32*1024)
				for {
					n, readErr := c.Read(buf)
					if n > 0 {
						if writeErr := fw.Write(Frame{Type: FrameData, ConnID: cid, Data: buf[:n]}); writeErr != nil {
							logger.Error("write DATA frame failed", "conn", cid, "err", writeErr)
							_ = c.Close()
							conns.Delete(cid)
							return
						}
					}
					if readErr != nil {
						if writeErr := fw.Write(Frame{Type: FrameClose, ConnID: cid}); writeErr != nil {
							logger.Error("write CLOSE frame failed", "conn", cid, "err", writeErr)
						}
						conns.Delete(cid)
						_ = c.Close()
						return
					}
				}
			}(id, conn)
		}
	}()

	// Wait for subprocess to exit or frame reader to end
	if err := <-done; err != nil && err != io.EOF {
		logger.Error("read frame failed", "err", err)
	}

	// Close all connections
	conns.Range(func(_, v any) bool {
		_ = v.(net.Conn).Close()
		return true
	})

	signal.Stop(sigCh)
	return cmd.Wait()
}

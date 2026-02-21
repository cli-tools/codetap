package relay

import (
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	"codetap/internal/domain"
)

// HostSide creates a Unix socket listener, spawns the remote command, and
// multiplexes accepted connections over the subprocess stdin/stdout.
func HostSide(socketPath string, command []string, logger domain.Logger) error {
	// Remove stale socket
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	logger.Info("listening", "socket", socketPath)

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
		cmd.Process.Signal(sig)
	}()

	fw := NewFrameWriter(stdinPipe)
	conns := &sync.Map{} // map[uint32]net.Conn
	var nextID atomic.Uint32

	// Read frames from subprocess stdout → dispatch to connections
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
					v.(net.Conn).Write(frame.Data)
				}
			case FrameClose:
				if v, ok := conns.LoadAndDelete(frame.ConnID); ok {
					v.(net.Conn).Close()
				}
			}
		}
	}()

	// Accept connections on the listener
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // listener closed
			}
			id := nextID.Add(1)
			conns.Store(id, conn)
			logger.Info("connection accepted", "conn", id)

			// Send OPEN frame to subprocess
			fw.Write(Frame{Type: FrameOpen, ConnID: id})

			// Read from connection → write DATA frames to subprocess
			go func(cid uint32, c net.Conn) {
				buf := make([]byte, 32*1024)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						fw.Write(Frame{Type: FrameData, ConnID: cid, Data: buf[:n]})
					}
					if err != nil {
						fw.Write(Frame{Type: FrameClose, ConnID: cid})
						conns.Delete(cid)
						return
					}
				}
			}(id, conn)
		}
	}()

	// Wait for subprocess to exit or frame reader to end
	select {
	case <-done:
	}

	// Close all connections
	conns.Range(func(_, v any) bool {
		v.(net.Conn).Close()
		return true
	})

	signal.Stop(sigCh)
	return cmd.Wait()
}

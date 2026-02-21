package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"

	"codetap/internal/domain"
)

// ContainerSide relays traffic between stdio and a local VS Code Server socket.
// It reads mux frames from r (stdin), connects to the server socket for each
// OPEN frame, and writes response frames to w (stdout).
func ContainerSide(r io.Reader, w io.Writer, serverSocket string, sessionInfo *SessionInfo, logger domain.Logger) error {
	fw := NewFrameWriter(w)
	conns := &sync.Map{} // map[uint32]net.Conn

	if sessionInfo != nil {
		payload, err := json.Marshal(sessionInfo)
		if err != nil {
			return fmt.Errorf("marshal relay metadata: %w", err)
		}
		if err := fw.Write(Frame{Type: FrameMeta, Data: payload}); err != nil {
			return fmt.Errorf("write metadata frame: %w", err)
		}
	}

	// Read frames from stdin and dispatch.
	for {
		frame, err := ReadFrame(r)
		if err != nil {
			// stdin closed - shut down all connections.
			conns.Range(func(_, v any) bool {
				_ = v.(net.Conn).Close()
				return true
			})
			if err == io.EOF {
				return nil
			}
			return err
		}

		switch frame.Type {
		case FrameOpen:
			conn, dialErr := net.Dial("unix", serverSocket)
			if dialErr != nil {
				logger.Error("connect to server socket", "conn", frame.ConnID, "err", dialErr)
				if writeErr := fw.Write(Frame{Type: FrameClose, ConnID: frame.ConnID}); writeErr != nil {
					return writeErr
				}
				continue
			}
			conns.Store(frame.ConnID, conn)
			logger.Info("connection opened", "conn", frame.ConnID)

			// Read from server socket -> write DATA frames to stdout.
			go func(id uint32, c net.Conn) {
				buf := make([]byte, 32*1024)
				for {
					n, readErr := c.Read(buf)
					if n > 0 {
						if writeErr := fw.Write(Frame{Type: FrameData, ConnID: id, Data: buf[:n]}); writeErr != nil {
							logger.Error("write DATA frame failed", "conn", id, "err", writeErr)
							return
						}
					}
					if readErr != nil {
						if writeErr := fw.Write(Frame{Type: FrameClose, ConnID: id}); writeErr != nil {
							logger.Error("write CLOSE frame failed", "conn", id, "err", writeErr)
						}
						conns.Delete(id)
						_ = c.Close()
						return
					}
				}
			}(frame.ConnID, conn)

		case FrameData:
			if v, ok := conns.Load(frame.ConnID); ok {
				conn := v.(net.Conn)
				if _, writeErr := conn.Write(frame.Data); writeErr != nil {
					logger.Error("write to server socket failed", "conn", frame.ConnID, "err", writeErr)
					_ = conn.Close()
					conns.Delete(frame.ConnID)
					if closeFrameErr := fw.Write(Frame{Type: FrameClose, ConnID: frame.ConnID}); closeFrameErr != nil {
						return closeFrameErr
					}
				}
			}

		case FrameClose:
			if v, ok := conns.LoadAndDelete(frame.ConnID); ok {
				_ = v.(net.Conn).Close()
				logger.Info("connection closed", "conn", frame.ConnID)
			}
		}
	}
}

package relay

import (
	"io"
	"net"
	"sync"

	"codetap/internal/domain"
)

// ContainerSide relays traffic between stdio and a local VS Code Server socket.
// It reads mux frames from r (stdin), connects to the server socket for each
// OPEN frame, and writes response frames to w (stdout).
func ContainerSide(r io.Reader, w io.Writer, serverSocket string, logger domain.Logger) error {
	fw := NewFrameWriter(w)
	conns := &sync.Map{} // map[uint32]net.Conn

	// Read frames from stdin and dispatch
	for {
		frame, err := ReadFrame(r)
		if err != nil {
			// stdin closed — shut down all connections
			conns.Range(func(_, v any) bool {
				v.(net.Conn).Close()
				return true
			})
			if err == io.EOF {
				return nil
			}
			return err
		}

		switch frame.Type {
		case FrameOpen:
			conn, err := net.Dial("unix", serverSocket)
			if err != nil {
				logger.Error("connect to server socket", "conn", frame.ConnID, "err", err)
				fw.Write(Frame{Type: FrameClose, ConnID: frame.ConnID})
				continue
			}
			conns.Store(frame.ConnID, conn)
			logger.Info("connection opened", "conn", frame.ConnID)

			// Read from server socket → write DATA frames to stdout
			go func(id uint32, c net.Conn) {
				buf := make([]byte, 32*1024)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						fw.Write(Frame{Type: FrameData, ConnID: id, Data: buf[:n]})
					}
					if err != nil {
						fw.Write(Frame{Type: FrameClose, ConnID: id})
						conns.Delete(id)
						return
					}
				}
			}(frame.ConnID, conn)

		case FrameData:
			if v, ok := conns.Load(frame.ConnID); ok {
				v.(net.Conn).Write(frame.Data)
			}

		case FrameClose:
			if v, ok := conns.LoadAndDelete(frame.ConnID); ok {
				v.(net.Conn).Close()
				logger.Info("connection closed", "conn", frame.ConnID)
			}
		}
	}
}

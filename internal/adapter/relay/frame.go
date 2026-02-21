package relay

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// Frame types for the multiplexing protocol.
const (
	FrameOpen  byte = 0x01 // New connection
	FrameData  byte = 0x02 // Data payload
	FrameClose byte = 0x03 // Connection closed
	FrameInit  byte = 0x04 // Init phase: commit negotiation
)

// Frame is a multiplexed message with a connection ID and payload.
type Frame struct {
	Type   byte
	ConnID uint32
	Data   []byte
}

// MaxFramePayload limits individual frame payloads to 1MB.
const MaxFramePayload = 1 << 20

// WriteFrame writes a framed message to w.
// Wire format: [type:1][conn_id:4 BE][length:4 BE][payload].
func WriteFrame(w io.Writer, f Frame) error {
	header := make([]byte, 9)
	header[0] = f.Type
	binary.BigEndian.PutUint32(header[1:5], f.ConnID)
	binary.BigEndian.PutUint32(header[5:9], uint32(len(f.Data)))
	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if len(f.Data) > 0 {
		if _, err := w.Write(f.Data); err != nil {
			return fmt.Errorf("write frame data: %w", err)
		}
	}
	return nil
}

// ReadFrame reads a framed message from r.
func ReadFrame(r io.Reader) (Frame, error) {
	header := make([]byte, 9)
	if _, err := io.ReadFull(r, header); err != nil {
		return Frame{}, err
	}
	f := Frame{
		Type:   header[0],
		ConnID: binary.BigEndian.Uint32(header[1:5]),
	}
	length := binary.BigEndian.Uint32(header[5:9])
	if length > MaxFramePayload {
		return Frame{}, fmt.Errorf("frame payload too large: %d bytes", length)
	}
	if length > 0 {
		f.Data = make([]byte, length)
		if _, err := io.ReadFull(r, f.Data); err != nil {
			return Frame{}, fmt.Errorf("read frame data: %w", err)
		}
	}
	return f, nil
}

// FrameWriter wraps an io.Writer with mutex protection for concurrent writes.
type FrameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewFrameWriter creates a thread-safe frame writer.
func NewFrameWriter(w io.Writer) *FrameWriter {
	return &FrameWriter{w: w}
}

// Write sends a frame with mutex protection.
func (fw *FrameWriter) Write(f Frame) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return WriteFrame(fw.w, f)
}

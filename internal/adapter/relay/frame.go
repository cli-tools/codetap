package relay

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
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

	validType := f.Type >= FrameOpen && f.Type <= FrameInit
	if !validType || length > MaxFramePayload {
		return Frame{}, recoverTextError(header, r)
	}

	if length > 0 {
		f.Data = make([]byte, length)
		if _, err := io.ReadFull(r, f.Data); err != nil {
			return Frame{}, fmt.Errorf("read frame data: %w", err)
		}
	}
	return f, nil
}

// recoverTextError attempts to interpret the already-read header bytes plus
// any remaining data as a text error message from the remote side. This
// typically happens when ssh, docker, or a shell writes an error to stdout
// instead of the expected binary frame protocol.
func recoverTextError(header []byte, r io.Reader) error {
	// Read more context with a short timeout so we don't block indefinitely
	// if the remote keeps the stream open after writing garbage.
	extra := make([]byte, 1024)
	type result struct{ n int }
	ch := make(chan result, 1)
	go func() {
		n, _ := r.Read(extra)
		ch <- result{n}
	}()
	var n int
	select {
	case res := <-ch:
		n = res.n
	case <-time.After(2 * time.Second):
	}
	all := append(header, extra[:n]...)

	if looksLikeText(all) {
		msg := strings.TrimRight(string(all), "\r\n \t")
		return fmt.Errorf("remote command wrote text instead of expected binary frame:\n  %s", msg)
	}
	return fmt.Errorf("invalid frame: type=0x%02x length=%d (expected binary frame protocol)",
		header[0], binary.BigEndian.Uint32(header[5:9]))
}

// looksLikeText reports whether data appears to be human-readable text rather
// than binary frame data.
func looksLikeText(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	printable := 0
	for _, b := range data {
		if b >= 0x20 && b <= 0x7e || b == '\n' || b == '\r' || b == '\t' {
			printable++
		}
	}
	return printable*100/len(data) > 80
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

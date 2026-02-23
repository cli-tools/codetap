package relay

import (
	"bytes"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		f    Frame
	}{
		{"open", Frame{Type: FrameOpen, ConnID: 1}},
		{"close", Frame{Type: FrameClose, ConnID: 42}},
		{"data", Frame{Type: FrameData, ConnID: 7, Data: []byte("hello world")}},
		{"empty data", Frame{Type: FrameData, ConnID: 0, Data: nil}},
		{"large conn id", Frame{Type: FrameData, ConnID: 0xFFFFFFFF, Data: []byte("x")}},
		{"init", Frame{Type: FrameInit, ConnID: 0, Data: []byte("abc123def456abc123def456abc123def456abc1")}},
		{"init empty", Frame{Type: FrameInit, ConnID: 0, Data: nil}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, tt.f); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}

			got, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}

			if got.Type != tt.f.Type {
				t.Errorf("Type = %d, want %d", got.Type, tt.f.Type)
			}
			if got.ConnID != tt.f.ConnID {
				t.Errorf("ConnID = %d, want %d", got.ConnID, tt.f.ConnID)
			}
			if !bytes.Equal(got.Data, tt.f.Data) {
				t.Errorf("Data = %q, want %q", got.Data, tt.f.Data)
			}
		})
	}
}

func TestFrameMultipleRoundTrips(t *testing.T) {
	var buf bytes.Buffer

	frames := []Frame{
		{Type: FrameInit, ConnID: 0, Data: []byte("abc123")},
		{Type: FrameOpen, ConnID: 1},
		{Type: FrameData, ConnID: 1, Data: []byte("first")},
		{Type: FrameData, ConnID: 2, Data: []byte("second")},
		{Type: FrameClose, ConnID: 1},
	}

	for _, f := range frames {
		if err := WriteFrame(&buf, f); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}

	for i, want := range frames {
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame[%d]: %v", i, err)
		}
		if got.Type != want.Type || got.ConnID != want.ConnID {
			t.Errorf("frame %d: got (%d, %d), want (%d, %d)",
				i, got.Type, got.ConnID, want.Type, want.ConnID)
		}
		if !bytes.Equal(got.Data, want.Data) {
			t.Errorf("frame %d data: got %q, want %q", i, got.Data, want.Data)
		}
	}
}

func TestReadFrame_TextError(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSub string // substring expected in error
	}{
		{
			"docker exec error",
			`OCI runtime exec failed: exec failed: unable to start container process: exec: "/root/.local/bin/codetap": no such file or directory`,
			"OCI runtime exec failed",
		},
		{
			"bash not found",
			"bash: /root/.local/bin/codetap: No such file or directory\n",
			"No such file or directory",
		},
		{
			"permission denied",
			"Permission denied (publickey,password).\r\n",
			"Permission denied",
		},
		{
			"ssh error",
			"ssh: connect to host rat2 port 22: Connection refused\n",
			"Connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := bytes.NewBufferString(tt.input)
			_, err := ReadFrame(buf)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantSub)
			}
			if !strings.Contains(err.Error(), "remote command wrote text") {
				t.Errorf("error %q missing user-friendly prefix", err.Error())
			}
		})
	}
}

func TestReadFrame_BinaryGarbage(t *testing.T) {
	// 9 bytes of non-text binary with invalid frame type
	buf := bytes.NewBuffer([]byte{0xFF, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01})
	_, err := ReadFrame(buf)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "remote command wrote text") {
		t.Error("binary garbage should not be reported as text")
	}
	if !strings.Contains(err.Error(), "invalid frame") {
		t.Errorf("expected 'invalid frame' error, got: %v", err)
	}
}

func TestLooksLikeText(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"ascii text", []byte("hello world"), true},
		{"error message", []byte("bash: command not found\n"), true},
		{"binary", []byte{0x01, 0x00, 0x00, 0x00, 0x05, 0xFF, 0xFE, 0x80}, false},
		{"empty", nil, false},
		{"mostly text", []byte("error\x00message"), true},       // 11/13 = 84%
		{"mostly binary", []byte{0x01, 0x02, 0x03, 'a'}, false}, // 1/4 = 25%
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeText(tt.data)
			if got != tt.want {
				t.Errorf("looksLikeText(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestFrameWriter_ConcurrentSafety(t *testing.T) {
	var buf bytes.Buffer
	fw := NewFrameWriter(&buf)

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(id uint32) {
			done <- fw.Write(Frame{Type: FrameData, ConnID: id, Data: []byte("test")})
		}(uint32(i))
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Fatalf("WriteFrame[%d]: %v", i, err)
		}
	}

	// Read all 10 frames back
	for i := 0; i < 10; i++ {
		f, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame[%d]: %v", i, err)
		}
		if f.Type != FrameData {
			t.Errorf("frame %d: type = %d, want %d", i, f.Type, FrameData)
		}
	}
}

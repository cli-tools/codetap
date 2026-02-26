package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codetap/internal/adapter/relay"
	"codetap/internal/domain"
)

func newTestService(
	dl *mockDownloader,
	ex *mockExtractor,
	pr *mockProvisioner,
	sr domain.ServerRunner,
	st *mockStore,
	tg *mockTokenGen,
) *Service {
	return NewService(dl, ex, pr, sr, st, tg, &mockLogger{})
}

func testConfig(socketDir string) Config {
	return Config{
		Name:      "test-session",
		Commit:    "abc123",
		Arch:      "x64",
		Folder:    "/workspace",
		SocketDir: socketDir,
	}
}

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestRun_AlreadyProvisioned(t *testing.T) {
	dir := setupTestDir(t)
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: true, binPath: "/opt/server/bin/code-server"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore(dir)
	tg := &mockTokenGen{token: "test-token-123"}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	if err := svc.Run(testConfig(dir)); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if dl.called {
		t.Error("expected downloader NOT to be called when already provisioned")
	}
	if ex.called {
		t.Error("expected extractor NOT to be called when already provisioned")
	}
	if !sr.called {
		t.Error("expected runner to be called")
	}
	if sr.lastBin != "/opt/server/bin/code-server" {
		t.Errorf("runner got bin %q, want %q", sr.lastBin, "/opt/server/bin/code-server")
	}
	if sr.lastToken != "test-token-123" {
		t.Errorf("runner got token %q, want %q", sr.lastToken, "test-token-123")
	}
}

func TestRun_NeedsDownloadAndExtract(t *testing.T) {
	dir := setupTestDir(t)
	dl := &mockDownloader{downloadFn: func(c, a string) (string, error) {
		return "/cache/" + c + "-" + a + ".tar.gz", nil
	}}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: false, binPath: "/repo/abc123/bin/code-server", dirPath: "/repo/abc123"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore(dir)
	tg := &mockTokenGen{token: "generated-token"}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	if err := svc.Run(testConfig(dir)); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if !dl.called {
		t.Error("expected downloader to be called")
	}
	if dl.lastCommit != "abc123" || dl.lastArch != "x64" {
		t.Errorf("downloader called with (%q, %q), want (%q, %q)",
			dl.lastCommit, dl.lastArch, "abc123", "x64")
	}
	if !ex.called {
		t.Error("expected extractor to be called")
	}
	if ex.lastTarball != "/cache/abc123-x64.tar.gz" {
		t.Errorf("extractor got tarball %q", ex.lastTarball)
	}
	if ex.lastTarget != "/repo/abc123" {
		t.Errorf("extractor got target %q, want %q", ex.lastTarget, "/repo/abc123")
	}
}

func TestRun_DownloadError(t *testing.T) {
	dir := setupTestDir(t)
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) {
		return "", errors.New("network error")
	}}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: false, dirPath: "/repo/abc123"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore(dir)
	tg := &mockTokenGen{token: "tok"}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	err := svc.Run(testConfig(dir))
	if err == nil {
		t.Fatal("expected error from download failure")
	}
	if !dl.called {
		t.Error("expected downloader to be called")
	}
	if ex.called {
		t.Error("expected extractor NOT to be called after download error")
	}
	if sr.called {
		t.Error("expected runner NOT to be called after download error")
	}
}

func TestRun_ExtractError(t *testing.T) {
	dir := setupTestDir(t)
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) { return "/cache/t.tar.gz", nil }}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return errors.New("corrupt tarball") }}
	pr := &mockProvisioner{provisioned: false, dirPath: "/repo/abc123"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore(dir)
	tg := &mockTokenGen{token: "tok"}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	err := svc.Run(testConfig(dir))
	if err == nil {
		t.Fatal("expected error from extract failure")
	}
	if sr.called {
		t.Error("expected runner NOT to be called after extract error")
	}
}

func TestRun_TokenError(t *testing.T) {
	dir := setupTestDir(t)
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: true, binPath: "/bin/code-server"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore(dir)
	tg := &mockTokenGen{err: errors.New("entropy exhausted")}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	err := svc.Run(testConfig(dir))
	if err == nil {
		t.Fatal("expected error from token generation failure")
	}
	if sr.called {
		t.Error("expected runner NOT to be called after token error")
	}
}

func TestRun_Cleanup(t *testing.T) {
	dir := setupTestDir(t)
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore(dir)

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		sr, st,
		&mockTokenGen{token: "tok"},
	)

	if err := svc.Run(testConfig(dir)); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(st.removed) == 0 {
		t.Error("expected cleanup to remove session files")
	}
	found := false
	for _, name := range st.removed {
		if name == "test-session" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'test-session' to be removed, got %v", st.removed)
	}
}

func TestRun_StartError(t *testing.T) {
	dir := setupTestDir(t)
	sr := &mockRunner{startFn: func(_, _, _ string) error { return errors.New("server crashed") }}
	st := newMockStore(dir)

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		sr, st,
		&mockTokenGen{token: "tok"},
	)

	err := svc.Run(testConfig(dir))
	if err == nil {
		t.Fatal("expected Run() to return server error")
	}
	// When Start() fails, no ctl socket was created so there's nothing to clean up.
	// The error should propagate up.
	if !sr.called {
		t.Error("expected runner to be called")
	}
}

func TestRun_CtlSocketCreated(t *testing.T) {
	dir := setupTestDir(t)
	st := newMockStore(dir)
	runner := newBlockingRunner()

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		runner, st,
		&mockTokenGen{token: "test-token"},
	)

	runDone := make(chan error, 1)
	go func() {
		runDone <- svc.Run(testConfig(dir))
	}()

	ctlPath := st.CtlSocketPath("test-session")
	waitForCtlSocket(t, ctlPath)

	// Query INFO.
	conn, err := net.DialTimeout("unix", ctlPath, time.Second)
	if err != nil {
		runner.Stop()
		t.Fatalf("dial ctl socket: %v", err)
	}
	_, _ = fmt.Fprintf(conn, "CTAP1 INFO\n")
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	conn.Close()
	if err != nil {
		runner.Stop()
		t.Fatalf("read INFO response: %v", err)
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(line), &info); err != nil {
		runner.Stop()
		t.Fatalf("parse INFO response: %v", err)
	}
	if info["name"] != "test-session" {
		t.Errorf("INFO name = %v, want test-session", info["name"])
	}
	if info["commit"] != "abc123" {
		t.Errorf("INFO commit = %v, want abc123", info["commit"])
	}
	if info["folder"] != "/workspace" {
		t.Errorf("INFO folder = %v, want /workspace", info["folder"])
	}

	runner.Stop()
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestRun_ConnectSameVersion(t *testing.T) {
	dir := setupTestDir(t)
	st := newMockStore(dir)
	runner := newBlockingRunner()

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		runner, st,
		&mockTokenGen{token: "my-secret-token"},
	)

	runDone := make(chan error, 1)
	go func() {
		runDone <- svc.Run(testConfig(dir))
	}()

	ctlPath := st.CtlSocketPath("test-session")
	waitForCtlSocket(t, ctlPath)

	// CONNECT with matching commit.
	conn, err := net.DialTimeout("unix", ctlPath, time.Second)
	if err != nil {
		runner.Stop()
		t.Fatalf("dial: %v", err)
	}
	_, _ = fmt.Fprintf(conn, "CTAP1 CONNECT abc123 client-1\n")
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		runner.Stop()
		t.Fatalf("read CONNECT response: %v", err)
	}

	if line != "OK my-secret-token\n" {
		t.Errorf("CONNECT response = %q, want %q", line, "OK my-secret-token\n")
	}

	conn.Close()
	runner.Stop()
	<-runDone
}

func TestRun_ConnectVersionMismatchRejected(t *testing.T) {
	dir := setupTestDir(t)
	st := newMockStore(dir)
	runner := newBlockingRunner()

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		runner, st,
		&mockTokenGen{token: "tok"},
	)

	runDone := make(chan error, 1)
	go func() {
		runDone <- svc.Run(testConfig(dir))
	}()

	ctlPath := st.CtlSocketPath("test-session")
	waitForCtlSocket(t, ctlPath)

	// First client connects with matching version (creates a lease).
	conn1, _ := net.DialTimeout("unix", ctlPath, time.Second)
	_, _ = fmt.Fprintf(conn1, "CTAP1 CONNECT abc123 client-1\n")
	reader1 := bufio.NewReader(conn1)
	line1, _ := reader1.ReadString('\n')
	if !startsWith(line1, "OK") {
		t.Fatalf("first CONNECT should succeed, got %q", line1)
	}

	// Second client connects with different version — should be rejected.
	conn2, _ := net.DialTimeout("unix", ctlPath, time.Second)
	_, _ = fmt.Fprintf(conn2, "CTAP1 CONNECT def456 client-2\n")
	reader2 := bufio.NewReader(conn2)
	line2, _ := reader2.ReadString('\n')
	if !startsWith(line2, "ERR") {
		t.Errorf("second CONNECT should be rejected, got %q", line2)
	}
	conn2.Close()

	conn1.Close()
	runner.Stop()
	<-runDone
}

func TestRun_LeaseCleanupOnDisconnect(t *testing.T) {
	dir := setupTestDir(t)
	st := newMockStore(dir)
	runner := newBlockingRunner()

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		runner, st,
		&mockTokenGen{token: "tok"},
	)

	runDone := make(chan error, 1)
	go func() {
		runDone <- svc.Run(testConfig(dir))
	}()

	ctlPath := st.CtlSocketPath("test-session")
	waitForCtlSocket(t, ctlPath)

	// Connect client-1.
	conn1, _ := net.DialTimeout("unix", ctlPath, time.Second)
	_, _ = fmt.Fprintf(conn1, "CTAP1 CONNECT abc123 client-1\n")
	reader1 := bufio.NewReader(conn1)
	_, _ = reader1.ReadString('\n')

	// Disconnect client-1.
	conn1.Close()
	time.Sleep(50 * time.Millisecond) // allow monitorLease to run

	// Now client-2 with a different version should succeed (no conflicting leases).
	// It will try to restart, which will fail since our mock doesn't support restart
	// in the same way, but it should at least not be rejected with "version mismatch".
	conn2, _ := net.DialTimeout("unix", ctlPath, time.Second)
	_, _ = fmt.Fprintf(conn2, "CTAP1 CONNECT def456 client-2\n")
	reader2 := bufio.NewReader(conn2)
	line2, _ := reader2.ReadString('\n')

	// With no conflicting leases, it attempts restart. The response depends on
	// whether restart succeeds, but it should NOT say "version mismatch".
	if startsWith(line2, "ERR version mismatch") {
		t.Errorf("should not get version mismatch after lease cleanup, got %q", line2)
	}
	conn2.Close()

	runner.Stop()
	<-runDone
}

func TestClean_RemovesStale(t *testing.T) {
	dir := setupTestDir(t)
	st := newMockStore(dir)

	// Create stale ctl socket files (not listening).
	for _, name := range []string{"dead-1", "dead-2"} {
		_ = os.WriteFile(filepath.Join(dir, name+".ctl.sock"), nil, 0644)
	}

	// Create an alive ctl socket.
	alivePath := filepath.Join(dir, "alive.ctl.sock")
	listener, err := net.Listen("unix", alivePath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	// Serve a minimal INFO response so QueryCtlInfo succeeds.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				reader := bufio.NewReader(c)
				line, _ := reader.ReadString('\n')
				if line == "CTAP1 INFO\n" {
					_, _ = c.Write([]byte(`{"name":"alive"}` + "\n"))
				}
				c.Close()
			}(conn)
		}
	}()

	svc := newTestService(nil, nil, nil, nil, st, &mockTokenGen{})

	if err := svc.Clean(); err != nil {
		t.Fatalf("Clean() error: %v", err)
	}

	if len(st.removed) != 2 {
		t.Fatalf("expected 2 removals, got %d: %v", len(st.removed), st.removed)
	}

	for _, name := range st.removed {
		if name == "alive" {
			t.Error("should NOT remove alive session")
		}
	}
}

func TestClean_KeepsAlive(t *testing.T) {
	dir := setupTestDir(t)
	st := newMockStore(dir)

	// Create alive ctl sockets.
	for _, name := range []string{"alive-1", "alive-2"} {
		p := filepath.Join(dir, name+".ctl.sock")
		l, err := net.Listen("unix", p)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		go func(listener net.Listener) {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					reader := bufio.NewReader(c)
					_, _ = reader.ReadString('\n')
					_, _ = c.Write([]byte(`{"name":"x"}` + "\n"))
					c.Close()
				}(conn)
			}
		}(l)
	}

	svc := newTestService(nil, nil, nil, nil, st, &mockTokenGen{})

	if err := svc.Clean(); err != nil {
		t.Fatalf("Clean() error: %v", err)
	}

	if len(st.removed) != 0 {
		t.Errorf("expected 0 removals, got %d: %v", len(st.removed), st.removed)
	}
}

func TestList_ReturnsEntries(t *testing.T) {
	dir := setupTestDir(t)
	st := newMockStore(dir)

	// Create one alive and one dead ctl socket.
	alivePath := filepath.Join(dir, "session-1.ctl.sock")
	listener, _ := net.Listen("unix", alivePath)
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				reader := bufio.NewReader(c)
				_, _ = reader.ReadString('\n')
				_, _ = c.Write([]byte(`{"name":"session-1","commit":"aaa","folder":"/ws","pid":1}` + "\n"))
				c.Close()
			}(conn)
		}
	}()

	// Dead socket (file exists but nothing listening).
	_ = os.WriteFile(filepath.Join(dir, "session-2.ctl.sock"), nil, 0644)

	svc := newTestService(nil, nil, nil, nil, st, &mockTokenGen{})

	entries, err := svc.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Find each entry.
	var alive, dead bool
	for _, e := range entries {
		if e.Name == "session-1" && e.Alive {
			alive = true
		}
		if e.Name == "session-2" && !e.Alive {
			dead = true
		}
	}
	if !alive {
		t.Error("expected session-1 to be alive")
	}
	if !dead {
		t.Error("expected session-2 to be dead")
	}
}

func TestProvision_AlreadyProvisioned(t *testing.T) {
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }}
	pr := &mockProvisioner{provisioned: true, binPath: "/repo/abc/bin/code-server"}

	svc := newTestService(dl, nil, pr, nil, nil, nil)

	path, err := svc.Provision("abc", "x64")
	if err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	if path != "/repo/abc/bin/code-server" {
		t.Errorf("got path %q", path)
	}
	if dl.called {
		t.Error("should not download when already provisioned")
	}
}

func TestProvision_DownloadsAndExtracts(t *testing.T) {
	dl := &mockDownloader{downloadFn: func(c, a string) (string, error) {
		return "/cache/" + c + ".tar.gz", nil
	}}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: false, binPath: "/repo/abc/bin/code-server", dirPath: "/repo/abc"}

	svc := newTestService(dl, ex, pr, nil, nil, nil)

	path, err := svc.Provision("abc", "x64")
	if err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	if path != "/repo/abc/bin/code-server" {
		t.Errorf("got path %q", path)
	}
	if !dl.called {
		t.Error("expected download")
	}
	if !ex.called {
		t.Error("expected extract")
	}
}

func TestReadInitCommit_Success(t *testing.T) {
	var buf bytes.Buffer
	commit := "abc123def456abc123def456abc123def456abc1"
	if err := relay.WriteFrame(&buf, relay.Frame{
		Type: relay.FrameInit, ConnID: 0, Data: []byte(commit),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := readInitCommit(&buf)
	if err != nil {
		t.Fatalf("readInitCommit() error: %v", err)
	}
	if got != commit {
		t.Errorf("got %q, want %q", got, commit)
	}
}

func TestReadInitCommit_WrongFrameType(t *testing.T) {
	var buf bytes.Buffer
	if err := relay.WriteFrame(&buf, relay.Frame{
		Type: relay.FrameData, ConnID: 1, Data: []byte("hello"),
	}); err != nil {
		t.Fatal(err)
	}

	_, err := readInitCommit(&buf)
	if err == nil {
		t.Fatal("expected error for wrong frame type")
	}
}

func TestReadInitCommit_EmptyCommit(t *testing.T) {
	var buf bytes.Buffer
	if err := relay.WriteFrame(&buf, relay.Frame{
		Type: relay.FrameInit, ConnID: 0, Data: nil,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := readInitCommit(&buf)
	if err != nil {
		t.Fatalf("readInitCommit() error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestReadInitCommit_ReadError(t *testing.T) {
	var buf bytes.Buffer

	_, err := readInitCommit(&buf)
	if err == nil {
		t.Fatal("expected error from empty reader")
	}
}

// --- Helpers ---

// blockingMockRunner is a mock runner whose wait() blocks until Stop() is called.
type blockingMockRunner struct {
	mu     sync.Mutex
	called bool
	stopCh chan struct{} // closed by Stop() to unblock wait
}

func newBlockingRunner() *blockingMockRunner {
	return &blockingMockRunner{stopCh: make(chan struct{})}
}

func (m *blockingMockRunner) Start(bin, sock, token string) (func() error, func(), error) {
	m.mu.Lock()
	m.called = true
	m.mu.Unlock()

	// Create the socket file so waitForSocket succeeds in tests.
	_ = os.WriteFile(sock, nil, 0600)

	wait := func() error {
		<-m.stopCh
		return nil
	}
	stop := func() {
		select {
		case <-m.stopCh:
		default:
			close(m.stopCh)
		}
	}
	return wait, stop, nil
}

// Stop unblocks any waiting Start().
func (m *blockingMockRunner) Stop() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
}

func waitForCtlSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("unix", path, 100*time.Millisecond); err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for ctl socket %s", path)
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

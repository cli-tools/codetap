package app

import (
	"errors"
	"testing"

	"codetap/internal/domain"
)

func newTestService(
	dl *mockDownloader,
	ex *mockExtractor,
	pr *mockProvisioner,
	sr *mockRunner,
	st *mockStore,
	tg *mockTokenGen,
) *Service {
	return NewService(dl, ex, pr, sr, st, tg, &mockLogger{})
}

func testConfig() Config {
	return Config{
		Name:      "test-session",
		Commit:    "abc123",
		Arch:      "x64",
		Folder:    "/workspace",
		SocketDir: "/tmp/test-codetap",
	}
}

func TestRun_AlreadyProvisioned(t *testing.T) {
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: true, binPath: "/opt/server/bin/code-server"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore("/tmp/test-codetap")
	tg := &mockTokenGen{token: "test-token-123"}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	if err := svc.Run(testConfig()); err != nil {
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
	dl := &mockDownloader{downloadFn: func(c, a string) (string, error) {
		return "/cache/" + c + "-" + a + ".tar.gz", nil
	}}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: false, binPath: "/repo/abc123/bin/code-server", dirPath: "/repo/abc123"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore("/tmp/test-codetap")
	tg := &mockTokenGen{token: "generated-token"}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	if err := svc.Run(testConfig()); err != nil {
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
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) {
		return "", errors.New("network error")
	}}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: false, dirPath: "/repo/abc123"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore("/tmp/test-codetap")
	tg := &mockTokenGen{token: "tok"}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	err := svc.Run(testConfig())
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
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) { return "/cache/t.tar.gz", nil }}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return errors.New("corrupt tarball") }}
	pr := &mockProvisioner{provisioned: false, dirPath: "/repo/abc123"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore("/tmp/test-codetap")
	tg := &mockTokenGen{token: "tok"}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	err := svc.Run(testConfig())
	if err == nil {
		t.Fatal("expected error from extract failure")
	}
	if sr.called {
		t.Error("expected runner NOT to be called after extract error")
	}
}

func TestRun_TokenError(t *testing.T) {
	dl := &mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }}
	ex := &mockExtractor{extractFn: func(_, _ string) error { return nil }}
	pr := &mockProvisioner{provisioned: true, binPath: "/bin/code-server"}
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore("/tmp/test-codetap")
	tg := &mockTokenGen{err: errors.New("entropy exhausted")}

	svc := newTestService(dl, ex, pr, sr, st, tg)

	err := svc.Run(testConfig())
	if err == nil {
		t.Fatal("expected error from token generation failure")
	}
	if sr.called {
		t.Error("expected runner NOT to be called after token error")
	}
}

func TestRun_Cleanup(t *testing.T) {
	sr := &mockRunner{startFn: func(_, _, _ string) error { return nil }}
	st := newMockStore("/tmp/test-codetap")

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		sr, st,
		&mockTokenGen{token: "tok"},
	)

	if err := svc.Run(testConfig()); err != nil {
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

func TestRun_CleanupOnError(t *testing.T) {
	sr := &mockRunner{startFn: func(_, _, _ string) error { return errors.New("server crashed") }}
	st := newMockStore("/tmp/test-codetap")

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		sr, st,
		&mockTokenGen{token: "tok"},
	)

	if err := svc.Run(testConfig()); err == nil {
		t.Fatal("expected Run() to return server error")
	}

	found := false
	for _, name := range st.removed {
		if name == "test-session" {
			found = true
		}
	}
	if !found {
		t.Error("expected cleanup even when server errors")
	}
}

func TestRun_MetadataWritten(t *testing.T) {
	st := newMockStore("/tmp/test-codetap")

	svc := newTestService(
		&mockDownloader{downloadFn: func(_, _ string) (string, error) { return "", nil }},
		&mockExtractor{extractFn: func(_, _ string) error { return nil }},
		&mockProvisioner{provisioned: true, binPath: "/bin/cs"},
		&mockRunner{startFn: func(_, _, _ string) error { return nil }},
		st,
		&mockTokenGen{token: "my-token"},
	)

	if err := svc.Run(testConfig()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	meta, ok := st.metadata["test-session"]
	if !ok {
		t.Fatal("expected metadata to be written for 'test-session'")
	}
	if meta.Commit != "abc123" {
		t.Errorf("metadata commit = %q, want %q", meta.Commit, "abc123")
	}
	if meta.Arch != "x64" {
		t.Errorf("metadata arch = %q, want %q", meta.Arch, "x64")
	}
	if meta.Folder != "/workspace" {
		t.Errorf("metadata folder = %q, want %q", meta.Folder, "/workspace")
	}

	tok, ok := st.tokens["test-session"]
	if !ok {
		t.Fatal("expected token to be written")
	}
	if tok != "my-token" {
		t.Errorf("token = %q, want %q", tok, "my-token")
	}
}

func TestList_ReturnsEntries(t *testing.T) {
	st := newMockStore("/tmp/test-codetap")
	st.entries = []domain.SocketEntry{
		{Name: "session-1", Path: "/tmp/s1.sock", Alive: true},
		{Name: "session-2", Path: "/tmp/s2.sock", Alive: false},
	}

	svc := newTestService(nil, nil, nil, nil, st, &mockTokenGen{})

	entries, err := svc.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "session-1" || !entries[0].Alive {
		t.Errorf("entry 0: got %+v", entries[0])
	}
	if entries[1].Name != "session-2" || entries[1].Alive {
		t.Errorf("entry 1: got %+v", entries[1])
	}
}

func TestClean_RemovesStale(t *testing.T) {
	st := newMockStore("/tmp/test-codetap")
	st.entries = []domain.SocketEntry{
		{Name: "alive", Alive: true},
		{Name: "dead-1", Alive: false},
		{Name: "dead-2", Alive: false},
	}

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
	st := newMockStore("/tmp/test-codetap")
	st.entries = []domain.SocketEntry{
		{Name: "alive-1", Alive: true},
		{Name: "alive-2", Alive: true},
	}

	svc := newTestService(nil, nil, nil, nil, st, &mockTokenGen{})

	if err := svc.Clean(); err != nil {
		t.Fatalf("Clean() error: %v", err)
	}

	if len(st.removed) != 0 {
		t.Errorf("expected 0 removals, got %d: %v", len(st.removed), st.removed)
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

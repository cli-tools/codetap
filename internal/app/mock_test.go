package app

import (
	"os"
	"path/filepath"
	"strings"
)

// mockDownloader records calls and returns configured values.
type mockDownloader struct {
	downloadFn func(commit, arch string) (string, error)
	called     bool
	lastCommit string
	lastArch   string
}

func (m *mockDownloader) Download(commit, arch string) (string, error) {
	m.called = true
	m.lastCommit = commit
	m.lastArch = arch
	return m.downloadFn(commit, arch)
}

// mockExtractor records calls and returns configured values.
type mockExtractor struct {
	extractFn   func(tarball, target string) error
	called      bool
	lastTarball string
	lastTarget  string
}

func (m *mockExtractor) Extract(tarball, target string) error {
	m.called = true
	m.lastTarball = tarball
	m.lastTarget = target
	return m.extractFn(tarball, target)
}

// mockProvisioner returns configured provisioning state.
type mockProvisioner struct {
	provisioned bool
	binPath     string
	dirPath     string
}

func (m *mockProvisioner) IsProvisioned(commit string) bool   { return m.provisioned }
func (m *mockProvisioner) ServerBinPath(commit string) string { return m.binPath }
func (m *mockProvisioner) ServerDir(commit string) string     { return m.dirPath }

// mockRunner records the Start call and returns configured error.
type mockRunner struct {
	startFn    func(bin, sock, token string) error
	called     bool
	lastBin    string
	lastSocket string
	lastToken  string
}

func (m *mockRunner) Start(bin, sock, token string) (func() error, func(), error) {
	m.called = true
	m.lastBin = bin
	m.lastSocket = sock
	m.lastToken = token
	err := m.startFn(bin, sock, token)
	if err != nil {
		return nil, nil, err
	}
	// Create the socket file so waitForSocket succeeds in tests.
	_ = os.WriteFile(sock, nil, 0600)
	wait := func() error { return nil }
	stop := func() {}
	return wait, stop, nil
}

// mockStore is an in-memory MetadataStore.
type mockStore struct {
	socketDir string
	removed   []string
	// ctlSockNames tracks names that have a ctl socket file (for ListSessionNames).
	ctlSockNames []string
}

func newMockStore(socketDir string) *mockStore {
	return &mockStore{socketDir: socketDir}
}

func (m *mockStore) SocketPath(name string) string {
	return filepath.Join(m.socketDir, name+".sock")
}

func (m *mockStore) CtlSocketPath(name string) string {
	return filepath.Join(m.socketDir, name+".ctl.sock")
}

func (m *mockStore) ListSessionNames() ([]string, error) {
	// If ctlSockNames is set, return those. Otherwise, scan directory.
	if m.ctlSockNames != nil {
		return m.ctlSockNames, nil
	}
	// Scan the actual directory for *.ctl.sock files.
	entries, err := os.ReadDir(m.socketDir)
	if err != nil {
		return nil, nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ctl.sock") {
			names = append(names, strings.TrimSuffix(e.Name(), ".ctl.sock"))
		}
	}
	return names, nil
}

func (m *mockStore) Remove(name string) error {
	m.removed = append(m.removed, name)
	os.Remove(m.SocketPath(name))
	os.Remove(m.CtlSocketPath(name))
	return nil
}

func (m *mockStore) EnsureDir() error {
	return os.MkdirAll(m.socketDir, 0755)
}

// mockTokenGen returns a fixed token.
type mockTokenGen struct {
	token string
	err   error
}

func (m *mockTokenGen) Generate() (string, error) {
	return m.token, m.err
}

// mockLogger is a no-op logger.
type mockLogger struct {
	messages []string
}

func (m *mockLogger) Info(msg string, args ...any)  { m.messages = append(m.messages, msg) }
func (m *mockLogger) Error(msg string, args ...any) { m.messages = append(m.messages, "ERROR: "+msg) }

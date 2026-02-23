package app

import "codetap/internal/domain"

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
	wait := func() error { return nil }
	stop := func() {}
	return wait, stop, nil
}

// mockStore is an in-memory MetadataStore.
type mockStore struct {
	metadata  map[string]domain.Metadata
	tokens    map[string]string
	removed   []string
	entries   []domain.SocketEntry
	socketDir string
}

func newMockStore(socketDir string) *mockStore {
	return &mockStore{
		metadata:  make(map[string]domain.Metadata),
		tokens:    make(map[string]string),
		socketDir: socketDir,
	}
}

func (m *mockStore) WriteMetadata(meta domain.Metadata) error {
	m.metadata[meta.Name] = meta
	return nil
}

func (m *mockStore) WriteToken(name, token string) error {
	m.tokens[name] = token
	return nil
}

func (m *mockStore) ReadMetadata(name string) (domain.Metadata, error) {
	return m.metadata[name], nil
}

func (m *mockStore) ReadToken(name string) (string, error) {
	return m.tokens[name], nil
}

func (m *mockStore) ListEntries() ([]domain.SocketEntry, error) {
	return m.entries, nil
}

func (m *mockStore) Remove(name string) error {
	m.removed = append(m.removed, name)
	return nil
}

func (m *mockStore) SocketPath(name string) string {
	return m.socketDir + "/" + name + ".sock"
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

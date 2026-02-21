package domain

// Downloader fetches the VS Code Server tarball for a given commit and arch.
// If already cached, it returns the cached path immediately.
type Downloader interface {
	Download(commit, arch string) (tarballPath string, err error)
}

// Extractor unpacks a server tarball into a target directory.
type Extractor interface {
	Extract(tarballPath, targetDir string) error
}

// Provisioner checks whether a server is already extracted and ready.
type Provisioner interface {
	IsProvisioned(commit string) bool
	ServerBinPath(commit string) string
	ServerDir(commit string) string
}

// ServerRunner starts the VS Code Server process on a Unix socket.
// Start blocks until the process exits.
type ServerRunner interface {
	Start(binPath, socketPath, token string) error
}

// MetadataStore persists and reads socket metadata and connection tokens.
type MetadataStore interface {
	WriteMetadata(m Metadata) error
	WriteToken(name, token string) error
	ReadMetadata(name string) (Metadata, error)
	ReadToken(name string) (string, error)
	ListEntries() ([]SocketEntry, error)
	Remove(name string) error
	SocketPath(name string) string
}

// TokenGenerator creates cryptographically secure connection tokens.
type TokenGenerator interface {
	Generate() (string, error)
}

// Logger provides structured logging.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

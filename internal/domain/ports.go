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
// Start launches the process and returns a wait function that blocks until the
// process exits and a stop function that terminates the process group.
type ServerRunner interface {
	Start(binPath, socketPath, token string) (wait func() error, stop func(), err error)
}

// MetadataStore manages socket paths and session discovery in the socket directory.
// All session metadata is served over the CTAP1 control protocol rather than files.
type MetadataStore interface {
	SocketPath(name string) string
	CtlSocketPath(name string) string
	ListSessionNames() ([]string, error)
	Remove(name string) error
	EnsureDir() error
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

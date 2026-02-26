package store

import (
	"os"
	"path/filepath"
	"strings"
)

// FileStore manages socket paths and session discovery in a directory.
type FileStore struct {
	socketDir string
}

// NewFileStore creates a store rooted at socketDir.
func NewFileStore(socketDir string) *FileStore {
	return &FileStore{socketDir: socketDir}
}

// SocketPath returns the data socket path for a session.
func (s *FileStore) SocketPath(name string) string {
	return filepath.Join(s.socketDir, name+".sock")
}

// CtlSocketPath returns the control socket path for a session.
func (s *FileStore) CtlSocketPath(name string) string {
	return filepath.Join(s.socketDir, name+".ctl.sock")
}

// ListSessionNames discovers sessions by globbing *.ctl.sock and returns
// the session names (without the .ctl.sock suffix).
func (s *FileStore) ListSessionNames() ([]string, error) {
	pattern := filepath.Join(s.socketDir, "*.ctl.sock")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(matches))
	for _, m := range matches {
		base := filepath.Base(m)
		name := strings.TrimSuffix(base, ".ctl.sock")
		names = append(names, name)
	}
	return names, nil
}

// Remove deletes the control and data socket files for the given session name.
func (s *FileStore) Remove(name string) error {
	os.Remove(s.CtlSocketPath(name))
	os.Remove(s.SocketPath(name))
	return nil
}

// EnsureDir creates the socket directory if it does not exist.
func (s *FileStore) EnsureDir() error {
	return os.MkdirAll(s.socketDir, 0755)
}

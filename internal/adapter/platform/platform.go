package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const defaultSocketDir = "/dev/shm/codetap"

// Platform resolves architecture and filesystem paths.
type Platform struct {
	homeDir string
}

// New creates a Platform rooted at the current user's home directory.
func New() (*Platform, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	return &Platform{homeDir: home}, nil
}

// DetectArch returns the Microsoft-style architecture string (x64, arm64).
func (p *Platform) DetectArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}
}

// ResolveSocketDir returns the socket directory, checking flag, env, then default.
func (p *Platform) ResolveSocketDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("CODETAP_SOCKET_DIR"); v != "" {
		return v
	}
	return defaultSocketDir
}

// CacheDir returns the tarball cache directory (~/.codetap/cache).
func (p *Platform) CacheDir() string {
	return filepath.Join(p.homeDir, ".codetap", "cache")
}

// RepositoryDir returns the base directory for extracted servers (~/.codetap/repository).
func (p *Platform) RepositoryDir() string {
	return filepath.Join(p.homeDir, ".codetap", "repository")
}

// ResolveCommit resolves the commit hash from flag, env var, or file.
func (p *Platform) ResolveCommit(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv("CODETAP_COMMIT"); v != "" {
		return v, nil
	}
	commitFile := filepath.Join(p.homeDir, ".codetap", ".commit")
	data, err := os.ReadFile(commitFile)
	if err == nil {
		s := trimSpace(data)
		if s != "" {
			return s, nil
		}
	}
	return "", nil
}

func trimSpace(b []byte) string {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	return string(b[start:end])
}

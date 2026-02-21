package store

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codetap/internal/domain"
)

// FileStore persists socket metadata and tokens as files in a directory.
type FileStore struct {
	socketDir string
}

// NewFileStore creates a store rooted at socketDir.
func NewFileStore(socketDir string) *FileStore {
	return &FileStore{socketDir: socketDir}
}

// SocketPath returns the full path for a named socket.
func (s *FileStore) SocketPath(name string) string {
	return filepath.Join(s.socketDir, name+".sock")
}

func (s *FileStore) metadataPath(name string) string {
	return filepath.Join(s.socketDir, name+".json")
}

func (s *FileStore) tokenPath(name string) string {
	return filepath.Join(s.socketDir, name+".token")
}

func (s *FileStore) commitPath(name string) string {
	return filepath.Join(s.socketDir, name+".commit")
}

// WriteMetadata writes the metadata JSON file (mode 0644).
func (s *FileStore) WriteMetadata(m domain.Metadata) error {
	if err := os.MkdirAll(s.socketDir, 0755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	return os.WriteFile(s.metadataPath(m.Name), data, 0644)
}

// WriteToken writes the connection token file (mode 0600).
func (s *FileStore) WriteToken(name, token string) error {
	if err := os.MkdirAll(s.socketDir, 0755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	return os.WriteFile(s.tokenPath(name), []byte(token), 0600)
}

// ReadMetadata reads and parses a metadata JSON file.
func (s *FileStore) ReadMetadata(name string) (domain.Metadata, error) {
	data, err := os.ReadFile(s.metadataPath(name))
	if err != nil {
		return domain.Metadata{}, fmt.Errorf("read metadata: %w", err)
	}

	var m domain.Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return domain.Metadata{}, fmt.Errorf("parse metadata: %w", err)
	}
	return m, nil
}

// ReadToken reads a connection token file.
func (s *FileStore) ReadToken(name string) (string, error) {
	data, err := os.ReadFile(s.tokenPath(name))
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// ListEntries discovers all sockets in the directory by scanning for *.json files.
func (s *FileStore) ListEntries() ([]domain.SocketEntry, error) {
	pattern := filepath.Join(s.socketDir, "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob socket dir: %w", err)
	}

	var entries []domain.SocketEntry
	for _, jsonPath := range matches {
		base := filepath.Base(jsonPath)
		name := strings.TrimSuffix(base, ".json")

		m, err := s.ReadMetadata(name)
		if err != nil {
			continue
		}

		sockPath := s.SocketPath(name)
		alive := isSocketAlive(sockPath)

		entries = append(entries, domain.SocketEntry{
			Name:     name,
			Path:     sockPath,
			Metadata: m,
			Alive:    alive,
		})
	}

	return entries, nil
}

// Remove deletes the socket, token, metadata, and commit files for the given name.
func (s *FileStore) Remove(name string) error {
	os.Remove(s.SocketPath(name))
	os.Remove(s.tokenPath(name))
	os.Remove(s.metadataPath(name))
	os.Remove(s.commitPath(name))
	return nil
}

func isSocketAlive(path string) bool {
	conn, err := net.DialTimeout("unix", path, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

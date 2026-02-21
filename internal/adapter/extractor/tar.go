package extractor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"codetap/internal/domain"
)

// TarExtractor extracts VS Code Server tarballs using the system tar command.
type TarExtractor struct {
	repoBaseDir string
	logger      domain.Logger
}

// NewTarExtractor creates an extractor that places servers under repoBaseDir/<commit>/.
func NewTarExtractor(repoBaseDir string, logger domain.Logger) *TarExtractor {
	return &TarExtractor{
		repoBaseDir: repoBaseDir,
		logger:      logger,
	}
}

// IsProvisioned checks if the server binary exists for the given commit.
func (e *TarExtractor) IsProvisioned(commit string) bool {
	bin := e.ServerBinPath(commit)
	info, err := os.Stat(bin)
	return err == nil && !info.IsDir()
}

// ServerBinPath returns the path to the code-server binary for a commit.
func (e *TarExtractor) ServerBinPath(commit string) string {
	return filepath.Join(e.repoBaseDir, commit, "bin", "code-server")
}

// ServerDir returns the directory for an extracted server for the given commit.
func (e *TarExtractor) ServerDir(commit string) string {
	return filepath.Join(e.repoBaseDir, commit)
}

// Extract unpacks the tarball into repoBaseDir/<commit>/.
// The MS tarball has a top-level directory that is stripped.
func (e *TarExtractor) Extract(tarballPath, targetDir string) error {
	bin := filepath.Join(targetDir, "bin", "code-server")
	if info, err := os.Stat(bin); err == nil && !info.IsDir() {
		e.logger.Info("server already extracted", "path", targetDir)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return fmt.Errorf("create server base dir: %w", err)
	}

	// Extract to a temp dir first, then rename atomically
	tmpDir, err := os.MkdirTemp(filepath.Dir(targetDir), ".extract-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	e.logger.Info("extracting server", "tarball", tarballPath, "target", targetDir)

	cmd := exec.Command("tar", "xzf", tarballPath, "-C", tmpDir, "--strip-components=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("tar extract: %w", err)
	}

	// Verify the binary exists in extracted output
	extractedBin := filepath.Join(tmpDir, "bin", "code-server")
	if _, err := os.Stat(extractedBin); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("extracted tarball missing bin/code-server — corrupt download? delete %s and retry", tarballPath)
	}

	if err := os.Rename(tmpDir, targetDir); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("rename extracted dir: %w", err)
	}

	e.logger.Info("extraction complete", "path", targetDir)
	return nil
}

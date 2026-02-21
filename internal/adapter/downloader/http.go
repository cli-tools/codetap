package downloader

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"codetap/internal/domain"
)

const urlTemplate = "https://update.code.visualstudio.com/commit:%s/server-linux-%s/stable"

// HTTPDownloader downloads VS Code Server tarballs via HTTP.
type HTTPDownloader struct {
	cacheDir string
	logger   domain.Logger
}

// NewHTTPDownloader creates a downloader that caches tarballs in cacheDir.
func NewHTTPDownloader(cacheDir string, logger domain.Logger) *HTTPDownloader {
	return &HTTPDownloader{
		cacheDir: cacheDir,
		logger:   logger,
	}
}

// Download fetches the server tarball for the given commit and arch.
// Returns the path to the cached tarball. Skips download if already cached.
func (d *HTTPDownloader) Download(commit, arch string) (string, error) {
	filename := fmt.Sprintf("%s-%s.tar.gz", commit, arch)
	dest := filepath.Join(d.cacheDir, filename)

	if _, err := os.Stat(dest); err == nil {
		d.logger.Info("using cached tarball", "path", dest)
		return dest, nil
	}

	if err := os.MkdirAll(d.cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	url := fmt.Sprintf(urlTemplate, commit, arch)
	d.logger.Info("downloading VS Code Server", "commit", commit, "arch", arch)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("VS Code Server commit %s not found for arch %s — verify the commit hash matches your VS Code version", commit, arch)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	// Write to temp file then rename atomically
	tmp, err := os.CreateTemp(d.cacheDir, ".download-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("write tarball: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", closeErr)
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("rename tarball: %w", err)
	}

	d.logger.Info("download complete", "path", dest)
	return dest, nil
}

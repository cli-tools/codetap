package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDetectArch(t *testing.T) {
	p := &Platform{homeDir: "/tmp"}
	arch, err := p.DetectArch()
	if err != nil {
		t.Fatalf("DetectArch() error: %v", err)
	}
	switch runtime.GOARCH {
	case "amd64":
		if arch != "x64" {
			t.Errorf("expected x64, got %s", arch)
		}
	case "arm64":
		if arch != "arm64" {
			t.Errorf("expected arm64, got %s", arch)
		}
	default:
		t.Skipf("unsupported arch: %s", runtime.GOARCH)
	}
}

func TestResolveSocketDir_FlagPriority(t *testing.T) {
	p := &Platform{homeDir: "/tmp"}
	t.Setenv("CODETAP_SOCKET_DIR", "/env/dir")

	result := p.ResolveSocketDir("/flag/dir")
	if result != "/flag/dir" {
		t.Errorf("expected /flag/dir, got %s", result)
	}
}

func TestResolveSocketDir_EnvPriority(t *testing.T) {
	p := &Platform{homeDir: "/tmp"}
	t.Setenv("CODETAP_SOCKET_DIR", "/env/dir")

	result := p.ResolveSocketDir("")
	if result != "/env/dir" {
		t.Errorf("expected /env/dir, got %s", result)
	}
}

func TestResolveSocketDir_Default(t *testing.T) {
	p := &Platform{homeDir: "/tmp"}
	t.Setenv("CODETAP_SOCKET_DIR", "")

	result := p.ResolveSocketDir("")
	if result != "/dev/shm/codetap" {
		t.Errorf("expected /dev/shm/codetap, got %s", result)
	}
}

func TestCacheDir(t *testing.T) {
	p := &Platform{homeDir: "/home/user"}
	want := "/home/user/.codetap/cache"
	if got := p.CacheDir(); got != want {
		t.Errorf("CacheDir() = %q, want %q", got, want)
	}
}

func TestRepositoryDir(t *testing.T) {
	p := &Platform{homeDir: "/home/user"}
	want := "/home/user/.codetap/repository"
	if got := p.RepositoryDir(); got != want {
		t.Errorf("RepositoryDir() = %q, want %q", got, want)
	}
}

func TestResolveCommit_Flag(t *testing.T) {
	p := &Platform{homeDir: "/tmp"}
	commit, err := p.ResolveCommit("abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commit != "abc123" {
		t.Errorf("expected abc123, got %s", commit)
	}
}

func TestResolveCommit_Env(t *testing.T) {
	p := &Platform{homeDir: "/tmp"}
	t.Setenv("CODETAP_COMMIT", "env-commit")

	commit, err := p.ResolveCommit("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commit != "env-commit" {
		t.Errorf("expected env-commit, got %s", commit)
	}
}

func TestResolveCommit_File(t *testing.T) {
	tmpDir := t.TempDir()
	p := &Platform{homeDir: tmpDir}
	t.Setenv("CODETAP_COMMIT", "")

	commitDir := filepath.Join(tmpDir, ".codetap")
	os.MkdirAll(commitDir, 0755)
	os.WriteFile(filepath.Join(commitDir, ".commit"), []byte("file-commit\n"), 0644)

	commit, err := p.ResolveCommit("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commit != "file-commit" {
		t.Errorf("expected file-commit, got %s", commit)
	}
}

func TestResolveCommit_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	p := &Platform{homeDir: tmpDir}
	t.Setenv("CODETAP_COMMIT", "")

	commit, err := p.ResolveCommit("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commit != "" {
		t.Errorf("expected empty string, got %q", commit)
	}
}

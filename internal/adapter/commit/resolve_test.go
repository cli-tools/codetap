package commit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testCommit = "abc123def456abc123def456abc123def456abc1"

func TestResolve_HexHash(t *testing.T) {
	r := NewResolver("x64")
	got, err := r.Resolve(testCommit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testCommit {
		t.Errorf("got %q, want %q", got, testCommit)
	}
}

func TestResolve_HexHash_Uppercase(t *testing.T) {
	r := NewResolver("x64")
	upper := strings.ToUpper(testCommit)
	got, err := r.Resolve(upper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testCommit {
		t.Errorf("got %q, want %q (lowercased)", got, testCommit)
	}
}

func TestResolve_Empty(t *testing.T) {
	r := NewResolver("x64")
	got, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestResolve_Whitespace(t *testing.T) {
	r := NewResolver("x64")
	got, err := r.Resolve("  \n  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestResolve_InvalidFormat(t *testing.T) {
	r := NewResolver("x64")
	_, err := r.Resolve("not-a-commit")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid commit value") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolve_Semver(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/update/server-linux-x64/stable/1.109.5") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(updateResponse{
			Version:        testCommit,
			ProductVersion: "1.109.5",
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer ts.Close()

	r := &Resolver{arch: "x64", baseURL: ts.URL, client: ts.Client()}
	got, err := r.Resolve("1.109.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testCommit {
		t.Errorf("got %q, want %q", got, testCommit)
	}
}

func TestResolve_Latest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "0000000000000000000000000000000000000000") {
			t.Errorf("expected dummy hash in URL, got path: %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(updateResponse{
			Version:        testCommit,
			ProductVersion: "1.109.5",
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer ts.Close()

	r := &Resolver{arch: "x64", baseURL: ts.URL, client: ts.Client()}
	got, err := r.Resolve("latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testCommit {
		t.Errorf("got %q, want %q", got, testCommit)
	}
}

func TestResolve_Latest_CaseInsensitive(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(updateResponse{Version: testCommit}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer ts.Close()

	r := &Resolver{arch: "x64", baseURL: ts.URL, client: ts.Client()}
	got, err := r.Resolve("LATEST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testCommit {
		t.Errorf("got %q, want %q", got, testCommit)
	}
}

func TestResolve_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	r := &Resolver{arch: "x64", baseURL: ts.URL, client: ts.Client()}
	_, err := r.Resolve("latest")
	if err == nil {
		t.Fatal("expected error for API 500")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolve_APIBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer ts.Close()

	r := &Resolver{arch: "x64", baseURL: ts.URL, client: ts.Client()}
	_, err := r.Resolve("latest")
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
	if !strings.Contains(err.Error(), "invalid API response") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolve_APIBadVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(updateResponse{Version: "not-a-hash"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer ts.Close()

	r := &Resolver{arch: "x64", baseURL: ts.URL, client: ts.Client()}
	_, err := r.Resolve("latest")
	if err == nil {
		t.Fatal("expected error for non-hex version")
	}
	if !strings.Contains(err.Error(), "unexpected version format") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolve_ArchInURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "server-linux-arm64") {
			t.Errorf("expected arm64 in URL, got: %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(updateResponse{Version: testCommit}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer ts.Close()

	r := &Resolver{arch: "arm64", baseURL: ts.URL, client: ts.Client()}
	_, err := r.Resolve("latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProbeCodeCLI_NotFound(t *testing.T) {
	// With empty PATH, "code" should not be found
	t.Setenv("PATH", "")
	got, err := ProbeCodeCLI()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

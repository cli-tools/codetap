package commit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	hexHashRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
	semverRe  = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

const defaultBaseURL = "https://update.code.visualstudio.com"

// updateResponse is the JSON returned by the Microsoft Update API.
type updateResponse struct {
	Version        string `json:"version"`        // 40-char commit hash
	ProductVersion string `json:"productVersion"` // e.g., "1.109.5"
}

// Resolver resolves a VS Code Server commit hash from various input formats.
type Resolver struct {
	arch    string
	client  *http.Client
	baseURL string
}

// NewResolver creates a Resolver for the given architecture (x64 or arm64).
func NewResolver(arch string) *Resolver {
	return &Resolver{
		arch:    arch,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Resolve takes a raw input and returns a 40-character commit hash.
// Input may be a 40-char hex hash, a semver like "1.109.5", or "latest".
// Returns ("", nil) if input is empty.
func (r *Resolver) Resolve(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}

	lower := strings.ToLower(input)

	if hexHashRe.MatchString(lower) {
		return lower, nil
	}

	if semverRe.MatchString(input) {
		return r.resolveVersion(input)
	}

	if lower == "latest" {
		return r.resolveLatest()
	}

	return "", fmt.Errorf("invalid commit value %q: expected 40-char hex hash, version (e.g. 1.109.5), or \"latest\"", input)
}

// resolveVersion resolves a semantic version to a commit hash via the update API.
func (r *Resolver) resolveVersion(version string) (string, error) {
	url := fmt.Sprintf("%s/api/update/server-linux-%s/stable/%s", r.baseURL, r.arch, version)
	return r.fetchCommit(url)
}

// resolveLatest resolves "latest" to the current stable commit hash.
func (r *Resolver) resolveLatest() (string, error) {
	url := fmt.Sprintf("%s/api/update/server-linux-%s/stable/0000000000000000000000000000000000000000", r.baseURL, r.arch)
	return r.fetchCommit(url)
}

func (r *Resolver) fetchCommit(url string) (string, error) {
	resp, err := r.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("resolve commit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve commit: update API returned HTTP %d", resp.StatusCode)
	}

	var result updateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("resolve commit: invalid API response: %w", err)
	}

	if !hexHashRe.MatchString(result.Version) {
		return "", fmt.Errorf("resolve commit: API returned unexpected version format %q", result.Version)
	}

	return result.Version, nil
}

// ProbeCodeCLI attempts to detect the commit hash from a local "code --version".
// Returns ("", nil) if the command is not available or fails.
func ProbeCodeCLI() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "code", "--version").Output()
	if err != nil {
		return "", nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "", nil
	}

	candidate := strings.TrimSpace(lines[1])
	if hexHashRe.MatchString(candidate) {
		return candidate, nil
	}

	return "", nil
}

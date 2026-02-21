package token

import (
	"crypto/rand"
	"fmt"
)

// RandomGenerator produces cryptographically secure hex tokens.
type RandomGenerator struct{}

// NewRandomGenerator creates a token generator backed by crypto/rand.
func NewRandomGenerator() *RandomGenerator {
	return &RandomGenerator{}
}

// Generate returns a 32-byte random hex string (64 chars).
func (g *RandomGenerator) Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
}

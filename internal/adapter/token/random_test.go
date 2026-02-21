package token

import "testing"

func TestGenerate_Length(t *testing.T) {
	g := NewRandomGenerator()
	tok, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(tok))
	}
}

func TestGenerate_Uniqueness(t *testing.T) {
	g := NewRandomGenerator()
	t1, _ := g.Generate()
	t2, _ := g.Generate()
	if t1 == t2 {
		t.Error("consecutive tokens should differ")
	}
}

func TestGenerate_HexOnly(t *testing.T) {
	g := NewRandomGenerator()
	tok, _ := g.Generate()
	for _, c := range tok {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex character: %c", c)
		}
	}
}

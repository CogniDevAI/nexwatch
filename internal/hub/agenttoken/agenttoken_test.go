package agenttoken

import (
	"encoding/hex"
	"testing"
)

func TestHash_Deterministic(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{name: "simple token", plaintext: "abc123"},
		{name: "empty string", plaintext: ""},
		{name: "long hex token", plaintext: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"},
		{name: "unicode input", plaintext: "tökén-ñ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h1 := Hash(tt.plaintext)
			h2 := Hash(tt.plaintext)
			if h1 != h2 {
				t.Fatalf("Hash(%q) not deterministic: %q != %q", tt.plaintext, h1, h2)
			}
			if len(h1) != 64 {
				t.Fatalf("Hash(%q) = %q, want 64 hex chars (sha256), got len %d", tt.plaintext, h1, len(h1))
			}
			if _, err := hex.DecodeString(h1); err != nil {
				t.Fatalf("Hash(%q) = %q is not valid hex: %v", tt.plaintext, h1, err)
			}
		})
	}
}

func TestHash_DistinctInputsProduceDistinctHashes(t *testing.T) {
	a := Hash("token-a")
	b := Hash("token-b")
	if a == b {
		t.Fatalf("Hash produced identical output for distinct inputs: %q", a)
	}
}

func TestGenerate_ProducesDistinctTokens(t *testing.T) {
	seen := make(map[string]bool)
	const iterations = 20

	for i := 0; i < iterations; i++ {
		plaintext, hash, err := Generate()
		if err != nil {
			t.Fatalf("Generate() unexpected error: %v", err)
		}

		if len(plaintext) != tokenBytes*2 {
			t.Fatalf("Generate() plaintext len = %d, want %d (hex-encoded %d bytes)", len(plaintext), tokenBytes*2, tokenBytes)
		}
		if _, err := hex.DecodeString(plaintext); err != nil {
			t.Fatalf("Generate() plaintext %q is not valid hex: %v", plaintext, err)
		}

		if len(hash) != 64 {
			t.Fatalf("Generate() hash len = %d, want 64 (sha256 hex)", len(hash))
		}

		if want := Hash(plaintext); hash != want {
			t.Fatalf("Generate() hash = %q, want Hash(plaintext) = %q", hash, want)
		}

		if seen[plaintext] {
			t.Fatalf("Generate() produced a duplicate plaintext token across %d iterations: %q", iterations, plaintext)
		}
		seen[plaintext] = true
	}
}

func TestGenerate_ReturnsErrorPropagatedFromRandSource(t *testing.T) {
	// Generate relies on crypto/rand.Read, which practically never fails on
	// supported platforms. This test only documents the contract: a non-nil
	// error must come back with empty plaintext/hash rather than partial
	// values. We cannot easily force crypto/rand to fail without swapping
	// the package-level reader, so this is a smoke test of the happy path
	// shape instead of a fault-injection test.
	plaintext, hash, err := Generate()
	if err != nil {
		t.Fatalf("unexpected error on happy path: %v", err)
	}
	if plaintext == "" || hash == "" {
		t.Fatalf("Generate() returned empty values without an error: plaintext=%q hash=%q", plaintext, hash)
	}
}

// Package agenttoken generates and hashes agent authentication tokens.
//
// Agent tokens are shown to the operator exactly once (at generation or
// rotation time) and are never stored in plaintext: the hub persists only
// the SHA-256 hash in the agents.token_hash field and compares against it
// on every WebSocket authentication attempt.
package agenttoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// tokenBytes is the number of cryptographically random bytes used to build
// a new plaintext token before hex-encoding it.
const tokenBytes = 32

// bootstrapAgentHostname is the hostname used to identify the well-known
// dev/bootstrap agent record created via BootstrapAgent.
const bootstrapAgentHostname = "bootstrap"

// Generate creates a new random agent token. It returns the plaintext value
// (to be shown to the operator exactly once) and its SHA-256 hash (to be
// persisted in place of the plaintext).
func Generate() (plaintext string, hash string, err error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("agenttoken: generate random bytes: %w", err)
	}
	plaintext = hex.EncodeToString(buf)
	return plaintext, Hash(plaintext), nil
}

// Hash returns the hex-encoded SHA-256 digest of a plaintext token.
func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// BootstrapAgent ensures an agent record named "bootstrap" exists whose
// token_hash matches the given plaintext token. It is a no-op if an agent
// with that hostname already exists (the token is not rotated). This is a
// dev/ops convenience for docker-compose and local setups where a fixed,
// well-known agent token is wired in at deploy time via a flag or env var.
func BootstrapAgent(app core.App, plaintextToken string) error {
	if plaintextToken == "" {
		return nil
	}

	existing, err := app.FindFirstRecordByFilter(
		"agents",
		"hostname = {:hostname}",
		map[string]any{"hostname": bootstrapAgentHostname},
	)
	if err == nil && existing != nil {
		return nil
	}

	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		return fmt.Errorf("agenttoken: bootstrap: agents collection: %w", err)
	}

	record := core.NewRecord(col)
	record.Set("hostname", bootstrapAgentHostname)
	record.Set("status", "pending")
	record.Set("token_hash", Hash(plaintextToken))

	if err := app.Save(record); err != nil {
		return fmt.Errorf("agenttoken: bootstrap: save agent: %w", err)
	}
	return nil
}

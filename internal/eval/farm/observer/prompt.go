package observer

import (
	_ "embed"

	"crypto/sha256"
	"encoding/hex"
)

// PromptText is the observer's fixed instruction text, passed as `claude
// -p`'s argument (§7.4). The per-run digest travels separately, on stdin.
//
//go:embed prompt.md
var PromptText string

// PromptSha256 returns the lowercase-hex sha256 of the embedded observer
// prompt — the observation's recorded promptSha256 (§7.4).
func PromptSha256() string {
	sum := sha256.Sum256([]byte(PromptText))
	return hex.EncodeToString(sum[:])
}

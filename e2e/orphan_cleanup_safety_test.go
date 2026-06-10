//go:build e2e

// Tests for: e2e — orphan-cleanup hostname matching safety (C5).
package e2e_test

import "testing"

// TestHasTestPrefix_SafetyGuard pins C5: the orphan cleanup must match
// generated test hostnames (prefix + hex tag) but NEVER a real-word hostname
// that merely shares a prefix — deleting those in a mis-pointed project is the
// exact accident the hex-suffix guard prevents.
func TestHasTestPrefix_SafetyGuard(t *testing.T) {
	t.Parallel()
	match := []string{
		"bsa1b2dev",   // bootstrap: bs + 4hex + dev
		"b6c0d1e2dev", // bootstrap_advanced
		"ba9f8e7stage",
		"zcpdb1a2b",
		"lrv0a1b", // laravel_recipe: lrv + 4hex
		"inc0a1b2",
	}
	noMatch := []string{
		"backend",   // ba + ckend (not 4 hex)
		"basket",    // ba + sket
		"inventory", // in + ventory
		"installer", // in + staller
		"bstaging",  // bs + taging
		"api",
		"db",
		"core",
	}
	for _, h := range match {
		if !hasTestPrefix(h) {
			t.Errorf("hasTestPrefix(%q) = false, want true (generated test hostname)", h)
		}
	}
	for _, h := range noMatch {
		if hasTestPrefix(h) {
			t.Errorf("hasTestPrefix(%q) = true, want false — real-word hostname must NOT be deleted", h)
		}
	}
}

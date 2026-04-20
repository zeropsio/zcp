package workflow

import (
	"encoding/json"
	"testing"
)

// validEditorialReviewPayload returns a minimal all-pass
// EditorialReviewReturn serialized as JSON — the canonical attestation
// shape test fixtures pass to validateEditorialReview when they don't
// need a specific failure scenario. All counts zero, surfaces walked
// non-empty, reclassification delta empty, citation coverage 0/0,
// cross-surface ledger empty. Shared by recipe_close_ordering_test.go
// and the editorial-review validator tests.
func validEditorialReviewPayload() string {
	ret := EditorialReviewReturn{
		SurfacesWalked: []string{"/var/www/README.md"},
	}
	data, err := json.Marshal(ret)
	if err != nil {
		// Unreachable — EditorialReviewReturn has no funcs/channels —
		// but we panic with context so a struct-shape regression is
		// caught immediately at test startup.
		panic("validEditorialReviewPayload: marshal failed: " + err.Error())
	}
	return string(data)
}

// mustMarshalEditorialReviewReturn is a test helper that fails the
// test on marshal failure — unreachable with any well-formed
// EditorialReviewReturn but explicit so tests don't silently ship a
// zero-value attestation when a struct field changes shape.
func mustMarshalEditorialReviewReturn(t *testing.T, ret EditorialReviewReturn) string {
	t.Helper()
	data, err := json.Marshal(ret)
	if err != nil {
		t.Fatalf("marshal EditorialReviewReturn: %v", err)
	}
	return string(data)
}

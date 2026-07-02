// Tests for: shape.go — exported single-value shape checks + the "unknown"
// sentinels used by S3's middleware extraction site (spec §5.3 "Values
// failing shape checks → unknown"). Single-owner discipline: the sentinel
// values themselves must satisfy the very pattern they stand in for, so a
// substituted event never fails wire.ValidateLite/ValidateStrict.
package wire_test

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

func TestValidIdentifierShape_Table(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"valid tool name", "zerops_deploy", true},
		{"valid action", "deploy", true},
		{"valid single letter", "a", true},
		{"uppercase rejected", "Zerops", false},
		{"path-shaped rejected", "/etc/passwd", false},
		{"secret-shaped rejected", "sk-ABCDEF1234567890", false},
		{"whitespace rejected", "not an action", false},
		{"exactly 64 chars accepted", "a" + strings.Repeat("b", 63), true},
		{"65 chars rejected", "a" + strings.Repeat("b", 64), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wire.ValidIdentifierShape(tt.in); got != tt.want {
				t.Errorf("ValidIdentifierShape(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidErrorCodeShape_Table(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"valid code", "DIAGNOSIS_REQUIRED", true},
		{"lowercase rejected", "diagnosis_required", false},
		{"path-shaped rejected", "/etc/passwd", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wire.ValidErrorCodeShape(tt.in); got != tt.want {
				t.Errorf("ValidErrorCodeShape(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestUnknownSentinels_SatisfyOwnShape pins the design invariant the
// extraction site depends on: substituting the sentinel never produces an
// event that ValidateLite/ValidateStrict would then reject.
func TestUnknownSentinels_SatisfyOwnShape(t *testing.T) {
	if !wire.ValidIdentifierShape(wire.UnknownIdentifier) {
		t.Errorf("UnknownIdentifier %q must satisfy ValidIdentifierShape", wire.UnknownIdentifier)
	}
	if !wire.ValidErrorCodeShape(wire.UnknownErrorCode) {
		t.Errorf("UnknownErrorCode %q must satisfy ValidErrorCodeShape", wire.UnknownErrorCode)
	}
}

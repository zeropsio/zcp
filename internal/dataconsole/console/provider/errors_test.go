package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorCode_SentinelCodes(t *testing.T) {
	t.Parallel()
	for _, tc := range errorCodeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := fmt.Errorf("wrapped: %w", tc.err)
			if got := ErrorCode(err); got != tc.code {
				t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, tc.code)
			}
		})
	}
}

func TestErrorCode_UnknownInternal(t *testing.T) {
	t.Parallel()
	if got := ErrorCode(errors.New("raw driver failure")); got != codeInternal {
		t.Fatalf("unknown ErrorCode = %q, want internal", got)
	}
}

func TestErrorCode_HTTPStatusPairing_NoDrift(t *testing.T) {
	t.Parallel()
	for _, tc := range errorCodeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ErrorCode(tc.err); got == "" || got == codeInternal {
				t.Fatalf("ErrorCode(%v) = %q, want sentinel-specific code", tc.err, got)
			}
			if got := HTTPStatus(tc.err); got != tc.status {
				t.Fatalf("HTTPStatus(%v) = %d, want %d", tc.err, got, tc.status)
			}
		})
	}
}

func errorCodeCases() []struct {
	name   string
	err    error
	code   string
	status int
} {
	return []struct {
		name   string
		err    error
		code   string
		status int
	}{
		{name: "not_found", err: ErrNotFound, code: "not_found", status: 404},
		{name: "read_only", err: ErrReadOnly, code: "read_only", status: 403},
		{name: "needs_confirm", err: ErrNeedsConfirm, code: "needs_confirm", status: 409},
		{name: "conflict", err: ErrConflict, code: "conflict", status: 409},
		{name: "too_large", err: ErrTooLarge, code: "too_large", status: 413},
		{name: "unsupported", err: ErrUnsupported, code: "unsupported", status: 422},
		{name: "unreachable", err: ErrUnreachable, code: "unreachable", status: 503},
		{name: "upstream", err: ErrUpstream, code: "upstream", status: 502},
		{name: "invalid", err: ErrInvalid, code: "invalid", status: 400},
	}
}

package provider

import (
	"errors"
	"fmt"
	"strings"
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

// ---- PublicDetailer / WithPublicDetail (EXT-1: the opt-in carrier that
// lets a caller-sanitized detail cross the wire in the client envelope) ----

func TestWithPublicDetail_EmptyDetail_ReturnsErrUnchanged(t *testing.T) {
	t.Parallel()
	base := fmt.Errorf("op: %w", ErrInvalid)
	if got := WithPublicDetail(base, ""); got != base {
		t.Fatalf("WithPublicDetail(err, \"\") = %v, want the identical error value unchanged", got)
	}
}

func TestWithPublicDetail_PreservesSentinelMatching(t *testing.T) {
	t.Parallel()
	base := fmt.Errorf("op: %w", ErrInvalid)
	detailed := WithPublicDetail(base, `syntax error at or near "SELEKT"`)
	if !errors.Is(detailed, ErrInvalid) {
		t.Fatalf("WithPublicDetail(...) = %v, want errors.Is(_, ErrInvalid)", detailed)
	}
	if HTTPStatus(detailed) != 400 {
		t.Fatalf("HTTPStatus(detailed) = %d, want 400", HTTPStatus(detailed))
	}
	if ErrorCode(detailed) != "invalid" {
		t.Fatalf("ErrorCode(detailed) = %q, want invalid", ErrorCode(detailed))
	}
}

func TestWithPublicDetail_ErrorsAsFindsDetail(t *testing.T) {
	t.Parallel()
	base := fmt.Errorf("op: %w", ErrInvalid)
	detailed := WithPublicDetail(base, `syntax error at or near "SELEKT"`)
	var pd PublicDetailer
	if !errors.As(detailed, &pd) {
		t.Fatalf("errors.As(%v, &PublicDetailer) = false, want true", detailed)
	}
	if got := pd.PublicDetail(); got != `syntax error at or near "SELEKT"` {
		t.Fatalf("PublicDetail() = %q, want the wrapped reason", got)
	}
}

// TestWithPublicDetail_SurvivesFurtherWrapping matters because a provider's
// error travels through at least one more fmt.Errorf("...: %w", ...) layer
// (the provider interface's own method-level wrap) before writeErr sees it —
// if the detail didn't survive that, EXT-1 would silently do nothing.
func TestWithPublicDetail_SurvivesFurtherWrapping(t *testing.T) {
	t.Parallel()
	base := fmt.Errorf("op: %w", ErrInvalid)
	detailed := WithPublicDetail(base, "reason text")
	outer := fmt.Errorf("outer context: %w", detailed)
	if !errors.Is(outer, ErrInvalid) {
		t.Fatal("errors.Is(outer, ErrInvalid) = false, want true — sentinel must survive further %w wrapping")
	}
	var pd PublicDetailer
	if !errors.As(outer, &pd) || pd.PublicDetail() != "reason text" {
		t.Fatal("errors.As(outer, &PublicDetailer) did not recover the detail through further wrapping")
	}
}

func TestWithPublicDetail_ErrorTextIncludesBaseAndDetail(t *testing.T) {
	t.Parallel()
	base := fmt.Errorf("tabular: query: %w", ErrInvalid)
	detailed := WithPublicDetail(base, `syntax error at or near "SELEKT"`)
	got := detailed.Error()
	if !strings.Contains(got, "tabular: query") || !strings.Contains(got, `syntax error at or near "SELEKT"`) {
		t.Fatalf("detailed.Error() = %q, want it to contain both the base message and the detail", got)
	}
}

// TestPublicDetailer_AbsentOnPlainSentinelError guards the "purely additive"
// requirement: an error that never went through WithPublicDetail must not
// accidentally satisfy PublicDetailer — otherwise every existing plain
// sentinel wrap across every family would start leaking arbitrary %w'd text.
func TestPublicDetailer_AbsentOnPlainSentinelError(t *testing.T) {
	t.Parallel()
	var pd PublicDetailer
	if errors.As(fmt.Errorf("op: %w", ErrInvalid), &pd) {
		t.Fatal("errors.As found a PublicDetailer on a plain sentinel-wrapped error — WithPublicDetail must be the sole opt-in constructor")
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
		{name: "wrong_type", err: ErrWrongType, code: "wrong_type", status: 409},
		{name: "timeout", err: ErrTimeout, code: "timeout", status: 504},
	}
}

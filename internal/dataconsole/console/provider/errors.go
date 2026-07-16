package provider

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

const codeInternal = "internal"

// Typed error set mapped to HTTP status by the server. Providers return these
// (wrapped via fmt.Errorf("...: %w", Err...)) so the transport never inspects
// driver-specific error text.
var (
	ErrNotFound     = errors.New("not found")             // 404
	ErrReadOnly     = errors.New("read-only")             // 403 — writes disabled
	ErrNeedsConfirm = errors.New("confirmation required") // 409 — write without confirm
	ErrConflict     = errors.New("conflict")              // 409 — optimistic concurrency (0 rows)
	ErrTooLarge     = errors.New("too large")             // 413 — blob over the edit guard
	ErrUnsupported  = errors.New("unsupported")           // 422 — e.g. edit on a no-PK table
	ErrUnreachable  = errors.New("unreachable")           // 503 — network reachability (VPN down)
	ErrUpstream     = errors.New("upstream error")        // 502 — sanitized service error (incl. auth)
	ErrInvalid      = errors.New("invalid request")       // 400
	ErrWrongType    = errors.New("wrong type")            // 409 — write would overwrite a different-shaped value
	ErrTimeout      = errors.New("timeout")               // 504 — upstream accepted the request but did not confirm completion in time
)

// HTTPStatus maps a sentinel error to its status code (default 500).
func HTTPStatus(err error) int {
	switch {
	case err == nil:
		return 200
	case errors.Is(err, ErrNotFound):
		return 404
	case errors.Is(err, ErrReadOnly):
		return 403
	case errors.Is(err, ErrNeedsConfirm), errors.Is(err, ErrConflict), errors.Is(err, ErrWrongType):
		return 409
	case errors.Is(err, ErrTooLarge):
		return 413
	case errors.Is(err, ErrUnsupported):
		return 422
	case errors.Is(err, ErrUnreachable):
		return 503
	case errors.Is(err, ErrUpstream):
		return 502
	case errors.Is(err, ErrInvalid):
		return 400
	case errors.Is(err, ErrTimeout):
		return 504
	default:
		return 500
	}
}

// ErrorCode maps a sentinel error to its stable public machine code (default
// "internal"). Keep this beside HTTPStatus so the status/code contract cannot
// drift across layers.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrReadOnly):
		return "read_only"
	case errors.Is(err, ErrNeedsConfirm):
		return "needs_confirm"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.Is(err, ErrUnsupported):
		return "unsupported"
	case errors.Is(err, ErrUnreachable):
		return "unreachable"
	case errors.Is(err, ErrUpstream):
		return "upstream"
	case errors.Is(err, ErrInvalid):
		return "invalid"
	case errors.Is(err, ErrWrongType):
		return "wrong_type"
	case errors.Is(err, ErrTimeout):
		return "timeout"
	default:
		return codeInternal
	}
}

// IsNetUnreachable reports whether err is a network REACHABILITY failure
// (connection refused / timeout / no route / DNS) — as opposed to an auth or
// protocol error. It is the classifier behind ErrUnreachable, so the UI's VPN
// gate fires only on a genuine private-network reachability failure, never on a
// bad credential (which is a 502, not "bring up the VPN").
func IsNetUnreachable(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return true // a dial/read/write failed at the socket layer
	}
	// Driver-wrapped errors often flatten the cause to a string; match the
	// canonical reachability phrases as a fallback.
	s := strings.ToLower(err.Error())
	for _, m := range []string{"connection refused", "no such host", "i/o timeout",
		"no route to host", "network is unreachable", "operation timed out", "dial tcp"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// HealthErr classifies a preflight/connection error into ErrUnreachable (VPN
// gate) vs ErrUpstream (auth/protocol), without leaking the raw driver text.
func HealthErr(op string, err error) error {
	if IsNetUnreachable(err) {
		return fmt.Errorf("%s: %w", op, ErrUnreachable)
	}
	return fmt.Errorf("%s: %w", op, ErrUpstream)
}

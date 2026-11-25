package platform

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/zeropsio/zerops-go/apiError"
)

// witness: internal/platform/errors.go | return c == ClassUnreachable → return c == ClassRefused
func TestErrorClass_E1_SSHConnectionRefusedIsUnreachableAndWaitedOn(t *testing.T) {
	t.Parallel()
	err := &SSHExecError{Hostname: "webstage", Output: "ssh: connect to host webstage port 22: Connection refused\n", Err: ExitStatusError(255)}
	if c := ClassOf(err); c != ClassUnreachable || !c.WaitedOn() {
		t.Fatalf("ClassOf = %q (waited on %v), want unreachable and waited on", c, c.WaitedOn())
	}
}

// witness: internal/platform/errors.go | if exit == sshUnreachableExit { → if exit == sshUnreachableExit && !strings.Contains(output, "timed out") {
func TestErrorClass_E2_SSHConnectTimedOutIsUnreachableAndWaitedOn(t *testing.T) {
	t.Parallel()
	err := &SSHExecError{Hostname: "webstage", Output: "ssh: connect to host webstage port 22: Connection timed out\n", Err: ExitStatusError(255)}
	if c := ClassOf(err); c != ClassUnreachable || !c.WaitedOn() {
		t.Fatalf("ClassOf = %q (waited on %v), want unreachable and waited on", c, c.WaitedOn())
	}
}

// witness: internal/platform/errors.go | if exit == sshUnreachableExit { → if exit != 0 {
func TestErrorClass_E4_ArbitraryCommandExitIsUnclassified(t *testing.T) {
	t.Parallel()
	err := &SSHExecError{Hostname: "appdev", Output: "fatal: not a git repository (or any of the parent directories): .git\n", Err: ExitStatusError(128)}
	if c := ClassOf(err); c != "" {
		t.Fatalf("ClassOf = %q, want unclassified", c)
	}
	if code := SSHExitCode(err); code != 128 {
		t.Fatalf("SSHExitCode = %d, want 128", code)
	}
}

// witness: internal/platform/errors.go | case IsTransient(pe): → case pe.Code == ErrNetworkError:
func TestErrorClass_E5_ServerErrorAndRateLimitAreUnreachableAndWaitedOn(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		err := mapSDKError(apiError.Error{HttpStatusCode: status, ErrorCode: "x", Message: "try later"}, "")
		if c := ClassOf(err); c != ClassUnreachable || !c.WaitedOn() {
			t.Errorf("status %d: ClassOf = %q (waited on %v), want unreachable and waited on", status, c, c.WaitedOn())
		}
	}
}

// witness: internal/platform/errors.go | case IsTransient(pe): → case pe.Code != ErrDeployFailed:
func TestErrorClass_E6_ForbiddenIsRefusedNotWaitedOn(t *testing.T) {
	t.Parallel()
	err := mapSDKError(apiError.Error{HttpStatusCode: http.StatusForbidden, ErrorCode: "noAccess", Message: "no access"}, "")
	if c := ClassOf(err); c != ClassRefused || c.WaitedOn() {
		t.Fatalf("ClassOf = %q (waited on %v), want refused and not waited on", c, c.WaitedOn())
	}
}

// tlsHandshakeTimeoutError is what net/http answers when the TLS handshake does
// not finish in time: a net.Error whose Timeout is true.
type tlsHandshakeTimeoutError struct{}

func (tlsHandshakeTimeoutError) Error() string   { return "net/http: TLS handshake timeout" }
func (tlsHandshakeTimeoutError) Timeout() bool   { return true }
func (tlsHandshakeTimeoutError) Temporary() bool { return true }

// witness: internal/platform/errors.go | errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) → errors.Is(err, io.ErrUnexpectedEOF)
func TestErrorClass_E10_ConnectionEndedMidAnswerIsUnreachable(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{io.EOF, io.ErrUnexpectedEOF} {
		err := mapSDKError(&url.Error{Op: "Get", URL: "https://api.app-prg1.zerops.io/api/rest/public/service-stack/x", Err: cause}, "")
		if c := ClassOf(err); c != ClassUnreachable {
			t.Errorf("%v: ClassOf = %q, want unreachable", cause, c)
		}
	}
}

// witness: internal/platform/errors.go | if errors.As(err, &netErr) && netErr.Timeout() { → if errors.As(err, &netErr) && !netErr.Timeout() {
func TestErrorClass_E11_TLSHandshakeTimeoutIsUnreachable(t *testing.T) {
	t.Parallel()
	err := mapSDKError(&url.Error{Op: "Get", URL: "https://api.app-prg1.zerops.io/api/rest/public/service-stack/x", Err: tlsHandshakeTimeoutError{}}, "")
	if c := ClassOf(err); c != ClassUnreachable {
		t.Fatalf("ClassOf = %q, want unreachable", c)
	}
}

// witness: internal/platform/errors.go | case pe.APICode != "": → default:
func TestErrorClass_E12_ZcpsOwnRefusalIsNotCalledRefused(t *testing.T) {
	t.Parallel()
	err := NewPlatformError(ErrAdoptTargetBusy, "appdev is being adopted by another session", "Wait for it to finish")
	if c := ClassOf(err); c != "" {
		t.Fatalf("ClassOf = %q, want unclassified", c)
	}
}

// witness: internal/platform/zerops_errors.go | return err.Error() + " (" + words + ")" → return err.Error()
func TestErrorClass_E13_ARefusalKeepsTheFieldsItNamed(t *testing.T) {
	t.Parallel()
	refusal := &PlatformError{Code: ErrInvalidZeropsYml, Message: "Invalid parameter provided.", APIMeta: []APIMetaItem{{
		Metadata: map[string][]string{"build.base": {"unknown base alpine/static@1.0"}, "build.os": {""}},
	}}}
	many := &PlatformError{Message: "Invalid.", APIMeta: []APIMetaItem{{Metadata: map[string][]string{
		"a": {"x"}, "b": {"x"}, "c": {"x"}, "d": {"x"}, "e": {"x"}, "f": {"x"}, "g": {"x"},
	}}}}
	plain := fmt.Errorf("unavailable")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"fields", fmt.Errorf("x: %w", refusal), "x: Invalid parameter provided. (build.base: unknown base alpine/static@1.0; build.os)"},
		{"no meta", plain, "unavailable"},
		{"many fields", many, "Invalid. (7 fields)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := WordsOf(tc.err); got != tc.want {
				t.Errorf("WordsOf = %q, want %q", got, tc.want)
			}
		})
	}
}

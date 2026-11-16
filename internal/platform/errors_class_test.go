package platform

import (
	"net/http"
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

// witness: internal/platform/errors.go | strings.HasSuffix(line, "Connection timed out") → strings.HasSuffix(line, "Connection refused")
func TestErrorClass_E2_SSHConnectTimedOutIsRefusedNotWaitedOn(t *testing.T) {
	t.Parallel()
	err := &SSHExecError{Hostname: "webstage", Output: "ssh: connect to host webstage port 22: Connection timed out\n", Err: ExitStatusError(255)}
	if c := ClassOf(err); c != ClassRefused || c.WaitedOn() {
		t.Fatalf("ClassOf = %q (waited on %v), want refused and not waited on", c, c.WaitedOn())
	}
}

// witness: internal/platform/errors.go | if exit != sshUnreachableExit { → if exit == 0 {
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

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Error codes for ZCP.
const (
	ErrAuthRequired      = "AUTH_REQUIRED"
	ErrAuthTokenExpired  = "AUTH_TOKEN_EXPIRED"
	ErrTokenNoProject    = "TOKEN_NO_PROJECT"
	ErrTokenMultiProject = "TOKEN_MULTI_PROJECT"
	// ErrTokenScopeMismatch fires on the launch-production existing-project
	// path when a project-scoped token authenticates and resolves to
	// exactly one project, but that project's ID does NOT match the
	// user-supplied ExistingProjectID. Distinct from the no-project /
	// multi-project codes because the token IS validly scoped, just to
	// the wrong project — the agent must regenerate a token on the
	// target project's dashboard.
	ErrTokenScopeMismatch     = "TOKEN_SCOPE_MISMATCH"
	ErrServiceNotFound        = "SERVICE_NOT_FOUND"
	ErrServiceRequired        = "SERVICE_REQUIRED"
	ErrFileNotFound           = "FILE_NOT_FOUND"
	ErrInvalidZeropsYml       = "INVALID_ZEROPS_YML"
	ErrInvalidImportYml       = "INVALID_IMPORT_YML"
	ErrImportHasProject       = "IMPORT_HAS_PROJECT"
	ErrInvalidScaling         = "INVALID_SCALING"
	ErrInvalidParameter       = "INVALID_PARAMETER"
	ErrInvalidEnvFormat       = "INVALID_ENV_FORMAT"
	ErrInvalidHostname        = "INVALID_HOSTNAME"
	ErrProcessNotFound        = "PROCESS_NOT_FOUND"
	ErrProcessAlreadyTerminal = "PROCESS_ALREADY_TERMINAL"
	ErrPermissionDenied       = "PERMISSION_DENIED"
	ErrAPIError               = "API_ERROR"
	ErrAPITimeout             = "API_TIMEOUT"
	ErrAPIRateLimited         = "API_RATE_LIMITED"
	ErrNetworkError           = "NETWORK_ERROR"
	ErrInvalidUsage           = "INVALID_USAGE"
	ErrMountFailed            = "MOUNT_FAILED"
	ErrUnmountFailed          = "UNMOUNT_FAILED"
	ErrNotImplemented         = "NOT_IMPLEMENTED"
	ErrWorkflowActive         = "WORKFLOW_ACTIVE"
	ErrSessionNotFound        = "SESSION_NOT_FOUND"
	ErrBootstrapNotActive     = "BOOTSTRAP_NOT_ACTIVE"
	ErrSSHDeployFailed        = "SSH_DEPLOY_FAILED"
	ErrDeployFailed           = "DEPLOY_FAILED"
	ErrPrerequisiteMissing    = "PREREQUISITE_MISSING"
	// ErrAdoptRequired narrows ErrPrerequisiteMissing for the specific
	// "service exists in Zerops but lacks ZCP bootstrap metadata, and
	// the agent must run bootstrap+route=adopt before this operation"
	// case. The semantic is encoded in the code name so the Recovery
	// shape (zerops_workflow action=start workflow=bootstrap route=adopt)
	// is self-evident at every emit site. Pinned by
	// `TestErrAdoptRequiredCarriesAdoptRecovery` — every emit site must
	// carry the corresponding specific Recovery.
	//
	// Why narrowed from ErrPrerequisiteMissing: PREREQUISITE_MISSING was
	// emitted at 18 sites covering 5+ semantic categories (transient
	// API failure, manual operator action, filesystem state, deploy
	// strategy gate, adopt required). A single test invariant on the
	// broad code would false-positive on legitimate non-actionable
	// preconditions; splitting at the source makes the contract per-code.
	ErrAdoptRequired = "ADOPT_REQUIRED"
	// ErrAdoptTargetBusy is the "wait, something's running" gate: adopt
	// refuses a target with a LIVE build/deploy/lifecycle process (the agent
	// would otherwise adopt a service mid-first-deploy, which reads
	// READY_TO_DEPLOY the whole time). Recovery = wait for the process to
	// settle (watch events / re-discover) then re-run adopt; the refusal names
	// the cancelable processId as the stuck-process escape. Non-destructive,
	// self-clearing — never gated on a FAILED/terminal target (recovery stays
	// open).
	ErrAdoptTargetBusy    = "ADOPT_TARGET_BUSY"
	ErrWorkflowRequired   = "WORKFLOW_REQUIRED"
	ErrSelfServiceBlocked = "SELF_SERVICE_BLOCKED"
	// ErrConsentRequired marks a call that would delete a service without the
	// person's explicit consent to deleting that service by name. Recovery:
	// ask the person, then call again with their consent.
	ErrConsentRequired = "CONSENT_REQUIRED"
	ErrGitTokenMissing = "GIT_TOKEN_MISSING"
	// ErrGitTokenInvalid signals that a GIT_TOKEN was supplied but the
	// remote rejected the auth probe (bad token, expired PAT, insufficient
	// scope, or URL unreachable). Distinct from ErrGitTokenMissing — the
	// recovery is "regenerate / re-scope PAT", not "set GIT_TOKEN". Emitted
	// by the git-push-setup probe-first verifier.
	ErrGitTokenInvalid    = "GIT_TOKEN_INVALID"
	ErrSubagentMisuse     = "SUBAGENT_MISUSE"
	ErrExportBlocked      = "EXPORT_BLOCKED"
	ErrMissingEvidence    = "MISSING_EVIDENCE"
	ErrTopicEmpty         = "TOPIC_EMPTY"
	ErrWorkSessionCorrupt = "WORK_SESSION_CORRUPT"
	// ErrPreflightFailed signals a deploy preflight check failure. Carried
	// alongside structured CheckWire entries so the agent can re-run the
	// failed check or fix the underlying issue. Replaces the legacy
	// preflight wire shapes (jsonResult of StepCheckResult, ad-hoc batch
	// {preFlightFailedFor, result} map).
	ErrPreflightFailed = "PREFLIGHT_FAILED"
	// ErrUnknown wraps generic Go errors at the tools/convert.go boundary
	// when the underlying error doesn't carry a typed PlatformError code.
	// Eliminates the plain-text wire shape — every tool error response is
	// typed JSON.
	ErrUnknown = "UNKNOWN"
	// ErrDiagnosisRequired marks a destructive operation that refused to
	// proceed because the agent has not acknowledged what would be lost.
	// The error response carries a structured wouldDestroy payload (the
	// DiagnosedDestruction shape in tools/errwire.go); the second call
	// must include a matching confirmDestructive ack to proceed. Plan v4
	// §3.1 (diagnose-before-destruct invariant). Pinned by
	// TestImport_OverrideOnFailedRequiresAck and friends.
	ErrDiagnosisRequired = "DIAGNOSIS_REQUIRED"
	// ErrDelegationUnavailable signals the integration token has no unused
	// one-time delegation to mint a launch token from — never granted one,
	// already consumed it (F4: a successful mint deletes the delegation),
	// or it was revoked. Recovery is the manual launchKey path (D-6): the
	// agent asks the user to create a token in the dashboard. See
	// docs/spec-workflows.md P-LP-15 (delegated launch-token minting).
	ErrDelegationUnavailable = "DELEGATION_UNAVAILABLE"
	// ErrGitPushNonFastForward signals that a git-push strategy deploy (or
	// its local-mode twin) was rejected by the remote as non-fast-forward
	// — the tracked ref carries commits the local push lacks (a common
	// real-life shape: a fresh GitHub repo seeded with a README, or a
	// second agent/human pushing concurrently). Replaces the generic
	// ErrSSHDeployFailed/ErrDeployFailed for this specific rejection
	// family so the response carries structured divergence counts
	// (remoteAhead/localAhead/unrelated) and a `next` block naming
	// exactly three choices (rebase/merge/replace-remote) — zcp classifies
	// but never executes any of them; the decision belongs to the user.
	// docs/spec-workflows.md §12 GF-11.
	ErrGitPushNonFastForward = "GIT_PUSH_NON_FAST_FORWARD"
	// ErrGitWorktreeUnsupported signals that a self-deploy's source
	// container has .git as a regular FILE (a linked-worktree or
	// submodule-style gitdir pointer), not a directory. zcli's git
	// archiver (`--workspace-state all`) walks .git byte-for-byte
	// (filepath.Walk) to carry it into the replacement container — for a
	// pointer file that ships only the pointer, producing a broken
	// repository on the other side. Self-deploy only (GF-12, docs/
	// spec-workflows.md §8 DM); a cross-deploy from a distinct source
	// isn't affected by the target's own worktree shape.
	ErrGitWorktreeUnsupported = "GIT_WORKTREE_UNSUPPORTED"
	// ErrGitSubmodulesUnsupported signals that a self-deploy's source
	// container carries a .gitmodules file. `git archive` (the
	// deploy-from-commit path) and the working-tree self-deploy archiver
	// both ship submodule directories EMPTY — no --recurse-submodules —
	// so the replacement container would start without that code. GF-12,
	// docs/spec-workflows.md §8 DM.
	ErrGitSubmodulesUnsupported = "GIT_SUBMODULES_UNSUPPORTED"
	// ErrOrgAmbiguous: an org-filtered call on a client of a token in
	// several orgs that no project bound (ZeropsClient.OrgID).
	ErrOrgAmbiguous = "ORG_AMBIGUOUS"
	// ErrOrgWriteRefused: org mode's reader (docs/spec-mate.md §2.11) stopped
	// a request that was not a read — org mode never writes.
	ErrOrgWriteRefused = "ORG_WRITE_REFUSED"
)

// Error subcodes narrow a specific top-level code into a stable, more
// diagnosable class WITHOUT multiplying the top-level code space
// (docs/spec-telemetry.md §4.2 error_subcode). Optional — PlatformError.Subcode
// is only set at call sites where a single top-level code was found to
// conflate ≥3 distinct root causes (telemetry-production-readiness plan S4,
// eval evidence in plans/telemetry-analysis-2026-07-02.md §3). Every
// ErrorWire/wire.Event carries this value verbatim when non-empty; the
// middleware peeks it exactly like Code (spec §5.3).
const (
	// SubcodeAmbiguousScope narrows ErrInvalidParameter for the bootstrap
	// adopt route's dev/stage pairing refusal — two or more scoped adoptable
	// runtimes sharing a runtime base, with no explicit plan to disambiguate them
	// (workflow.ErrAdoptPairingChoice). The agent must re-call with an
	// explicit plan choosing pair-vs-independent, not just retry the same
	// input.
	SubcodeAmbiguousScope = "AMBIGUOUS_SCOPE"
	// SubcodePlanTypeMismatch narrows ErrInvalidParameter for the bootstrap
	// recipe route: a submitted plan target (rename / managed-EXISTS-flip)
	// doesn't match any runtime or dependency the active recipe derives
	// (workflow.ErrRecipePlanMismatch).
	SubcodePlanTypeMismatch = "PLAN_TYPE_MISMATCH"
	// SubcodeWorkerPlanShape narrows ErrInvalidParameter for the bootstrap
	// classic/explicit-plan route: the submitted BootstrapTarget list itself
	// fails shape validation — hostname pattern, missing/invalid
	// bootstrapMode, unresolvable type, or a dependency resolution/mode
	// mismatch (workflow.ErrPlanShapeInvalid).
	SubcodeWorkerPlanShape = "WORKER_PLAN_SHAPE"
)

// PlatformError carries a ZCP error code, message, and suggestion.
type PlatformError struct {
	Code       string
	Message    string
	Suggestion string
	APICode    string        // raw API error code, empty if not from API
	Diagnostic string        // raw command output for LLM debugging (SSH output, etc.)
	APIMeta    []APIMetaItem // server-provided field-level detail, empty when API did not send meta
	Cause      error         // underlying error, preserved so errors.Is/As keep working through the map (e.g. context.Canceled)
	// Subcode optionally narrows Code into a stable, more diagnosable class
	// (the Subcode* catalog above) — empty for every call site that hasn't
	// been split. Copied verbatim into ErrorWire.Subcode / wire.Event.ErrorSubcode.
	Subcode string
	// serverSide marks the platform's own 5xx answer (mapAPIError), for
	// IsTransient.
	serverSide bool
}

// IsTransient reports an error worth sending the same call again for: the
// network, a timeout, the rate limit, or the platform's own 5xx. A call the
// platform refused for what it carried (a 4xx), or for the key it came with,
// is not: a retry only repeats the answer.
func IsTransient(err error) bool {
	var pe *PlatformError
	if !errors.As(err, &pe) {
		return false
	}
	switch pe.Code {
	case ErrNetworkError, ErrAPITimeout, ErrAPIRateLimited:
		return true
	}
	return pe.serverSide
}

// APIMetaItem mirrors one element of the Zerops API's `error.meta[]` array.
// The server emits this shape for every 4xx response; ZCP surfaces it to the
// LLM so the failing field and reason are visible without trial-and-error.
// Example from live probe: metadata={"storage.mode": ["mode not supported"]}.
type APIMetaItem struct {
	Code     string              `json:"code,omitempty"`
	Error    string              `json:"error,omitempty"`
	Metadata map[string][]string `json:"metadata,omitempty"`
}

// SSHExecError carries structured SSH execution failure data.
// Separates OS-level exit error from remote command output so classifiers
// can distinguish progress messages from actual errors.
type SSHExecError struct {
	Hostname string
	Output   string // combined stdout+stderr from remote command
	Err      error  // underlying exec error (exit code, signal, etc.)
}

// Error is the host and the exit; Output — which may hold the command's
// stdout — stays out of it and reaches the agent whole as the diagnostic.
func (e *SSHExecError) Error() string {
	return fmt.Sprintf("ssh %s: %s", e.Hostname, e.Err.Error())
}

const (
	sshErrorTailLines = 3
	sshErrorTailBytes = 512
)

// outputTail is output's last sshErrorTailLines non-empty lines joined by
// " | ", cut to its last sshErrorTailBytes.
func outputTail(output string) string {
	var lines []string
	for l := range strings.SplitSeq(output, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > sshErrorTailLines {
		lines = lines[len(lines)-sshErrorTailLines:]
	}
	tail := strings.Join(lines, " | ")
	if len(tail) > sshErrorTailBytes {
		tail = "…" + tail[len(tail)-sshErrorTailBytes:]
	}
	return tail
}

func (e *SSHExecError) Unwrap() error { return e.Err }

func (e *PlatformError) Error() string {
	return e.Message
}

// Unwrap exposes the underlying cause (when set at the mapping seam) so
// errors.Is(mappedErr, context.Canceled) and similar keep working after the
// API error is wrapped in a PlatformError.
func (e *PlatformError) Unwrap() error { return e.Cause }

// NewPlatformError creates a PlatformError with the given code, message, and suggestion.
func NewPlatformError(code, message, suggestion string) *PlatformError {
	return &PlatformError{
		Code:       code,
		Message:    message,
		Suggestion: suggestion,
	}
}

// MapNetworkError determines if an error is a network error and returns the appropriate code.
func MapNetworkError(err error) (code string, isNetwork bool) {
	if err == nil {
		return "", false
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return ErrNetworkError, true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ErrNetworkError, true
	}

	msg := err.Error()
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "i/o timeout") {
		return ErrNetworkError, true
	}

	if strings.Contains(msg, "context deadline exceeded") {
		return ErrAPITimeout, true
	}

	return "", false
}

// ErrorClass is zcp's one class of a failure, derived once here and read by
// every consumer (docs: followup-ssh-errors §2.3): whether waiting can change
// it. The source's own words (platform message and code, ssh/git/zcli stderr
// and exit) travel beside it verbatim. "" is no failure.
type ErrorClass string

const (
	// ClassUnreachable may change by waiting: ssh's 255 before connect, a
	// 5xx, a 429, a timeout. The only class a wait loop waits on, bounded.
	ClassUnreachable ErrorClass = "unreachable"
	// ClassRefused will not change until something is changed: a 401/403 or
	// other 4xx, an SSH admission refusal, a quota.
	ClassRefused ErrorClass = "refused"
	// ClassFailed is an operation that ran and failed: a build, a command's
	// exit.
	ClassFailed ErrorClass = "failed"
)

// ClassOf is err's class: the one derivation of the class, first from an
// error that states its own (a Classed in err's chain: a site that knows what
// only it can, such as a readiness probe or a commit deploy's refusal), then
// from an SSHExecError (SSHExitCode), a PlatformError or the transport.
// Refused only on a positive marker — the platform API's own 4xx answer
// (APICode); unreachable on a 5xx, a 429, a timeout, a dropped connection
// (EOF) or a network error; failed on a failed deploy. "" for nil, a stop the
// caller asked for (context.Canceled), and anything zcp does not classify —
// an arbitrary command's own exit, zcp's own refusal of its input — passed on
// verbatim: zcp never matches the platform's text.
func ClassOf(err error) ErrorClass {
	if err == nil {
		return ""
	}
	var classed Classed
	if errors.As(err, &classed) {
		return classed.Class()
	}
	if errors.Is(err, context.Canceled) {
		return ""
	}
	var sshErr *SSHExecError
	if errors.As(err, &sshErr) {
		if errors.Is(err, context.DeadlineExceeded) {
			return ClassUnreachable
		}
		return classOfSSH(sshErr.Output, SSHExitCode(err))
	}
	var pe *PlatformError
	if errors.As(err, &pe) {
		switch {
		case IsTransient(pe):
			return ClassUnreachable
		case pe.Code == ErrDeployFailed:
			return ClassFailed
		case pe.APICode != "":
			return ClassRefused
		}
	}
	if transportFailed(err) {
		return ClassUnreachable
	}
	return ""
}

// transportFailed reports whether err is the way to a source failing rather
// than the source's answer: a timeout (context deadline, a net.Error's — a
// TLS handshake's among them), a connection that ended mid-answer (EOF), or a
// network error.
func transportFailed(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	_, network := MapNetworkError(err)
	return network
}

// sshUnreachableExit is the exit status ssh itself ends with when it did not
// run the command: every transport failure, before or after connect.
const sshUnreachableExit = 255

// classOfSSH is the class of a command run over ssh that ended with exit
// and output: unreachable when nothing could run — the container at its
// process limit (fork answered EAGAIN), or ssh's own 255, whatever its words
// (live, 2026-11-16: a stopping or starting container answers "Connection
// refused", a stopped one "Connection timed out", and both pass with a
// restart; a wrong wait costs one bound, a wrong refusal tells the agent to
// give up). Any other exit is the command's own: "".
func classOfSSH(output string, exit int) ErrorClass {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "fork") && strings.Contains(lower, "resource temporarily unavailable") {
		return ClassUnreachable
	}
	if exit == sshUnreachableExit {
		return ClassUnreachable
	}
	return ""
}

// classOfProbe is the class of a readiness probe's last error (SSHProbeError.Class).
func classOfProbe(err error) ErrorClass {
	if c := ClassOf(err); c != "" {
		return c
	}
	if SSHExitCode(err) > 0 {
		return ClassRefused
	}
	return ""
}

// Classed is an error that states its own class, which ClassOf honours
// before any rule of its own.
type Classed interface {
	error
	Class() ErrorClass
}

// WaitedOn reports whether a wait loop waits on a failure of class c, bounded:
// true for ClassUnreachable only. A wait whose bound runs out reports the
// last cause it saw.
func (c ErrorClass) WaitedOn() bool {
	return c == ClassUnreachable
}

// ExitStatusError is a command's exit status as an error, for a runner that reads
// the status from somewhere other than its process (a relayed session's
// done marker): SSHExitCode reads it as it reads an *exec.ExitError.
type ExitStatusError int

func (s ExitStatusError) Error() string { return fmt.Sprintf("exit status %d", int(s)) }

// ExitCode is the status, as *exec.ExitError's.
func (s ExitStatusError) ExitCode() int { return int(s) }

// SSHExitCode is the exit status ssh ended with behind err — the remote
// command's, or ssh's own 255 — and -1 when err carries none (nil, a context
// stop, ssh that never ran). Both SSH runners carry it in their SSHExecError's
// Err: the system runner an *exec.ExitError, the relayed one an ExitStatusError.
func SSHExitCode(err error) int {
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// SSHProbeError is a readiness probe (`true` over ssh) on Hostname that did
// not answer: Last is the last ssh error, verbatim; Waited how long it was
// tried — zero when it was not waited on (its Class is not WaitedOn).
type SSHProbeError struct {
	Hostname string
	Last     error
	Waited   time.Duration
}

// Class is the probe's class: as any ssh command's, except that `true`
// cannot fail, so any exit that is not ssh's own (an admission refusal
// answers 2) is refused.
func (e *SSHProbeError) Class() ErrorClass { return classOfProbe(e.Last) }

func (e *SSHProbeError) Error() string {
	class := ""
	if c := e.Class(); c != "" {
		class = " (" + string(c) + ")"
	}
	last := e.Last.Error()
	var sshErr *SSHExecError
	if errors.As(e.Last, &sshErr) {
		// The probe runs `true`, which writes nothing: its output is ssh's
		// or the platform's own words, safe to carry in the message.
		if tail := outputTail(sshErr.Output); tail != "" {
			last += ": " + tail
		}
	}
	if e.Waited > 0 {
		return fmt.Sprintf("ssh to %s did not answer within %s%s; last: %s", e.Hostname, e.Waited, class, last)
	}
	return fmt.Sprintf("ssh to %s did not answer%s, not waited on: %s", e.Hostname, class, last)
}

func (e *SSHProbeError) Unwrap() error { return e.Last }

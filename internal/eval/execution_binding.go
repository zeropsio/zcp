package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// ExecutionBinding is an explicit candidate binding
// (docs/spec-testing-architecture.md §10.4): the runner never uses its own
// executable as the agent's tool — the candidate binary is the target under
// test, the project is the only one this run may touch, and WorkDir/
// ResultsDir/PrivateBin/ClaudeHome are the candidate-owned surface.
type ExecutionBinding struct {
	Candidate       string
	CandidateSHA256 string
	ProjectID       string
	AckDisposable   string
	WorkDir         string
	ResultsDir      string
	RunID           string
	PrivateBin      string
	ClaudeHome      string
}

// ExecutionBindingRecord is the binding projection persisted in meta.json
// (§10.4 "Binding values land in meta.json as binding ... never a token").
type ExecutionBindingRecord struct {
	Candidate       string `json:"candidate"`
	CandidateSHA256 string `json:"candidateSha256"`
	ProjectID       string `json:"projectId"`
	RunID           string `json:"runId"`
}

// ProcessIdentity is one observation of a live candidate `serve` process
// (§10.4 "Observed process identity").
type ProcessIdentity struct {
	PID              int       `json:"pid"`
	Digest           string    `json:"digest,omitempty"`
	MatchesCandidate bool      `json:"matchesCandidate"`
	ProjectID        string    `json:"projectId,omitempty"`
	ServiceID        string    `json:"serviceId,omitempty"`
	AutoUpdate       string    `json:"autoUpdate,omitempty"`
	Classification   string    `json:"classification"`
	ObservedAt       time.Time `json:"observedAt"`
}

const (
	processIdentityCounted     = "counted"
	processIdentityUnobserved  = "unobservable"
	processIdentityUnsupported = "unsupported"

	bindingPreflightCancelled = "binding: preflight cancelled"
)

// bindingRecord projects an ExecutionBinding into its persisted meta.json
// shape. Never carries a credential.
func bindingRecord(b *ExecutionBinding) *ExecutionBindingRecord {
	if b == nil {
		return nil
	}
	return &ExecutionBindingRecord{
		Candidate:       b.Candidate,
		CandidateSHA256: b.CandidateSHA256,
		ProjectID:       b.ProjectID,
		RunID:           b.RunID,
	}
}

// preflightBinding implements the §10.4 zero-mutation preflight, in order:
// candidate identity, acknowledgement, target freshness, root safety, then
// (only once every check passed) the candidate-owned surface is created.
// Returns "" on success or a "binding: <reason>" message on the first
// failing check.
func (r *Runner) preflightBinding(ctx context.Context) string {
	b := r.config.Binding
	if err := ctx.Err(); err != nil {
		return bindingPreflightCancelled
	}
	if err := verifyCandidateBinary(b.Candidate, b.CandidateSHA256); err != nil {
		return "binding: " + err.Error()
	}
	if b.AckDisposable != "yes" {
		return "binding: acknowledgement must be exactly \"yes\""
	}
	if err := ctx.Err(); err != nil {
		return bindingPreflightCancelled
	}
	if err := assertFreshTarget(ctx, r.client, r.projectID); err != nil {
		return "binding: " + err.Error()
	}
	if err := assertSafeRoots(b.WorkDir, b.ResultsDir); err != nil {
		return "binding: " + err.Error()
	}
	if err := ctx.Err(); err != nil {
		return bindingPreflightCancelled
	}
	if err := createCandidateOwnedSurface(b); err != nil {
		return "binding: " + err.Error()
	}
	return ""
}

// verifyCandidateBinary checks candidate is a regular executable file whose
// SHA-256 equals wantSHA256.
func verifyCandidateBinary(candidate, wantSHA256 string) error {
	if candidate == "" || wantSHA256 == "" {
		return fmt.Errorf("candidate binary and SHA-256 are required")
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("candidate %q is not a regular file", candidate)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("candidate %q is not executable", candidate)
	}
	digest, err := sha256File(candidate)
	if err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if digest != wantSHA256 {
		return fmt.Errorf("candidate SHA-256 mismatch: got %s, want %s", digest, wantSHA256)
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// assertFreshTarget implements the §10.4 freshness check: a direct service
// read shows only system services plus the protected control service
// (ProtectedService), and a direct process read shows no live process other
// than those services' own lifecycle. The evaluator runs from the control
// service's initCommands, i.e. while that service's own stack.build process
// is still RUNNING (init runs before the start unit; the platform marks the
// process finished only ~2 min after creation — live D16, gate2–gate4). Once
// the service read has passed, the only services in the project are the
// allowed ones, so a live stack-scoped process can only belong to them: it
// is skipped when every ServiceStacks[] ref resolves (by id or name) to an
// allowed service, or when it carries no refs but is a `stack.*` action. A
// live process referencing a service outside that set, or a project-level
// action with no refs, still refuses.
func assertFreshTarget(ctx context.Context, client platform.Client, projectID string) error {
	services, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
		return fmt.Errorf("fresh-target service read: %w", err)
	}
	allowed := make(map[string]bool, 2*len(services))
	for _, svc := range services {
		if !svc.IsSystem() && svc.Name != ProtectedService {
			return fmt.Errorf("target is not fresh: service %q present", svc.Name)
		}
		allowed[svc.ID] = true
		allowed[svc.Name] = true
	}
	processes, err := client.GetProjectProcessesDirect(ctx, projectID)
	if err != nil {
		return fmt.Errorf("fresh-target process read: %w", err)
	}
	for _, proc := range processes {
		switch proc.Status {
		case platform.ProcessStatusPending, platform.ProcessStatusRunning, platform.ProcessStatusRollbacking, platform.ProcessStatusCanceling:
			if isAllowedServiceProcess(proc, allowed) {
				continue
			}
			return fmt.Errorf("target is not fresh: live process %s (%s)", proc.ID, proc.Status)
		}
	}
	return nil
}

// isAllowedServiceProcess reports whether proc is the lifecycle of a service
// the fresh-target service read already allowed (see assertFreshTarget):
// every non-BUILD ref resolves by id or name into allowed, or the process has
// no refs and is a stack-scoped action. BUILD refs are the transient build
// container a RUNNING stack.build attaches as a second entry (live gate2 row:
// zcp + buildzcpv<ts>); it is never listed among the project's services, so
// it cannot be resolved and is skipped. A ref-less project-level action is
// never allowed.
func isAllowedServiceProcess(proc platform.Process, allowed map[string]bool) bool {
	if len(proc.ServiceStacks) == 0 {
		return strings.HasPrefix(proc.ActionName, "stack.")
	}
	for _, ref := range proc.ServiceStacks {
		if ref.Category == buildCategory {
			continue
		}
		if !allowed[ref.ID] && !allowed[ref.Name] {
			return false
		}
	}
	return true
}

// buildCategory is the serviceStackTypeCategory of a build container ref.
const buildCategory = "BUILD"

// assertSafeRoots implements the §10.4 root-safety check: work dir and
// results dir must be absolute, distinct, not nested in each other, not
// "/", and not a home directory root.
func assertSafeRoots(workDir, resultsDir string) error {
	home, _ := os.UserHomeDir()
	for _, root := range []struct {
		name, path string
	}{{"work dir", workDir}, {"results dir", resultsDir}} {
		if !filepath.IsAbs(root.path) {
			return fmt.Errorf("%s %q must be absolute", root.name, root.path)
		}
		clean := filepath.Clean(root.path)
		if clean == "/" {
			return fmt.Errorf("%s must not be \"/\"", root.name)
		}
		if home != "" && clean == filepath.Clean(home) {
			return fmt.Errorf("%s must not be a home directory root", root.name)
		}
	}
	work := filepath.Clean(workDir)
	results := filepath.Clean(resultsDir)
	if work == results {
		return fmt.Errorf("work dir and results dir must be distinct")
	}
	if isPathNested(work, results) || isPathNested(results, work) {
		return fmt.Errorf("work dir and results dir must not be nested in each other")
	}
	return nil
}

// isPathNested reports whether inner is nested inside outer (outer is a
// proper ancestor directory of inner).
func isPathNested(outer, inner string) bool {
	rel, err := filepath.Rel(outer, inner)
	if err != nil {
		return false
	}
	// Only a ".."-prefixed relative path escapes outer; a dot-named first
	// segment (".results") is still nested.
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// createCandidateOwnedSurface creates the work dir + sentinel (exporting
// ZCP_EVAL_SENTINEL_FILE so assertWorkDirSafe honours it — documented use of
// os.Setenv, §10.4 "the binding writes the sentinel ... and exports
// ZCP_EVAL_SENTINEL_FILE for the run"), the private bin dir with a zcp
// symlink to the candidate, and the private Claude home.
func createCandidateOwnedSurface(b *ExecutionBinding) error {
	if err := os.MkdirAll(b.WorkDir, 0o700); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	sentinel := filepath.Join(b.WorkDir, SentinelFilename)
	if err := os.WriteFile(sentinel, nil, 0o600); err != nil {
		return fmt.Errorf("write sentinel: %w", err)
	}
	if err := os.Setenv("ZCP_EVAL_SENTINEL_FILE", SentinelFilename); err != nil {
		return fmt.Errorf("export sentinel env: %w", err)
	}
	if b.PrivateBin != "" {
		if err := os.MkdirAll(b.PrivateBin, 0o700); err != nil {
			return fmt.Errorf("create private bin: %w", err)
		}
		link := filepath.Join(b.PrivateBin, "zcp")
		_ = os.Remove(link)
		if err := os.Symlink(b.Candidate, link); err != nil {
			return fmt.Errorf("symlink candidate into private bin: %w", err)
		}
	}
	if b.ClaudeHome != "" {
		if err := os.MkdirAll(b.ClaudeHome, 0o700); err != nil {
			return fmt.Errorf("create private claude home: %w", err)
		}
	}
	return nil
}

// bindingRefusalResult freezes result as not-run for a §10.4 preflight
// refusal: zero platform reads beyond the ones the failing check itself
// made, and only meta.json is written — freezeTaskEnd's full write set
// (platform-snapshot.json, verification.json) is not needed here because
// nothing was ever observed.
func (r *Runner) bindingRefusalResult(sc *Scenario, outDir string, result *BehavioralResult, startedAt time.Time, reason string) *BehavioralResult {
	mode := VerificationObserve
	if sc.Verification != nil && sc.Verification.Mode != "" {
		mode = sc.Verification.Mode
	}
	now := time.Now().UTC()
	result.Error = reason
	result.Task = &TaskOutcome{Mode: mode, Result: CheckNotRun, FrozenAt: now}
	result.TaskEnd = &TaskEndEvidence{ObservedAt: now, Settled: true, Persisted: true}
	result.Duration = Duration(time.Since(startedAt))
	if err := writeBehavioralResult(outDir, result); err != nil {
		result.TaskEnd.Persisted = false
		result.TaskEnd.PersistError = err.Error()
		fmt.Fprintf(os.Stderr, "warning: write meta.json: %v\n", err)
	}
	return result
}

// candidateEnv builds the environment for a candidate-owned subprocess
// (`<candidate> init`, or a `claude` child via Runner.claudeEnv): the
// evaluator's own environment with HOME/PATH/ZCP_AUTO_UPDATE overridden, so
// projectId/serviceId pass through unchanged from os.Environ (§10.4
// "Candidate owns the agent surface").
func (r *Runner) candidateEnv() []string {
	b := r.config.Binding
	overrides := map[string]string{
		"ZCP_AUTO_UPDATE": "0",
	}
	if b.ClaudeHome != "" {
		overrides["HOME"] = b.ClaudeHome
	}
	if b.PrivateBin != "" {
		overrides["PATH"] = b.PrivateBin + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	return environmentWithOverrides(os.Environ(), overrides)
}

// ProcessIdentityAccepted implements the §10.4 fourth acceptance dimension:
// at least one counted observation whose digest matches the candidate and
// whose projectId equals the binding's, and no counted observation
// contradicting either.
func ProcessIdentityAccepted(result *BehavioralResult) (ok bool, reason string) {
	if result.Binding == nil {
		return true, ""
	}
	sawCounted := false
	sawUnsupported := false
	matched := false
	var contradictionPID int
	for _, obs := range result.ProcessIdentity {
		switch obs.Classification {
		case processIdentityUnsupported:
			sawUnsupported = true
		case processIdentityCounted:
			sawCounted = true
			if obs.MatchesCandidate && obs.ProjectID == result.Binding.ProjectID {
				matched = true
			} else if contradictionPID == 0 {
				contradictionPID = obs.PID
			}
		}
	}
	if contradictionPID != 0 {
		return false, fmt.Sprintf("process identity: contradiction pid %d", contradictionPID)
	}
	if matched {
		return true, ""
	}
	if !sawCounted && sawUnsupported {
		return false, "process identity: unsupported OS"
	}
	return false, "process identity: none"
}

// pollProcessIdentityDuring runs work while polling for new observed
// candidate `serve` processes at RunnerConfig.IdentityPollInterval,
// appending every observation to result.ProcessIdentity (§10.4 "Observed
// process identity"). A nil binding or capture window makes this a plain
// passthrough. Repeated "unsupported" observations (non-Linux) are recorded
// only once.
func (r *Runner) pollProcessIdentityDuring(ctx context.Context, result *BehavioralResult, work func() error) error {
	if r.config.Binding == nil || r.config.Capture == nil {
		return work()
	}
	interval := r.config.IdentityPollInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	sessionDir := r.config.Capture.SessionDir
	// The window identity the candidate's environ must carry is the capture
	// id itself (what the wrapper exported as ZCP_CAPTURE_SESSION_ID), not a
	// path component.
	windowID := r.config.Capture.CaptureID
	candidateSHA := r.config.Binding.CandidateSHA256
	// A pid observed in an earlier phase is never re-observed: the same map
	// state is carried across agent.initial and every agent.resume.<n>.
	seen := make(map[int]bool)
	sawUnsupported := false
	for _, o := range result.ProcessIdentity {
		seen[o.PID] = true
		if o.Classification == processIdentityUnsupported {
			sawUnsupported = true
		}
	}
	appendObservations := func(obs []ProcessIdentity) {
		for _, o := range obs {
			if o.Classification == processIdentityUnsupported {
				if sawUnsupported {
					continue
				}
				sawUnsupported = true
			}
			result.ProcessIdentity = append(result.ProcessIdentity, o)
		}
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				appendObservations(observeProcessIdentity(ctx, sessionDir, windowID, candidateSHA, seen))
			case <-stop:
				return
			}
		}
	}()

	err := work()
	close(stop)
	<-done
	appendObservations(observeProcessIdentity(ctx, sessionDir, windowID, candidateSHA, seen))
	return err
}

// MatchingProcessIdentityPID returns the pid of the first counted,
// non-contradicting observation — used only to print "ok (pid N)".
func MatchingProcessIdentityPID(result *BehavioralResult) int {
	if result.Binding == nil {
		return 0
	}
	for _, obs := range result.ProcessIdentity {
		if obs.Classification == processIdentityCounted && obs.MatchesCandidate && obs.ProjectID == result.Binding.ProjectID {
			return obs.PID
		}
	}
	return 0
}

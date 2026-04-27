package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// WorkflowWork is the registry workflow name for per-PID work sessions.
// Work sessions are ephemeral, PID-scoped artifacts recording an LLM task's
// deploy/verify history. Infrastructure workflows (bootstrap, recipe) live
// alongside a work session but in a different layer.
const WorkflowWork = "work"

const (
	workSessionDirName = "work"
	workSessionVersion = "1"
	workSessionMaxHist = 10

	// CloseReasonExplicit — LLM called action=close.
	CloseReasonExplicit = "explicit"
	// CloseReasonAutoComplete — all services deployed+verified.
	CloseReasonAutoComplete = "auto-complete"
	// CloseReasonAbandoned — orphan cleanup or reset.
	CloseReasonAbandoned = "abandoned"
	// CloseReasonIterationCap — infrastructure workflow hit maxIterations(); work
	// session auto-closes so the LLM can report to the user instead of looping.
	CloseReasonIterationCap = "iteration-cap"
)

// WorkSession records the lifecycle of one LLM task tied to a process.
// Stored at .zcp/state/work/{pid}.json. Never claimed across PID restart —
// dies with the process. Code work survives in git / filesystem.
type WorkSession struct {
	Version        string                     `json:"version"`
	PID            int                        `json:"pid"`
	ProjectID      string                     `json:"projectId"`
	Environment    string                     `json:"environment"`
	Intent         string                     `json:"intent"`
	Services       []string                   `json:"services"`
	CreatedAt      string                     `json:"createdAt"`
	LastActivityAt string                     `json:"lastActivityAt"`
	Deploys        map[string][]DeployAttempt `json:"deploys,omitempty"`
	Verifies       map[string][]VerifyAttempt `json:"verifies,omitempty"`
	ClosedAt       string                     `json:"closedAt,omitempty"`
	CloseReason    string                     `json:"closeReason,omitempty"`
}

// DeployAttempt is one zerops_deploy invocation for a hostname.
//
// FailureClass is populated by the deploy handler when an error occurs,
// using the operation result to classify the failure (build / start /
// network / config / other). Empty when the deploy succeeded. The envelope
// projection (deployAttemptsToInfo) carries this to AttemptInfo so the
// LLM sees a typed category alongside the human-readable Error.
type DeployAttempt struct {
	AttemptedAt  string                `json:"attemptedAt"`
	SucceededAt  string                `json:"succeededAt,omitempty"`
	Setup        string                `json:"setup,omitempty"`
	Strategy     string                `json:"strategy,omitempty"`
	Error        string                `json:"error,omitempty"`
	FailureClass topology.FailureClass `json:"failureClass,omitempty"`
}

// VerifyAttempt is one zerops_verify invocation for a hostname.
//
// FailureClass is populated when Passed=false and the failing check
// distinguishes the category (typically FailureClassVerify, occasionally
// FailureClassStart for service-not-running). Empty when the verify
// passed.
type VerifyAttempt struct {
	AttemptedAt  string                `json:"attemptedAt"`
	PassedAt     string                `json:"passedAt,omitempty"`
	Summary      string                `json:"summary,omitempty"`
	Passed       bool                  `json:"passed"`
	FailureClass topology.FailureClass `json:"failureClass,omitempty"`
}

// workSessionMu serializes work-session file updates within a single process.
// MCP STDIO requests are serialized by the server, but belt-and-braces.
var workSessionMu sync.Mutex

// ErrHostnameOutOfScope is returned by Record{Deploy,Verify}Attempt when the
// hostname is not declared in ws.Services. Prevents silent pollution of
// ws.Deploys/ws.Verifies with entries that EvaluateAutoClose never reads —
// single-source-of-truth invariant (spec-work-session.md §7.5).
var ErrHostnameOutOfScope = errors.New("hostname is not in work session scope")

// inScope reports whether hostname is declared in ws.Services.
func inScope(ws *WorkSession, hostname string) bool {
	return slices.Contains(ws.Services, hostname)
}

// CurrentWorkSession returns the work session for the current PID, or nil
// if none exists. Errors other than not-found are returned as-is.
func CurrentWorkSession(stateDir string) (*WorkSession, error) {
	return LoadWorkSession(stateDir, os.Getpid())
}

// LoadWorkSession reads the per-PID work session from disk.
// Returns (nil, nil) when no file exists.
func LoadWorkSession(stateDir string, pid int) (*WorkSession, error) {
	if stateDir == "" {
		return nil, nil //nolint:nilnil // not-found sentinel
	}
	path := workSessionPath(stateDir, pid)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil //nolint:nilnil
		}
		return nil, fmt.Errorf("read work session: %w", err)
	}
	var ws WorkSession
	if err := json.Unmarshal(data, &ws); err != nil {
		// Surface as PlatformError so handleLifecycleStatus and any other
		// recovery surface can attach the canonical reset suggestion.
		// Recovery primitive (action="status") is the agent's only
		// re-orientation call after compaction; if it dies on a corrupt
		// file it must teach the next step itself.
		pe := platform.NewPlatformError(
			platform.ErrWorkSessionCorrupt,
			fmt.Sprintf("Work session file is corrupt: %v", err),
			`Discard the file and start fresh: zerops_workflow action="reset" workflow="develop". Code work survives in git/filesystem; only attempt history is lost.`,
		)
		pe.Diagnostic = fmt.Sprintf("path=%s; parser-detail=%v", path, err)
		return nil, pe
	}
	return &ws, nil
}

// SaveWorkSession atomically writes the work session to disk.
func SaveWorkSession(stateDir string, ws *WorkSession) error {
	if stateDir == "" {
		return fmt.Errorf("save work session: empty state dir")
	}
	if ws == nil {
		return fmt.Errorf("save work session: nil session")
	}
	ws.Version = workSessionVersion
	dir := filepath.Join(stateDir, workSessionDirName)
	return atomicWriteJSON(dir, ".work-*.tmp", workSessionPath(stateDir, ws.PID), ws)
}

// DeleteWorkSession removes the per-PID work session file. Idempotent.
func DeleteWorkSession(stateDir string, pid int) error {
	if stateDir == "" {
		return nil
	}
	path := workSessionPath(stateDir, pid)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete work session: %w", err)
	}
	return nil
}

// NewWorkSession constructs a fresh work session for the current PID.
func NewWorkSession(projectID, environment, intent string, services []string) *WorkSession {
	now := time.Now().UTC().Format(time.RFC3339)
	return &WorkSession{
		Version:        workSessionVersion,
		PID:            os.Getpid(),
		ProjectID:      projectID,
		Environment:    environment,
		Intent:         intent,
		Services:       append([]string(nil), services...),
		CreatedAt:      now,
		LastActivityAt: now,
		Deploys:        map[string][]DeployAttempt{},
		Verifies:       map[string][]VerifyAttempt{},
	}
}

// RecordDeployAttempt appends (or updates the last) deploy attempt for a hostname.
// If result indicates success, the existing attempt's SucceededAt is set;
// otherwise Error is set. Mutates and saves the current-PID work session.
// No-op when no work session exists.
// Returns ErrHostnameOutOfScope when hostname is not declared in ws.Services.
func RecordDeployAttempt(stateDir, hostname string, attempt DeployAttempt) error {
	workSessionMu.Lock()
	defer workSessionMu.Unlock()

	ws, err := CurrentWorkSession(stateDir)
	if err != nil {
		return err
	}
	if ws == nil {
		return nil
	}
	if !inScope(ws, hostname) {
		return fmt.Errorf("%w: %q", ErrHostnameOutOfScope, hostname)
	}
	if ws.Deploys == nil {
		ws.Deploys = map[string][]DeployAttempt{}
	}
	ws.Deploys[hostname] = append(ws.Deploys[hostname], attempt)
	if len(ws.Deploys[hostname]) > workSessionMaxHist {
		ws.Deploys[hostname] = ws.Deploys[hostname][len(ws.Deploys[hostname])-workSessionMaxHist:]
	}
	ws.LastActivityAt = time.Now().UTC().Format(time.RFC3339)
	if ws.ClosedAt == "" && EvaluateAutoClose(ws) {
		ws.ClosedAt = ws.LastActivityAt
		ws.CloseReason = CloseReasonAutoComplete
	}
	// Persistent deploy marker: once a deploy lands successfully, stamp the
	// meta so the fact survives session closure. Only the first successful
	// deploy stamps (idempotent). FindServiceMeta resolves both halves
	// of a container+standard dev/stage pair to the single meta file.
	if attempt.SucceededAt != "" {
		_ = stampFirstDeployedAt(stateDir, hostname)
	}
	return SaveWorkSession(stateDir, ws)
}

// stampFirstDeployedAt writes FirstDeployedAt on the meta for hostname if
// unset. Resolves hostname through FindServiceMeta so a stage-side
// deploy stamps the dev-keyed meta file (container+standard case).
// Best-effort: meta-less services (adopted without a local record) return
// nil without error — stamping a missing file is a no-op, not a bug.
func stampFirstDeployedAt(stateDir, hostname string) error {
	meta, err := FindServiceMeta(stateDir, hostname)
	if err != nil {
		return fmt.Errorf("stamp first deployed: %w", err)
	}
	if meta == nil || meta.FirstDeployedAt != "" {
		return nil
	}
	meta.FirstDeployedAt = time.Now().UTC().Format(time.RFC3339)
	if err := WriteServiceMeta(stateDir, meta); err != nil {
		return fmt.Errorf("stamp first deployed: write meta: %w", err)
	}
	return nil
}

// RecordVerifyAttempt appends one verify attempt for a hostname. Triggers
// auto-close evaluation.
// Returns ErrHostnameOutOfScope when hostname is not declared in ws.Services.
//
// Under plan phase A.3 this no longer mutates ServiceMeta. "Deployed" is
// derived at envelope build time from platform.Status and the session's
// own deploy attempts; a persistent FirstDeployedAt flag turned into a
// cross-boundary write from session state into bootstrap state and stuck
// adopted services at "never deployed" forever.
func RecordVerifyAttempt(stateDir, hostname string, attempt VerifyAttempt) error {
	workSessionMu.Lock()
	defer workSessionMu.Unlock()

	ws, err := CurrentWorkSession(stateDir)
	if err != nil {
		return err
	}
	if ws == nil {
		return nil
	}
	if !inScope(ws, hostname) {
		return fmt.Errorf("%w: %q", ErrHostnameOutOfScope, hostname)
	}
	if ws.Verifies == nil {
		ws.Verifies = map[string][]VerifyAttempt{}
	}
	ws.Verifies[hostname] = append(ws.Verifies[hostname], attempt)
	if len(ws.Verifies[hostname]) > workSessionMaxHist {
		ws.Verifies[hostname] = ws.Verifies[hostname][len(ws.Verifies[hostname])-workSessionMaxHist:]
	}
	ws.LastActivityAt = time.Now().UTC().Format(time.RFC3339)
	if ws.ClosedAt == "" && EvaluateAutoClose(ws) {
		ws.ClosedAt = ws.LastActivityAt
		ws.CloseReason = CloseReasonAutoComplete
	}
	return SaveWorkSession(stateDir, ws)
}

// TouchWorkSession updates LastActivityAt without recording a deploy/verify.
// Used by tools that are activity-worthy but not lifecycle events (mount).
func TouchWorkSession(stateDir string) error {
	workSessionMu.Lock()
	defer workSessionMu.Unlock()

	ws, err := CurrentWorkSession(stateDir)
	if err != nil || ws == nil {
		return err
	}
	ws.LastActivityAt = time.Now().UTC().Format(time.RFC3339)
	return SaveWorkSession(stateDir, ws)
}

// HasSuccessfulDeploy reports whether ws has any deploy attempt with a
// non-empty SucceededAt. Distinct from EvaluateAutoClose, which requires
// every in-scope service to be both deployed and verified.
func HasSuccessfulDeploy(ws *WorkSession) bool {
	if ws == nil {
		return false
	}
	for _, attempts := range ws.Deploys {
		for _, a := range attempts {
			if a.SucceededAt != "" {
				return true
			}
		}
	}
	return false
}

// HasSuccessfulDeployFor reports whether ws recorded a successful deploy for
// the specific hostname. Used by envelope derivation to decide Deployed —
// a per-host projection of HasSuccessfulDeploy. Empty hostname or nil ws
// returns false.
func HasSuccessfulDeployFor(ws *WorkSession, hostname string) bool {
	if ws == nil || hostname == "" {
		return false
	}
	for _, a := range ws.Deploys[hostname] {
		if a.SucceededAt != "" {
			return true
		}
	}
	return false
}

// EvaluateAutoClose returns true when every service in scope has at least one
// succeeded deploy and at least one passed verify. Empty scope → false.
func EvaluateAutoClose(ws *WorkSession) bool {
	if ws == nil || len(ws.Services) == 0 {
		return false
	}
	for _, h := range ws.Services {
		if !serviceAutoCloseReady(ws, h) {
			return false
		}
	}
	return true
}

// AutoCloseProgress summarises how many services in scope have crossed the
// auto-close threshold and names the ones still pending. Surfaced to the
// agent in side-effect responses (verify, deploy) so the work session is
// observably advancing — the fizzy log shows that without this the agent
// defaulted to curl because verify's tracking purpose wasn't visible.
type AutoCloseProgress struct {
	SessionID string   `json:"sessionId"`
	Ready     int      `json:"ready"`
	Total     int      `json:"total"`
	Pending   []string `json:"pending,omitempty"`
}

// AutoCloseProgressFor loads the current-PID work session and computes the
// progress snapshot. Returns nil when no session exists — the caller
// attaches a non-nil value only when a session is on disk, so the JSON
// response's autoCloseProgress field is omitted otherwise.
//
// A session whose last recorded event tipped it to all-green reports
// ready==total; reading the snapshot back here still reflects the final
// ratio even after ClosedAt is set.
func AutoCloseProgressFor(stateDir string) *AutoCloseProgress {
	ws, err := CurrentWorkSession(stateDir)
	if err != nil || ws == nil {
		return nil
	}
	return AutoCloseProgressOf(ws)
}

// AutoCloseProgressOf computes the progress snapshot from an already-loaded
// WorkSession. Callers that already hold the struct (e.g. after
// RecordDeployAttempt returned the mutated session) use this to avoid a
// duplicate disk read.
func AutoCloseProgressOf(ws *WorkSession) *AutoCloseProgress {
	if ws == nil {
		return nil
	}
	progress := &AutoCloseProgress{
		SessionID: workSessionID(ws.PID),
		Total:     len(ws.Services),
	}
	for _, h := range ws.Services {
		if serviceAutoCloseReady(ws, h) {
			progress.Ready++
			continue
		}
		progress.Pending = append(progress.Pending, h)
	}
	return progress
}

// serviceAutoCloseReady is the per-host gate used by both EvaluateAutoClose
// (boolean all-green) and AutoCloseProgressFor (counts + pending list).
// Extracted to keep the two paths reading off the same definition of "ready".
func serviceAutoCloseReady(ws *WorkSession, host string) bool {
	deploys := ws.Deploys[host]
	if len(deploys) == 0 || deploys[len(deploys)-1].SucceededAt == "" {
		return false
	}
	verifies := ws.Verifies[host]
	if len(verifies) == 0 || !verifies[len(verifies)-1].Passed {
		return false
	}
	return true
}

// CleanStaleWorkSessions scans .zcp/state/work/ for files belonging to dead
// PIDs and removes them, also unregistering their registry entries.
// Intended to run at Engine boot.
func CleanStaleWorkSessions(stateDir string) {
	if stateDir == "" {
		return
	}
	dir := filepath.Join(stateDir, workSessionDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		pidStr := strings.TrimSuffix(entry.Name(), ".json")
		pid, parseErr := strconv.Atoi(pidStr)
		if parseErr != nil {
			continue
		}
		if isProcessAlive(pid) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
		_ = UnregisterSession(stateDir, workSessionID(pid))
	}
}

// WorkSessionID returns the registry session ID for a work session.
// Format: "work-{pid}". Stable so registry lookup works across calls.
func WorkSessionID(pid int) string {
	return workSessionID(pid)
}

func workSessionID(pid int) string {
	return "work-" + strconv.Itoa(pid)
}

func workSessionPath(stateDir string, pid int) string {
	return filepath.Join(stateDir, workSessionDirName, strconv.Itoa(pid)+".json")
}

// MigrateRemoveLegacyWorkState deletes pre-WorkSession artifacts:
//   - .zcp/state/active_session file
//   - .zcp/state/develop/ directory (DevelopMarker files)
//
// Idempotent, best-effort. Called from NewEngine at startup.
func MigrateRemoveLegacyWorkState(stateDir string) {
	if stateDir == "" {
		return
	}
	_ = os.Remove(filepath.Join(stateDir, "active_session"))
	_ = os.RemoveAll(filepath.Join(stateDir, "develop"))
}

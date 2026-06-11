package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
)

// launchState is the on-disk record persisted under
// .zcp/state/launch-production/{launchID}.json. Survives compaction +
// process restart. Used by handleLaunchProduction to recover mid-launch
// state and provide idempotent resume.
//
// P-LP-1 invariant: the launchKey is NEVER written here. The struct has
// no field for it. Tests pin via TestLaunchState_NoLaunchKeyFieldExists.
type launchState struct {
	LaunchID              string                                   `json:"launchId"`
	SourceProjectID       string                                   `json:"sourceProjectId"`
	SourceRepoURL         string                                   `json:"sourceRepoUrl,omitempty"`
	TargetProjectID       string                                   `json:"targetProjectId,omitempty"`
	TargetProjectName     string                                   `json:"targetProjectName"`
	TargetServiceHostname string                                   `json:"targetServiceHostname,omitempty"`
	ImportedServices      []importedServiceEntry                   `json:"importedServices,omitempty"`
	SourceSnapshot        ops.SourceSnapshot                       `json:"sourceSnapshot"`
	Classifications       map[string]topology.SecretClassification `json:"classifications,omitempty"`
	Status                topology.LaunchProductionStatus          `json:"status"`
	// CreatedAt is the moment launchID was first written.
	CreatedAt time.Time `json:"createdAt"`
	// LastUpdate is the latest mutation timestamp.
	LastUpdate time.Time `json:"lastUpdate"`
	// LastError carries the structured failure reason when Status=failed.
	// Excludes launchKey unconditionally.
	LastError string `json:"lastError,omitempty"`
	// PipelineConfigurations records per-runtime pipeline-integration state
	// observed at the most recent configuring-pipeline check. Populated
	// after the mutation pipeline lands and re-populated on every resume
	// call with a launchKey. P-LP-7: ZCP only reads (never PUTs); these
	// entries are observation, not mutation records.
	PipelineConfigurations map[string]pipelineConfigEntry `json:"pipelineConfigurations,omitempty"`
	// PipelineCheckedAt is the timestamp of the last successful pipeline
	// check. Zero when no check has been run yet.
	PipelineCheckedAt time.Time `json:"pipelineCheckedAt,omitzero"`
	// WindowClosedAt records when action="confirm-production" closed the
	// launch window. HONEST-STATUS ONLY — the enforcement is the deleted
	// staged secret (the window is closed because there is nothing left
	// to read), never this stamp. Zero while the window is open.
	WindowClosedAt time.Time `json:"windowClosedAt,omitzero"`
	// Warnings carries non-fatal launch-time advisories from bundle
	// composition (e.g. an unreferenced promoted managed dep, compose
	// notes, grant-role fallback). Persisted so both the fresh launched
	// response and a later resume surface them — the success path used to
	// drop launchBundle.Warnings entirely.
	Warnings []string `json:"warnings,omitempty"`
	// RuntimeProds records the PRODUCTION-side runtime entries the bundle
	// imported (one per promoted runtime), keyed by the prod hostname the
	// platform actually assigns. The pipeline check matches
	// ImportedServices[].Name (prod hostname) against these — NOT against
	// the source hostname (LAUNCH-1: source `appdev` never matches prod
	// `app`, so the old single-source-hostname match silently checked
	// nothing and reported "configured"). Persisted so a resume call can
	// re-run the per-runtime check without re-reading the source project.
	RuntimeProds []launchRuntimeProd `json:"runtimeProds,omitempty"`
}

// launchRuntimeProd is one promoted runtime's production-side identity +
// the per-runtime data the pipeline-config recommendation needs.
// ProdHostname is the name the platform assigns the imported runtime
// (matches ImportedServices[].Name); RepoURL/SetupName feed the
// dashboard recommendation payload for that runtime.
type launchRuntimeProd struct {
	ProdHostname string `json:"prodHostname"`
	RepoURL      string `json:"repoUrl,omitempty"`
	SetupName    string `json:"setupName,omitempty"`
}

// pipelineConfigEntry records one runtime's pipeline-integration observation.
// Captured at every configuring-pipeline check; updated in place on resume.
type pipelineConfigEntry struct {
	// Configured is true when GetServiceStackIntegrationStatus returned
	// IntegrationConfigured for this runtime's service-stack.
	Configured bool `json:"configured"`
	// SkipReason carries why this runtime was skipped (e.g.
	// "user-opted-out" when SkipPipelineSetup=true, or "lookup-failed"
	// when GetStatus returned a non-NotConfigured error).
	SkipReason string `json:"skipReason,omitempty"`
	// DeepLink is the Zerops dashboard URL pointing at this runtime's
	// source-code config page. Populated when not configured.
	DeepLink string `json:"deepLink,omitempty"`
	// CurrentConfig is the live integration shape read from the platform.
	// Populated when Configured == true so resumes display "what's
	// already wired" without re-querying.
	CurrentConfig *pipelineConfigCurrent `json:"currentConfig,omitempty"`
	// Recommendation is the suggested config the agent surfaces in the
	// dashboard-config blocker (populated when not configured).
	Recommendation *pipelineConfigRecommendation `json:"recommendation,omitempty"`
}

// pipelineConfigCurrent mirrors platform.IntegrationStatus.Configured shape
// for state-file storage. Omits the IntegrationState (always Configured
// when this struct is non-nil).
type pipelineConfigCurrent struct {
	Provider           string `json:"provider"`
	RepositoryFullName string `json:"repositoryFullName"`
	EventType          string `json:"eventType"`
	BranchName         string `json:"branchName,omitempty"`
	TagRegex           string `json:"tagRegex,omitempty"`
	ZeropsYamlSetup    string `json:"zeropsYamlSetup,omitempty"`
	IsActive           bool   `json:"isActive"`
}

// pipelineConfigRecommendation is the suggested config payload surfaced in
// the not-configured blocker. Agent echoes these to the user when guiding
// dashboard setup.
type pipelineConfigRecommendation struct {
	RepositoryFullName string `json:"repositoryFullName"`
	EventType          string `json:"eventType"`
	TagRegex           string `json:"tagRegex"`
	ZeropsYamlSetup    string `json:"zeropsYamlSetup"`
}

// importedServiceEntry records one service stack created by
// CreateAndImportProject — used by status calls to know what to poll.
type importedServiceEntry struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ProcessIDs []string `json:"processIDs,omitempty"`
	// ImportError, if non-empty, indicates this service's import had a
	// per-service error (still part of the same ImportResult — the API
	// returns per-service errors alongside successful entries).
	ImportError string `json:"importError,omitempty"`
}

// launchStateDir is the subdirectory under stateDir where launch state
// files live. One file per launchID.
const launchStateDir = "launch-production"

// generateLaunchID derives a deterministic launchID from
// (sourceProjectID, targetProjectName). Same inputs → same launchID so
// retries find the existing state file. Hash truncated to 16 hex chars
// (8 bytes) for human-readable file names.
func generateLaunchID(sourceProjectID, targetProjectName string) string {
	sum := sha256.Sum256([]byte(sourceProjectID + "::" + targetProjectName))
	return hex.EncodeToString(sum[:8])
}

// launchStatePath returns the absolute path to the state file for a
// given launchID under the configured stateDir.
func launchStatePath(stateDir, launchID string) string {
	return filepath.Join(stateDir, launchStateDir, launchID+".json")
}

// ErrLaunchStateMissing is returned when readLaunchState is called for
// a launchID that has no state file. Used in place of (nil, nil) to
// satisfy strict lint (nilnil).
var ErrLaunchStateMissing = errors.New("launch state file missing")

// readLaunchState reads + decodes the state file. Returns
// ErrLaunchStateMissing when the file doesn't exist (first invocation);
// other errors propagate.
func readLaunchState(stateDir, launchID string) (*launchState, error) {
	path := launchStatePath(stateDir, launchID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrLaunchStateMissing
		}
		return nil, fmt.Errorf("read launch state %s: %w", path, err)
	}
	var s launchState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode launch state %s: %w", path, err)
	}
	return &s, nil
}

// writeLaunchState marshals the state and atomically writes it to disk.
// Uses temp-file-and-rename for crash safety.
func writeLaunchState(stateDir string, state *launchState) error {
	if state == nil {
		return errors.New("write launch state: nil state")
	}
	if state.LaunchID == "" {
		return errors.New("write launch state: missing LaunchID")
	}
	dir := filepath.Join(stateDir, launchStateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	state.LastUpdate = time.Now().UTC()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = state.LastUpdate
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal launch state: %w", err)
	}
	finalPath := launchStatePath(stateDir, state.LaunchID)
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename to final: %w", err)
	}
	return nil
}

// findActiveLaunchState walks the launch-production state directory and
// returns the most-recently-updated non-terminal launch matching the
// given sourceProjectID. Used by the generic status handler to surface
// a launch-active envelope when the workflow was interrupted mid-flight
// (typical compaction-recovery path).
//
// Returns:
//
//   - active: the non-terminal state with the latest LastUpdate, or nil
//     when none exists for this sourceProjectID.
//   - all:    every non-terminal state for this sourceProjectID, sorted
//     LastUpdate descending. Allows the caller to disambiguate when
//     multiple are active.
//   - err:    propagated only on filesystem traversal failure. A missing
//     state directory is NOT an error (returns nil active + nil slice +
//     nil error).
//
// Pure read-only — does not construct ProjectAdminClient or hit any
// network endpoint. P-LP-2 (no admin client outside the launch handler)
// is preserved.
//
// Terminal states (LaunchStatusLaunched, LaunchStatusFailed) are
// filtered out — once a launch reaches a terminal state, status
// recovery should fall through to the generic envelope so the user can
// start a fresh workflow or use the dedicated launch resume path.
func findActiveLaunchState(stateDir, sourceProjectID string) (*launchState, []*launchState, error) {
	if stateDir == "" || sourceProjectID == "" {
		return nil, nil, nil
	}
	dir := filepath.Join(stateDir, launchStateDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var active []*launchState
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// One JSON state file per launchID; ignore audit log + temp files.
		if name == launchAuditLogName || !hasJSONSuffix(name) || hasTempSuffix(name) {
			continue
		}
		launchID := stripJSONSuffix(name)
		state, readErr := readLaunchState(stateDir, launchID)
		if readErr != nil || state == nil {
			// Corrupt file or stale temp left behind — skip silently.
			// Recovery is a best-effort surface; one bad entry must not
			// block status for the rest.
			continue
		}
		if state.SourceProjectID != sourceProjectID {
			continue
		}
		if isTerminalLaunchStatus(state.Status) {
			continue
		}
		active = append(active, state)
	}

	if len(active) == 0 {
		return nil, nil, nil
	}

	sort.Slice(active, func(i, j int) bool {
		return active[i].LastUpdate.After(active[j].LastUpdate)
	})
	return active[0], active, nil
}

// isTerminalLaunchStatus reports whether the given status is a launch
// terminal state — recovery should fall through to generic envelope.
func isTerminalLaunchStatus(s topology.LaunchProductionStatus) bool {
	return s == topology.LaunchStatusLaunched || s == topology.LaunchStatusFailed
}

// findRecentLaunchState walks the launch-production state directory and
// returns the most-recently-updated launch matching the given
// sourceProjectID — INCLUDING terminal states (LaunchStatusLaunched,
// LaunchStatusFailed).
//
// Sister to findActiveLaunchState (which intentionally filters terminal
// states for pipeline-resume callers). This variant exists for
// `action="status"` surfacing — when a launch reached `failed` (e.g.
// schema validation rejection) the status handler must surface the
// terminal state with reset guidance instead of returning generic `idle`.
// Without this, the agent calls status, gets `idle`, may retry start,
// and either hits projectEnvDuplicateKey or burns a new launchKey on
// resume of cached state.
//
// Return shape matches findActiveLaunchState: (most-recent, all, err).
// Filters: corrupt state files skipped silently; missing dir returns
// (nil, nil, nil).
//
// FIX 1 PR 1 (eval root-cause review 2026-05-19).
func findRecentLaunchState(stateDir, sourceProjectID string) (*launchState, []*launchState, error) {
	if stateDir == "" || sourceProjectID == "" {
		return nil, nil, nil
	}
	dir := filepath.Join(stateDir, launchStateDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var matches []*launchState
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == launchAuditLogName || !hasJSONSuffix(name) || hasTempSuffix(name) {
			continue
		}
		launchID := stripJSONSuffix(name)
		state, readErr := readLaunchState(stateDir, launchID)
		if readErr != nil || state == nil {
			continue
		}
		if state.SourceProjectID != sourceProjectID {
			continue
		}
		matches = append(matches, state)
	}

	if len(matches) == 0 {
		return nil, nil, nil
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].LastUpdate.After(matches[j].LastUpdate)
	})
	return matches[0], matches, nil
}

func hasJSONSuffix(name string) bool {
	return len(name) > len(".json") && name[len(name)-len(".json"):] == ".json"
}

func hasTempSuffix(name string) bool {
	return len(name) > len(".tmp") && name[len(name)-len(".tmp"):] == ".tmp"
}

func stripJSONSuffix(name string) string {
	if !hasJSONSuffix(name) {
		return name
	}
	return name[:len(name)-len(".json")]
}

// launchAuditLogPath returns the path to the append-only audit log.
// One log per stateDir (per project context), shared across all launchIDs.
const launchAuditLogName = "launch-audit-log.json"

// launchAuditEntry is one append-only record of a launch mutation.
// Records who-did-what when, with no secret values and no launchKey.
type launchAuditEntry struct {
	Timestamp         time.Time                                `json:"timestamp"`
	LaunchID          string                                   `json:"launchId"`
	Action            string                                   `json:"action"` // e.g. "create-and-import", "delete-target", "publish-failed"
	SourceProjectID   string                                   `json:"sourceProjectId"`
	TargetProjectID   string                                   `json:"targetProjectId,omitempty"`
	TargetProjectName string                                   `json:"targetProjectName,omitempty"`
	SourceCommitSHA   string                                   `json:"sourceCommitSha,omitempty"`
	SourceYAMLSHA256  string                                   `json:"sourceYamlSha256,omitempty"`
	Classifications   map[string]topology.SecretClassification `json:"classifications,omitempty"`
	HAOptOut          []string                                 `json:"haOptOut,omitempty"`
	Result            string                                   `json:"result"` // success | failure
	ErrorMessage      string                                   `json:"errorMessage,omitempty"`
}

// appendAuditLog appends one entry to the launch audit log. Open mode is
// O_APPEND so concurrent writes from parallel invocations don't clobber
// each other.
func appendAuditLog(stateDir string, entry launchAuditEntry) error {
	dir := filepath.Join(stateDir, launchStateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, launchAuditLogName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = f.Close() }()
	entry.Timestamp = time.Now().UTC()
	// One JSON object per line for easy tailing.
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit entry: %w", err)
	}
	return nil
}

// launchRuntimeScaling is the consented production container range for
// one promoted runtime (WorkflowInput.RuntimeScaling values).
type launchRuntimeScaling struct {
	MinContainers int `json:"minContainers,omitempty"`
	MaxContainers int `json:"maxContainers,omitempty"`
}

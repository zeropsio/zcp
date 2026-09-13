package farm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// ProjectPrefix names every project the controller creates or deletes
// (docs/spec-eval-farm.md §3.2 FM-19). Guard, below, is the sole call site
// for DeleteProject and the sole place membership in this prefix is
// checked before a mutating call — every listing the controller builds is
// filtered through the same predicate before anything else looks at it
// (FM-20).
const ProjectPrefix = "zcp-farm-"

// productionProjectName uses a role marker outside both legacy and generated
// run-id grammars. It therefore cannot equal a primary project name, even
// when a scenario itself is named "prod" or ends in "-prod".
func productionProjectName(runID string) string { return ProjectPrefix + "prod__" + runID }

// ProductionProjectName returns the sole production-project name that a new
// run may declare for controller cleanup.
func ProductionProjectName(runID string) string { return productionProjectName(runID) }

// Per-run acceptance verdicts (§5.1 vocabulary; "not-run" is a row-level
// status this slice never assigns at the whole-run level — §5.1: "a run
// whose project was never created is not reported at all").
const (
	ResultPassed          = "passed"
	ResultFailed          = "failed"
	ResultBlocked         = "blocked"
	batchEndedByInterrupt = "interrupt"
)

// DetailNoBundle is the RunResult.Detail / GCCandidate.Exempt value for a
// run whose budget elapsed without ever producing runs/<runId>/done.json
// (FM-3, FM-21) — shared by RunBatch's waitForDone and gc.go's GC so the
// two agree on the same literal (goconst).
const DetailNoBundle = "no bundle"

// DetailInterrupted is the RunResult.Detail value for a run RunBatch was
// still waiting on when its context was cancelled (Ctrl-C / SIGTERM, R3,
// FM-9's "interrupt" endedBy value). Its project is kept and any minted
// launch token id is recorded rather than revoked — the same FM-21-style
// exemption as DetailNoBundle, so `gc` can finish the job later.
const DetailInterrupted = "interrupted"

// PlatformClient is the account-wide platform surface the controller needs.
// The real implementation (NewAccountClient) wraps platform.NewZeropsClient
// and platform.NewProjectAdminClient — both existing SDK-based constructors
// (§3 FM-15: "never a new HTTP client") — built once from
// ZCP_FARM_ACCOUNT_TOKEN.
type PlatformClient interface {
	ListProjects(ctx context.Context, clientID string) ([]platform.Project, error)
	CreateAndImportProject(ctx context.Context, yaml string) (*platform.ImportResult, error)
	// ImportServiceStack imports the run's zcp@1 service into an
	// already-created project shell — §2.1 step 3, POST
	// /project/{id}/service-stack/import.
	ImportServiceStack(ctx context.Context, projectID, yaml string) (*platform.ImportResult, error)
	GetProject(ctx context.Context, projectID string) (*platform.Project, error)
	DeleteProject(ctx context.Context, projectID string) (*platform.Process, error)
	MintDelegatedLaunchToken(ctx context.Context, name string) (platform.MintedToken, error)
	// MintProjectScopedToken mints the run's own project-scoped ZCP_API_KEY
	// (§2.1 step 2, platform.MintProjectScopedToken) — a REST-created
	// zcp@1 service gets no first-class injected key the way the GUI's
	// import route does.
	MintProjectScopedToken(ctx context.Context, clientID, projectID, name string) (platform.MintedToken, error)
	RevokeIntegrationToken(ctx context.Context, clientID, tokenID string) error
	// GetProjectProcessesDirect reads all of a project's processes via the
	// DIRECT (non-ES) read (D19) — waitForDone polls it alongside
	// done.json so a FAILED creation-phase process (stack.create,
	// stack.import) settles the run blocked instead of burning the whole
	// run budget waiting for a done.json a dead project will never write.
	GetProjectProcessesDirect(ctx context.Context, projectID string) ([]platform.Process, error)
}

// accountClient adapts platform.ProjectAdminClient (CreateAndImportProject/
// GetProject/DeleteProject) and *platform.ZeropsClient (ListProjects/
// ImportServices/MintDelegatedLaunchToken/MintProjectScopedToken/
// RevokeIntegrationToken) — both constructed from the same account-wide
// token — to PlatformClient.
type accountClient struct {
	admin platform.ProjectAdminClient
	z     *platform.ZeropsClient
}

func (a *accountClient) ListProjects(ctx context.Context, clientID string) ([]platform.Project, error) {
	return a.z.ListProjects(ctx, clientID)
}

func (a *accountClient) CreateAndImportProject(ctx context.Context, yaml string) (*platform.ImportResult, error) {
	return a.admin.CreateAndImportProject(ctx, yaml)
}

func (a *accountClient) ImportServiceStack(ctx context.Context, projectID, yaml string) (*platform.ImportResult, error) {
	return a.z.ImportServices(ctx, projectID, yaml)
}

func (a *accountClient) GetProject(ctx context.Context, projectID string) (*platform.Project, error) {
	return a.admin.GetProject(ctx, projectID)
}

func (a *accountClient) DeleteProject(ctx context.Context, projectID string) (*platform.Process, error) {
	return a.admin.DeleteProject(ctx, projectID)
}

func (a *accountClient) MintDelegatedLaunchToken(ctx context.Context, name string) (platform.MintedToken, error) {
	return a.z.MintDelegatedLaunchToken(ctx, name)
}

func (a *accountClient) MintProjectScopedToken(ctx context.Context, clientID, projectID, name string) (platform.MintedToken, error) {
	return a.z.MintProjectScopedToken(ctx, clientID, projectID, name)
}

func (a *accountClient) RevokeIntegrationToken(ctx context.Context, clientID, tokenID string) error {
	return a.z.RevokeIntegrationToken(ctx, clientID, tokenID)
}

func (a *accountClient) GetProjectProcessesDirect(ctx context.Context, projectID string) ([]platform.Process, error) {
	return a.z.GetProjectProcessesDirect(ctx, projectID)
}

// NewAccountClient constructs the controller's platform client from the
// farm host's account-wide token (ZCP_FARM_ACCOUNT_TOKEN, §2.4 FM-15),
// targeting the operator-configured org (clientID, ZCP_FARM_CLIENT_ID).
// D11: the underlying admin client is built with
// platform.NewProjectAdminClientForClient, never platform.NewProjectAdminClient
// — the latter's ClientUserList[0] selection picks the wrong org for a
// token that is a member of more than one, and this constructor refuses
// (before any write) when the token is not a member of clientID at all.
// The returned closer must be called once the controller is done with it —
// it zeros the admin client's held reference.
func NewAccountClient(accountToken, apiHost, clientID string) (PlatformClient, func(), error) {
	z, err := platform.NewZeropsClient(accountToken, apiHost)
	if err != nil {
		return nil, nil, fmt.Errorf("farm: construct account client: %w", err)
	}
	admin, err := platform.NewProjectAdminClientForClient(accountToken, apiHost, clientID)
	if err != nil {
		return nil, nil, fmt.Errorf("farm: construct account admin client: %w", err)
	}
	return &accountClient{admin: admin, z: z}, admin.Close, nil
}

// Guard is the sole call site for DeleteProject (§3.2 FM-19/FM-20): it
// refuses — without making any platform call — when name does not carry the
// zcp-farm- prefix, and re-reads the project by id right before deleting it
// (check-before-mutate, CLAUDE.md O3) to confirm the live name still does.
func Guard(ctx context.Context, client PlatformClient, projectID, name string) error {
	if !strings.HasPrefix(name, ProjectPrefix) {
		return fmt.Errorf("farm: refusing to delete project %s (name %q): name does not carry the %q prefix", projectID, name, ProjectPrefix)
	}
	proj, err := client.GetProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("farm: guard: re-read project %s before delete: %w", projectID, err)
	}
	if !strings.HasPrefix(proj.Name, ProjectPrefix) {
		return fmt.Errorf("farm: refusing to delete project %s: live name %q lost the %q prefix", projectID, proj.Name, ProjectPrefix)
	}
	if _, err := client.DeleteProject(ctx, projectID); err != nil {
		return fmt.Errorf("farm: delete project %s: %w", projectID, err)
	}
	return nil
}

// ScenarioRun is one scenario `farm run` will create a project for.
type ScenarioRun struct {
	ID string
	// Launch marks a launch scenario: the controller mints a per-run
	// ZCP_E2E_LAUNCH_KEY before creating its project and, once the run
	// settles, also deletes its explicitly named production target and
	// revokes the token (§3.4 FM-23).
	Launch bool
	// ProductionProjectName is an exact, scenario-declared production target.
	// Empty means no structured ownership was declared; the controller never
	// guesses/deletes a target from Launch alone, while launch-token handling
	// still follows the Launch flag.
	ProductionProjectName string
}

// RunOptions is the input to RunBatch (§3.3 FM-21/FM-22).
type RunOptions struct {
	Batch    string
	ClientID string // ZCP_FARM_CLIENT_ID (§2.4 FM-15)
	// Set is recorded verbatim in the manifest ("gate" | "all" |
	// "<id,id,...>") — RunBatch itself only ever sees the resolved
	// Scenarios list.
	Set             string
	CandidateSHA256 string
	EvaluatorSHA256 string
	// WrapperSHA256 pins the content-addressed farm/wrapper/<sha>.sh every
	// run project fetches and sha256-verifies before exec (project_yaml.go).
	WrapperSHA256   string
	ScenariosDigest string
	Scenarios       []ScenarioRun
	// OAuthToken is the run's sole model-request credential
	// (CLAUDE_CODE_OAUTH_TOKEN) — the agent credential is OAuth-only, no
	// api-key mode and no fallback (§2.4/FM-16, spec commit 79ced2cc).
	OAuthToken string
	// Observer is `farm run`'s --observer choice ("<model>" or "off"),
	// recorded verbatim in the manifest (§1.4, §3.3, §7.7); RunBatch never
	// reads it beyond that — it never reaches a run project.
	Observer  string
	Sink      Sink
	RunBudget time.Duration
	// Note is `farm run --note`: why the batch ran, recorded verbatim in
	// the manifest (§3.3). RunBatch never reads it beyond that.
	Note string
	// RunBudgetSec is RunBudget in seconds, recorded in the manifest so a
	// reader can tell a stalled run from a running one (§3.3, §8.8).
	RunBudgetSec int
	// CandidateInfo is the candidate binary's embedded Go build info, when
	// `farm push --candidate` recorded it (§3.3) — nil when the candidate
	// was built without VCS stamping, or was pushed before this field
	// existed.
	CandidateInfo *CandidateInfo
	// PollInterval is how often RunBatch re-checks the bucket for
	// done.json while waiting; zero defaults to 2s.
	PollInterval time.Duration
	// MaxConcurrent bounds how many run projects RunBatch keeps alive at
	// once (§3.3 FM-65): 0 means unlimited (today's behaviour, every
	// scheduled run created before any settles) — `farm run
	// --max-concurrent` sets it, defaulting to 8.
	MaxConcurrent int
	// Now defaults to time.Now; tests inject a fixed/advancing clock.
	Now func() time.Time
}

// RunResult is one run's outcome — the CLI prints
// "<runId> <scenario> passed|failed|blocked|not-run" from these.
type RunResult struct {
	RunID                 string
	Scenario              string
	ProjectID             string // empty once the run's project has been deleted
	ProductionProjectName string
	Result                string
	Detail                string
	// Error carries the wrapped error message for a run RunBatch blocked
	// before or during creation (ProjectImportYAML, CreateAndImportProject,
	// a non-403 mint, ServiceImportYAML, ImportServiceStack — D10). Empty
	// for every run that reached waitForDone; Detail (not Error) carries
	// the reason for a run blocked at settle time (no bundle / digest
	// mismatch / meta.json unreadable).
	Error         string
	LaunchTokenID string // empty once revoked
}

// scheduledRun is one ScenarioRun RunBatch has assigned a runId to, before
// its project (if any) exists.
type scheduledRun struct {
	ScenarioRun
	RunID string
}

// activeRun is a scheduledRun whose project + (for a launch scenario)
// per-run tokens were created successfully — createRun's success output,
// carried into RunBatch's settle loop.
type activeRun struct {
	scheduledRun
	ProjectID             string
	LaunchTokenID         string
	RunTokenID            string
	ProductionProjectName string
	// Deadline is this run's own budget deadline, set once at creation —
	// createdAt + RunBudget (R7). Runs are still waited on in order, but
	// each against its own deadline: a hung run earlier in the batch must
	// never extend a later run's budget by however long it took to give up
	// on the earlier one.
	Deadline time.Time
}

type batchAbortError struct {
	cause  error
	result RunResult
}

func (e *batchAbortError) Error() string { return e.cause.Error() }
func (e *batchAbortError) Unwrap() error { return e.cause }

// createRun performs one scheduled run's creation steps (§2.1: mint launch
// token if needed, create the project shell, mint the run's project-scoped
// token, import the zcp service) and reports exactly one of three outcomes:
//   - active set, the other two zero — created successfully, ready for
//     RunBatch's settle loop.
//   - blockedResult set, the other two zero — a per-run failure (D10):
//     already printed to stderr and Guard-rolled-back where a project was
//     created; the caller folds blockedResult into results/summary and
//     continues the batch.
//   - abortErr set, the other two zero — the run-token mint came back 403
//     (the farm's account token is itself an integration token without
//     delegation): every other run would fail identically, so RunBatch
//     aborts the batch.
func createRun(ctx context.Context, client PlatformClient, opts RunOptions, r scheduledRun) (active *activeRun, blockedResult *RunResult, abortErr error) {
	var launchTokenID, launchKeyValue string
	if r.Launch {
		minted, err := client.MintDelegatedLaunchToken(ctx, "farm-"+r.RunID)
		if err != nil {
			// No project was created for this run — §5.1: "a run whose
			// project was never created is not reported at all" (in the
			// bucket/project sense: no project, no token to revoke) — but
			// D10 requires the run itself still be reported blocked.
			rr := recordBlocked(r.RunID, r.ID, r.ProductionProjectName, fmt.Errorf("mint launch token: %w", err))
			return nil, &rr, nil
		}
		launchTokenID = minted.TokenID
		launchKeyValue = minted.Token
	}

	desc := RunDescriptor{
		BatchID:         opts.Batch,
		RunID:           r.RunID,
		ScenarioID:      r.ID,
		EvaluatorSHA256: opts.EvaluatorSHA256,
		WrapperSHA256:   opts.WrapperSHA256,
		CandidateSHA256: opts.CandidateSHA256,
		ScenariosDigest: opts.ScenariosDigest,
		Sink:            opts.Sink,
		OAuthToken:      opts.OAuthToken,
		LaunchKey:       launchKeyValue,
	}

	// §2.1 step 1: create the empty project shell. The REST import route
	// never injects a first-class ZCP_API_KEY the way the GUI's route
	// does, so the service is imported in a later step once a
	// project-scoped token exists to carry.
	projectYAML, err := ProjectImportYAML(desc)
	if err != nil {
		rr := recordBlockedRevokingLaunchToken(ctx, client, opts, r, launchTokenID, fmt.Errorf("build project import yaml: %w", err))
		return nil, &rr, nil
	}
	result, err := client.CreateAndImportProject(ctx, string(projectYAML))
	if err != nil {
		rr := recordBlockedRevokingLaunchToken(ctx, client, opts, r, launchTokenID, fmt.Errorf("create project: %w", err))
		return nil, &rr, nil
	}
	runProjectName := ProjectPrefix + r.RunID

	// §2.1 step 2: mint the run's own project-scoped ZCP_API_KEY. A 403
	// here means the minting credential (ZCP_FARM_ACCOUNT_TOKEN) is
	// itself an integration token — every other run would fail the same
	// way, so this aborts the whole batch (after rolling back the shell
	// project this run just created) instead of skipping just this run
	// (brief: "a mint 403 aborts the batch before any project exists").
	minted, err := client.MintProjectScopedToken(ctx, opts.ClientID, result.ProjectID, "farm-run-"+r.RunID)
	if err != nil {
		if isScopedMintForbidden(err) {
			rollbackErr := Guard(ctx, client, result.ProjectID, runProjectName)
			var revokeErr error
			if launchTokenID != "" {
				revokeErr = client.RevokeIntegrationToken(ctx, opts.ClientID, launchTokenID)
			}
			rr := recordBlocked(r.RunID, r.ID, r.ProductionProjectName, fmt.Errorf("mint run token: %w", err))
			if rollbackErr != nil {
				rr.ProjectID = result.ProjectID
				rr.Error = fmt.Sprintf("%s; rollback failed: %v", rr.Error, rollbackErr)
			}
			if revokeErr != nil {
				rr.LaunchTokenID = launchTokenID
				rr.Error = fmt.Sprintf("%s; launch token revoke failed: %v", rr.Error, revokeErr)
			}
			return nil, nil, &batchAbortError{cause: fmt.Errorf("farm run: mint run token for %s: %w", r.RunID, err), result: rr}
		}
		rr := recordBlockedAfterRollback(ctx, client, opts, r, result.ProjectID, runProjectName, launchTokenID, fmt.Errorf("mint run token: %w", err))
		return nil, &rr, nil
	}
	desc.RunToken = minted.Token

	// §2.1 step 3: import the zcp service, now carrying the minted run
	// token as ZCP_API_KEY.
	serviceYAML, err := ServiceImportYAML(desc)
	if err != nil {
		rr := recordBlockedAfterRollback(ctx, client, opts, r, result.ProjectID, runProjectName, launchTokenID, fmt.Errorf("build service import yaml: %w", err))
		return nil, &rr, nil
	}
	if _, err := client.ImportServiceStack(ctx, result.ProjectID, string(serviceYAML)); err != nil {
		rr := recordBlockedAfterRollback(ctx, client, opts, r, result.ProjectID, runProjectName, launchTokenID, fmt.Errorf("import service stack: %w", err))
		return nil, &rr, nil
	}

	return &activeRun{
		scheduledRun:          r,
		ProjectID:             result.ProjectID,
		LaunchTokenID:         launchTokenID,
		RunTokenID:            minted.TokenID,
		ProductionProjectName: r.ProductionProjectName,
	}, nil, nil
}

// recordBlockedRevokingLaunchToken is recordBlocked plus R2 (FM-23): a
// per-run failure BEFORE any project was created still leaves a launch
// scenario's already-minted token dangling unless it is revoked right now;
// if the revoke itself fails, the id is kept on the result so RunBatch's
// end-of-batch manifest/summary pass and `gc` can finish the job later.
func recordBlockedRevokingLaunchToken(ctx context.Context, client PlatformClient, opts RunOptions, r scheduledRun, launchTokenID string, err error) RunResult {
	rr := recordBlocked(r.RunID, r.ID, r.ProductionProjectName, err)
	revokeLaunchTokenOnto(ctx, client, opts, launchTokenID, &rr)
	return rr
}

// recordBlockedAfterRollback is recordBlocked plus R6 and R2: it rolls the
// just-created project shell back through Guard first — on a rollback
// failure it keeps rr.ProjectID (the leaked project, instead of the "" that
// makes the summary look like it was already cleaned up) and appends the
// guard's own error to rr.Error, so the leak is visible instead of silently
// discarded — then revokes the run's launch token, if any, recording its id
// back onto rr when that revoke itself fails (FM-23).
func recordBlockedAfterRollback(ctx context.Context, client PlatformClient, opts RunOptions, r scheduledRun, projectID, projectName, launchTokenID string, err error) RunResult {
	rollbackErr := Guard(ctx, client, projectID, projectName)
	rr := recordBlocked(r.RunID, r.ID, r.ProductionProjectName, err)
	if rollbackErr != nil {
		rr.ProjectID = projectID
		rr.Error = fmt.Sprintf("%s; rollback failed: %v", rr.Error, rollbackErr)
	}
	revokeLaunchTokenOnto(ctx, client, opts, launchTokenID, &rr)
	return rr
}

// revokeLaunchTokenOnto revokes launchTokenID immediately (a no-op when
// empty — not a launch scenario, or the token was never minted); on failure
// it records the id on rr instead of dropping it, so RunBatch's
// end-of-batch manifest/summary pass and `gc` can finish the revoke once
// the project is gone (R2, FM-23).
func revokeLaunchTokenOnto(ctx context.Context, client PlatformClient, opts RunOptions, launchTokenID string, rr *RunResult) {
	if launchTokenID == "" {
		return
	}
	if err := client.RevokeIntegrationToken(ctx, opts.ClientID, launchTokenID); err != nil {
		rr.LaunchTokenID = launchTokenID
	}
}

// scheduleRuns validates scenarios (ValidScenarioID, no duplicate id, a
// production project name only ever the controller-owned one and only for a
// launch scenario) and assigns each one its runId, before any project is
// created — RunBatch's first step.
func scheduleRuns(batch string, scenarios []ScenarioRun) ([]scheduledRun, []ManifestRun, error) {
	seenScenarios := make(map[string]struct{}, len(scenarios))
	scheduled := make([]scheduledRun, 0, len(scenarios))
	manifestRuns := make([]ManifestRun, 0, len(scenarios))
	for _, sc := range scenarios {
		if !ValidScenarioID(sc.ID) {
			return nil, nil, fmt.Errorf("farm run: invalid scenario %q", sc.ID)
		}
		if _, exists := seenScenarios[sc.ID]; exists {
			return nil, nil, fmt.Errorf("farm run: duplicate scenario %q", sc.ID)
		}
		seenScenarios[sc.ID] = struct{}{}

		runID, err := EncodeRunID(batch, sc.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("farm run: encode %s: %w", sc.ID, err)
		}
		if sc.ProductionProjectName != "" && sc.ProductionProjectName != productionProjectName(runID) {
			return nil, nil, fmt.Errorf("farm run: scenario %s has foreign production project %q", sc.ID, sc.ProductionProjectName)
		}
		if !sc.Launch && sc.ProductionProjectName != "" {
			return nil, nil, fmt.Errorf("farm run: non-launch scenario %s has production project", sc.ID)
		}
		scheduled = append(scheduled, scheduledRun{ScenarioRun: sc, RunID: runID})
		mr := ManifestRun{RunID: runID, Scenario: sc.ID, ProjectName: ProjectPrefix + runID}
		mr.ProductionProjectName = sc.ProductionProjectName
		manifestRuns = append(manifestRuns, mr)
	}
	return scheduled, manifestRuns, nil
}

// batchState is RunBatch's create/settle bookkeeping, factored out of the
// function body so every run's outcome — creation-phase blocked, settled
// early to respect opts.MaxConcurrent (§3.3 FM-65), or settled by the
// trailing pass — is recorded through the one path finalizeOldestActive and
// finalizeRemainingActives share, and reassembled in scheduled order by
// orderedResults regardless of the order in which runs actually settle.
type batchState struct {
	client       PlatformClient
	sink         *SinkClient
	opts         RunOptions
	scheduled    []scheduledRun
	now          func() time.Time
	pollInterval time.Duration

	actives          []activeRun
	resultsByRunID   map[string]RunResult
	cleanupErrs      []error
	endedByBudget    bool
	endedByInterrupt bool
}

func (s *batchState) orderedResults() []RunResult {
	ordered := make([]RunResult, 0, len(s.resultsByRunID))
	for _, r := range s.scheduled {
		if rr, ok := s.resultsByRunID[r.RunID]; ok {
			ordered = append(ordered, rr)
		}
	}
	return ordered
}

// recordFinalized folds one finalizeActiveRun outcome into s — shared by
// finalizeOldestActive (the windowed early settle) and
// finalizeRemainingActives (the trailing settle pass) so both paths update
// endedByBudget/endedByInterrupt/cleanupErrs identically.
func (s *batchState) recordFinalized(rr RunResult, budget, interrupted bool, cleanupErr error) {
	s.resultsByRunID[rr.RunID] = rr
	if cleanupErr != nil {
		s.cleanupErrs = append(s.cleanupErrs, cleanupErr)
	}
	// FM-21's sole exemption (budget elapsed) and R3's interrupt exemption
	// share the same shape: the project is kept for inspection, the token
	// (if any) stays unrevoked and is recorded so `gc` can finish the job
	// later.
	if interrupted {
		s.endedByInterrupt = true
	} else if budget {
		s.endedByBudget = true
	}
}

// finalizeOldestActive settles actives[0] — the longest-running active
// run — so §3.3 FM-65's window never exceeds opts.MaxConcurrent.
func (s *batchState) finalizeOldestActive(ctx context.Context) {
	a := s.actives[0]
	s.actives = s.actives[1:]
	rr, budget, interrupted, cleanupErr := finalizeActiveRun(ctx, s.client, s.sink, a, s.opts, s.now, s.pollInterval)
	s.recordFinalized(rr, budget, interrupted, cleanupErr)
}

// finalizeRemainingActives settles every run still open once every
// scheduled run has been created (or the batch stopped creating more).
func (s *batchState) finalizeRemainingActives(ctx context.Context) {
	for _, a := range s.actives {
		rr, budget, interrupted, cleanupErr := finalizeActiveRun(ctx, s.client, s.sink, a, s.opts, s.now, s.pollInterval)
		s.recordFinalized(rr, budget, interrupted, cleanupErr)
	}
}

// runCreates creates each scheduled run in order, settling the oldest
// active run first whenever opts.MaxConcurrent's window is full (§3.3
// FM-65). aborted reports whether a batch-ending failure occurred (a
// scoped-mint 403, or a manifest write failure): RunBatch must return
// results/err immediately in that case, never fall through to its own
// trailing settle pass.
func (s *batchState) runCreates(ctx context.Context, manifest *BatchManifest, manifestRuns []ManifestRun) (results []RunResult, aborted bool, err error) {
	runTokenIDs := make(map[string]string, len(s.scheduled)) // runID -> minted run token id, for the manifest-evidence update below
	for _, r := range s.scheduled {
		// §3.3 FM-65: never more than opts.MaxConcurrent run projects alive
		// at once (0 = unlimited, today's behaviour) — settle the oldest
		// active run before creating the next.
		for s.opts.MaxConcurrent > 0 && len(s.actives) >= s.opts.MaxConcurrent {
			s.finalizeOldestActive(ctx)
		}
		active, blockedResult, abortErr := createRun(ctx, s.client, s.opts, r)
		if abortErr != nil {
			var abort *batchAbortError
			if errors.As(abortErr, &abort) && abort.result.RunID != "" {
				s.resultsByRunID[abort.result.RunID] = abort.result
			}
			if len(runTokenIDs) > 0 {
				if err := persistRunTokenIDs(ctx, s.sink, s.opts.Batch, manifest, manifestRuns, runTokenIDs); err != nil {
					abortErr = errors.Join(abortErr, fmt.Errorf("farm run: update manifest with run token ids: %w", err))
				}
			}
			results, finalizeErr := finalizeAfterFailure(ctx, s.client, s.sink, s.opts, s.actives, s.orderedResults(), abortErr, s.now, s.pollInterval)
			return results, true, finalizeErr
		}
		if blockedResult != nil {
			s.resultsByRunID[blockedResult.RunID] = *blockedResult
			continue
		}
		// R7: the deadline is anchored at THIS run's own creation moment,
		// never recomputed when its wait turn comes up in the settle loop
		// below — otherwise a hung earlier run's wait time would silently
		// extend every later run's budget by however long it took to give
		// up on the earlier one.
		active.Deadline = s.now().Add(s.opts.RunBudget)
		runTokenIDs[active.RunID] = active.RunTokenID
		s.actives = append(s.actives, *active)
	}

	// Record each minted run token's id (never the value, §3.4-style
	// discipline) against its manifest entry — evidence for `gc`/`status`,
	// never the registry (§1.4). This is a second write to the same
	// manifest.json the pre-creation write above already produced; FM-22
	// pins only that the FIRST write happens before any project exists,
	// not that the manifest is written exactly once.
	if len(runTokenIDs) > 0 {
		// R3: this write must land even if ctx is cancelled mid-batch — an
		// interrupted operator still wants the minted run token ids on
		// record.
		if err := persistRunTokenIDs(ctx, s.sink, s.opts.Batch, manifest, manifestRuns, runTokenIDs); err != nil {
			cause := fmt.Errorf("farm run: update manifest with run token ids: %w", err)
			results, finalizeErr := finalizeAfterFailure(ctx, s.client, s.sink, s.opts, s.actives, s.orderedResults(), cause, s.now, s.pollInterval)
			return results, true, finalizeErr
		}
	}
	return nil, false, nil
}

// RunBatch runs opts.Scenarios as one batch (§3.3 FM-21/FM-22): it writes
// batches/<batch>/manifest.json before creating any project, creates one
// project per scenario (never more than opts.MaxConcurrent alive at once,
// §3.3 FM-65 — 0 means unlimited), watches the bucket for each run's
// done.json (or its budget elapsing), verifies the parts digests FM-5
// requires, deletes each settled run's project(s) and — for launch
// scenarios — revokes the launch token, then writes
// batches/<batch>/summary.json. Results are returned in scheduled order
// regardless of the order in which runs actually settle.
func RunBatch(ctx context.Context, client PlatformClient, sink *SinkClient, opts RunOptions) ([]RunResult, error) {
	if !ValidBatchID(opts.Batch) {
		return nil, fmt.Errorf("farm run: invalid batch %q", opts.Batch)
	}
	scheduled, manifestRuns, err := scheduleRuns(opts.Batch, opts.Scenarios)
	if err != nil {
		return nil, err
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	pollInterval := opts.PollInterval
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}
	startedAt := now().UTC().Format(time.RFC3339)

	manifest := BatchManifest{
		Batch:           opts.Batch,
		CreatedAt:       startedAt,
		StartedAt:       startedAt,
		Set:             opts.Set,
		CandidateSha256: opts.CandidateSHA256,
		EvaluatorSha256: opts.EvaluatorSHA256,
		ScenariosDigest: opts.ScenariosDigest,
		Observer:        opts.Observer,
		Note:            opts.Note,
		RunBudgetSec:    opts.RunBudgetSec,
		CandidateInfo:   opts.CandidateInfo,
		MaxConcurrent:   opts.MaxConcurrent,
		Runs:            manifestRuns,
	}
	if err := CreateManifest(ctx, sink, opts.Batch, manifest); err != nil {
		return nil, fmt.Errorf("farm run: write manifest: %w", err)
	}

	state := &batchState{
		client: client, sink: sink, opts: opts, scheduled: scheduled,
		now: now, pollInterval: pollInterval,
		resultsByRunID: make(map[string]RunResult, len(scheduled)),
	}
	if createResults, aborted, createErr := state.runCreates(ctx, &manifest, manifestRuns); aborted {
		return createResults, createErr
	}
	state.finalizeRemainingActives(ctx)
	results := state.orderedResults()

	endedBy := "settled"
	switch {
	case state.endedByInterrupt:
		endedBy = batchEndedByInterrupt
	case state.endedByBudget:
		endedBy = "budget"
	}
	summary := BatchSummary{Batch: opts.Batch, FinishedAt: now().UTC().Format(time.RFC3339), EndedBy: endedBy}
	for _, rr := range results {
		// RunResult and SummaryRun share the same field names/types/order
		// (only the json tags differ) — a direct conversion instead of a
		// field-by-field literal.
		summary.Runs = append(summary.Runs, SummaryRun(rr))
	}
	// R3: write the summary through a cancellation-immune context — the
	// whole point of the interrupt exemption is that the operator's own
	// Ctrl-C/SIGTERM must not also block the write that records it.
	if err := PutSummary(context.WithoutCancel(ctx), sink, opts.Batch, summary); err != nil {
		return results, errors.Join(fmt.Errorf("farm run: write summary: %w", err), errors.Join(state.cleanupErrs...))
	}
	if err := errors.Join(state.cleanupErrs...); err != nil {
		return results, fmt.Errorf("farm run: cleanup: %w", err)
	}
	return results, nil
}

func persistRunTokenIDs(ctx context.Context, sink *SinkClient, batch string, manifest *BatchManifest, runs []ManifestRun, ids map[string]string) error {
	for i := range runs {
		if id, ok := ids[runs[i].RunID]; ok {
			runs[i].RunTokenID = id
		}
	}
	manifest.Runs = runs
	return PutManifest(context.WithoutCancel(ctx), sink, batch, *manifest)
}

// finalizeAfterFailure applies the ordinary settlement rules to runs created
// before a later creation/final-write failure. It deliberately uses the same
// summary boundary as a normal batch, so retained project/token ids remain
// recoverable when cleanup or storage fails.
func finalizeAfterFailure(ctx context.Context, client PlatformClient, sink *SinkClient, opts RunOptions, actives []activeRun, blocked []RunResult, cause error, now func() time.Time, pollInterval time.Duration) ([]RunResult, error) {
	results := append([]RunResult(nil), blocked...)
	var cleanupErrs []error
	endedByBudget := false
	endedByInterrupt := false
	for _, a := range actives {
		rr, budget, interrupted, cleanupErr := finalizeActiveRun(ctx, client, sink, a, opts, now, pollInterval)
		results = append(results, rr)
		if cleanupErr != nil {
			cleanupErrs = append(cleanupErrs, cleanupErr)
		}
		endedByBudget = endedByBudget || budget
		endedByInterrupt = endedByInterrupt || interrupted
	}
	endedBy := "settled"
	if endedByInterrupt {
		endedBy = batchEndedByInterrupt
	} else if endedByBudget {
		endedBy = "budget"
	}
	summary := BatchSummary{Batch: opts.Batch, FinishedAt: now().UTC().Format(time.RFC3339), EndedBy: endedBy}
	for _, rr := range results {
		summary.Runs = append(summary.Runs, SummaryRun(rr))
	}
	if err := PutSummary(context.WithoutCancel(ctx), sink, opts.Batch, summary); err != nil {
		return results, errors.Join(cause, fmt.Errorf("farm run: final summary unavailable: %w", err), errors.Join(cleanupErrs...))
	}
	return results, errors.Join(cause, errors.Join(cleanupErrs...))
}

func finalizeActiveRun(ctx context.Context, client PlatformClient, sink *SinkClient, a activeRun, opts RunOptions, now func() time.Time, pollInterval time.Duration) (RunResult, bool, bool, error) {
	result, detail, settled := waitForDone(ctx, client, sink, a.RunID, a.ID, opts.CandidateSHA256, opts.EvaluatorSHA256, a.ProjectID, a.Deadline, now, pollInterval)
	rr := RunResult{RunID: a.RunID, Scenario: a.ID, ProjectID: a.ProjectID, Result: result, Detail: detail, LaunchTokenID: a.LaunchTokenID}
	rr.ProductionProjectName = a.ProductionProjectName
	if !settled {
		return rr, detail != DetailInterrupted, detail == DetailInterrupted, nil
	}
	runProjectName := ProjectPrefix + a.RunID
	var cleanupErrs []error
	if err := Guard(ctx, client, a.ProjectID, runProjectName); err == nil {
		rr.ProjectID = ""
	} else {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("farm run: cleanup project %s: %w", a.RunID, err))
	}
	if a.Launch {
		if a.ProductionProjectName != "" {
			prodID, ok, err := findProjectByName(ctx, client, opts.ClientID, a.ProductionProjectName)
			if err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("farm run: list production project %s: %w", a.RunID, err))
			} else if ok {
				if err := Guard(ctx, client, prodID, a.ProductionProjectName); err != nil {
					cleanupErrs = append(cleanupErrs, fmt.Errorf("farm run: cleanup production project %s: %w", a.RunID, err))
				}
			}
		}
		if a.LaunchTokenID != "" {
			if err := client.RevokeIntegrationToken(ctx, opts.ClientID, a.LaunchTokenID); err == nil {
				rr.LaunchTokenID = ""
			} else {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("farm run: revoke launch token %s: %w", a.LaunchTokenID, err))
			}
		}
	}
	return rr, false, false, errors.Join(cleanupErrs...)
}

// recordBlocked is D10's single call site for a creation-phase run
// failure: prints "<runId> <scenario> error: <wrapped error>" to stderr at
// the moment the failure happens, and returns the RunResult (result
// "blocked", Error carrying err's message) for RunBatch to fold into its
// results/summary — never a bare `continue` that reports nothing.
func recordBlocked(runID, scenario, productionName string, err error) RunResult {
	fmt.Fprintf(os.Stderr, "%s %s error: %v\n", runID, scenario, err)
	rr := RunResult{RunID: runID, Scenario: scenario, Result: ResultBlocked, Error: err.Error()}
	rr.ProductionProjectName = productionName
	return rr
}

// isScopedMintForbidden reports whether err is the typed platform error
// MintProjectScopedToken returns when the minting credential is an
// integration token without delegation (platform.ErrDelegationUnavailable —
// the same code MintDelegatedLaunchToken uses for the identical platform
// restriction). Brief: "on a 403 with either code the controller ... exits
// nonzero BEFORE creating any project" — the cmd/zcp layer turns this into
// the one-line fix message.
func isScopedMintForbidden(err error) bool {
	var pe *platform.PlatformError
	return errors.As(err, &pe) && pe.Code == platform.ErrDelegationUnavailable
}

// findProjectByName looks up a project by exact name in the account's live
// project list (§3.3, launch scenario's -prod target).
func findProjectByName(ctx context.Context, client PlatformClient, clientID, name string) (id string, ok bool, err error) {
	projects, err := client.ListProjects(ctx, clientID)
	if err != nil {
		return "", false, err
	}
	for _, p := range projects {
		if p.Name == name {
			return p.ID, true, nil
		}
	}
	return "", false, nil
}

// doneJSON is the wrapper's runs/<runId>/done.json (§1.2 FM-4).
type doneJSON struct {
	RunID      string `json:"runId"`
	ScenarioID string `json:"scenarioId"`
	Parts      map[string]struct {
		TreeDigest string `json:"treeDigest"`
	} `json:"parts"`
	EvaluatorSha256 string `json:"evaluatorSha256"`
	CandidateSha256 string `json:"candidateSha256"`
}

// resultMeta is the subset of a run's meta.json this package reads: the
// aggregated task result docs/spec-testing-architecture.md §10.1/§10.2
// already computes and persists (`task {mode, result, frozenAt}`) — the
// controller never re-derives a verdict from raw evidence itself, it reads
// the one the single-run report contract already owns. The evaluator does
// not write this at a fixed runs/<runId>/results/meta.json: the wrapper
// passes --results-dir $RUNDIR/results and the evaluator nests it as
// results/<suite>/<scenario>/meta.json (D15), so readResultMeta locates it
// by listing rather than by a fixed path. Distinct from coverage.go's own
// runMeta (which reads only scenarioId) — two different narrow readers of
// the same meta.json file.
type resultMeta struct {
	Task struct {
		Result string `json:"result"`
	} `json:"task"`
}

// verificationJSON is the subset of verification.json
// (docs/spec-testing-architecture.md §10.1, format "zcp-eval-verification-2")
// this package reads: the row id and result of every executable check, used
// to name the blocking/failing checks behind a blocked/failed task.result
// (S22 finding 1).
type verificationJSON struct {
	Checks []struct {
		ID     string `json:"id"`
		Result string `json:"result"`
	} `json:"checks"`
}

// waitForDone polls the bucket for runs/<runId>/done.json until it appears
// or deadline (the run's own creation-time budget deadline, R7) elapses
// (§3.3 FM-21), and — D19 — also polls the run's project for a FAILED
// creation-phase process on every iteration where the run has not yet
// written runs/<runId>/started.json: a platform-side project.create
// incident (live 2026-09-10: stack.create FAILED, stack.build CANCELED,
// GET /project/{id} -> 500) leaves a dead project that will never write
// done.json, so waiting out the full budget for one is pure waste. R1: once
// started.json exists — the run's own wrapper/agent is underway — this
// process poll stops entirely, so a platform-side failure of the agent's
// OWN later zerops_import (a stack.import for one of ITS services, not the
// controller's) can never be mistaken for the project's own creation
// failing; creationPhaseFailure additionally only counts a process whose
// ServiceStacks[] names the control service or carries no ref at all, for
// the same reason. settled reports whether the run produced a verdict at
// all — false means either the budget-elapsed exemption or R3's interrupt
// exemption (ctx cancelled — detail is DetailInterrupted): either way the
// caller must not delete the run's project or revoke its token. A FAILED
// creation-phase process always returns settled=true (§3.3 FM-21: the dead
// project is still deleted); a transient error reading processes is never
// itself a verdict — polling continues.
func waitForDone(ctx context.Context, client PlatformClient, sink *SinkClient, runID, scenarioID, candidateSHA256, evaluatorSHA256, projectID string, deadline time.Time, now func() time.Time, pollInterval time.Duration) (result, detail string, settled bool) {
	for {
		select {
		case <-ctx.Done():
			return ResultBlocked, DetailInterrupted, false
		default:
		}

		body, err := sink.Get(ctx, "runs/"+runID+"/done.json")
		if err == nil {
			return settleFromDoneForIdentity(ctx, sink, runID, scenarioID, candidateSHA256, evaluatorSHA256, body)
		}

		started, _, headErr := sink.Head(ctx, "runs/"+runID+"/started.json")
		if headErr == nil && !started {
			if actionName, failReason, found := creationPhaseFailure(ctx, client, projectID); found {
				return ResultBlocked, fmt.Sprintf("platform: %s FAILED: %s", actionName, failReason), true
			}
		}

		if now().After(deadline) {
			return ResultBlocked, DetailNoBundle, false
		}

		select {
		case <-ctx.Done():
			return ResultBlocked, DetailInterrupted, false
		case <-time.After(pollInterval):
		}
	}
}

// creationPhaseActionPrefixes names the platform actionName values whose
// FAILED status means the run's project itself is unusable (D19) — derived
// from internal/ops/events.go's actionNameMap, the only place in this
// codebase these literals are pinned against a live-verified API response
// ("API returns stack.* format (verified 2026-03-23 against live Zerops
// API)"). "project.create" itself never appears as a literal anywhere in
// this codebase; the live 2026-09-10 incident this slice fixes for
// (POST /client/{id}/project/import -> internalServerError) surfaced as a
// FAILED stack.create process, which this prefix already covers.
var creationPhaseActionPrefixes = []string{"stack.create", "stack.import"}

// isCreationPhaseAction reports whether actionName is one of
// creationPhaseActionPrefixes.
func isCreationPhaseAction(actionName string) bool {
	for _, prefix := range creationPhaseActionPrefixes {
		if strings.HasPrefix(actionName, prefix) {
			return true
		}
	}
	return false
}

// creationPhaseFailure looks for a FAILED creation-phase process on
// projectID whose ServiceStacks[] names the control service or carries no
// service ref at all (R1 — referencesControlServiceOrProject). A transient
// error reading the project's processes (including the GET /project/{id}
// 5xx the live incident also produced) is never itself a verdict — it
// reports found=false, not an error, so waitForDone keeps polling instead
// of settling on a possibly-recoverable blip.
func creationPhaseFailure(ctx context.Context, client PlatformClient, projectID string) (actionName, failReason string, found bool) {
	processes, err := client.GetProjectProcessesDirect(ctx, projectID)
	if err != nil {
		return "", "", false
	}
	for _, p := range processes {
		if p.Status != platform.ProcessStatusFailed || !isCreationPhaseAction(p.ActionName) || !referencesControlServiceOrProject(p.ServiceStacks) {
			continue
		}
		reason := ""
		if p.FailReason != nil {
			reason = *p.FailReason
		}
		return p.ActionName, reason, true
	}
	return "", "", false
}

// referencesControlServiceOrProject reports whether refs is empty (a
// project-level process with no specific service — e.g. the project shell's
// own stack.create) or names the control service (serviceHostname, "zcp") —
// R1: a stack.import the run's own agent triggers for one of ITS imported
// services, mid-run, has a ref naming THAT service, never "zcp", so it never
// counts as the platform failing to create the run's own project.
func referencesControlServiceOrProject(refs []platform.ServiceStackRef) bool {
	if len(refs) == 0 {
		return true
	}
	for _, ref := range refs {
		if ref.Name == serviceHostname {
			return true
		}
	}
	return false
}

func settleFromDoneForIdentity(ctx context.Context, sink *SinkClient, runID, scenarioID, candidateSHA256, evaluatorSHA256 string, body []byte) (result, detail string, settled bool) {
	var done doneJSON
	if err := json.Unmarshal(body, &done); err != nil {
		return ResultBlocked, "done.json: parse: " + err.Error(), true
	}
	if done.RunID != runID || (scenarioID != "" && done.ScenarioID != scenarioID) || (candidateSHA256 != "" && done.CandidateSha256 != candidateSHA256) || (evaluatorSHA256 != "" && done.EvaluatorSha256 != evaluatorSHA256) {
		return ResultBlocked, fmt.Sprintf("done.json: identity mismatch (run=%q scenario=%q candidate=%q evaluator=%q)", done.RunID, done.ScenarioID, done.CandidateSha256, done.EvaluatorSha256), true
	}
	for part, claim := range done.Parts {
		recomputed, err := recomputePartDigest(ctx, sink, runID, part)
		if err != nil {
			return ResultBlocked, fmt.Sprintf("%s: recompute digest: %v", part, err), true
		}
		if recomputed != claim.TreeDigest {
			return ResultBlocked, fmt.Sprintf("%s: digest mismatch (done.json claims %s, bucket has %s)", part, claim.TreeDigest, recomputed), true
		}
	}
	meta, verificationKey, err := readResultMeta(ctx, sink, runID)
	if err != nil {
		return ResultBlocked, "results/meta.json: " + err.Error(), true
	}
	taskResult := meta.Task.Result
	if taskResult == ResultBlocked || taskResult == ResultFailed {
		detail, err = blockingCheckDetail(ctx, sink, verificationKey, taskResult)
		if err != nil {
			return ResultBlocked, "verification.json: " + err.Error(), true
		}
	}
	return taskResult, detail, true
}

// blockingCheckDetail names the checks behind a blocked or failed
// taskResult (S22 finding 1): it reads verification.json at
// verificationKey — the sibling of meta.json readResultMeta already located
// — and joins the sorted ids of every row whose result equals taskResult,
// capped at 5 with a "+N more" suffix beyond that. verificationKey == ""
// means readResultMeta's listing found no verification.json sibling, which
// is not itself an error: a bundle can legitimately be missing it, so the
// detail says so instead.
func blockingCheckDetail(ctx context.Context, sink *SinkClient, verificationKey, taskResult string) (string, error) {
	if verificationKey == "" {
		return "no verification.json in bundle", nil
	}
	body, err := sink.Get(ctx, verificationKey)
	if err != nil {
		return "", err
	}
	var v verificationJSON
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	var ids []string
	for _, row := range v.Checks {
		if row.Result == taskResult {
			ids = append(ids, row.ID)
		}
	}
	sort.Strings(ids)
	const maxNamed = 5
	if len(ids) > maxNamed {
		return fmt.Sprintf("%s +%d more", strings.Join(ids[:maxNamed], ", "), len(ids)-maxNamed), nil
	}
	return strings.Join(ids, ", "), nil
}

// recomputePartDigest downloads every object under runs/<runId>/<part>/ to a
// temp dir and returns farm.TreeDigest over it — the independent recompute
// FM-5 requires, never inferred from done.json's own claim or from listing
// order. R4b: a bucket key's relative path is untrusted input (the bucket is
// shared read/write across every run in this account, FM-8) — a key like
// runs/<runId>/results/../../x would otherwise join outside dir, so any key
// whose cleaned relative path is absolute or escapes upward is rejected
// before either the download or the write.
func recomputePartDigest(ctx context.Context, sink *SinkClient, runID, part string) (string, error) {
	prefix := "runs/" + runID + "/" + part + "/"
	keys, err := sink.List(ctx, prefix)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "farm-verify-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for _, key := range keys {
		rel := strings.TrimPrefix(key, prefix)
		cleaned := filepath.Clean(filepath.FromSlash(rel))
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("recompute digest: key %q resolves outside %s", key, prefix)
		}
		objBody, err := sink.Get(ctx, key)
		if err != nil {
			return "", err
		}
		dest := filepath.Join(dir, cleaned)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dest, objBody, 0o600); err != nil {
			return "", err
		}
	}
	return TreeDigest(dir)
}

// readResultMeta locates runID's meta.json by listing
// runs/<runId>/results/ (D15: the evaluator nests it as
// results/<suite>/<scenario>/meta.json, not at a fixed path) and requiring
// exactly one key ending in "/meta.json" — zero or more than one is an
// error naming the count, never a silent pick of the first match. It also
// reuses that same listing (never a second List call) to report whether a
// verification.json sibling — same directory as the chosen meta.json key —
// exists (S22 finding 1); verificationKey is "" when it does not.
func readResultMeta(ctx context.Context, sink *SinkClient, runID string) (meta resultMeta, verificationKey string, err error) {
	prefix := "runs/" + runID + "/results/"
	keys, err := sink.List(ctx, prefix)
	if err != nil {
		return resultMeta{}, "", err
	}
	var metaKeys []string
	for _, key := range keys {
		if strings.HasSuffix(key, "/meta.json") {
			metaKeys = append(metaKeys, key)
		}
	}
	if len(metaKeys) != 1 {
		return resultMeta{}, "", fmt.Errorf("found %d meta.json under %s, want exactly 1", len(metaKeys), prefix)
	}
	body, err := sink.Get(ctx, metaKeys[0])
	if err != nil {
		return resultMeta{}, "", err
	}
	var m resultMeta
	if err := json.Unmarshal(body, &m); err != nil {
		return resultMeta{}, "", err
	}

	wantVerificationKey := strings.TrimSuffix(metaKeys[0], "meta.json") + "verification.json"
	for _, key := range keys {
		if key == wantVerificationKey {
			verificationKey = key
			break
		}
	}
	return m, verificationKey, nil
}

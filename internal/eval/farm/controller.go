package farm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// prodSuffix names a launch scenario's production target project, appended
// to the run project's own name (§3.2 FM-19, §3.4 FM-23).
const prodSuffix = "-prod"

// Per-run acceptance verdicts (§5.1 vocabulary; "not-run" is a row-level
// status this slice never assigns at the whole-run level — §5.1: "a run
// whose project was never created is not reported at all").
const (
	ResultPassed  = "passed"
	ResultFailed  = "failed"
	ResultBlocked = "blocked"
)

// DetailNoBundle is the RunResult.Detail / GCCandidate.Exempt value for a
// run whose budget elapsed without ever producing runs/<runId>/done.json
// (FM-3, FM-21) — shared by RunBatch's waitForDone and gc.go's GC so the
// two agree on the same literal (goconst).
const DetailNoBundle = "no bundle"

// PlatformClient is the account-wide platform surface the controller needs.
// The real implementation (NewAccountClient) wraps platform.NewZeropsClient
// and platform.NewProjectAdminClient — both existing SDK-based constructors
// (§3 FM-15: "never a new HTTP client") — built once from
// ZCP_FARM_ACCOUNT_TOKEN.
type PlatformClient interface {
	ListProjects(ctx context.Context, clientID string) ([]platform.Project, error)
	CreateAndImportProject(ctx context.Context, yaml string) (*platform.ImportResult, error)
	GetProject(ctx context.Context, projectID string) (*platform.Project, error)
	DeleteProject(ctx context.Context, projectID string) (*platform.Process, error)
	MintDelegatedLaunchToken(ctx context.Context, name string) (platform.MintedToken, error)
	RevokeIntegrationToken(ctx context.Context, clientID, tokenID string) error
}

// accountClient adapts platform.ProjectAdminClient (CreateAndImportProject/
// GetProject/DeleteProject) and *platform.ZeropsClient (ListProjects/
// MintDelegatedLaunchToken/RevokeIntegrationToken) — both constructed from
// the same account-wide token — to PlatformClient.
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

func (a *accountClient) GetProject(ctx context.Context, projectID string) (*platform.Project, error) {
	return a.admin.GetProject(ctx, projectID)
}

func (a *accountClient) DeleteProject(ctx context.Context, projectID string) (*platform.Process, error) {
	return a.admin.DeleteProject(ctx, projectID)
}

func (a *accountClient) MintDelegatedLaunchToken(ctx context.Context, name string) (platform.MintedToken, error) {
	return a.z.MintDelegatedLaunchToken(ctx, name)
}

func (a *accountClient) RevokeIntegrationToken(ctx context.Context, clientID, tokenID string) error {
	return a.z.RevokeIntegrationToken(ctx, clientID, tokenID)
}

// NewAccountClient constructs the controller's platform client from the
// farm host's account-wide token (ZCP_FARM_ACCOUNT_TOKEN, §2.4 FM-15). The
// returned closer must be called once the controller is done with it — it
// zeros the admin client's held reference.
func NewAccountClient(accountToken, apiHost string) (PlatformClient, func(), error) {
	z, err := platform.NewZeropsClient(accountToken, apiHost)
	if err != nil {
		return nil, nil, fmt.Errorf("farm: construct account client: %w", err)
	}
	admin, err := platform.NewProjectAdminClient(accountToken, apiHost)
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
	// settles, also deletes its zcp-farm-<runId>-prod target and revokes
	// the token (§3.4 FM-23).
	Launch bool
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
	ScenariosDigest string
	Scenarios       []ScenarioRun
	CredentialMode  CredentialMode
	Credential      string
	Sink            Sink
	RunBudget       time.Duration
	// PollInterval is how often RunBatch re-checks the bucket for
	// done.json while waiting; zero defaults to 2s.
	PollInterval time.Duration
	// Now defaults to time.Now; tests inject a fixed/advancing clock.
	Now func() time.Time
}

// RunResult is one run's outcome — the CLI prints
// "<runId> <scenario> passed|failed|blocked|not-run" from these.
type RunResult struct {
	RunID         string
	Scenario      string
	ProjectID     string // empty once the run's project has been deleted
	Result        string
	Detail        string
	LaunchTokenID string // empty once revoked
}

// RunBatch runs opts.Scenarios as one batch (§3.3 FM-21/FM-22): it writes
// batches/<batch>/manifest.json before creating any project, creates one
// project per scenario, watches the bucket for each run's done.json (or its
// budget elapsing), verifies the parts digests FM-5 requires, deletes each
// settled run's project(s) and — for launch scenarios — revokes the launch
// token, then writes batches/<batch>/summary.json.
func RunBatch(ctx context.Context, client PlatformClient, sink *SinkClient, opts RunOptions) ([]RunResult, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	pollInterval := opts.PollInterval
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}
	startedAt := now().UTC().Format(time.RFC3339)

	type scheduledRun struct {
		ScenarioRun
		RunID string
	}
	scheduled := make([]scheduledRun, 0, len(opts.Scenarios))
	manifestRuns := make([]ManifestRun, 0, len(opts.Scenarios))
	for _, sc := range opts.Scenarios {
		runID := opts.Batch + "-" + sc.ID
		scheduled = append(scheduled, scheduledRun{ScenarioRun: sc, RunID: runID})
		manifestRuns = append(manifestRuns, ManifestRun{
			RunID: runID, Scenario: sc.ID, ProjectName: ProjectPrefix + runID,
		})
	}

	manifest := BatchManifest{
		Batch:           opts.Batch,
		CreatedAt:       startedAt,
		StartedAt:       startedAt,
		Set:             opts.Set,
		CandidateSha256: opts.CandidateSHA256,
		EvaluatorSha256: opts.EvaluatorSHA256,
		ScenariosDigest: opts.ScenariosDigest,
		CredentialMode:  string(opts.CredentialMode),
		Runs:            manifestRuns,
	}
	if err := PutManifest(ctx, sink, opts.Batch, manifest); err != nil {
		return nil, fmt.Errorf("farm run: write manifest: %w", err)
	}

	type activeRun struct {
		scheduledRun
		ProjectID     string
		LaunchTokenID string
	}
	var actives []activeRun
	for _, r := range scheduled {
		var launchTokenID, launchKeyValue string
		if r.Launch {
			minted, err := client.MintDelegatedLaunchToken(ctx, "farm-"+r.RunID)
			if err != nil {
				// No project was created for this run — §5.1: "a run whose
				// project was never created is not reported at all."
				continue
			}
			launchTokenID = minted.TokenID
			launchKeyValue = minted.Token
		}

		desc := RunDescriptor{
			BatchID:         opts.Batch,
			RunID:           r.RunID,
			ScenarioID:      r.ID,
			EvaluatorSHA256: opts.EvaluatorSHA256,
			CandidateSHA256: opts.CandidateSHA256,
			ScenariosDigest: opts.ScenariosDigest,
			Sink:            opts.Sink,
			Credentials:     []Credential{{Mode: opts.CredentialMode, Value: opts.Credential}},
			LaunchKey:       launchKeyValue,
		}
		yamlBody, err := ImportYAML(desc)
		if err != nil {
			continue
		}
		result, err := client.CreateAndImportProject(ctx, string(yamlBody))
		if err != nil {
			continue
		}
		actives = append(actives, activeRun{scheduledRun: r, ProjectID: result.ProjectID, LaunchTokenID: launchTokenID})
	}

	results := make([]RunResult, 0, len(actives))
	endedByBudget := false
	for _, a := range actives {
		result, detail, settled := waitForDone(ctx, sink, a.RunID, opts.RunBudget, now, pollInterval)
		rr := RunResult{RunID: a.RunID, Scenario: a.ID, ProjectID: a.ProjectID, Result: result, Detail: detail, LaunchTokenID: a.LaunchTokenID}

		if !settled {
			// FM-21's sole exemption: budget elapsed without done.json —
			// the project is kept for inspection, the token (if any) stays
			// unrevoked and is recorded so `gc` can finish the job later.
			endedByBudget = true
			results = append(results, rr)
			continue
		}

		runProjectName := ProjectPrefix + a.RunID
		if err := Guard(ctx, client, a.ProjectID, runProjectName); err == nil {
			rr.ProjectID = ""
		}
		if a.Launch {
			if prodID, ok := findProjectByName(ctx, client, opts.ClientID, runProjectName+prodSuffix); ok {
				_ = Guard(ctx, client, prodID, runProjectName+prodSuffix)
			}
			if a.LaunchTokenID != "" {
				if err := client.RevokeIntegrationToken(ctx, opts.ClientID, a.LaunchTokenID); err == nil {
					rr.LaunchTokenID = ""
				}
			}
		}
		results = append(results, rr)
	}

	endedBy := "settled"
	if endedByBudget {
		endedBy = "budget"
	}
	summary := BatchSummary{Batch: opts.Batch, FinishedAt: now().UTC().Format(time.RFC3339), EndedBy: endedBy}
	for _, rr := range results {
		// RunResult and SummaryRun share the same field names/types/order
		// (only the json tags differ) — a direct conversion instead of a
		// field-by-field literal.
		summary.Runs = append(summary.Runs, SummaryRun(rr))
	}
	if err := PutSummary(ctx, sink, opts.Batch, summary); err != nil {
		return results, fmt.Errorf("farm run: write summary: %w", err)
	}
	return results, nil
}

// findProjectByName looks up a project by exact name in the account's live
// project list (§3.3, launch scenario's -prod target).
func findProjectByName(ctx context.Context, client PlatformClient, clientID, name string) (id string, ok bool) {
	projects, err := client.ListProjects(ctx, clientID)
	if err != nil {
		return "", false
	}
	for _, p := range projects {
		if p.Name == name {
			return p.ID, true
		}
	}
	return "", false
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

// resultMeta is the subset of runs/<runId>/results/meta.json this package
// reads: the aggregated task result docs/spec-testing-architecture.md
// §10.1/§10.2 already computes and persists (`task {mode, result,
// frozenAt}`) — the controller never re-derives a verdict from raw evidence
// itself, it reads the one the single-run report contract already owns.
// Distinct from coverage.go's own runMeta (which reads only scenarioId) —
// two different narrow readers of the same results/meta.json file.
type resultMeta struct {
	Task struct {
		Result string `json:"result"`
	} `json:"task"`
}

// waitForDone polls the bucket for runs/<runId>/done.json until it appears
// or budget elapses (§3.3 FM-21). settled reports whether the run produced
// a bundle at all — false means the budget-elapsed exemption: the caller
// must not delete the run's project or revoke its token.
func waitForDone(ctx context.Context, sink *SinkClient, runID string, budget time.Duration, now func() time.Time, pollInterval time.Duration) (result, detail string, settled bool) {
	deadline := now().Add(budget)
	for {
		body, err := sink.Get(ctx, "runs/"+runID+"/done.json")
		if err == nil {
			return settleFromDone(ctx, sink, runID, body)
		}
		if now().After(deadline) {
			return ResultBlocked, DetailNoBundle, false
		}
		time.Sleep(pollInterval)
	}
}

// settleFromDone verifies done.json's part digests against what actually
// landed in the bucket (FM-5: write order, and object listing order, are
// never trusted) and, only if every part matches, reads the run's own
// aggregated task result. A run that completed — done.json exists — always
// reports settled=true, even when a part's digest mismatches: FM-5 makes
// that verdict `blocked`, not `failed`, but the run itself DID complete, so
// its project is still deleted by the caller.
func settleFromDone(ctx context.Context, sink *SinkClient, runID string, body []byte) (result, detail string, settled bool) {
	var done doneJSON
	if err := json.Unmarshal(body, &done); err != nil {
		return ResultBlocked, "done.json: parse: " + err.Error(), true
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
	meta, err := readResultMeta(ctx, sink, runID)
	if err != nil {
		return ResultBlocked, "results/meta.json: " + err.Error(), true
	}
	return meta.Task.Result, "", true
}

// recomputePartDigest downloads every object under runs/<runId>/<part>/ to a
// temp dir and returns farm.TreeDigest over it — the independent recompute
// FM-5 requires, never inferred from done.json's own claim or from listing
// order.
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
		objBody, err := sink.Get(ctx, key)
		if err != nil {
			return "", err
		}
		rel := strings.TrimPrefix(key, prefix)
		dest := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dest, objBody, 0o600); err != nil {
			return "", err
		}
	}
	return TreeDigest(dir)
}

// readResultMeta reads runs/<runId>/results/meta.json.
func readResultMeta(ctx context.Context, sink *SinkClient, runID string) (resultMeta, error) {
	body, err := sink.Get(ctx, "runs/"+runID+"/results/meta.json")
	if err != nil {
		return resultMeta{}, err
	}
	var m resultMeta
	if err := json.Unmarshal(body, &m); err != nil {
		return resultMeta{}, err
	}
	return m, nil
}

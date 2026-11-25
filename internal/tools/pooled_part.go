package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A part planned in a Mate is born pooled by its plan (docs/spec-mate.md
// §2.8b B-9): the plan's discover call ensures the repository all its parts
// share in HQ, seeds its `main` from the one pool template when it is HQ's
// empty first commit (B-10) or checks the parts' setups on it, imports each part's dev
// pool and stage the project lacks, and then runs the stand-up's own pooled
// build (standFrom) from the plan's parts in place of the recipe's tier —
// under the stand-up's one lock and status, with its rows and verdict. Every
// step is skipped where its fact holds, so the same plan again continues
// where a call stopped; a failure before the import leaves the project
// untouched. The engine then gets the parts as existing targets, the shape
// the adopt route hands it: their pool and stage exist and their meta is
// recorded complete.

// partRepositories is where a pooled part's repository is ensured and its
// `main` seeded: the Mate's HQ (hq.Client), under the Mate's own identity.
type partRepositories interface {
	EnsureRepo(ctx context.Context, name string) (hq.Repo, error)
	OpenScratch(ctx context.Context, appID, repo string) (*hq.Scratch, error)
	OpenChange(ctx context.Context, repo, title, branch string) (hq.OpenedChange, error)
	SeedChange(ctx context.Context, repo string, number int) (hq.Change, error)
	CloseChange(ctx context.Context, repo string, number int) (hq.Change, error)
	ReadMateState() (hq.MateState, error)
}

// pooledBirth is one call's way to create a Mate's plan's parts: the server's
// one stand-up and the call's progress, on the stand-up's status section.
type pooledBirth struct {
	d        standupDeps
	progress *standupProgress
}

// birth is the call's pooledBirth; nil without a stand-up (outside a Mate).
func (s *Standup) birth(ctx context.Context, req *mcp.CallToolRequest) *pooledBirth {
	if s == nil {
		return nil
	}
	progress := newStandupProgress(buildProgressCallback(ctx, req))
	progress.status = s.deps.status
	return &pooledBirth{d: s.deps, progress: progress}
}

// pooledPlanPart is one part of a Mate's plan.
type pooledPlanPart struct {
	part, pool, stage, typ string
}

// poolPart is the part as the pool template reads it.
func (p pooledPlanPart) poolPart() ops.PoolPart {
	return ops.PoolPart{Part: p.part, Pool: p.pool, Stage: p.stage, Type: p.typ, MaxContainers: bundle.DefaultPoolMaxContainers}
}

// tierPair is the part as the stand-up's tier names it: the pool pair keyed
// by the pool, its setup the pool's hostname, the stage's the part's name.
func (p pooledPlanPart) tierPair(repository, repositoryURL string) workflow.MateTierPair {
	return workflow.MateTierPair{
		Repository: repositoryURL, RepoName: repository, DevPool: true,
		Dev:    workflow.MateTierRuntime{Hostname: p.pool, Type: bundle.PoolType, Setup: p.pool},
		Stages: []workflow.MateTierRuntime{{Hostname: p.stage, Type: p.typ, Setup: p.part}},
	}
}

// pooledPlanResponse is the plan's discover answer once its parts stand: the
// bootstrap's next step and each pool's and stage's verdict row.
type pooledPlanResponse struct {
	bootstrapResponse
	Parts []pooledPlanRow `json:"parts"`
}

// pooledPlanRow is one pool or stage of the plan's parts after its birth: its
// verdict row as the stand-up derived it, without the stand-up's next step
// (zerops_standup reads a recipe, which a plan's parts are not stood up from).
type pooledPlanRow struct {
	Hostname   string               `json:"hostname"`
	Role       string               `json:"role"`
	Repository string               `json:"repository,omitempty"`
	Setup      string               `json:"setup,omitempty"`
	State      mate.StandupRowState `json:"state"`
	Commit     string               `json:"commit,omitempty"`
	Class      platform.ErrorClass  `json:"class,omitempty"`
	Cause      string               `json:"cause,omitempty"`
}

// pooledPlanRows are the verdict rows of the stand-up's report.
func pooledPlanRows(stood standupResponse) []pooledPlanRow {
	rows := make([]pooledPlanRow, 0, len(stood.Services))
	for _, s := range stood.Services {
		if s.Verdict == nil {
			continue
		}
		rows = append(rows, pooledPlanRow{
			Hostname: s.Hostname, Role: s.Role, Repository: s.Repository, Setup: s.Setup,
			State: s.Verdict.State, Commit: s.Verdict.Commit, Class: s.Verdict.Class, Cause: s.Verdict.Cause,
		})
	}
	return rows
}

// handleMatePooledPlan is a Mate's classic discover with a plan: the pooled
// rule, the preflight and the plan's validation before any side effect, the
// parts' birth, and the discover step completed with the parts as existing
// targets once the verdict is done or ready. A verdict that failed completes
// nothing and answers the failed rows.
func handleMatePooledPlan(ctx context.Context, engine *workflow.Engine, client platform.Client, schemas *schema.Schemas, schemaCache *schema.Cache, input WorkflowInput, projectID string, sshDeployer ops.SSHDeployer, rt runtime.Info, birth *pooledBirth) *mcp.CallToolResult {
	if pe := matePooledPlanError(input.Plan, rt.MateEnabled); pe != nil {
		return convertError(pe, WithRecoveryStatus())
	}
	if pe := explicitPlanPreflight(ctx, client, projectID, input.Plan, sshDeployer, rt); pe != nil {
		return convertError(pe, WithRecoveryStatus())
	}
	if _, err := workflow.ValidateBootstrapTargets(input.Plan, schemas, nil); err != nil {
		pe := platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Plan validation failed: %v", err),
			"Provide valid plan: [{runtime: {devHostname: \"<part>pool\", stageHostname: \"<part>stage\", type, bootstrapMode: \"standard\"}, dependencies: [{hostname, type, resolution}]}]. Hostnames: lowercase a-z0-9, max 25 chars.")
		pe.Subcode = bootstrapPlanSubcode(err)
		return convertError(pe, WithRecoveryStatus())
	}
	if state, err := engine.GetState(); err != nil || state.Bootstrap == nil || !state.Bootstrap.Active || state.Bootstrap.CurrentStepName() != workflow.StepDiscover {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			"The plan is completed at the bootstrap's discover step, and this session is not at it.",
			"Start bootstrap with action=\"start\" workflow=\"bootstrap\" route=\"classic\", then send the plan at step=\"discover\"."), WithRecoveryStatus())
	}
	parts := make([]pooledPlanPart, 0, len(input.Plan))
	for _, target := range input.Plan {
		pool, stage, part, _ := matePooledPart(target.Runtime)
		parts = append(parts, pooledPlanPart{part: part, pool: pool, stage: stage, typ: target.Runtime.Type})
	}
	repository := strings.TrimSpace(input.Repository)
	if repository == "" && len(parts) == 1 {
		repository = parts[0].part
	}
	if repository == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Plan validation failed: the plan has several parts and names no repository: every part of a plan shares one HQ repository.",
			"Re-submit the same plan with repository=\"<name>\", the HQ repository its parts share; parts of different repositories are planned in separate bootstraps."), WithRecoveryStatus())
	}
	if birth == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"This zcp has no stand-up to create the plan's parts with: it runs without an SSH deployer.",
			"Nothing was touched. Run the bootstrap from the Mate's zcp."), WithRecoveryStatus())
	}
	defer birth.progress.quiesce(ctx)

	stood, err := birth.ensurePooledParts(ctx, parts, repository, schemas)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	if headline := mate.StandupHeadline(stood.StandUp); headline != mate.HeadlineDone && headline != mate.HeadlineReady {
		return convertError(pooledPlanFailed(stood), WithRecoveryStatus())
	}

	targets := slices.Clone(input.Plan)
	for i := range targets {
		targets[i].Runtime.IsExisting = true
	}
	resp, err := engine.BootstrapCompletePlan(targets, schemas, nil)
	if err != nil {
		pe := platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Plan validation failed after the parts were created: %v", err),
			"The parts stand; send the same plan again: it continues from the running versions.")
		pe.Subcode = bootstrapPlanSubcode(err)
		return convertError(pe, WithRecoveryStatus())
	}
	if needsStacks(resp) {
		populateStacks(ctx, resp, schemaCache)
	}
	rows := pooledPlanRows(stood)
	resp.Message = strings.TrimSpace(pooledPlanStoodLine(mate.StandupHeadline(stood.StandUp), rows, parts) + " " + resp.Message)
	return jsonResult(pooledPlanResponse{
		bootstrapResponse: bootstrapResponse{BootstrapResponse: resp, Envelope: freshEnvelope(ctx, engine.StateDir(), client, projectID, rt)},
		Parts:             rows,
	})
}

// pooledPlanStoodLine is what the plan's answer says of its parts once they
// stand: the verdict and each row, what a stage that is not built means, and
// the lease that works in a part.
func pooledPlanStoodLine(headline mate.StandupHeadline, rows []pooledPlanRow, parts []pooledPlanPart) string {
	said := make([]string, 0, len(rows))
	for _, r := range rows {
		row := fmt.Sprintf("%s %s", r.Hostname, r.State)
		if r.Commit != "" {
			row += " at " + shortCommit(r.Commit)
		}
		if r.Cause != "" {
			row += " (" + r.Cause + ")"
		}
		said = append(said, row)
	}
	pools := make([]string, 0, len(parts))
	for _, p := range parts {
		pools = append(pools, fmt.Sprintf("%q", p.pool))
	}
	line := fmt.Sprintf("The plan's parts stand, verdict %s: %s.", headline, strings.Join(said, ", "))
	if headline == mate.HeadlineReady {
		line += " A stage that is not built has its row's cause; until it runs its part has no preview, so never tell the person it is up."
	}
	return line + fmt.Sprintf(" Work in a part through a lease of its pool, a branch per lease: zerops_lease action=\"acquire\" pools=[%s] branch=\"<your branch>\".", strings.Join(pools, ", "))
}

func standupBuildIDs(row *mate.StandupRow) string {
	var ids string
	if row.ProcessID != "" {
		ids += ", process " + row.ProcessID
	}
	if row.AppVersionID != "" {
		ids += ", app version " + row.AppVersionID
	}
	return ids
}

// pooledPlanFailed is the plan's answer when its parts did not stand: each
// failed row with its cause and next step; nothing was completed.
func pooledPlanFailed(stood standupResponse) *platform.PlatformError {
	var rows []string
	for _, s := range stood.Services {
		if s.Verdict == nil || s.Verdict.State == mate.RowDone {
			continue
		}
		row := standupRowWords(s.Verdict)
		// A row's own next step names zerops_standup, which reads a recipe
		// a plan's parts are not stood up from: the way on is this plan.
		if next := s.Verdict.Next; next != "" && !strings.Contains(next, "zerops_standup") {
			row += " — " + next
		}
		rows = append(rows, row)
	}
	return platform.NewPlatformError(platform.ErrDeployFailed,
		fmt.Sprintf("The plan's parts did not stand (verdict %s): %s.", stood.StandUp, strings.Join(rows, "; ")),
		"Nothing of the bootstrap was completed. Fix what each row names, then send the same plan again: it continues from here, keeps what was made and builds only what has no build.")
}

// ensurePooledParts is the parts' whole birth, idempotent: the repository
// (1), its seed or the check of its setups (2), the import of what the
// project lacks (3), then the stand-up's pooled build from the plan's tier
// (4, 5) under the project's one stand-up. A run already holding it answers
// with its start and its verdict so far, and nothing is started.
func (b *pooledBirth) ensurePooledParts(ctx context.Context, parts []pooledPlanPart, repository string, schemas *schema.Schemas) (standupResponse, error) {
	d := b.d
	release, held, err := holdStandup(d.batch.stateDir)
	switch {
	case err != nil:
		return standupResponse{}, platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Could not take the project's stand-up: %v.", err), "Nothing was touched. Send the same plan again.")
	case !held:
		return standupResponse{}, platform.NewPlatformError(platform.ErrWorkflowActive,
			runningStandupLine(d.statusPath), "Nothing was started. Send the same plan again once that run has ended.")
	}
	defer release()

	hqc, err := hq.Open(d.batch.httpClient, d.identity())
	if err != nil {
		return standupResponse{}, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("HQ cannot be asked for this Mate: %v. The plan needs HQ for its parts' repository.", err),
			"Nothing was touched. Send the same plan again once the Mate's HQ identity is in zcp's environment.")
	}
	appID, err := b.ensurePartRepository(ctx, hqc, repository)
	if err != nil {
		return standupResponse{}, err
	}
	if err := b.seedOrCheckMain(ctx, hqc, appID, repository, parts, schemas); err != nil {
		return standupResponse{}, err
	}
	if err := b.importParts(ctx, parts); err != nil {
		return standupResponse{}, err
	}
	repositoryURL, err := hqc.RepoURL(appID, repository)
	if err != nil {
		return standupResponse{}, platform.NewPlatformError(platform.ErrAPIError,
			fmt.Sprintf("Could not name %s/%s in HQ: %v.", appID, repository, err), "Send the same plan again; it continues from here.")
	}
	src := standupSource{appID: appID}
	for _, p := range parts {
		src.tier.Pairs = append(src.tier.Pairs, p.tierPair(repository, topology.CanonicalRepoURL(repositoryURL)))
	}
	var stood standupResponse
	d.runHeld(ctx, b.progress, func(ctx context.Context, d standupDeps) (*mcp.CallToolResult, string) {
		stood = d.standFrom(ctx, hqc, src, b.progress)
		return &mcp.CallToolResult{}, stood.StandUp
	})
	return stood, nil
}

// pooledHQError is an HQ call of the birth that failed, before the import:
// its class and HQ's words, and the project untouched.
func pooledHQError(what string, err error) error {
	class := classOr(err)
	way := "Nothing was imported. Send the same plan again; it continues from here."
	if class == platform.ClassUnreachable {
		way = "Nothing was imported: HQ did not answer. Send the same plan again once it answers."
	}
	var refused *hq.RefusedError
	if errors.As(err, &refused) {
		way = "Nothing was imported. Change what HQ's refusal names, then send the same plan again."
	}
	return platform.NewPlatformError(platform.ErrAPIError, fmt.Sprintf("%s (%s): %v.", what, class, err), way)
}

// ensurePartRepository is step 1: the repository in the Mate's application,
// made if new with `main` one commit of the empty tree, and the application
// HQ answers it in (never the Mate server's file, B-3).
func (b *pooledBirth) ensurePartRepository(ctx context.Context, repos partRepositories, repository string) (string, error) {
	b.progress.say("ensuring the repository " + repository + " in HQ")
	var repo hq.Repo
	err := b.d.askHQ(ctx, b.progress, "the repository "+repository, func(ctx context.Context) (err error) {
		repo, err = repos.EnsureRepo(ctx, repository)
		return err
	})
	if err == nil && repo.AppID == "" {
		err = errors.New("HQ named no application")
	}
	if err != nil {
		return "", pooledHQError(fmt.Sprintf("HQ did not ensure the repository %q", repository), err)
	}
	return repo.AppID, nil
}

// seedOrCheckMain is step 2: HQ's empty first commit as `main` gets the
// seed, from the one pool template, as the repository's first change, which
// HQ's Core lands at zcp's ask (B-10; zcp never pushes `main`); a `main`
// holding code is written nothing — a seed change an earlier run left open
// there is closed unless main holds it — and is refused when it lacks a
// part's setups, naming each one and its file; a repository with no `main` is
// refused before any change is opened, for HQ lands a first change only onto
// its own empty first commit.
func (b *pooledBirth) seedOrCheckMain(ctx context.Context, repos partRepositories, appID, repository string, parts []pooledPlanPart, schemas *schema.Schemas) error {
	full := appID + "/" + repository
	scratch, err := repos.OpenScratch(ctx, appID, repository)
	if err != nil {
		return pooledHQError("Could not open "+full+" in HQ", err)
	}
	defer scratch.Close()
	head, state, err := b.readMain(ctx, scratch, full)
	if err != nil {
		return err
	}
	switch state {
	case ops.SeedMainWritten:
		if err := b.closeSeedsLeftOpen(ctx, repos, scratch, full, repository); err != nil {
			return err
		}
		return checkMainSetups(ctx, scratch, appID, repository, head, parts)
	case ops.SeedMainUnborn:
		return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("%s has no %s (refused): a person made it empty, and HQ lands a Mate's first change only onto the empty first commit of a repository HQ made, so zcp cannot seed it", full, hqBase),
			"Nothing was opened, written or imported. A person writes main's first commit, holding the parts' pool setups, or the plan names a repository the Mate makes (repository=\"<new name>\"); then send the plan again.")
	case ops.SeedMainEmpty:
	}

	poolParts := make([]ops.PoolPart, 0, len(parts))
	for _, p := range parts {
		poolParts = append(poolParts, p.poolPart())
	}
	if schemas == nil || schemas.ZeropsYml == nil || len(schemas.ZeropsYml.BuildBases) == 0 || len(schemas.ZeropsYml.RunBases) == 0 {
		schemas = schema.Embedded()
	}
	files, err := ops.SeedFiles(poolParts, schemas)
	if err != nil {
		var baseErr *ops.SeedBaseError
		if errors.As(err, &baseErr) {
			return platform.NewPlatformError(platform.ErrInvalidParameter, baseErr.Error(),
				fmt.Sprintf("Nothing was opened, written or imported. Choose a type whose bases Zerops lists, or have a person write main with the two setups %q and %q, then send the same plan again.", baseErr.Part+"pool", baseErr.Part))
		}
		return platform.NewPlatformError(platform.ErrAPIError, fmt.Sprintf("Could not write the seed: %v.", err), "Nothing was imported.")
	}
	// The seed's tree holds its files alone: written over the empty tree,
	// never over HQ's root commit, which the seed does not descend from.
	tree, err := scratch.TreeWith(ctx, "", files)
	if err != nil {
		return pooledHQError("Could not write the seed's tree", err)
	}
	b.progress.say(fmt.Sprintf("opening the seed as %s's first change", full))
	var opened hq.OpenedChange
	if err := b.d.askHQ(ctx, b.progress, "the seed's change", func(ctx context.Context) (err error) {
		opened, err = repos.OpenChange(ctx, repository, ops.SeedTitle, "")
		return err
	}); err != nil {
		return pooledHQError("HQ did not open the seed's change in "+full, err)
	}
	change := opened.Change
	err = pushSeed(ctx, scratch, opened, tree, files)
	if foreign := (*foreignSeedChangeError)(nil); errors.As(err, &foreign) {
		return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("%s's %s is empty, but HQ answered the seed with this Mate's open change #%d there (%q), which %s (refused): the seed is written only onto a change of its own", full, hqBase, change.Number, change.Title, foreign.why),
			fmt.Sprintf("Nothing was written to change #%d and nothing was imported. A person merges or closes change #%d, then send the same plan again.", change.Number, change.Number))
	}
	if err != nil {
		return pooledHQError(fmt.Sprintf("Could not put the seed on change #%d of %s", change.Number, full), err)
	}

	b.progress.say(fmt.Sprintf("asking HQ to land the seed, change #%d of %s", change.Number, full))
	_, seedErr := repos.SeedChange(ctx, repository, change.Number)
	if seedErr == nil {
		return nil
	}
	// A refused ask is not a failure by itself: whoever wrote main meanwhile
	// may have written the setups. main as it is now decides.
	refusal := pooledHQError(fmt.Sprintf("HQ did not take the seed, change #%d of %s", change.Number, full), seedErr)
	head, state, err = b.readMain(ctx, scratch, full)
	if err != nil || state != ops.SeedMainWritten {
		return refusal
	}
	if err := b.closeOrphanedSeed(ctx, repos, scratch, repository, change.Number, change.Branch); err != nil {
		return pooledHQError(fmt.Sprintf("%s's %s was written meanwhile, and the seed's change #%d could not be closed", full, hqBase, change.Number), err)
	}
	return checkMainSetups(ctx, scratch, appID, repository, head, parts)
}

// closeSeedsLeftOpen closes, on a `main` holding code, each seed change of
// the Mate an earlier run left open in repository (its ask refused or
// stopped while main was empty) that main does not hold: the Mate's changes
// as HQ lists them in its state are the one source of which are open.
func (b *pooledBirth) closeSeedsLeftOpen(ctx context.Context, repos partRepositories, scratch *hq.Scratch, full, repository string) error {
	state, err := repos.ReadMateState()
	if err != nil {
		return pooledHQError("Could not read this Mate's changes in "+full, err)
	}
	for _, c := range state.Changes {
		if c.Repo != repository || c.Title == nil || !ops.SeedLeftOpen(*c.Title, c.State) {
			continue
		}
		if err := b.closeOrphanedSeed(ctx, repos, scratch, repository, c.Number, c.Branch); err != nil {
			return pooledHQError(fmt.Sprintf("%s's %s holds code, and the seed's change #%d an earlier run left open could not be closed", full, hqBase, c.Number), err)
		}
	}
	return nil
}

// closeOrphanedSeed closes the seed's change number, on branch, once main
// was written by another than the seed's landing: its head read against
// main's whole history, so a seed Core landed whose answer was lost — main
// its head, or a change landed on top of it since — is left as it is. HQ
// answering the change no longer open is the seed settled, not a failure.
func (b *pooledBirth) closeOrphanedSeed(ctx context.Context, repos partRepositories, scratch *hq.Scratch, repository string, number int, branch string) error {
	main, found, err := scratch.Fetch(ctx, hqBase)
	if err != nil {
		return fmt.Errorf("read main's history: %w", err)
	}
	head, headFound, err := scratch.Fetch(ctx, branch)
	if err != nil {
		return fmt.Errorf("read change #%d's head: %w", number, err)
	}
	onMain := false
	if found && headFound {
		if onMain, err = scratch.IsAncestor(ctx, head, main); err != nil {
			return fmt.Errorf("read main's history: %w", err)
		}
	}
	if !ops.SeedOrphaned(found, onMain) {
		return nil
	}
	b.progress.say(fmt.Sprintf("closing the seed's change #%d: main was written by another", number))
	if _, err := repos.CloseChange(ctx, repository, number); !ops.SeedCloseDone(err) {
		return fmt.Errorf("close change #%d: %w", number, err)
	}
	return nil
}

// readMain is full's `main` as HQ holds it now: its head and what it is to
// the seed.
func (b *pooledBirth) readMain(ctx context.Context, scratch *hq.Scratch, full string) (string, ops.SeedMain, error) {
	var head string
	var found bool
	if err := b.d.askHQ(ctx, b.progress, full+"'s "+hqBase, func(ctx context.Context) (err error) {
		head, found, err = scratch.FetchTip(ctx, hqBase)
		return err
	}); err != nil {
		return "", 0, pooledHQError("Could not read "+full+"'s "+hqBase+" from HQ", err)
	}
	var paths []string
	if found {
		var err error
		if paths, err = scratch.Paths(ctx, head); err != nil {
			return "", 0, pooledHQError("Could not read "+full+"'s "+hqBase, err)
		}
	}
	return head, ops.SeedMainOf(found, paths), nil
}

// checkMainSetups refuses a `main` at head that holds code without the
// parts' pool setups, naming each gap; nil when it holds them all.
func checkMainSetups(ctx context.Context, scratch *hq.Scratch, appID, repository, head string, parts []pooledPlanPart) error {
	full := appID + "/" + repository
	var lacks []string
	for _, p := range parts {
		sp := newStandupPair(p.tierPair(repository, ""), appID)
		lack, err := poolSetupLacks(ctx, scratch, head, sp, nil)
		if err != nil {
			return pooledHQError(fmt.Sprintf("Could not read %s at %s", full, shortCommit(head)), err)
		}
		lacks = append(lacks, lack...)
	}
	if len(lacks) > 0 {
		return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("%s's %s at %s holds code without the parts' pool setups (refused): %s", full, hqBase, shortCommit(head), strings.Join(lacks, " ")),
			"Nothing was imported and nothing was written to HQ. Add what this names to main through a change, then send the same plan again.")
	}
	return nil
}

// foreignSeedChangeError is a change HQ handed back that the seed is not
// written onto; why says what it is.
type foreignSeedChangeError struct{ why string }

func (e *foreignSeedChangeError) Error() string { return "the change " + e.why }

// pushSeed brings the seed change's branch to tree as ops.SeedOnto decides
// on the change HQ answered: a commit with no parent on a branch with no
// head, so the change is the first into an empty repository; a head that is
// the seed already (a plan sent again finds its own change) kept; a commit of
// tree on a head holding the seed's files; any other change refused, written
// nothing.
func pushSeed(ctx context.Context, scratch *hq.Scratch, opened hq.OpenedChange, tree string, files map[string]string) error {
	branch := opened.Change.Branch
	head, found, err := scratch.Fetch(ctx, branch)
	if err != nil {
		return err
	}
	c := ops.SeedChange{Created: opened.Created, Title: opened.Change.Title, HeadFound: found}
	if found {
		if c.HeadTree, err = scratch.Tree(ctx, head); err != nil {
			return err
		}
		if c.HeadPaths, err = scratch.Paths(ctx, head); err != nil {
			return err
		}
	}
	var parents []string
	switch ops.SeedOnto(c, tree, files) {
	case ops.SeedWriteRefused:
		if !c.Created && c.Title != ops.SeedTitle {
			return &foreignSeedChangeError{why: "is not the seed's"}
		}
		return &foreignSeedChangeError{why: "holds files besides the seed's"}
	case ops.SeedWriteNothing:
		return nil
	case ops.SeedWriteOnHead:
		parents = []string{head}
	case ops.SeedWriteRoot:
	}
	commit, err := scratch.Commit(ctx, hq.CommitSpec{Tree: tree, Parents: parents,
		Message: ops.SeedTitle + ": zerops.yaml and the dev-server declaration",
		Name:    ops.DeployGitIdentity.Name, Email: ops.DeployGitIdentity.Email})
	if err != nil {
		return err
	}
	return scratch.Push(ctx, commit, branch)
}

// importParts is step 3: one import, through ops.Import as zerops_import
// makes it, of every part's pool (the one PoolStanza) and stage (the part's
// type, no build: its first build is the commit deploy) the project lacks.
// Services already there are left as they are.
func (b *pooledBirth) importParts(ctx context.Context, parts []pooledPlanPart) error {
	d := b.d
	live, err := d.liveServices(ctx)
	if err != nil {
		return platform.NewPlatformError(platform.ErrAPIError, fmt.Sprintf("Could not list this project's services: %v.", err), "Nothing was imported. Send the same plan again.")
	}
	var services []any
	var names []string
	for _, p := range parts {
		if live[p.pool] == nil {
			stanza, err := bundle.PoolStanza{Hostname: p.pool, EnableSubdomainAccess: true, ZcpHostname: d.batch.rtInfo.ServiceName}.Render()
			if err != nil {
				return platform.NewPlatformError(platform.ErrAPIError, fmt.Sprintf("Could not write %s's import: %v.", p.pool, err), "Nothing was imported.")
			}
			var doc struct {
				Services []any `yaml:"services"`
			}
			if err := yaml.Unmarshal([]byte(stanza), &doc); err != nil {
				return platform.NewPlatformError(platform.ErrAPIError, fmt.Sprintf("Could not read %s's import: %v.", p.pool, err), "Nothing was imported.")
			}
			services = append(services, doc.Services...)
			names = append(names, p.pool)
		}
		if live[p.stage] == nil {
			services = append(services, map[string]any{"hostname": p.stage, "type": p.typ})
			names = append(names, p.stage)
		}
	}
	if len(services) == 0 {
		return nil
	}
	content, err := yaml.Marshal(map[string]any{"services": services})
	if err != nil {
		return platform.NewPlatformError(platform.ErrAPIError, fmt.Sprintf("Could not write the parts' import: %v.", err), "Nothing was imported.")
	}
	b.progress.say("importing " + strings.Join(names, ", "))
	result, err := ops.Import(ctx, d.batch.client, d.batch.projectID, string(content), "", false)
	if err != nil {
		return fmt.Errorf("import %s: %w", strings.Join(names, ", "), err)
	}
	if len(result.ServiceErrors) > 0 {
		said := make([]string, 0, len(result.ServiceErrors))
		for _, e := range result.ServiceErrors {
			message := e.Message
			if words := platform.APIMetaWords(e.Meta); words != "" {
				message += " (" + words + ")"
			}
			said = append(said, fmt.Sprintf("%s: %s (%s)", e.Service, message, e.Code))
		}
		return platform.NewPlatformError(platform.ErrInvalidImportYml,
			fmt.Sprintf("The platform refused the import of %s: %s.", strings.Join(names, ", "), strings.Join(said, "; ")),
			"The repository and its main stay. Fix what the platform names, then send the same plan again; it continues from here.")
	}
	return nil
}

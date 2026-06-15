package port

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// Deps carries the composition-root wiring the port tool needs. The server
// (the only composer of the authoring domain) fills it inside the
// ZCP_AUTHORING gate; nothing here couples the port flow to core beyond the
// L2 allowlist.
type Deps struct {
	// Schemas returns the live Zerops schema snapshot — the C2-style
	// provider closure over the server's schema.Cache (embedded-seeded,
	// never nil in production). Recon resolves managed-type existence
	// against it. Nil-tolerant: a nil provider classifies every dependency
	// self-run (the conservative default).
	Schemas func() *schema.Schemas
	// StateDir is the .zcp/state root. The port session sidecar lives in
	// the authoring-owned `port/` namespace under it (boundary contract C3).
	StateDir string
	// ProjectID + Environment describe the hosting project — session
	// observability metadata, not behavior inputs.
	ProjectID   string
	Environment string
}

// PortInput is the input schema for zerops_port. The loop is AGENT-DRIVEN:
// the agent runs every deploy via the existing core tools, observes the
// result, and passes what it observed here; the handlers classify, grade,
// and record — they never deploy.
type PortInput struct {
	Action string `json:"action" jsonschema:"One of: start, iterate, harden, capture, status. start = recon: deterministic classification of the agent-researched target descriptor into a PortPlan + feasibility band, ZERO deploy. iterate = one deploy-debug loop turn: pass the observed FailureClassification (or deploySucceeded=true). harden = grade agent-reported rubric observations into the measured FitCeiling (call without rubric first to get the harden PLAN). capture = Stage B: emit the honored-tier recipe + curated publish (gated on a feasible scored FitCeiling). status = compaction recovery: re-orient on the live port session."`

	// Target is the agent-provided descriptor for action=start.
	Target *PortTargetDescriptor `json:"target,omitempty" jsonschema:"For action=start only: agent-provided target descriptor for the OSS to port. Shape: {name, acquisitionHint, dependencies:[...], runtimes:[...], prebuiltUrl, crossServiceOrdering:bool}. acquisitionHint is one of 'source-repo' (build from source), 'binary-url' (prebuilt binary download), 'image-only' (only a container image exists — ported via crane image-lift, IN-band), 'k8s-runtime' (needs Kubernetes runtime orchestration — bails). dependencies + runtimes are the declared service-type tokens (e.g. 'postgresql', 'clickhouse', 'nodejs@22'). prebuiltUrl (optional) makes the source-build→prebuilt-binary escalation available to the loop. crossServiceOrdering=true when the software needs strict cross-service init ordering (one service's init must wait for another to be ready) — raises the band toward HARD and records the retry-until-ready (zsc execOnce --retryUntilSuccessful) choreography fix; NOT a bail. Recon classifies — it does not research the software."`

	// TargetService names the deploy target the agent observed this turn.
	TargetService string `json:"targetService,omitempty" jsonschema:"For action=iterate only: the hostname the observed deploy targeted (optional — omit for a project-level failure with no single hostname)."`

	// Deploy-debug loop observations (action=iterate). Per-turn threaded —
	// no server-side persistence beyond the PortSession attempt history.
	FailureClass    string   `json:"failureClass,omitempty"    jsonschema:"For action=iterate only: the observed FailureClass read off the deploy response's failureClassification — one of 'build', 'start', 'verify', 'network', 'config', 'credential', 'other'. Required on a failing turn; omit (with deploySucceeded=true) when the deploy reached its target state. The loop reads this FIRST to derive the fix-class — never parse logs to choose."`
	Signals         []string `json:"signals,omitempty"         jsonschema:"For action=iterate only: the observed signal IDs from failureClassification.signals (e.g. ['build:command-not-found'], ['init:db-connection-refused'], ['build:oom-killed']). Refines the fix-class within a FailureClass. Persisted on the port session attempt history."`
	DeploySucceeded FlexBool `json:"deploySucceeded,omitempty" jsonschema:"For action=iterate only: set true when the deploy reached its target state this turn (no failure observed). Records a non-failing attempt and steers toward the next rubric check; failureClass is not required when this is true."`
	ImportOverride  FlexBool `json:"importOverride,omitempty"  jsonschema:"For action=iterate only: set true when the only available fix is an import.yaml edit to an EXISTING hostname (resources / type version / mounts / startWithoutCode) that the glue zerops.yaml cannot express. The derived guidance then warns about the import-override gate tax (override=true + confirmDestructive ack, costs an iteration, wipes container/env state). Prefer glue-zerops.yaml edits; set this only when an import edit is unavoidable."`

	// Rubric carries the agent-reported observations for action=harden.
	Rubric *PortRubricInput `json:"rubric,omitempty" jsonschema:"For action=harden only: the agent-reported rubric observations the handler grades into the measured FitCeiling. Shape: {buildSucceeded, buildHadWarnings, reachedActive, stableAfterHold, httpRootPassed, coreFlowProbePassed, harden:{sentinelSurvivedRedeploy, sentinelOnDurableSurface, appContainers, haDeps:[managed dep types measured running HA, e.g. \"clickhouse@25.3\"], haVerifyPassed}}. The agent runs the deploy/verify/sentinel/scale probes via the existing tools and reports what it OBSERVED; the handler does not re-judge. haDeps is the per-dependency HA topology AND the C6 managed-HA gate — only list deps you PROVED run HA; others emit NON_HA. HA-production (tier 5) needs a non-empty haDeps + >=2 app containers + haVerifyPassed. Call harden WITHOUT rubric first to get the harden plan (the checkpoints to run)."`
	// GlueRepo carries the buildFromGit glue-repo coordinates the FitCeiling
	// records for Stage B.
	GlueRepo *PortGlueRepoInput `json:"glueRepo,omitempty" jsonschema:"For action=harden only: the glue-repo coordinates Stage B needs to publish — {url, committedSha, buildFromGitReady}. buildFromGitReady=false flags a deferred commit (the recipe import's buildFromGit will not resolve yet)."`
	// Unresolved carries loop-discovered unresolved constraints (HARD-band
	// no-signal class) to merge into the FitCeiling honesty residue.
	Unresolved []string `json:"unresolved,omitempty" jsonschema:"For action=harden only: loop-discovered constraints the port could not resolve (e.g. a no-failure-signal gotcha) — merged into the FitCeiling unresolvedConstraints honesty residue."`

	// PublishDryRun gates the Stage B publish (action=capture).
	PublishDryRun FlexBool `json:"publishDryRun,omitempty" jsonschema:"For action=capture only: when true, the curated publish (app -> zerops-recipe-apps, recipe envs -> the recipes catalog) runs as a DRY-RUN — nothing is pushed, the would-commit diffs are returned. The recipe output dir is always emitted regardless. Default false (live publish)."`
}

// PortRubricInput is the agent-reported rubric observation set for the port
// harden+score step. Each field is what the agent OBSERVED running the probe
// via the existing tools; the handler grades them (it does not re-run
// anything).
type PortRubricInput struct {
	BuildSucceeded      FlexBool         `json:"buildSucceeded,omitempty"`
	BuildHadWarnings    FlexBool         `json:"buildHadWarnings,omitempty"`
	ReachedActive       FlexBool         `json:"reachedActive,omitempty"`
	StableAfterHold     FlexBool         `json:"stableAfterHold,omitempty"`
	HTTPRootPassed      FlexBool         `json:"httpRootPassed,omitempty"`
	CoreFlowProbePassed FlexBool         `json:"coreFlowProbePassed,omitempty"`
	Harden              *PortHardenInput `json:"harden,omitempty"`
}

// PortHardenInput is the agent-reported harden-probe results (sentinel + HA
// scale) the handler grades into C5/C6.
type PortHardenInput struct {
	SentinelSurvivedRedeploy FlexBool `json:"sentinelSurvivedRedeploy,omitempty"`
	SentinelOnDurableSurface FlexBool `json:"sentinelOnDurableSurface,omitempty"`
	AppContainers            int      `json:"appContainers,omitempty"`
	// HADeps names the managed dependency types the agent MEASURED running in HA
	// mode (e.g. ["clickhouse@25.3"] for PostHog, where Postgres/Valkey stay
	// NON_HA). SINGLE source of truth for the managed-HA fact: it drives BOTH the
	// emitted per-service `mode:` (real topology, not a family-table assumption)
	// AND the C6 grade's managed-HA condition (len>0). Matched by canonical base name.
	HADeps         []string `json:"haDeps,omitempty"`
	HAVerifyPassed FlexBool `json:"haVerifyPassed,omitempty"`
}

// PortGlueRepoInput is the agent-reported glue-repo coordinates for the FitCeiling.
type PortGlueRepoInput struct {
	URL               string   `json:"url,omitempty"`
	CommittedSHA      string   `json:"committedSha,omitempty"`
	BuildFromGitReady FlexBool `json:"buildFromGitReady,omitempty"`
}

// Register installs the zerops_port tool. server.go registers it ONLY
// inside the ZCP_AUTHORING gate (docs/spec-authoring-boundary.md §4) —
// mirroring recipe.Register.
func Register(srv *mcp.Server, deps Deps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolName,
		Description: "OSS port flow (authoring): take foreign self-hosted software (Strapi, PostHog, umami, ...) and get it running WELL on Zerops, then capture the working deployment as a curated recipe. Actions: start (recon — pass target={name, acquisitionHint, dependencies, runtimes}; the agent researches the OSS off-platform, recon classifies it into a PortPlan + feasibility band with zero deploy), iterate (the agent-driven deploy-debug loop: deploy via the EXISTING tools, read failureClassification off the response, pass the observed class + signals here; the handler derives the next fix-class and self-terminates on stall/cap/budget), harden (grade agent-observed rubric probes into the measured FitCeiling; call without rubric for the harden plan), capture (Stage B: emit honored tiers + curated publish — gated on a feasible scored FitCeiling; checkpoint by design so a human can inspect first), status (compaction recovery). This is NOT the framework-recipe tool (zerops_recipe) — use this for porting third-party OSS, zerops_recipe for authoring framework showcase recipes.",
		Annotations: &mcp.ToolAnnotations{Title: "Port OSS software to Zerops (authoring)"},
		InputSchema: portInputSchema(),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in PortInput) (*mcp.CallToolResult, any, error) {
		return dispatch(in, deps), nil, nil
	})
}

// portInputSchema derives the input schema from PortInput, then patches
// every FlexBool property (top-level + nested rubric/harden/glueRepo) to
// the oneOf[boolean,string] form so stringified booleans from agents are
// tolerated at the unmarshal layer instead of rejected at the schema layer.
// Mirrors workflowInputSchema's patch model in core. Fallback nil keeps the
// SDK's inference on the practically-impossible derivation error.
func portInputSchema() *jsonschema.Schema {
	s, err := jsonschema.For[PortInput](nil)
	if err != nil || s == nil {
		return nil
	}
	patchFlexBoolProperties(s)
	return s
}

// flexBoolProperties enumerates every FlexBool-typed property name in the
// PortInput tree. Patched wherever the name appears (the names are unique
// to their structs).
var flexBoolProperties = map[string]bool{
	"deploySucceeded":          true,
	"importOverride":           true,
	"publishDryRun":            true,
	"buildSucceeded":           true,
	"buildHadWarnings":         true,
	"reachedActive":            true,
	"stableAfterHold":          true,
	"httpRootPassed":           true,
	"coreFlowProbePassed":      true,
	"sentinelSurvivedRedeploy": true,
	"sentinelOnDurableSurface": true,
	"haVerifyPassed":           true,
	"buildFromGitReady":        true,
}

// patchFlexBoolProperties walks the schema tree (properties + $defs) and
// replaces every enumerated FlexBool property with the tolerant
// oneOf[boolean,string] schema, preserving the inferred description.
func patchFlexBoolProperties(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	for key, prop := range s.Properties {
		if flexBoolProperties[key] {
			desc := ""
			if prop != nil {
				desc = prop.Description
			}
			s.Properties[key] = flexBoolSchema(desc)
			continue
		}
		patchFlexBoolProperties(prop)
	}
	for _, def := range s.Defs {
		patchFlexBoolProperties(def)
	}
	patchFlexBoolProperties(s.Items)
}

// dispatch routes an action to its handler. Unknown actions error with the
// action menu (there is no generic switch to fall through to — the port
// tool owns its whole dispatch).
func dispatch(in PortInput, deps Deps) *mcp.CallToolResult {
	switch in.Action {
	case "start":
		return handleStart(in, deps)
	case "iterate":
		return handleIterate(in, deps.StateDir)
	case "harden":
		return handleHarden(in, deps.StateDir)
	case "capture":
		return handleCapture(in, deps.StateDir)
	case "status":
		return handleStatus(deps.StateDir)
	default:
		return errResult(platform.ErrInvalidParameter,
			"Unknown port action",
			`Pass action one of: "start" (recon), "iterate" (deploy-debug turn), "harden" (rubric grading), "capture" (Stage B emit + publish), "status" (recovery).`)
	}
}

// handleStart is the recon entry (Stage A0). The agent supplies a target
// descriptor (name, acquisition hint, declared deps, declared runtimes); the
// handler resolves the live schema catalog, runs ReconClassify, persists a
// PortSession sidecar, and returns the PortPlan + feasibility band. ZERO
// deploy, zero network beyond the schema fetch the cache already performs.
func handleStart(in PortInput, deps Deps) *mcp.CallToolResult {
	if in.Target == nil || in.Target.Name == "" {
		return errResult(platform.ErrInvalidParameter,
			"Port flow requires a target descriptor",
			`Pass target={name, acquisitionHint, dependencies:[...], runtimes:[...]} on action="start". The agent researches the OSS off-platform and supplies the structured descriptor; recon classifies it into a PortPlan with no deploy.`)
	}

	var schemas *schema.Schemas
	if deps.Schemas != nil {
		schemas = deps.Schemas()
	}

	plan := ReconClassify(*in.Target, schemas)

	// Persist the recon plan in a PortSession sidecar so the loop phases can
	// resume from it. Best-effort: a missing stateDir (rare) skips persistence
	// rather than failing the recon — the plan is still returned to the agent.
	if deps.StateDir != "" {
		ps := NewPortSession(deps.ProjectID, deps.Environment, "port "+in.Target.Name, plan, time.Now())
		if err := SavePortSession(deps.StateDir, ps); err != nil {
			return errResultFromErr(err)
		}
	}

	return jsonResult(map[string]any{
		"status":   "recon",
		"phase":    portPhaseActive,
		"portPlan": plan,
		"guidance": portReconGuidance(plan),
	})
}

// portReconGuidance frames the recon estimate for the agent: it is an
// ESTIMATE, not the fit ceiling, and a bail is the one true refusal.
func portReconGuidance(plan PortPlan) string {
	switch plan.Band {
	case BandBail:
		return "Recon BAILED: this software needs Kubernetes runtime orchestration that Zerops cannot express as prepareCommands/initCommands. See constraints. No deploy attempted."
	case BandHard:
		return "Recon estimate: HARD. Acquisition + dep mapping below are an ESTIMATE, not the ceiling — the deploy-debug loop measures the true FitCeiling. Expect cross-service init ordering and deep OSS-internals knowledge."
	case BandMedium:
		return "Recon estimate: MEDIUM. One dependency has no managed equivalent (self-run). The deploy-debug loop will measure the true FitCeiling."
	case BandEasy:
		return "Recon estimate: EASY. Source/crane acquisition + all deps managed. The deploy-debug loop will measure the true FitCeiling."
	default:
		return "Recon estimate: EASY. Source/crane acquisition + all deps managed. The deploy-debug loop will measure the true FitCeiling."
	}
}

// handleIterate is the agent-driven deploy-debug loop continuation (Stage
// A1). The agent runs the deploy via the EXISTING tools (zerops_deploy /
// zerops_import / zerops_env), observes the FailureClassification (class +
// signals), then calls THIS handler passing what it observed. The handler
// does NOT deploy — it loads the PortSession, derives the next fix-class via
// DeriveFixClass (reading the FailureClass FIRST, never parsing logs),
// records the attempt outcome, and returns the guidance for the agent's
// next turn.
func handleIterate(in PortInput, stateDir string) *mcp.CallToolResult {
	ps, res := loadSessionOr(stateDir,
		`Start the port flow first with zerops_port action="start" target={...}. The deploy-debug loop iterates an existing port session.`)
	if res != nil {
		return res
	}

	// Success path: the agent reports the deploy reached its target state this
	// turn. Record a non-failing attempt and steer toward the next rubric check.
	if bool(in.DeploySucceeded) {
		recorded := ps.RecordPortAttempt(PortAttempt{
			RecordedAt: time.Now().UTC().Format(time.RFC3339),
			Hostname:   in.TargetService,
			Succeeded:  true,
		})
		if saveErr := SavePortSession(stateDir, ps); saveErr != nil {
			return errResultFromErr(saveErr)
		}
		return jsonResult(map[string]any{
			"status":    "port-iterate",
			"phase":     portPhaseActive,
			"iteration": ps.Iteration,
			"attempt":   recorded,
			"guidance":  "Deploy reached its target state. Continue with the next rubric check (boot stability hold / HTTP serve / harden) or close the port once the FitCeiling is measured.",
		})
	}

	// Failure path: the agent MUST report the observed FailureClass it read off
	// the live FailureClassification — the loop reads the class FIRST, never logs.
	class := topology.FailureClass(in.FailureClass)
	if class == "" {
		return errResult(platform.ErrInvalidParameter,
			"Port iterate requires the observed failure class",
			`Run the deploy via the existing tools (zerops_deploy / zerops_import), read the failureClassification off the response, and pass failureClass=<build|start|verify|network|config|credential|other> + signals=[...] (the signal IDs from failureClassification.signals). Or pass deploySucceeded=true when the deploy reached its target state.`)
	}

	// Derive the fix-class. Prefer glue-zerops.yaml edits; when the agent flags
	// that the only available fix is an import.yaml edit to an existing hostname,
	// surface the override tax explicitly (it costs an iteration + wipes state).
	var fix PortFixClass
	if bool(in.ImportOverride) {
		fix = DeriveImportOverrideFixClass(class, in.Signals)
	} else {
		fix = DeriveFixClass(class, in.Signals)
	}

	recorded := ps.RecordPortAttempt(PortAttempt{
		RecordedAt: time.Now().UTC().Format(time.RFC3339),
		Hostname:   in.TargetService,
		Class:      class,
		Signals:    append([]string(nil), in.Signals...),
		FixKind:    fix.Kind,
		Escalate:   fix.Escalate,
	})

	// Evaluate the loop terminators + escalation ladder over the now-updated
	// attempt history. The decision drives one of three outcomes:
	// continue-with-fix (T0), escalate-strategy + re-budget (T1), or stop-bail
	// (a terminator fired). The handler mutates the session (escalation switches
	// the acquisition strategy + sets the re-budget origin; the cap terminator
	// stamps the terminal close) BEFORE the single persist.
	resp := portIterateDecision(ps, fix, recorded)

	if saveErr := SavePortSession(stateDir, ps); saveErr != nil {
		return errResultFromErr(saveErr)
	}
	return jsonResult(resp)
}

// portIterateDecision applies the terminator + escalation logic to the
// just-recorded failing turn and returns the agent-facing response body. It
// MUTATES ps in place (strategy switch + re-budget on T1; terminal close on
// the iteration cap) so the caller's single SavePortSession persists
// everything. Kept as a helper so handleIterate stays under the cyclo/
// maintidx gates.
func portIterateDecision(ps *PortSession, fix PortFixClass, recorded PortAttempt) map[string]any {
	now := time.Now().UTC()
	// progressRose is fed by the harden+score step (action="harden"), not by
	// iterate — a deploy-debug turn does not re-score the rubric. The seam stays
	// false here; the measured tier-rise breaks the phase stall on the harden turn.
	prog := EvaluatePortProgress(ps, now, false)
	esc := DecidePortEscalation(ps.Plan, ps.Attempts)

	resp := map[string]any{
		"status":     "port-iterate",
		"phase":      portPhaseActive,
		"iteration":  ps.Iteration,
		"fixClass":   fix,
		"attempt":    recorded,
		"classStall": prog.ClassStall,
		"phaseStall": prog.PhaseStall,
	}

	// T1 escalation: switch the acquisition strategy + re-budget so the cap
	// terminator measures a fresh sub-budget from this turn. Only when the loop
	// has NOT already tripped a stop terminator on this same turn.
	if !prog.Stop && esc.Tier == PortEscalateT1 {
		ps.Plan.Acquisition = esc.NewStrategy
		ps.RebudgetOrigin = ps.Iteration
		resp["escalation"] = esc
		resp["guidance"] = esc.Reason + ". Switch the acquisition strategy in the glue zerops.yaml (now: " +
			string(esc.NewStrategy) + ") and redeploy. The iteration budget was re-set so the new strategy is not starved."
		return resp
	}

	// A terminator fired → graceful stop. The iteration cap stamps the
	// session's terminal close (the existing close reason vocabulary).
	if prog.Stop {
		resp["stop"] = true
		resp["terminator"] = string(prog.Terminator)
		if prog.Terminator == PortTermIterationCap {
			ps.CloseOnIterationCap(now)
		}
		if esc.Tier == PortEscalateT2Bail {
			resp["escalation"] = esc
		}
		// Attach the measured FitCeiling at the bail/stop point. When the loop
		// scored one via action="harden" it is the honest partial report the
		// agent captures; otherwise the agent must harden+score before capture.
		if ps.FitCeiling != nil {
			resp["fitCeiling"] = ps.FitCeiling
			resp["guidance"] = prog.Reason + " Stop the loop and capture the measured FitCeiling attached here; do NOT keep redeploying."
		} else {
			resp["guidance"] = prog.Reason + ` Stop the loop, then run zerops_port action="harden" to score the partial FitCeiling before capture; do NOT keep redeploying.`
		}
		return resp
	}

	// T0 stay: apply the derived fix-class and continue.
	resp["guidance"] = fix.Guidance
	return resp
}

// handleHarden is the Stage A2 harden + score step. After the deploy-debug
// loop has the app building/booting/serving, the agent runs the harden
// probes via the EXISTING tools — write a persistence sentinel → redeploy →
// re-read; inject readiness/health; scale ≥2 + verify HA — and reports what
// it OBSERVED. This handler does NOT deploy or call ops: it grades the
// agent-reported rubric observations (the loop can't understand a foreign
// app's health endpoint, so the agent authors + observes the probes), builds
// the measured FitCeiling, and persists it on the PortSession. The handler
// is the agent-driven mirror of iterate — guidance + checkpoints out,
// observed results in, pure grader in the middle.
func handleHarden(in PortInput, stateDir string) *mcp.CallToolResult {
	ps, res := loadSessionOr(stateDir,
		`Start the port flow first with zerops_port action="start", run the deploy-debug loop to building/booting/serving, then action="harden".`)
	if res != nil {
		return res
	}

	// No rubric observations yet → return the harden PLAN (the checkpoints the
	// agent must run before it can report results). This is the guidance surface.
	if in.Rubric == nil {
		hp := PlanHarden(ps.Plan)
		return jsonResult(map[string]any{
			"status":     "port-harden-plan",
			"phase":      portPhaseActive,
			"hardenPlan": hp,
			"guidance":   "Run these harden checkpoints via the existing tools, then call zerops_port action=\"harden\" again with rubric={...} reporting what you OBSERVED: (1) build/boot/serve — confirm C1/C2/C3 after a STABILITY HOLD (an ACTIVE-then-exit / crash-loop is NOT stable). (2) author a core-flow probe (C4) — the loop can't infer a foreign app's health endpoint. (3) per durable dependency, write a sentinel → redeploy → re-read (C5; the container FS is ephemeral — assert on the managed/storage surface). (4) scale ≥2 + flip mode-bearing managed deps to HA + verify (C6; throughput-scaling is DISTINCT from HA replication).",
		})
	}

	fc := scorePortFitCeiling(ps, in)

	// progressRose: did the measured honored-tier RISE versus the previously
	// scored ceiling? This is the phase-stall seam that the harden turn feeds —
	// a rising ceiling means the loop advanced this turn even if the fix-class
	// phase did not, so it breaks the phase-stall streak.
	progressRose := measuredTierRose(ps, fc)

	ps.FitCeiling = &fc
	if saveErr := SavePortSession(stateDir, ps); saveErr != nil {
		return errResultFromErr(saveErr)
	}

	return jsonResult(map[string]any{
		"status":       "port-harden",
		"phase":        portPhaseActive,
		"fitCeiling":   fc,
		"progressRose": progressRose,
		"guidance":     portHardenGuidance(fc),
	})
}

// scorePortFitCeiling grades the agent-reported rubric observations into a
// measured FitCeiling. Pure-ish glue: it adapts the wire input to the
// engine graders + builder (all pure).
func scorePortFitCeiling(ps *PortSession, in PortInput) FitCeiling {
	r := in.Rubric
	var hr HardenResults
	if r.Harden != nil {
		hr = HardenResults{
			SentinelSurvivedRedeploy: bool(r.Harden.SentinelSurvivedRedeploy),
			SentinelOnDurableSurface: bool(r.Harden.SentinelOnDurableSurface),
			AppContainers:            r.Harden.AppContainers,
			HADeps:                   r.Harden.HADeps,
			HAVerifyPassed:           bool(r.Harden.HAVerifyPassed),
		}
	}
	// Derive the FILTERED HA breakdown ONCE, then grade C6 off it — the grade and
	// the emitted topology (capture) consume the same ManagedHADeps list, so they
	// cannot disagree (a bogus/storage/unplanned haDeps entry is dropped here).
	ha := DeriveAchievableHA(ps.Plan, hr.AppContainers, hr.HADeps)
	c5, c6 := GradeHarden(hr, ha)

	rubric := PortRubric{Grades: []PortGrade{
		C1Builds(bool(r.BuildSucceeded), bool(r.BuildHadWarnings)),
		C2BootsStable(bool(r.ReachedActive), bool(r.StableAfterHold)),
		C3ServesHTTP(bool(r.HTTPRootPassed)),
		C4CoreFlow(bool(r.CoreFlowProbePassed)),
		c5,
		c6,
	}}

	in2 := BuildFitCeilingInput{
		Plan:             ps.Plan,
		Rubric:           rubric,
		HA:               ha,
		FinalAcquisition: ps.Plan.Acquisition,
		ExtraConstraints: in.Unresolved,
	}
	if g := in.GlueRepo; g != nil {
		in2.Glue = GlueRepo{
			URL:               g.URL,
			CommittedSHA:      g.CommittedSHA,
			BuildFromGitReady: bool(g.BuildFromGitReady),
		}
	}
	return BuildFitCeiling(in2)
}

// measuredTierRose reports whether the new FitCeiling's honored ceiling is higher
// than the session's previously-scored ceiling. A first feasible score (no prior
// ceiling) counts as a rise.
func measuredTierRose(ps *PortSession, next FitCeiling) bool {
	if !next.Feasible {
		return false
	}
	prev, hadPrev := ps.MeasuredCeiling()
	if !hadPrev {
		return true
	}
	return next.MeasuredCeiling > prev
}

// portHardenGuidance frames the measured FitCeiling for the agent at the Stage A
// checkpoint.
func portHardenGuidance(fc FitCeiling) string {
	if !fc.Feasible {
		return "Measured FitCeiling: INFEASIBLE — the build/boot/serve gate (C1/C2/C3) did not pass, so no deployment tier is honored. Read whatDoesnt + the honored-tier reasons, fix the gate via the deploy-debug loop (action=\"iterate\"), then re-harden. Do NOT publish an infeasible port."
	}
	return "Measured FitCeiling scored. This is the HONEST report: whatRuns / whatDoesnt and the per-tier honored verdicts (each excluded tier carries its reason — ship a tier ONLY if its rubric prerequisite is met). The measured ceiling is the truth; the recon band was only an estimate. Stage A stops here at the checkpoint — capture publishes the honored tiers + glue-repo separately so a human can inspect the port first."
}

// handleStatus surfaces a live PortSession on action="status" — the
// compaction-recovery primitive. The core lifecycle status knows nothing
// about the port flow, so the port tool carries its own recovery envelope
// (BuildPortActiveRecovery). When no session exists for this PID the agent
// is told to start the flow.
func handleStatus(stateDir string) *mcp.CallToolResult {
	ps, res := loadSessionOr(stateDir,
		`Start the port flow with zerops_port action="start" target={...}.`)
	if res != nil {
		return res
	}
	return jsonResult(BuildPortActiveRecovery(ps))
}

// loadSessionOr loads the current-PID port session, translating the two
// non-viable states (no state dir, no session) into the error result every
// session-requiring action shares. Returns (session, nil) on success or
// (nil, errorResult) to return as-is.
func loadSessionOr(stateDir, startHint string) (*PortSession, *mcp.CallToolResult) {
	if stateDir == "" {
		return nil, errResult(platform.ErrNotImplemented,
			"Port flow requires a state directory",
			"Ensure ZCP is configured with a state directory (a working directory with .zcp/state).")
	}
	ps, err := LoadPortSession(stateDir, os.Getpid())
	if err != nil {
		return nil, errResultFromErr(err)
	}
	if ps == nil {
		return nil, errResult(platform.ErrSessionNotFound,
			"No active port session for this process", startHint)
	}
	return ps, nil
}

// --- result helpers (port-owned wire; no internal/tools import) ---

// errorWire is the port tool's error envelope. It mirrors the field
// vocabulary of the core ErrorWire (code / error / suggestion / recovery)
// so agents read one error grammar across tools, but it is port-owned: the
// recovery hint points at THIS tool's status action, and error responses
// stay leaf payloads (no lifecycle envelope — the P4 contract).
type errorWire struct {
	Code       string             `json:"code"`
	Error      string             `json:"error"`
	Suggestion string             `json:"suggestion,omitempty"`
	Recovery   *topology.Recovery `json:"recovery,omitempty"`
}

// errResult builds an IsError tool result carrying the port error envelope
// with the status-recovery hint.
func errResult(code, message, suggestion string) *mcp.CallToolResult {
	w := errorWire{
		Code:       code,
		Error:      message,
		Suggestion: suggestion,
		Recovery:   &topology.Recovery{Tool: toolName, Action: "status"},
	}
	text, err := json.Marshal(w)
	if err != nil {
		text = []byte(`{"code":"` + code + `","error":"marshal failure"}`)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(text)}},
		IsError: true,
	}
}

// errResultFromErr wraps an unexpected internal error (state I/O) into the
// wire envelope, typed ErrUnknown — the same plain-Go-error wrapping the
// core convertError boundary applies.
func errResultFromErr(err error) *mcp.CallToolResult {
	return errResult(platform.ErrUnknown, err.Error(), "")
}

// jsonResult marshals a success payload into the single-text-content result
// every ZCP tool returns.
func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return errResultFromErr(err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}

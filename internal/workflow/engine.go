package workflow

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// Engine orchestrates the workflow lifecycle.
//
// Concurrency model: zcp runs as a STDIO subprocess of a single Claude
// Code instance, so each Engine has exactly one client. Multiple Claude
// Code instances run separate zcp subprocesses with separate Engines;
// cross-process serialization on the state directory is provided by the
// registry flock in session_registry.go. Within a single process, the
// MCP go-sdk dispatches tool calls asynchronously (jsonrpc2.Async), so
// parallel tool_use blocks in one LLM turn land on concurrent goroutines
// — but the LLM serializes its own calls and the realistic race damage
// is a stale read that the next call corrects. The only operation that
// genuinely crashes Go on concurrent access is map read/write, so
// knowledgeCache uses sync.Map. Session state is load-mutate-save via
// atomic rename; torn reads are not possible and the worst concurrent-
// write outcome is a superseded state file that `reset` clears.
//
// Immutable fields (stateDir, environment, knowledge, recipeCorpus) are
// set in NewEngine and never mutated afterwards.
type Engine struct {
	stateDir       string
	sessionID      string
	completedState *WorkflowState // holds final state after session file is cleaned up
	environment    Environment
	knowledge      knowledge.Provider
	// knowledgeCache caches zerops_knowledge query results for the session
	// lifetime. sync.Map because concurrent map read+write would panic the
	// runtime; the other Engine fields tolerate the rare parallel tool_use
	// race without corruption.
	knowledgeCache sync.Map
	// recipeCorpus drives BootstrapRoute selection when intent matches a
	// viable recipe. Populated when NewEngine receives a concrete
	// *knowledge.Store (production); nil for test engines using only a
	// Provider mock — those stay on the classic route.
	recipeCorpus *StoreRecipeCorpus
}

// NewEngine creates a new workflow engine rooted at baseDir.
//
// At boot:
//  1. Migrates away legacy state (active_session file, develop/ markers).
//  2. Cleans stale work sessions whose PID is dead.
//  3. Auto-claims a single dead-PID infrastructure session (bootstrap/recipe)
//     so an MCP server restart seamlessly continues prior work. Work sessions
//     are per-process and never claimed — only cleaned.
func NewEngine(baseDir string, env Environment, kp knowledge.Provider) *Engine {
	e := &Engine{
		stateDir:    baseDir,
		environment: env,
		knowledge:   kp,
	}
	if store, ok := kp.(*knowledge.Store); ok {
		e.recipeCorpus = NewStoreRecipeCorpus(store)
	}

	MigrateRemoveLegacyWorkState(baseDir)
	CleanStaleWorkSessions(baseDir)

	sessions, _ := ListSessions(baseDir)
	var candidates []SessionEntry
	for _, s := range sessions {
		if s.Workflow == WorkflowWork {
			continue
		}
		if s.PID == os.Getpid() {
			continue
		}
		if isProcessAlive(s.PID) {
			continue
		}
		candidates = append(candidates, s)
	}
	if len(candidates) == 1 {
		c := candidates[0]
		if state, err := LoadSessionByID(baseDir, c.SessionID); err == nil {
			if err := e.claimSession(c.SessionID, state); err == nil {
				fmt.Fprintf(os.Stderr, "zcp: auto-recovered session %s from previous process\n", c.SessionID)
			}
		}
	}
	return e
}

// setSessionID updates the in-memory session reference.
// Registry is the persistent source of session ownership (see InitSessionAtomic).
func (e *Engine) setSessionID(id string) {
	e.sessionID = id
}

// clearSessionID clears the in-memory session reference.
func (e *Engine) clearSessionID() {
	e.sessionID = ""
	e.knowledgeCache = sync.Map{}
}

// KnowledgeProvider returns the knowledge provider this engine was
// constructed with. Exposed so tool-layer step checkers can query the
// chain recipe without re-parsing the knowledge store themselves.
// Named KnowledgeProvider (not Knowledge) to avoid shadowing the
// unexported `knowledge` field — an `e.Knowledge()` typo reading the
// unexported field would silently skip this nil guard.
func (e *Engine) KnowledgeProvider() knowledge.Provider {
	if e == nil {
		return nil
	}
	return e.knowledge
}

// GetKnowledgeCache returns a cached knowledge result, or (nil, false) if absent.
func (e *Engine) GetKnowledgeCache(key string) (any, bool) {
	if e == nil {
		return nil, false
	}
	return e.knowledgeCache.Load(key)
}

// SetKnowledgeCache stores a knowledge result in the session-level cache.
func (e *Engine) SetKnowledgeCache(key string, value any) {
	if e == nil {
		return
	}
	e.knowledgeCache.Store(key, value)
}

// claimSession takes ownership of a session: updates PID, saves state, updates registry.
func (e *Engine) claimSession(sessionID string, state *WorkflowState) error {
	state.PID = os.Getpid()
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, sessionID, state); err != nil {
		return err
	}
	e.setSessionID(sessionID)
	_ = updateRegistryPID(e.stateDir, sessionID, os.Getpid())
	return nil
}

// Environment returns the detected execution environment.
func (e *Engine) Environment() Environment {
	return e.environment
}

// Start creates a new workflow session with auto-reset, exclusivity, and registry refresh.
func (e *Engine) Start(projectID, workflowName, intent string) (*WorkflowState, error) {
	if e.sessionID != "" {
		if existing, err := LoadSessionByID(e.stateDir, e.sessionID); err == nil {
			bootstrapDone := existing.Bootstrap != nil && !existing.Bootstrap.Active
			recipeDone := existing.Recipe != nil && !existing.Recipe.Active
			if bootstrapDone || recipeDone {
				if err := ResetSessionByID(e.stateDir, e.sessionID); err != nil {
					return nil, fmt.Errorf("start auto-reset: %w", err)
				}
				e.clearSessionID()
			} else {
				return nil, fmt.Errorf("start: active session %s, reset first", e.sessionID)
			}
		}
	}

	state, err := InitSessionAtomic(e.stateDir, projectID, workflowName, intent)
	if err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	e.setSessionID(state.SessionID)
	e.completedState = nil
	return state, nil
}

// Reset clears the current session and removes incomplete ServiceMetas
// left by the abandoned session. This prevents bootstrap limbo where
// provisioned-but-not-completed services linger as orphaned metas.
func (e *Engine) Reset() error {
	e.completedState = nil
	if e.sessionID == "" {
		return nil
	}
	sessionID := e.sessionID
	err := ResetSessionByID(e.stateDir, sessionID)
	e.clearSessionID()
	if err != nil {
		return fmt.Errorf("reset session: %w", err)
	}
	cleanIncompleteMetasForSession(e.stateDir, sessionID)
	return nil
}

// Iterate resets bootstrap steps and increments the counter.
// When the iteration cap is reached, any active work session is closed with
// CloseReasonIterationCap so the LLM stops retrying and surfaces the failure
// to the user (3-tier STOP ladder at iteration 5).
func (e *Engine) Iterate() (*WorkflowState, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	if state.Iteration >= maxIterations() {
		closeWorkSessionOnCap(e.stateDir)
		return nil, fmt.Errorf("iterate: max iterations reached (%d), reset session to continue", maxIterations())
	}
	return IterateSession(e.stateDir, e.sessionID)
}

// ClearAwaitingEvidenceAfterIterate flips the recipe-state gate set by
// action=iterate, signalling that new evidence of work has been produced.
// The canonical touchpoint is a zerops_record_fact call — the facts log
// entry is both the writer subagent's structured input and the gate-clear
// signal for Cx-ITERATE-GUARD. No-op when there is no active session, no
// recipe state, or the gate is already cleared. See defect-class-registry
// §16.3.
func (e *Engine) ClearAwaitingEvidenceAfterIterate() error {
	if e.sessionID == "" {
		return nil
	}
	state, err := e.loadState()
	if err != nil {
		return fmt.Errorf("clear awaiting evidence: %w", err)
	}
	if state.Recipe == nil || !state.Recipe.AwaitingEvidenceAfterIterate {
		return nil
	}
	state.Recipe.AwaitingEvidenceAfterIterate = false
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return fmt.Errorf("clear awaiting evidence save: %w", err)
	}
	return nil
}

// closeWorkSessionOnCap closes the current-PID work session with
// CloseReasonIterationCap. Idempotent and best-effort: errors are swallowed
// because the cap signal itself is already being returned to the caller.
func closeWorkSessionOnCap(stateDir string) {
	ws, err := CurrentWorkSession(stateDir)
	if err != nil || ws == nil || ws.ClosedAt != "" {
		return
	}
	ws.ClosedAt = time.Now().UTC().Format(time.RFC3339)
	ws.CloseReason = CloseReasonIterationCap
	_ = SaveWorkSession(stateDir, ws)
}

// HasActiveSession returns true if this engine has an active session.
func (e *Engine) HasActiveSession() bool {
	return e.sessionID != ""
}

// GetState returns the current workflow state.
func (e *Engine) GetState() (*WorkflowState, error) {
	return e.loadState()
}

// SessionID returns the current session ID.
func (e *Engine) SessionID() string {
	return e.sessionID
}

// StateDir returns the engine's state directory path.
func (e *Engine) StateDir() string {
	return e.stateDir
}

// ListActiveSessions returns all active sessions from the registry.
// Registry access is synchronized via flock inside ListSessions; the
// Engine holds no in-memory state for this operation.
func (e *Engine) ListActiveSessions() ([]SessionEntry, error) {
	return ListSessions(e.stateDir)
}

// BootstrapDiscover inspects project state and returns the ranked list of
// route options without creating a session. The LLM then picks one by calling
// BootstrapStart with the chosen route (and recipe slug, when applicable).
//
// Options are ordered resume > adopt > recipe (top MaxRecipeOptions ≥
// MinRecipeConfidence) > classic. Classic is always last so the LLM can
// force a manual plan even when another route would auto-score higher.
//
// `existing` comes from the caller (the tool layer holds the platform
// client); metas are read from stateDir.
func (e *Engine) BootstrapDiscover(projectID, intent string, existing []platform.ServiceStack, self runtime.Info) (*BootstrapDiscoveryResponse, error) {
	metas, err := ListServiceMetas(e.stateDir)
	if err != nil {
		return nil, fmt.Errorf("bootstrap discover: list metas: %w", err)
	}
	var recipes RecipeCorpus
	if e.recipeCorpus != nil {
		recipes = e.recipeCorpus
	}
	options, err := BuildBootstrapRouteOptions(context.Background(), intent, existing, metas, recipes, self)
	if err != nil {
		return nil, fmt.Errorf("bootstrap discover: %w", err)
	}
	return &BootstrapDiscoveryResponse{
		Kind:         BootstrapKindRouteMenu,
		Intent:       intent,
		ProjectID:    projectID,
		RouteOptions: options,
		Message:      buildDiscoveryMessage(options),
	}, nil
}

// BootstrapStart commits a bootstrap session with classic route (manual
// plan, no preselection). Thin wrapper around BootstrapStartWithRoute so
// callers that don't care about route explicit-ness (most tests, simple
// drivers) stay terse.
func (e *Engine) BootstrapStart(projectID, intent string) (*BootstrapResponse, error) {
	return e.BootstrapStartWithRoute(projectID, intent, "", "")
}

// BootstrapStartWithRoute creates a new session with bootstrap state and
// commits the chosen route. Called after BootstrapDiscover — the LLM picks
// a route from the discovery response and submits it here.
//
// Route semantics:
//   - classic  — manual plan, no preselection. Empty string is accepted as
//     classic so the zero-value path produces a sane default.
//   - recipe   — recipeSlug MUST be set; the engine resolves the import YAML
//     via the recipe corpus and pre-populates RecipeMatch on state.
//   - adopt    — existing runtime services are flagged for adoption; the
//     discover step's plan is expected to mirror them.
//   - resume   — not handled here; callers detect resume at the tool layer
//     and dispatch to engine.Resume with the session ID from discovery.
func (e *Engine) BootstrapStartWithRoute(projectID, intent string, route BootstrapRoute, recipeSlug string) (*BootstrapResponse, error) {
	switch route {
	case "", BootstrapRouteClassic, BootstrapRouteAdopt, BootstrapRouteRecipe:
		// Accepted.
	case BootstrapRouteResume:
		return nil, fmt.Errorf("bootstrap start: route=resume must be dispatched to engine.Resume, not BootstrapStart")
	default:
		return nil, fmt.Errorf("bootstrap start: unknown route %q (want adopt, recipe, classic, or resume)", route)
	}

	if route == BootstrapRouteRecipe && strings.TrimSpace(recipeSlug) == "" {
		return nil, fmt.Errorf("bootstrap start: route=recipe requires recipeSlug")
	}
	if route != BootstrapRouteRecipe && recipeSlug != "" {
		return nil, fmt.Errorf("bootstrap start: recipeSlug set for non-recipe route %q", route)
	}

	// Resolve recipe slug BEFORE creating a session. Corpus lookup can fail
	// on typos or retired slugs — failing fast here avoids leaving an orphan
	// session behind. Everything else validated above is pure arg-shape; the
	// only I/O-ish step is this lookup.
	var preloadedMatch *RecipeMatch
	if route == BootstrapRouteRecipe {
		match, resolveErr := e.resolveRecipeMatch(recipeSlug)
		if resolveErr != nil {
			return nil, fmt.Errorf("bootstrap start: %w", resolveErr)
		}
		preloadedMatch = match
	}

	state, err := e.Start(projectID, WorkflowBootstrap, intent)
	if err != nil {
		return nil, fmt.Errorf("bootstrap start: %w", err)
	}

	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress

	switch route {
	case BootstrapRouteRecipe:
		bs.Route = BootstrapRouteRecipe
		bs.RecipeMatch = preloadedMatch
	case BootstrapRouteAdopt:
		bs.Route = BootstrapRouteAdopt
	case BootstrapRouteClassic, BootstrapRouteResume:
		// Classic leaves bs.Route unset — matches prior behaviour
		// (downstream treats missing route as classic). Resume cannot
		// reach this switch: BootstrapStartWithRoute rejects it earlier.
	}

	state.Bootstrap = bs

	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap start save: %w", err)
	}
	return bs.BuildResponse(state.SessionID, intent, state.Iteration, e.environment, e.knowledge), nil
}

// resolveRecipeMatch looks up the canonical import YAML for the slug the LLM
// picked. Returns error when the slug is unknown — guards against typos or
// stale LLM context pointing at a retired recipe.
func (e *Engine) resolveRecipeMatch(slug string) (*RecipeMatch, error) {
	if e.recipeCorpus == nil {
		return nil, fmt.Errorf("recipe route unavailable: recipe corpus not configured")
	}
	cand := e.recipeCorpus.LookupRecipe(slug)
	if cand == nil {
		return nil, fmt.Errorf("recipe route: unknown slug %q", slug)
	}
	mode, _ := InferRecipeShape(cand.ImportYAML)
	return &RecipeMatch{
		Slug:        slug,
		Title:       cand.Title,
		Description: cand.Description,
		Confidence:  1.0, // LLM picked explicitly — no longer a score
		ImportYAML:  cand.ImportYAML,
		Mode:        mode,
	}, nil
}

// buildDiscoveryMessage is the human-readable summary included in the
// discovery response. Enumerates options inline so a client without atom
// rendering still gets an intelligible payload. Leads with the two-call
// pattern (kind="route-menu" now → re-call start with route to get
// kind="session-active") because eval retros showed agents misreading the
// first response as a successful start when no SessionID came back.
func buildDiscoveryMessage(options []BootstrapRouteOption) string {
	var sb strings.Builder
	sb.WriteString(`This is the route-menu phase (kind="route-menu") — NO session is open yet. Pick one option and call `)
	sb.WriteString(`zerops_workflow action="start" workflow="bootstrap" route="<route>"`)
	sb.WriteString(" again to commit the route and open the session (kind=\"session-active\"). ")
	sb.WriteString("Recipe requires `recipeSlug`; resume requires existing session via `action=\"resume\"`.\n\nOptions:\n")
	for i, opt := range options {
		fmt.Fprintf(&sb, "  %d. route=%q", i+1, string(opt.Route))
		if opt.RecipeSlug != "" {
			fmt.Fprintf(&sb, " recipeSlug=%q (confidence %.2f)", opt.RecipeSlug, opt.Confidence)
		}
		sb.WriteString(" — ")
		sb.WriteString(opt.Why)
		if len(opt.Collisions) > 0 {
			fmt.Fprintf(&sb, " [hostname collisions: %s]", strings.Join(opt.Collisions, ", "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// BootstrapComplete completes the current step and advances to the next.
// Step advancement depends on checker results, not attestation content.
// If checker is nil or passes, step advances. If checker fails, step stays
// and the agent receives the failure details to fix and retry.
//
// The checker may call back into Engine methods (e.g. StoreDiscoveredEnvVars
// during provision); that's safe because Engine holds no mutex. State
// mutations after the checker pick up any writes the checker made via the
// on-disk session file reload pattern used elsewhere in this package.
func (e *Engine) BootstrapComplete(ctx context.Context, stepName string, attestation string, checker StepChecker) (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap complete: bootstrap not active")
	}

	// Defense-in-depth: non-discover steps require a plan from the discover step.
	if stepName != StepDiscover && state.Bootstrap.Plan == nil {
		return nil, fmt.Errorf("bootstrap complete: step %q requires plan from discover step", stepName)
	}

	var checkResult *StepCheckResult
	if checker != nil {
		result, checkErr := checker(ctx, state.Bootstrap.Plan, state.Bootstrap)
		if checkErr != nil {
			return nil, fmt.Errorf("step check: %w", checkErr)
		}
		if result != nil && !result.Passed {
			// Reload: checker may have persisted discovered env vars.
			state, err = e.loadState()
			if err != nil {
				return nil, fmt.Errorf("bootstrap complete reload: %w", err)
			}
			resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
			resp.CheckResult = result
			resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", stepName, result.Summary)
			return resp, nil
		}
		checkResult = result

		// Reload after successful check — checker may have persisted env vars.
		state, err = e.loadState()
		if err != nil {
			return nil, fmt.Errorf("bootstrap complete reload: %w", err)
		}
	}

	if err := state.Bootstrap.CompleteStep(stepName, attestation); err != nil {
		return nil, fmt.Errorf("bootstrap complete: %w", err)
	}

	// Write partial metas after provision (no BootstrappedAt = incomplete).
	if stepName == StepProvision {
		e.writeProvisionMetas(state)

		// Fast path: pure adoption plans (all isExisting + all EXISTS deps)
		// skip the administrative close step — adopted services already carry
		// complete metas, so there's nothing left to administratively transition.
		if state.Bootstrap.Plan != nil && state.Bootstrap.Plan.IsAllExisting() {
			if err := state.Bootstrap.SkipStep(StepClose, "all targets adopted"); err != nil {
				return nil, fmt.Errorf("adoption fast path: %w", err)
			}
		}
	}

	sessionID := e.sessionID // capture before outputs may reference it

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	var cleanupErr error
	if !state.Bootstrap.Active {
		e.writeBootstrapOutputs(state)
		cleanupErr = ResetSessionByID(e.stateDir, state.SessionID)
		if cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "zcp: cleanup completed session: %v\n", cleanupErr)
		}
		e.completedState = state
		e.clearSessionID()
	} else if err := saveSessionState(e.stateDir, sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete save: %w", err)
	}
	resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
	resp.CheckResult = checkResult
	if cleanupErr != nil {
		resp.Message += "\n\nWarning: session cleanup failed: " + cleanupErr.Error()
	}
	return resp, nil
}

// BootstrapCompletePlan validates a structured plan, completes the "plan" step, and stores it.
func (e *Engine) BootstrapCompletePlan(targets []BootstrapTarget, liveTypes []platform.ServiceStackType, liveServices []platform.ServiceStack) (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap complete plan: bootstrap not active")
	}
	if state.Bootstrap.CurrentStepName() != StepDiscover {
		return nil, fmt.Errorf("bootstrap complete plan: current step is %q, not %q", state.Bootstrap.CurrentStepName(), StepDiscover)
	}

	defaulted, err := ValidateBootstrapTargets(targets, liveTypes, liveServices)
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}

	// Recipe route: enforce the recipe's intended mode. Otherwise an agent can
	// accept a standard-mode recipe but submit a simple-mode plan, silently
	// bypassing the mode-specific rules the recipe's provision atoms don't ship.
	if state.Bootstrap.Route == BootstrapRouteRecipe {
		if err := ValidateBootstrapRecipeMode(state.Bootstrap.RecipeMatch, targets); err != nil {
			return nil, fmt.Errorf("bootstrap complete plan: %w", err)
		}
		// Recipe override pre-flight: confirm the plan shape (renamed runtime
		// hostnames, managed-dep resolution choices) can produce a valid
		// rewritten import YAML before we commit the plan. Rejecting here
		// gives the agent a precise diagnostic at plan-submit time rather
		// than an opaque failure at provision.
		if state.Bootstrap.RecipeMatch != nil && state.Bootstrap.RecipeMatch.ImportYAML != "" {
			probe := &ServicePlan{Targets: targets}
			if _, err := RewriteRecipeImportYAML(state.Bootstrap.RecipeMatch.ImportYAML, probe); err != nil {
				return nil, fmt.Errorf("bootstrap complete plan: %w", err)
			}
		}
	}

	// Per-hostname lock: reject if any target hostname has an incomplete meta
	// from a DIFFERENT session that is still alive. Orphaned metas (dead session)
	// are safe to overwrite — the new bootstrap takes ownership.
	if err := e.checkHostnameLocks(targets); err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}

	defaultedSet := make(map[string]bool, len(defaulted))
	for _, h := range defaulted {
		defaultedSet[h] = true
	}
	var parts []string
	for _, target := range targets {
		entry := target.Runtime.DevHostname + " (" + target.Runtime.Type + ")"
		parts = append(parts, entry)
		for _, dep := range target.Dependencies {
			depEntry := dep.Hostname + " (" + dep.Type
			if dep.Mode != "" {
				depEntry += ", " + dep.Mode
				if defaultedSet[dep.Hostname] {
					depEntry += " [defaulted]"
				}
			}
			depEntry += ")"
			parts = append(parts, depEntry)
		}
	}
	attestation := "Planned targets: " + strings.Join(parts, ", ")

	if err := state.Bootstrap.CompleteStep(StepDiscover, attestation); err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}

	state.Bootstrap.Plan = &ServicePlan{
		Targets:   targets,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete plan save: %w", err)
	}

	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge), nil
}

// BootstrapSkip skips the current step and returns the next.
func (e *Engine) BootstrapSkip(stepName, reason string) (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap skip: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap skip: bootstrap not active")
	}

	if err := state.Bootstrap.SkipStep(stepName, reason); err != nil {
		return nil, fmt.Errorf("bootstrap skip: %w", err)
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap skip save: %w", err)
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge), nil
}

// StoreDiscoveredEnvVars saves discovered env var names for a service hostname.
func (e *Engine) StoreDiscoveredEnvVars(hostname string, vars []string) error {
	state, err := e.loadState()
	if err != nil {
		return fmt.Errorf("store discovered env vars: %w", err)
	}
	if state.Bootstrap == nil {
		return fmt.Errorf("store discovered env vars: no bootstrap state")
	}
	if state.Bootstrap.DiscoveredEnvVars == nil {
		state.Bootstrap.DiscoveredEnvVars = make(map[string][]string)
	}
	state.Bootstrap.DiscoveredEnvVars[hostname] = vars

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveSessionState(e.stateDir, e.sessionID, state)
}

// BootstrapStatus returns the current bootstrap progress with full guidance for context recovery.
func (e *Engine) BootstrapStatus() (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap status: %w", err)
	}
	if state.Bootstrap == nil {
		return nil, fmt.Errorf("bootstrap status: no bootstrap state")
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge), nil
}

// Resume takes over an abandoned session (dead PID) by updating PID to current process.
// Idempotent: if this engine already owns the session (e.g. auto-recovered at startup),
// returns the current state without error.
func (e *Engine) Resume(sessionID string) (*WorkflowState, error) {
	if e.sessionID == sessionID {
		return LoadSessionByID(e.stateDir, sessionID)
	}
	state, err := LoadSessionByID(e.stateDir, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	if isProcessAlive(state.PID) {
		return nil, fmt.Errorf("resume: session %s still active (PID %d)", sessionID, state.PID)
	}
	if err := e.claimSession(sessionID, state); err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	return state, nil
}

// checkHostnameLocks verifies that no target hostname is locked by another active session.
// A hostname is locked when it has an incomplete ServiceMeta from a different session
// whose process is still alive. Orphaned metas (dead/missing session) are unlocked.
func (e *Engine) checkHostnameLocks(targets []BootstrapTarget) error {
	sessions, _ := ListSessions(e.stateDir)
	sessionPIDs := make(map[string]int, len(sessions))
	for _, s := range sessions {
		sessionPIDs[s.SessionID] = s.PID
	}

	for _, target := range targets {
		hostnames := []string{target.Runtime.DevHostname}
		if stage := target.Runtime.StageHostname(); stage != "" {
			hostnames = append(hostnames, stage)
		}
		for _, hostname := range hostnames {
			meta, err := ReadServiceMeta(e.stateDir, hostname)
			if err != nil || meta == nil {
				continue // no meta = no lock
			}
			if meta.IsComplete() {
				continue // completed = not locked
			}
			if meta.BootstrapSession == e.sessionID {
				continue // our own session = not locked
			}
			// Incomplete meta from another session — check if alive.
			pid, inRegistry := sessionPIDs[meta.BootstrapSession]
			if inRegistry && isProcessAlive(pid) {
				return fmt.Errorf("service %q is being bootstrapped by session %s (PID %d) — finish or reset that session first",
					hostname, meta.BootstrapSession, pid)
			}
			// Dead/missing session = orphaned meta, safe to overwrite.
		}
	}
	return nil
}

// loadState loads state for the current session.
func (e *Engine) loadState() (*WorkflowState, error) {
	if e.sessionID == "" {
		if e.completedState != nil {
			return e.completedState, nil
		}
		return nil, fmt.Errorf("no active session")
	}
	return LoadSessionByID(e.stateDir, e.sessionID)
}

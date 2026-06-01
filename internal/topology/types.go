package topology

// RuntimeClass partitions services by how deploy + start behaviour differ.
type RuntimeClass string

const (
	RuntimeDynamic     RuntimeClass = "dynamic"
	RuntimeStatic      RuntimeClass = "static"
	RuntimeImplicitWeb RuntimeClass = "implicit-webserver"
	RuntimeManaged     RuntimeClass = "managed"
	RuntimeUnknown     RuntimeClass = "unknown"
)

// Mode is the bootstrapped service's deploy mode in envelope terms.
// Distinct from the untyped-string meta.Mode ("dev", "standard", "simple"):
// envelope splits the dev half of a standard pair (ModeStandard) from its
// stage half (ModeStage) so atoms can target one role without matching the
// other. Dev-only services get ModeDev; simple-mode single services get
// ModeSimple. ModeLocalStage / ModeLocalOnly cover local-machine topologies.
type Mode string

const (
	ModeDev        Mode = "dev"
	ModeStandard   Mode = "standard"
	ModeStage      Mode = "stage"
	ModeSimple     Mode = "simple"
	ModeLocalStage Mode = "local-stage"
	ModeLocalOnly  Mode = "local-only"
)

// CloseDeployMode is the per-pair developer choice for what the develop
// workflow auto-does at close. One of three orthogonal dimensions of the
// deploy model — see plan
// `plans/archive/deploy-strategy-decomposition-2026-04-28.md` §3.1 for
// the full orthogonality matrix vs GitPushState (whether push capability
// is set up) and BuildIntegration (whether ZCP-managed CI is wired).
type CloseDeployMode string

const (
	// CloseModeUnset is the sentinel for services that have not yet
	// chosen a close-mode. Bootstrapped services start here; the develop
	// workflow surfaces a choice atom.
	CloseModeUnset CloseDeployMode = "unset"
	// CloseModeAuto means develop close auto-runs zcli push direct to the
	// dev half (the AttemptInfo.Strategy="zcli" mechanism). Auto-close fires
	// on deploy+verify success per scope.
	CloseModeAuto CloseDeployMode = "auto"
	// CloseModeGitPush means develop close auto-commits + pushes to the
	// configured remote. Build trigger is BuildIntegration's concern
	// (none/webhook/actions). Auto-close fires on push success.
	CloseModeGitPush CloseDeployMode = "git-push"
	// CloseModeManual means ZCP yields close orchestration to the user.
	// Tools remain callable; auto-close DOES NOT fire (gated by
	// CloseDeployMode ∈ {auto, git-push}).
	CloseModeManual CloseDeployMode = "manual"
)

// GitPushState is the per-pair record of whether git-push capability is
// set up. Orthogonal to CloseDeployMode — a service can have GitPush
// configured even when CloseDeployMode=auto (for ad-hoc release pushes
// that still fire BuildIntegration on the remote).
type GitPushState string

const (
	// GitPushUnconfigured is the default — no probe ever ran. Setup
	// has not been attempted, or a previously-configured state was
	// reset.
	GitPushUnconfigured GitPushState = "unconfigured"
	// GitPushConfigured means the last git-push-setup probe succeeded
	// end-to-end: the supplied token authenticated against the remote
	// URL, project env carries GIT_TOKEN (sensitive), and `origin` is
	// synced in the working tree's git config. Ready for git-push
	// close-mode and BuildIntegration setup.
	GitPushConfigured GitPushState = "configured"
	// GitPushBroken means a previously-configured state hit a credential
	// failure during git push — most likely cause is upstream PAT
	// rotation/revocation. Recovery is `zerops_workflow action=
	// git-push-setup` with a fresh PAT (the handler probes the token
	// before writing project state).
	GitPushBroken GitPushState = "broken"
)

// BuildIntegration is the per-pair record of which ZCP-managed CI
// integration responds to git pushes hitting the remote. UTILITY framing:
// ZCP wires these specific integrations; users may keep independent CI/CD
// that ZCP doesn't track, so BuildIntegration=none does NOT mean "no
// build will fire" — it means "no ZCP-managed integration is configured".
//
// Prerequisite: GitPushState == GitPushConfigured. Build integration
// without git-push capability is incoherent — the integration fires on
// git pushes only. Setup atoms surface this as a chained prereq.
type BuildIntegration string

const (
	// BuildIntegrationNone means ZCP has not configured any build
	// integration on the remote. The user may still have their own CI/CD.
	BuildIntegrationNone BuildIntegration = "none"
	// BuildIntegrationWebhook means the Zerops dashboard OAuth webhook
	// is wired — Zerops pulls and builds on git push.
	BuildIntegrationWebhook BuildIntegration = "webhook"
	// BuildIntegrationActions means a GitHub Actions workflow runs
	// `zcli push` from CI on git push. Mechanically push-dev (ZCP-side)
	// triggered by the user's CI, not Zerops pulling.
	BuildIntegrationActions BuildIntegration = "actions"
)

// ExportStatus discriminates the sub-states of `PhaseExportActive`. The
// export workflow is a stateless multi-call narrowing (probe → generate →
// publish); each call returns one of these statuses on the response
// envelope plus the matching atom guidance. Atoms filter on this axis
// via `exportStatus:` frontmatter so a single phase carries its
// distinct sub-renders without overmatch.
//
// Lives in topology (not workflow) because tools/workflow_export.go owns
// the handler-side status emission and consumes the typed enum to call
// BuildExportEnvelope.
type ExportStatus string

const (
	// ExportStatusUnset is the zero-value sentinel — the envelope was
	// constructed without an export status (e.g., non-export phases).
	ExportStatusUnset ExportStatus = ""
	// ExportStatusScopePrompt fires on the first export call with no
	// targetService selected. The handler returns the project's runtime
	// list and the agent picks one. Service-scoped axes MUST NOT be used
	// on atoms that match this status — target is unknown, so per-service
	// filtering has no anchor.
	ExportStatusScopePrompt ExportStatus = "scope-prompt"
	// ExportStatusScaffoldRequired fires when the chosen runtime
	// container's /var/www/zerops.yaml is missing or empty. Bundle
	// composition is impossible without a setup block to reference at
	// re-import. The agent emits a minimal yaml, commits, and re-calls.
	ExportStatusScaffoldRequired ExportStatus = "scaffold-required"
	// ExportStatusGitPushSetupRequired fires when meta.GitPushState !=
	// configured (or git remote is empty in /var/www). Phase C cannot
	// publish until git-push capability is provisioned. The response
	// chains to setup-git-push-container.
	ExportStatusGitPushSetupRequired ExportStatus = "git-push-setup-required"
	// ExportStatusClassifyPrompt fires when project envs are present but
	// EnvClassifications is incomplete. The handler returns the per-env
	// review table for the agent to bucket each env (infrastructure /
	// auto-secret / external-secret / plain-config).
	ExportStatusClassifyPrompt ExportStatus = "classify-prompt"
	// ExportStatusValidationFailed fires when bundle.Errors is non-empty
	// after Phase 5 schema validation. The agent inspects each error's
	// path + message and re-calls after fixing the offending field.
	ExportStatusValidationFailed ExportStatus = "validation-failed"
	// ExportStatusPublishReady is the Phase C success state: bundle
	// composed, classifications accepted, GitPushState=configured, schema
	// clean. The agent writes the YAMLs, commits, and pushes via
	// zerops_deploy strategy="git-push".
	ExportStatusPublishReady ExportStatus = "publish-ready"
)

// SecretClassification buckets project envVariables and zerops.yaml
// run.envVariables references into the four-category protocol per plan
// §3.4. The agent classifies each env via grep + zerops.yaml provenance +
// framework-convention reasoning, then surfaces the result in a per-env
// review table during Phase B (mandatory user gate before publish).
//
// ZCP's Go code stays out of classification heuristics — bucketing is
// LLM-driven. The enum exists so the per-request input
// WorkflowInput.EnvClassifications carries a typed map[hostname]bucket
// across handler calls without ad-hoc string parsing at every consumer.
type SecretClassification string

const (
	// SecretClassUnset is the zero-value sentinel — the env has not yet
	// been classified. The handler treats unset as "request classification"
	// in Phase B and emits the per-env review table for the agent to fill.
	SecretClassUnset SecretClassification = ""
	// SecretClassInfrastructure means the value (or a component thereof)
	// resolves to a managed-service-emitted reference (`${db_*}`,
	// `${redis_*}`, plus documented service-specific prefixes) or an
	// app-built compound URL assembled from such references. Drops from
	// import.yaml's project.envVariables; keeps the `${...}` reference
	// in zerops.yaml so re-imported managed services emit fresh values.
	SecretClassInfrastructure SecretClassification = "infrastructure"
	// SecretClassAutoSecret means the source (or framework convention)
	// uses the var as a local encryption / signing key. Includes Laravel
	// APP_KEY, Django SECRET_KEY, Rails SECRET_KEY_BASE, Express
	// session/JWT secrets — even when the encryption call lives inside
	// the framework. Emits as `<@generateRandomString(<32>)>`
	// in import.yaml; the atom must warn before regenerating when state,
	// cookies, sessions, or test fixtures depend on the old value.
	SecretClassAutoSecret SecretClassification = "auto-secret"
	// SecretClassExternalSecret means the source calls a third-party SDK
	// (Stripe, OpenAI, GitHub, Mailgun) using the var, including aliased
	// imports and webhook verification secrets. Emits as a comment +
	// `<@pickRandom(["REPLACE_ME"])>` placeholder. Empty / sentinel live
	// values (`STRIPE_SECRET=`, `disabled`, `test_xxx`, `sk_test_*`) are
	// review-required — do NOT blindly substitute REPLACE_ME for an
	// empty staging key.
	SecretClassExternalSecret SecretClassification = "external-secret"
	// SecretClassPlainConfig means the source uses the var as literal
	// runtime config (LOG_LEVEL, NODE_ENV, FEATURE_FLAGS). Emits the
	// literal value verbatim. Privacy-sensitive literals (real emails,
	// customer names, internal domain/webhook URLs, sender identities)
	// must be flagged for user review before verbatim emission.
	SecretClassPlainConfig SecretClassification = "plain-config"
)

// LaunchProductionStatus discriminates the sub-states of
// `PhaseLaunchProductionActive`. The launch-production workflow is a
// six-state machine: three read-side narrowing states (no mutation, no
// launch key needed) plus three mutation states (require a one-shot key
// from user input). Atoms filter via `launchStatus:` axis when ready.
//
// Lives in topology (not workflow) because tools/workflow_launch_production.go
// owns handler-side status emission and consumes the typed enum. Mirrors
// ExportStatus placement.
type LaunchProductionStatus string

const (
	// LaunchStatusUnset is the zero-value sentinel.
	LaunchStatusUnset LaunchProductionStatus = ""
	// LaunchStatusScopePrompt fires when launch inputs are incomplete
	// (productionProjectName, region, or other scope fields missing).
	LaunchStatusScopePrompt LaunchProductionStatus = "scope-prompt"
	// LaunchStatusSourceControlRequired fires when scope is complete but
	// the source-side prerequisites for promotion are not all in place:
	// the chosen runtime lacks ServiceMeta.GitPushState=configured (no
	// user-owned remote wired), live `/var/www` origin diverges from the
	// recorded RemoteURL (manual rewrite or recipe-template carryover),
	// or build-integration on the source pair is recommended but absent.
	// Stateless recoverable status — no state file written. Response
	// carries one blocker per failing check with structured Recovery
	// pointing at the existing `git-push-setup` / `zerops_deploy
	// strategy=git-push` / `build-integration` actions. Agent resolves
	// blockers shop-down with a re-call between each. Pinned by the
	// source-control-gate tests.
	LaunchStatusSourceControlRequired LaunchProductionStatus = "source-control-required"
	// LaunchStatusExistingProjectConflictPrompt fires on the existing-
	// project launch path (ExistingProjectID + ExistingProdToken
	// supplied) when the target project already has services whose
	// hostnames collide with what the launch bundle would create.
	// Stateless recoverable status — no state file written. Response
	// surfaces one conflict entry per colliding hostname; agent asks
	// the user per-promotable: skip (additive — promote only non-
	// colliding) or replace (requires confirmDestructive ack +
	// destructive intent — diagnose-before-destruct invariant extends
	// here). Agent re-calls with mergeStrategy=[host:skip|replace] +
	// confirmDestructive on the replace path. Pinned by
	// TestHandleLaunchProduction_ExistingProjectConflict_*.
	LaunchStatusExistingProjectConflictPrompt LaunchProductionStatus = "existing-project-conflict-prompt"
	// LaunchStatusClassifyPrompt fires when source project envs are
	// present but EnvClassifications is incomplete — agent must bucket
	// each env per the existing four-bucket protocol.
	LaunchStatusClassifyPrompt LaunchProductionStatus = "classify-prompt"
	// LaunchStatusReadyToLaunch fires when scope + classifications are
	// complete, bundle composed, source-control changes pushed, schema
	// clean. Response is preview-only — no mutation yet. Awaits the
	// one-shot launchKey to advance to LaunchStatusLaunching.
	LaunchStatusReadyToLaunch LaunchProductionStatus = "ready-to-launch"
	// LaunchStatusLaunching is the mutation pipeline in flight: the
	// handler holds a ProjectAdminClient backed by the launchKey and
	// is calling CreateAndImportProject + polling first deploy +
	// verifying secret presence.
	LaunchStatusLaunching LaunchProductionStatus = "launching"
	// LaunchStatusConfiguringPipeline fires after the mutation pipeline
	// completes (prod project exists + first deploy ran) and before the
	// terminal LaunchStatusLaunched. Per runtime service with buildFromGit,
	// the handler reads integration status. Not-configured runtimes
	// produce blocker[pipeline-not-configured] with a dashboard deep-link
	// and recommendation payload; user configures via Zerops UI then
	// re-runs the workflow with the same launchKey to recheck. Path B
	// design per docs/spec-launch-production-platform-spike.md §B and
	// plans/production-lifecycle-part2-2026-05-12.md.
	LaunchStatusConfiguringPipeline LaunchProductionStatus = "configuring-pipeline"
	// LaunchStatusFailed indicates a mutation step failed. Structured
	// blockers[] describes recovery; the agent reads them and either
	// retries with a fresh launchKey or aborts. Pipeline-config issues
	// never resolve to this state (P-LP-8) — they live as blockers on
	// LaunchStatusLaunched.
	LaunchStatusFailed LaunchProductionStatus = "failed"
	// LaunchStatusLaunched is the terminal success state. Response
	// carries the post-launch checklist + mandatory key-deletion atom
	// (P-LP-4 invariant). May include blockers[] for runtimes still
	// needing pipeline-config (P-LP-8).
	LaunchStatusLaunched LaunchProductionStatus = "launched"
)

// BlockerSeverity ranks Blocker entries returned by the launch workflow.
//
//   - BlockerSeverityBlock — must clear before the workflow advances.
//   - BlockerSeverityWarn  — informational; advances may proceed.
type BlockerSeverity string

const (
	BlockerSeverityBlock BlockerSeverity = "block"
	BlockerSeverityWarn  BlockerSeverity = "warn"
)

// BlockerCategory taxonomy for launch workflow blockers.
type BlockerCategory string

const (
	BlockerCategoryScope          BlockerCategory = "scope"           // missing/invalid scope input
	BlockerCategorySchema         BlockerCategory = "schema"          // import.yaml schema-validation failure
	BlockerCategorySourceControl  BlockerCategory = "source-control"  // unpushed setup: prod block
	BlockerCategorySourceDrift    BlockerCategory = "source-drift"    // P-LP-3 immutability guard fired
	BlockerCategoryExternalSecret BlockerCategory = "external-secret" // user must set in Zerops UI
	BlockerCategoryCost           BlockerCategory = "cost"            // estimated cost ack pending
	BlockerCategoryHealthCheck    BlockerCategory = "healthcheck"     // run.healthCheck missing in source
	BlockerCategoryOrphan         BlockerCategory = "orphan-project"  // target project created but import failed
	BlockerCategoryAuth           BlockerCategory = "auth"            // launchKey invalid / missing permission
	BlockerCategoryOther          BlockerCategory = "other"
)

// Blocker carries one launch-workflow obstruction the agent must surface
// to the user (and, where applicable, resolve before advancing).
type Blocker struct {
	ID       string          `json:"id"`
	Severity BlockerSeverity `json:"severity"`
	Category BlockerCategory `json:"category"`
	Message  string          `json:"message"`
	// Recovery is optional structured guidance — when set, names the tool
	// + action + args the agent invokes to clear this blocker. Mirrors
	// the existing Recovery hint shape on CheckResult.
	Recovery *Recovery `json:"recovery,omitempty"`
	// Suggestion is the human-readable actionable summary derived from
	// upstream platform error detail (e.g. expanded apiMeta field
	// rejections). Populated when the blocker originated from a typed
	// platform.PlatformError so the agent sees what to fix without
	// parsing APIMeta manually.
	Suggestion string `json:"suggestion,omitempty"`
	// APICode is the raw Zerops API error code when the blocker came
	// from an upstream API call (e.g. "errorList",
	// "projectImportInvalidParameter"). Empty for blockers ZCP synthesizes
	// locally.
	APICode string `json:"apiCode,omitempty"`
	// APIMeta is the structured field-level detail from the platform
	// (one entry per failing field). Surfaces the same data the
	// platform sends, so callers/agents can act on specific field
	// rejections instead of inferring from the message.
	APIMeta []APIMetaItem `json:"apiMeta,omitempty"`
}

// APIMetaItem mirrors one element of the Zerops API's error.meta[]
// array. Layer-2 promotion of platform.APIMetaItem so Blocker can
// expose it without an upward dependency.
type APIMetaItem struct {
	Code     string              `json:"code,omitempty"`
	Error    string              `json:"error,omitempty"`
	Metadata map[string][]string `json:"metadata,omitempty"`
}

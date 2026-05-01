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
	// GitPushUnconfigured is the default — no push capability exists.
	GitPushUnconfigured GitPushState = "unconfigured"
	// GitPushConfigured means GIT_TOKEN/.netrc/credentials are set up
	// and the remote URL is known. Ready for git-push close-mode and/or
	// BuildIntegration setup.
	GitPushConfigured GitPushState = "configured"
	// GitPushBroken means setup was attempted but produced damaged
	// artifacts (.netrc partial, GIT_TOKEN expired, etc.). Recovery is
	// explicit re-setup; ZCP does not auto-probe.
	GitPushBroken GitPushState = "broken"
	// GitPushUnknown means setup was previously claimed but the
	// capability needs a probe before the next push to confirm it
	// still works (token rotated, .netrc rewritten externally, etc.).
	GitPushUnknown GitPushState = "unknown"
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

// ExportVariant selects which half of a pair the export workflow packages
// into a self-referential single-repo bundle (zerops-project-import.yaml +
// zerops.yaml + code). Only meaningful for ModeStandard / ModeLocalStage —
// other modes have a single half so the variant is forced (dev for
// ModeDev, simple for ModeSimple, etc.). The agent passes the chosen
// variant on the second call to zerops_workflow workflow="export"; the
// first call returns a variant-prompt atom.
//
// See plan `plans/export-buildfromgit-2026-04-28.md` §3.2 for the mode
// matrix and §3.3 for the post-import mode mapping (dev half re-imports
// as ModeDev; stage half re-imports as ModeSimple per decision Q7-β).
type ExportVariant string

const (
	// ExportVariantUnset is the zero-value sentinel — the agent has not
	// yet committed to a variant on this call. The handler returns the
	// variant-prompt atom for ModeStandard / ModeLocalStage; for other
	// modes the variant is forced and unset is never observed past the
	// first handler invocation.
	ExportVariantUnset ExportVariant = ""
	// ExportVariantDev packages the dev half of the pair. Re-imports as
	// ModeDev (preserves "this was our dev environment" intent).
	ExportVariantDev ExportVariant = "dev"
	// ExportVariantStage packages the stage half of the pair. Re-imports
	// as ModeSimple — there is no dev to cross-deploy from in the new
	// project, so the pair collapses cleanly to a standalone.
	ExportVariantStage ExportVariant = "stage"
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

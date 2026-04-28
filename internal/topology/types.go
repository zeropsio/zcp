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
	// dev half — current "push-dev" mechanics. Auto-close fires on
	// deploy+verify success per scope.
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

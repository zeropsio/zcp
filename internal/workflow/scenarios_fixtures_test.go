package workflow

// Canonical scenario envelope fixtures for atom-corpus-verification
// Phase 1 (plans/atom-corpus-verification-2026-05-02.md). Each fixture
// pins an envelope shape that drives one rendered golden file at
// testdata/atom-goldens/<scenario-id>.md.
//
// Service order is behavior — fixtures deliberately set
// `Services []ServiceSnapshot` and `WorkSession.Services` order to
// match the golden's expected render order. compute_envelope.go sorts
// services by hostname; build_plan.go iterates work-session order.
// Disagreement between fixture order and intended render is a fixture
// bug, not a synthesizer bug.
//
// New scenarios append at the bottom and add a row to the table in the
// plan's "30 canonical scenarios" section. Removed scenarios drop the
// fixture, the golden file, and the plan-table row in one commit.

import (
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// canonicalGoldenScenarios returns the 30 scenarios pinned by the
// atom-corpus-verification plan. The list is composed from per-phase
// helpers (idle/bootstrap/develop/strategy-setup/export) so the
// per-phase fixtures stay grouped and the parent function stays
// readable. Defined as a function (not a package-level slice) so the
// time-seeded fixtures below stay test-time only and don't leak into
// LoadAtomCorpus' init path.
func canonicalGoldenScenarios() []goldenScenario {
	out := make([]goldenScenario, 0, 30)
	out = append(out, idleGoldenScenarios()...)
	out = append(out, bootstrapGoldenScenarios()...)
	out = append(out, developGoldenScenarios()...)
	out = append(out, strategySetupGoldenScenarios()...)
	out = append(out, exportGoldenScenarios()...)
	return out
}

// idleGoldenScenarios returns the 4 scenarios that pin atoms firing
// when no active workflow session exists (PhaseIdle).
func idleGoldenScenarios() []goldenScenario {
	return []goldenScenario{
		{
			id:          "idle/empty",
			description: "Fresh project, no services bootstrapped or adopted yet.",
			envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleEmpty,
			},
		},
		{
			id:          "idle/bootstrapped-with-managed",
			description: "Idle project with one runtime + one managed dep, both bootstrapped and deployed.",
			envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleBootstrapped,
				Services: []ServiceSnapshot{
					fixSnapBootstrappedDeployed("appdev", "nodejs@22", topology.RuntimeDynamic, topology.ModeDev),
					fixSnapManaged("db", "postgresql@16"),
				},
			},
		},
		{
			id:          "idle/adopt-only",
			description: "Idle project with one unmanaged runtime — eligible for adoption.",
			envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleAdopt,
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Status:       "ACTIVE",
					},
				},
			},
		},
		{
			id:          "idle/incomplete-resume",
			description: "Idle project with one resumable runtime — bootstrap session interrupted before completion.",
			envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleIncomplete,
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Resumable:    true,
					},
				},
			},
		},
	}
}

// bootstrapGoldenScenarios returns the 5 scenarios pinning atoms that
// fire across the recipe / classic / adopt routes during PhaseBootstrap
// Active.
func bootstrapGoldenScenarios() []goldenScenario {
	return []goldenScenario{
		{
			id:          "bootstrap/recipe/provision",
			description: "Recipe route, provision step in progress, target service ACTIVE awaiting first deploy.",
			envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Bootstrap: &BootstrapSessionSummary{
					Route: BootstrapRouteRecipe,
					Step:  StepProvision,
				},
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Status:       "ACTIVE",
						Bootstrapped: true,
					},
				},
			},
		},
		{
			id:          "bootstrap/recipe/close",
			description: "Recipe route, close step — bootstrap finishing, agent prompted for handoff to develop.",
			envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Bootstrap: &BootstrapSessionSummary{
					Route: BootstrapRouteRecipe,
					Step:  StepClose,
				},
				Services: []ServiceSnapshot{
					fixSnapBootstrappedDeployed("appdev", "nodejs@22", topology.RuntimeDynamic, topology.ModeDev),
				},
			},
		},
		{
			id:          "bootstrap/classic/discover-standard-dynamic",
			description: "Classic route, discover step — agent inspecting an empty project for a dynamic runtime in mode=standard.",
			envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Bootstrap: &BootstrapSessionSummary{
					Route: BootstrapRouteClassic,
					Step:  StepDiscover,
				},
			},
		},
		{
			id:          "bootstrap/classic/provision-local",
			description: "Classic route, provision step on a local-machine env (no Zerops container).",
			envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvLocal,
				Bootstrap: &BootstrapSessionSummary{
					Route: BootstrapRouteClassic,
					Step:  StepProvision,
				},
			},
		},
		{
			id:          "bootstrap/adopt/discover-existing-pair",
			description: "Adopt route, discover step — pre-existing dev/stage pair present in the project, agent adopting.",
			envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Bootstrap: &BootstrapSessionSummary{
					Route: BootstrapRouteAdopt,
					Step:  StepDiscover,
				},
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Status:       "ACTIVE",
					},
					{
						Hostname:     "appstage",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Status:       "ACTIVE",
					},
				},
			},
		},
	}
}

// developGoldenScenarios returns the 12 scenarios pinning atoms across
// the develop-active and develop-closed-auto phases — first-deploy,
// steady-state, pair shapes, git-push variants, failure-tier, scope
// narrowing, and closure reasons.
func developGoldenScenarios() []goldenScenario {
	return []goldenScenario{
		{
			id:          "develop/first-deploy-dev-dynamic-container",
			description: "develop-active, dev mode, never-deployed dynamic runtime, in-container.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					fixSnapBootstrappedNeverDeployed("appdev", "nodejs@22", topology.RuntimeDynamic, topology.ModeDev),
				},
				WorkSession: fixSession("appdev"),
			},
		},
		{
			id:          "develop/first-deploy-recipe-implicit-standard",
			description: "develop-active, mode=standard pair, php-nginx implicit-webserver runtime + db, never-deployed; bootstrap arrived via recipe route.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: append(
					fixSnapBootstrappedNeverDeployedPair("appdev", "appstage", "php-nginx@8.4", topology.RuntimeImplicitWeb),
					fixSnapManaged("db", "postgresql@16"),
				),
				WorkSession: fixSession("appdev"),
			},
		},
		{
			id:          "develop/post-adopt-standard-unset",
			description: "Adopted standard pair, both halves running, close-mode never picked.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services:    fixSnapDeployedPairUnset("appdev", "appstage", "nodejs@22", topology.RuntimeDynamic),
				WorkSession: fixSession("appdev"),
			},
		},
		{
			id:          "develop/mode-expansion-source",
			description: "Deployed dev/simple service running close-mode auto — common starter shape before agent expands to a standard pair.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					fixSnapDeployedDevAuto("appdev", "nodejs@22", topology.RuntimeDynamic),
				},
				WorkSession: fixSession("appdev"),
			},
		},
		{
			id:          "develop/steady-dev-auto-container",
			description: "Steady-state dev mode dynamic runtime, close-mode auto, deployed and active in container.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					fixSnapDeployedDevAuto("appdev", "nodejs@22", topology.RuntimeDynamic),
				},
				WorkSession: fixSession("appdev"),
			},
		},
		{
			id:          "develop/standard-auto-pair",
			description: "Standard dev+stage pair, close-mode auto on both halves, both deployed.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services:    fixSnapDeployedPairAuto("appdev", "appstage", "nodejs@22", topology.RuntimeDynamic),
				WorkSession: fixSession("appdev", "appstage"),
			},
		},
		{
			id:          "develop/git-push-configured-webhook",
			description: "Standard pair, close-mode git-push, GitPushState configured, BuildIntegration webhook.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services:    fixSnapGitPushIntegration("appdev", "appstage", "nodejs@22", topology.RuntimeDynamic, topology.GitPushConfigured, topology.BuildIntegrationWebhook),
				WorkSession: fixSession("appdev", "appstage"),
			},
		},
		{
			id:          "develop/git-push-unconfigured",
			description: "Standard pair, close-mode git-push, GitPushState unconfigured — agent must run git-push-setup before close.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services:    fixSnapGitPushIntegration("appdev", "appstage", "nodejs@22", topology.RuntimeDynamic, topology.GitPushUnconfigured, topology.BuildIntegrationNone),
				WorkSession: fixSession("appdev", "appstage"),
			},
		},
		{
			id:          "develop/failure-tier-3",
			description: "Active session, third-iteration failure history — three failed deploy attempts on appdev (close-mode auto).",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					fixSnapBootstrappedNeverDeployed("appdev", "nodejs@22", topology.RuntimeDynamic, topology.ModeDev),
				},
				WorkSession: &WorkSessionSummary{
					Intent:    "deploy",
					Services:  []string{"appdev"},
					CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
					Deploys: map[string][]AttemptInfo{
						"appdev": {
							{At: time.Date(2026, 5, 2, 10, 5, 0, 0, time.UTC), Iteration: 1, Reason: "build failed"},
							{At: time.Date(2026, 5, 2, 10, 10, 0, 0, time.UTC), Iteration: 2, Reason: "start failed"},
							{At: time.Date(2026, 5, 2, 10, 15, 0, 0, time.UTC), Iteration: 3, Reason: "verify failed"},
						},
					},
				},
			},
		},
		{
			id:          "develop/multi-service-scope-narrow",
			description: "Project has multiple runtimes; the active work session scopes to a single hostname so per-service axes only fire on that one.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					fixSnapDeployedDevAuto("appdev", "nodejs@22", topology.RuntimeDynamic),
					fixSnapDeployedDevAuto("workerdev", "nodejs@22", topology.RuntimeDynamic),
				},
				WorkSession: fixSession("appdev"),
			},
		},
		{
			id:          "develop/closed-auto-complete",
			description: "develop-closed-auto phase, close reason auto-complete (all services deployed and verified).",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopClosed,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					fixSnapDeployedDevAuto("appdev", "nodejs@22", topology.RuntimeDynamic),
				},
				WorkSession: &WorkSessionSummary{
					Intent:      "deploy",
					Services:    []string{"appdev"},
					CreatedAt:   time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC),
					ClosedAt:    fixTimePtr(2026, 5, 2, 9, 30, 0),
					CloseReason: CloseReasonAutoComplete,
				},
			},
		},
		{
			id:          "develop/closed-iteration-cap",
			description: "develop-closed-auto phase, close reason iteration-cap — workflow exhausted retry budget without success.",
			envelope: StateEnvelope{
				Phase:       PhaseDevelopClosed,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					fixSnapBootstrappedNeverDeployed("appdev", "nodejs@22", topology.RuntimeDynamic, topology.ModeDev),
				},
				WorkSession: &WorkSessionSummary{
					Intent:      "deploy",
					Services:    []string{"appdev"},
					CreatedAt:   time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC),
					ClosedAt:    fixTimePtr(2026, 5, 2, 9, 45, 0),
					CloseReason: CloseReasonIterationCap,
				},
			},
		},
	}
}

// strategySetupGoldenScenarios returns the 2 scenarios that pin
// strategy-setup-phase atoms (git-push provisioning + build-integration
// selection).
func strategySetupGoldenScenarios() []goldenScenario {
	return []goldenScenario{
		{
			id:          "strategy-setup/container-unconfigured",
			description: "strategy-setup phase, in-container, GitPushState unconfigured — agent walks through GIT_TOKEN/.netrc setup.",
			envelope: StateEnvelope{
				Phase:       PhaseStrategySetup,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{
						Hostname:         "appdev",
						TypeVersion:      "nodejs@22",
						RuntimeClass:     topology.RuntimeDynamic,
						Mode:             topology.ModeStandard,
						CloseDeployMode:  topology.CloseModeGitPush,
						GitPushState:     topology.GitPushUnconfigured,
						BuildIntegration: topology.BuildIntegrationNone,
						Bootstrapped:     true,
					},
				},
			},
		},
		{
			id:          "strategy-setup/configured-build-integration",
			description: "strategy-setup phase, GitPushState configured, BuildIntegration none — agent picks webhook vs actions.",
			envelope: StateEnvelope{
				Phase:       PhaseStrategySetup,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{
						Hostname:         "appdev",
						TypeVersion:      "nodejs@22",
						RuntimeClass:     topology.RuntimeDynamic,
						Mode:             topology.ModeStandard,
						CloseDeployMode:  topology.CloseModeGitPush,
						GitPushState:     topology.GitPushConfigured,
						BuildIntegration: topology.BuildIntegrationNone,
						Bootstrapped:     true,
						Deployed:         true,
					},
				},
			},
		},
	}
}

// exportGoldenScenarios returns the 7 scenarios pinning atoms across
// the export workflow's per-call sub-statuses (scope-prompt → variant-
// prompt → scaffold-required → git-push-setup-required → classify-
// prompt → validation-failed → publish-ready). Service shapes follow
// the audit decision in synthesize_export_audit.md (single-entry
// Services for known-target statuses, empty for scope-prompt).
func exportGoldenScenarios() []goldenScenario {
	return []goldenScenario{
		{
			id:          "export/scope-prompt",
			description: "Export workflow first call, no targetService selected — agent picks from runtimes list.",
			envelope: StateEnvelope{
				Phase:        PhaseExportActive,
				Environment:  EnvContainer,
				ExportStatus: topology.ExportStatusScopePrompt,
				// Services empty: target unknown.
			},
		},
		{
			id:          "export/variant-prompt",
			description: "Export workflow, targetService picked but Variant unset on a mode=standard pair — agent picks dev or stage half.",
			envelope: StateEnvelope{
				Phase:        PhaseExportActive,
				Environment:  EnvContainer,
				ExportStatus: topology.ExportStatusVariantPrompt,
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						Bootstrapped: true,
					},
				},
			},
		},
		{
			id:          "export/scaffold-required",
			description: "Export workflow, /var/www/zerops.yaml missing — agent must scaffold a minimal yaml first.",
			envelope: StateEnvelope{
				Phase:        PhaseExportActive,
				Environment:  EnvContainer,
				ExportStatus: topology.ExportStatusScaffoldRequired,
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						Bootstrapped: true,
					},
				},
			},
		},
		{
			id:          "export/git-push-setup-required",
			description: "Export workflow, GitPushState != configured — agent runs git-push-setup before publish.",
			envelope: StateEnvelope{
				Phase:        PhaseExportActive,
				Environment:  EnvContainer,
				ExportStatus: topology.ExportStatusGitPushSetupRequired,
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						GitPushState: topology.GitPushUnconfigured,
						Bootstrapped: true,
					},
				},
			},
		},
		{
			id:          "export/classify-prompt",
			description: "Export workflow, project envs unclassified — agent buckets each env into infrastructure/auto-secret/external-secret/plain-config.",
			envelope: StateEnvelope{
				Phase:        PhaseExportActive,
				Environment:  EnvContainer,
				ExportStatus: topology.ExportStatusClassifyPrompt,
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						GitPushState: topology.GitPushConfigured,
						Bootstrapped: true,
					},
				},
			},
		},
		{
			id:          "export/validation-failed",
			description: "Export workflow, schema validation surfaced blocking errors — agent fixes the failing field and re-calls.",
			envelope: StateEnvelope{
				Phase:        PhaseExportActive,
				Environment:  EnvContainer,
				ExportStatus: topology.ExportStatusValidationFailed,
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						GitPushState: topology.GitPushConfigured,
						Bootstrapped: true,
					},
				},
			},
		},
		{
			id:          "export/publish-ready",
			description: "Export workflow, bundle composed and validation clean — agent writes yamls, commits, pushes via git-push.",
			envelope: StateEnvelope{
				Phase:        PhaseExportActive,
				Environment:  EnvContainer,
				ExportStatus: topology.ExportStatusPublishReady,
				Services: []ServiceSnapshot{
					{
						Hostname:     "appdev",
						TypeVersion:  "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						GitPushState: topology.GitPushConfigured,
						Bootstrapped: true,
						Deployed:     true,
					},
				},
			},
		},
	}
}

// ── Fixture helpers ────────────────────────────────────────────────────
//
// Small constructors that compose ServiceSnapshot / WorkSessionSummary
// shapes used across multiple scenarios. They reduce per-scenario
// boilerplate without obscuring the per-fixture distinctions (mode,
// deploy state, close-mode, git-push state, etc.) that drive the
// rendered output.

// fixSnapBootstrappedDeployed returns a deployed bootstrapped runtime
// snapshot in close-mode auto — the post-bootstrap "happy path" shape.
func fixSnapBootstrappedDeployed(hostname, typeVersion string, rc topology.RuntimeClass, mode topology.Mode) ServiceSnapshot {
	return ServiceSnapshot{
		Hostname:        hostname,
		TypeVersion:     typeVersion,
		RuntimeClass:    rc,
		Mode:            mode,
		CloseDeployMode: topology.CloseModeAuto,
		Bootstrapped:    true,
		Deployed:        true,
	}
}

// fixSnapBootstrappedNeverDeployed returns a bootstrapped but never-
// deployed runtime snapshot — first-deploy branch shape.
func fixSnapBootstrappedNeverDeployed(hostname, typeVersion string, rc topology.RuntimeClass, mode topology.Mode) ServiceSnapshot {
	return ServiceSnapshot{
		Hostname:        hostname,
		TypeVersion:     typeVersion,
		RuntimeClass:    rc,
		Mode:            mode,
		CloseDeployMode: topology.CloseModeAuto,
		Bootstrapped:    true,
	}
}

// fixSnapBootstrappedNeverDeployedPair returns BOTH halves of a
// never-deployed standard pair as separate snapshots, matching
// production buildServiceSnapshots which emits one snapshot per
// platform.ServiceStack. The dev half carries StageHostname (per
// buildOneSnapshot:217-219) — the stage half does not. Mode resolves
// to ModeStandard for the dev half and ModeStage for the stage half
// (per resolveEnvelopeMode).
func fixSnapBootstrappedNeverDeployedPair(devHost, stageHost, typeVersion string, rc topology.RuntimeClass) []ServiceSnapshot {
	return []ServiceSnapshot{
		{
			Hostname:        devHost,
			TypeVersion:     typeVersion,
			RuntimeClass:    rc,
			Mode:            topology.ModeStandard,
			CloseDeployMode: topology.CloseModeAuto,
			StageHostname:   stageHost,
			Bootstrapped:    true,
		},
		{
			Hostname:        stageHost,
			TypeVersion:     typeVersion,
			RuntimeClass:    rc,
			Mode:            topology.ModeStage,
			CloseDeployMode: topology.CloseModeAuto,
			Bootstrapped:    true,
		},
	}
}

// fixSnapDeployedDevAuto returns a deployed dev-mode close-auto snapshot.
func fixSnapDeployedDevAuto(hostname, typeVersion string, rc topology.RuntimeClass) ServiceSnapshot {
	return ServiceSnapshot{
		Hostname:        hostname,
		TypeVersion:     typeVersion,
		RuntimeClass:    rc,
		Mode:            topology.ModeDev,
		CloseDeployMode: topology.CloseModeAuto,
		Bootstrapped:    true,
		Deployed:        true,
	}
}

// fixSnapDeployedPairAuto returns BOTH halves of a deployed standard
// pair in close-mode auto. Production emits one snapshot per
// ServiceStack; fixtures match that shape so atoms gated on
// modes:[stage] fire on the stage-half snapshot just as they would in
// the live envelope.
func fixSnapDeployedPairAuto(devHost, stageHost, typeVersion string, rc topology.RuntimeClass) []ServiceSnapshot {
	return []ServiceSnapshot{
		{
			Hostname:        devHost,
			TypeVersion:     typeVersion,
			RuntimeClass:    rc,
			Mode:            topology.ModeStandard,
			CloseDeployMode: topology.CloseModeAuto,
			StageHostname:   stageHost,
			Bootstrapped:    true,
			Deployed:        true,
		},
		{
			Hostname:        stageHost,
			TypeVersion:     typeVersion,
			RuntimeClass:    rc,
			Mode:            topology.ModeStage,
			CloseDeployMode: topology.CloseModeAuto,
			Bootstrapped:    true,
			Deployed:        true,
		},
	}
}

// fixSnapDeployedPairUnset returns BOTH halves of a deployed standard
// pair with CloseDeployMode=unset — the post-adopt shape where the
// agent has not yet picked a close mode for either half.
func fixSnapDeployedPairUnset(devHost, stageHost, typeVersion string, rc topology.RuntimeClass) []ServiceSnapshot {
	return []ServiceSnapshot{
		{
			Hostname:        devHost,
			TypeVersion:     typeVersion,
			RuntimeClass:    rc,
			Mode:            topology.ModeStandard,
			CloseDeployMode: topology.CloseModeUnset,
			StageHostname:   stageHost,
			Bootstrapped:    true,
			Deployed:        true,
		},
		{
			Hostname:        stageHost,
			TypeVersion:     typeVersion,
			RuntimeClass:    rc,
			Mode:            topology.ModeStage,
			CloseDeployMode: topology.CloseModeUnset,
			Bootstrapped:    true,
			Deployed:        true,
		},
	}
}

// fixSnapGitPushIntegration returns BOTH halves of a standard pair
// configured for close-mode git-push, parameterized over
// GitPushState + BuildIntegration so a single helper covers both
// configured/webhook and unconfigured shapes.
func fixSnapGitPushIntegration(devHost, stageHost, typeVersion string, rc topology.RuntimeClass, gps topology.GitPushState, bi topology.BuildIntegration) []ServiceSnapshot {
	return []ServiceSnapshot{
		{
			Hostname:         devHost,
			TypeVersion:      typeVersion,
			RuntimeClass:     rc,
			Mode:             topology.ModeStandard,
			CloseDeployMode:  topology.CloseModeGitPush,
			GitPushState:     gps,
			BuildIntegration: bi,
			StageHostname:    stageHost,
			Bootstrapped:     true,
			Deployed:         true,
		},
		{
			Hostname:         stageHost,
			TypeVersion:      typeVersion,
			RuntimeClass:     rc,
			Mode:             topology.ModeStage,
			CloseDeployMode:  topology.CloseModeGitPush,
			GitPushState:     gps,
			BuildIntegration: bi,
			Bootstrapped:     true,
			Deployed:         true,
		},
	}
}

// fixSnapManaged returns a managed-service (DB / cache / etc.) snapshot.
// Bootstrapped=true with no deploy / mode fields — managed services
// don't carry runtime-specific metadata.
func fixSnapManaged(hostname, typeVersion string) ServiceSnapshot {
	return ServiceSnapshot{
		Hostname:     hostname,
		TypeVersion:  typeVersion,
		RuntimeClass: topology.RuntimeManaged,
		Bootstrapped: true,
	}
}

// fixSession builds a deterministic WorkSessionSummary scoped to the
// supplied hostnames. CreatedAt is fixed at 2026-05-02 09:00 UTC so
// regenerated goldens stay byte-stable across runs (the test never
// uses time.Now via this helper).
func fixSession(hostnames ...string) *WorkSessionSummary {
	return &WorkSessionSummary{
		Intent:    "deploy",
		Services:  hostnames,
		CreatedAt: time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC),
	}
}

// fixTimePtr returns a *time.Time at the given UTC date for use in
// ClosedAt / similar optional time-pointer fields. Mirrors the
// time.Date constructor signature for inline readability.
func fixTimePtr(year int, month time.Month, day, hour, minute, second int) *time.Time {
	t := time.Date(year, month, day, hour, minute, second, 0, time.UTC)
	return &t
}

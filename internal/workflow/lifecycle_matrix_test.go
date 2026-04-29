// Lifecycle matrix simulation. Walks the full ZCP lifecycle (bootstrap →
// develop → close) across the canonical permutations of mode, runtime,
// close-mode, git-push state, and build integration, then dumps a
// markdown report listing which atoms fire for each scenario, the plan
// shape, and a curated set of anomalies.
//
// Gated on ZCP_RUN_MATRIX=1 so it stays out of the default test run —
// this is a diagnostic harness, not a coverage test. Run:
//
//	ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
//
// Output lands at internal/workflow/testdata/lifecycle-matrix.md and is
// the input the human reads to spot dumb errors before live testing.
package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestLifecycleMatrixDump(t *testing.T) {
	if os.Getenv("ZCP_RUN_MATRIX") == "" {
		t.Skip("set ZCP_RUN_MATRIX=1 to dump the lifecycle matrix simulation")
	}
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	scenarios := lifecycleScenarios()

	var rep strings.Builder
	fmt.Fprintf(&rep, "# ZCP Lifecycle Matrix Simulation\n\n")
	fmt.Fprintf(&rep, "Generated: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&rep, "Corpus: %d atoms\n", len(corpus))
	fmt.Fprintf(&rep, "Scenarios: %d\n\n", len(scenarios))

	type anomaly struct {
		Scenario string
		Severity string
		Detail   string
	}
	var anomalies []anomaly

	addAnom := func(sc, sev, msg string) {
		anomalies = append(anomalies, anomaly{Scenario: sc, Severity: sev, Detail: msg})
	}

	// Per-section grouping for the markdown output
	sections := orderedSections()
	bySection := map[string][]matrixScenario{}
	for _, sc := range scenarios {
		bySection[sc.Section] = append(bySection[sc.Section], sc)
	}

	for _, sec := range sections {
		group := bySection[sec]
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(&rep, "---\n\n# %s\n\n", sec)
		for _, sc := range group {
			plan := BuildPlan(sc.Envelope)
			matches, err := Synthesize(sc.Envelope, corpus)
			if err != nil {
				addAnom(sc.Name, "FATAL", fmt.Sprintf("Synthesize error: %v", err))
				continue
			}
			ids := uniqueAtomIDs(matches)
			renderCount := len(matches)
			bytes := totalBodyBytes(matches)

			fmt.Fprintf(&rep, "## %s\n\n", sc.Name)
			fmt.Fprintf(&rep, "_%s_\n\n", sc.Description)
			fmt.Fprintf(&rep, "**Phase**: `%s` &middot; **Env**: `%s`",
				sc.Envelope.Phase, sc.Envelope.Environment)
			if sc.Envelope.IdleScenario != "" {
				fmt.Fprintf(&rep, " &middot; **IdleScenario**: `%s`", sc.Envelope.IdleScenario)
			}
			fmt.Fprintln(&rep)
			fmt.Fprintf(&rep, "\n**Plan.Primary**: `%s` → %s\n",
				plan.Primary.Tool, plan.Primary.Label)
			if plan.Primary.Tool == "" {
				addAnom(sc.Name, "ERROR", "Plan.Primary is zero (empty Plan)")
			}

			if len(plan.Alternatives) > 0 {
				alts := make([]string, 0, len(plan.Alternatives))
				for _, a := range plan.Alternatives {
					alts = append(alts, fmt.Sprintf("`%s`", a.Label))
				}
				fmt.Fprintf(&rep, "**Alternatives**: %s\n", strings.Join(alts, ", "))
			}
			if len(matches) == 0 {
				fmt.Fprintf(&rep, "\n**Atoms**: NONE — agent receives no guidance!\n\n")
				addAnom(sc.Name, "ERROR", "zero atoms fired — agent flies blind")
			} else {
				fmt.Fprintf(&rep, "\n**Atoms** (%d unique, %d render-instances, %d bytes total):\n",
					len(ids), renderCount, bytes)
				for _, id := range ids {
					fmt.Fprintf(&rep, "- `%s`\n", id)
				}
			}
			fmt.Fprintln(&rep)

			// Anomaly checks per scenario
			if bytes > 25*1024 {
				addAnom(sc.Name, "WARN",
					fmt.Sprintf("briefing %d bytes > 25KB soft cap", bytes))
			}
			seenBody := map[string]int{}
			for _, m := range matches {
				if leak := findUnknownPlaceholder(m.Body); leak != "" {
					addAnom(sc.Name, "FATAL",
						fmt.Sprintf("placeholder leak in %s: %q", m.AtomID, leak))
				}
				if strings.Contains(m.Body, "{hostname}") {
					addAnom(sc.Name, "FATAL",
						fmt.Sprintf("unsubstituted {hostname} in %s", m.AtomID))
				}
				if strings.Contains(m.Body, "{stage-hostname}") &&
					!strings.Contains(sc.Description, "stage-hostname literal") {
					addAnom(sc.Name, "WARN",
						fmt.Sprintf("unsubstituted {stage-hostname} in %s", m.AtomID))
				}
				seenBody[m.Body]++
			}
			for body, n := range seenBody {
				if n > 1 {
					addAnom(sc.Name, "WARN",
						fmt.Sprintf("duplicate body rendered %d× (first 60 chars: %q)",
							n, firstChars(body, 60)))
				}
			}
			// Vocabulary drift checks — atoms still using old `strategy=`
			// language now that the user-facing field is closeDeployMode.
			for _, m := range matches {
				if !sc.AllowLegacyStrategyVocab && containsLegacyStrategyVocab(m.Body) {
					addAnom(sc.Name, "WARN",
						fmt.Sprintf("legacy `strategy` vocab in %s — should use closeDeployMode", m.AtomID))
				}
			}
		}
	}

	// Anomaly summary
	if len(anomalies) > 0 {
		fmt.Fprintf(&rep, "---\n\n# Anomalies (%d)\n\n", len(anomalies))
		bySeverity := map[string][]anomaly{}
		for _, a := range anomalies {
			bySeverity[a.Severity] = append(bySeverity[a.Severity], a)
		}
		for _, sev := range []string{"FATAL", "ERROR", "WARN"} {
			items := bySeverity[sev]
			if len(items) == 0 {
				continue
			}
			fmt.Fprintf(&rep, "## %s (%d)\n\n", sev, len(items))
			for _, a := range items {
				fmt.Fprintf(&rep, "- **%s** — %s\n", a.Scenario, a.Detail)
			}
			fmt.Fprintln(&rep)
		}
	}

	out := filepath.Join("testdata", "lifecycle-matrix.md")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	if err := os.WriteFile(out, []byte(rep.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	t.Logf("matrix dumped to %s (%d bytes, %d anomalies across %d scenarios)",
		out, len(rep.String()), len(anomalies), len(scenarios))
}

type matrixScenario struct {
	Section                  string
	Name                     string
	Description              string
	Envelope                 StateEnvelope
	AllowLegacyStrategyVocab bool // suppress legacy-vocab warning when test intentionally renders such an atom
}

func orderedSections() []string {
	return []string{
		"1. Idle entry points",
		"2. Bootstrap — classic route",
		"3. Bootstrap — recipe route",
		"4. Bootstrap — adopt route",
		"5. Develop — first-deploy branch",
		"6. Develop — iteration after first deploy",
		"7. Develop — close-mode variants",
		"8. Develop — git-push capability matrix",
		"9. Develop — failure tiers",
		"10. Develop — multi-service orchestration",
		"11. Strategy-setup synthesis",
		"12. Export workflow",
		"13. Develop closed (auto)",
	}
}

func lifecycleScenarios() []matrixScenario {
	out := make([]matrixScenario, 0, 64)
	out = append(out, idleScenarios()...)
	out = append(out, bootstrapClassicScenarios()...)
	out = append(out, bootstrapRecipeScenarios()...)
	out = append(out, bootstrapAdoptScenarios()...)
	out = append(out, developFirstDeployScenarios()...)
	out = append(out, developIterationScenarios()...)
	out = append(out, developCloseModeScenarios()...)
	out = append(out, developGitPushMatrixScenarios()...)
	out = append(out, developFailureTierScenarios()...)
	out = append(out, developMultiServiceScenarios()...)
	out = append(out, strategySetupScenarios()...)
	out = append(out, exportScenarios()...)
	out = append(out, developClosedScenarios()...)
	return out
}

// ----------------------------------------------------------------------------
// Section 1 — Idle entry points
// ----------------------------------------------------------------------------

func idleScenarios() []matrixScenario {
	const sec = "1. Idle entry points"
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "1.1 idle/empty (fresh user, no project state)",
			Description: "Brand-new project — should route the agent into bootstrap.",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleEmpty,
			},
		},
		{
			Section:     sec,
			Name:        "1.2 idle/adopt (only unmanaged runtimes)",
			Description: "Project has runtime services but no ServiceMeta files — adoption path.",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleAdopt,
				Services: []ServiceSnapshot{
					{Hostname: "legacy", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic},
				},
			},
		},
		{
			Section:     sec,
			Name:        "1.3 idle/bootstrapped (managed services exist)",
			Description: "User finished bootstrap, returning later to start a develop task.",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleBootstrapped,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					Bootstrapped: true, StageHostname: "appstage",
				}},
			},
		},
		{
			Section:     sec,
			Name:        "1.4 idle/incomplete (partial bootstrap meta exists)",
			Description: "Prior bootstrap session crashed mid-way; resume should be offered.",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleIncomplete,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Resumable: true,
				}},
			},
		},
		{
			Section:     sec,
			Name:        "1.5 idle/empty LOCAL env",
			Description: "Local-machine ZCP without any project — bootstrap entry should adapt.",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvLocal,
				IdleScenario: IdleEmpty,
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 2 — Bootstrap classic route (manual plan)
// ----------------------------------------------------------------------------

func bootstrapClassicScenarios() []matrixScenario {
	const sec = "2. Bootstrap — classic route"
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "2.1 classic/discover dynamic standard pair (container)",
			Description: "Free-form plan: dynamic runtime in standard mode + dev/stage hostnames.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			},
		},
		{
			Section:     sec,
			Name:        "2.2 classic/discover static SPA (container)",
			Description: "Static-runtime path (Vite SPA, etc.) — different deploy/build vocabulary.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "spa", TypeVersion: "static@1.0",
					RuntimeClass: topology.RuntimeStatic, Mode: topology.ModeSimple,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			},
		},
		{
			Section:     sec,
			Name:        "2.3 classic/discover implicit-webserver (PHP simple)",
			Description: "PHP implicit-webserver: no `start:` block, real start path.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "site", TypeVersion: "php-apache@8.3",
					RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeSimple,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			},
		},
		{
			Section:     sec,
			Name:        "2.4 classic/provision (container, dev mode)",
			Description: "Provision step — agent should see import.yaml + auto-mount guidance.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "apidev", TypeVersion: "go@1.23",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepProvision},
			},
		},
		{
			Section:     sec,
			Name:        "2.5 classic/close (container, simple mode)",
			Description: "Close step — finalize ServiceMeta, no first deploy.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepClose},
			},
		},
		{
			Section:     sec,
			Name:        "2.6 classic/discover (LOCAL env)",
			Description: "Local-mode bootstrap discover — should suppress mount/SSH guidance.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "bun@1.3",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			},
		},
		{
			Section:     sec,
			Name:        "2.7 classic/provision (LOCAL env)",
			Description: "Local provision — no auto-mount path.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepProvision},
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 3 — Bootstrap recipe route
// ----------------------------------------------------------------------------

func bootstrapRecipeScenarios() []matrixScenario {
	const sec = "3. Bootstrap — recipe route"
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "3.1 recipe/discover (container, hello-world slug)",
			Description: "Recipe discover: agent picks slug `nodejs-hello-world`.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
				}},
				Bootstrap: &BootstrapSessionSummary{
					Route: BootstrapRouteRecipe, Step: StepDiscover,
					RecipeMatch: &RecipeMatch{Slug: "nodejs-hello-world", Confidence: 0.9},
				},
			},
		},
		{
			Section:     sec,
			Name:        "3.2 recipe/provision (container, multi-service Laravel)",
			Description: "Laravel-minimal recipe: php-apache + db.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", TypeVersion: "php-apache@8.3",
						RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStandard,
						StageHostname: "appstage"},
					{Hostname: "db", TypeVersion: "postgresql@16",
						RuntimeClass: topology.RuntimeManaged},
				},
				Bootstrap: &BootstrapSessionSummary{
					Route: BootstrapRouteRecipe, Step: StepProvision,
					RecipeMatch: &RecipeMatch{Slug: "laravel-minimal", Confidence: 0.85},
				},
			},
		},
		{
			Section:     sec,
			Name:        "3.3 recipe/close (container)",
			Description: "Recipe close — finalize meta, hand off to develop.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
				}},
				Bootstrap: &BootstrapSessionSummary{
					Route: BootstrapRouteRecipe, Step: StepClose,
					RecipeMatch: &RecipeMatch{Slug: "nodejs-hello-world", Confidence: 0.9},
				},
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 4 — Bootstrap adopt route
// ----------------------------------------------------------------------------

func bootstrapAdoptScenarios() []matrixScenario {
	const sec = "4. Bootstrap — adopt route"
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "4.1 adopt/discover (container, single dev runtime)",
			Description: "Single existing runtime to adopt as dev mode.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Step: StepDiscover},
			},
		},
		{
			Section:     sec,
			Name:        "4.2 adopt/discover (container, dev+stage pair)",
			Description: "Two existing runtimes with dev/stage suffix → adopt as standard.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
						StageHostname: "appstage"},
				},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Step: StepDiscover},
			},
		},
		{
			Section:     sec,
			Name:        "4.3 adopt/provision (pure-adoption fast path)",
			Description: "Plan all-existing — close should be skippable.",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "go@1.23",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Step: StepProvision},
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 5 — Develop first-deploy branch (post-bootstrap, never-deployed)
// ----------------------------------------------------------------------------

func developFirstDeployScenarios() []matrixScenario {
	const sec = "5. Develop — first-deploy branch"
	now := time.Now().UTC()
	makeWorkSession := func(host string) *WorkSessionSummary {
		return &WorkSessionSummary{
			Intent: "implement first feature", Services: []string{host},
			CreatedAt: now,
		}
	}
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "5.1 develop never-deployed dev/dynamic (container)",
			Description: "Just bootstrapped, dev mode dynamic runtime, first develop iteration.",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Bootstrapped: true, Deployed: false,
				}},
				WorkSession: makeWorkSession("appdev"),
			},
		},
		{
			Section:     sec,
			Name:        "5.2 develop never-deployed simple/dynamic (container)",
			Description: "Simple-mode single service, healthCheck-driven start.",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
					Bootstrapped: true, Deployed: false,
				}},
				WorkSession: makeWorkSession("app"),
			},
		},
		{
			Section:     sec,
			Name:        "5.3 develop never-deployed standard dev half (container)",
			Description: "Standard-mode dev half, stage entry not yet written.",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					Bootstrapped: true, Deployed: false, StageHostname: "appstage",
				}},
				WorkSession: makeWorkSession("appdev"),
			},
		},
		{
			Section:     sec,
			Name:        "5.4 develop never-deployed PHP simple (implicit-webserver)",
			Description: "PHP simple — no `start:`; healthCheck on `/`.",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "site", TypeVersion: "php-apache@8.3",
					RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeSimple,
					Bootstrapped: true, Deployed: false,
				}},
				WorkSession: makeWorkSession("site"),
			},
		},
		{
			Section:     sec,
			Name:        "5.5 develop never-deployed static SPA",
			Description: "Static runtime — buildCommands generate dist; deployFiles selects ./dist.",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "spa", TypeVersion: "static@1.0",
					RuntimeClass: topology.RuntimeStatic, Mode: topology.ModeSimple,
					Bootstrapped: true, Deployed: false,
				}},
				WorkSession: makeWorkSession("spa"),
			},
		},
		{
			Section:     sec,
			Name:        "5.6 develop never-deployed dev/dynamic (LOCAL env)",
			Description: "Local-machine first deploy — local workflow atom path.",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeLocalOnly,
					Bootstrapped: true, Deployed: false,
				}},
				WorkSession: makeWorkSession("app"),
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 6 — Develop iteration (post-first-deploy, deployed=true)
// ----------------------------------------------------------------------------

func developIterationScenarios() []matrixScenario {
	const sec = "6. Develop — iteration after first deploy"
	now := time.Now().UTC()
	successfulHistory := func(host string) *WorkSessionSummary {
		return &WorkSessionSummary{
			Intent: "iterate feature", Services: []string{host}, CreatedAt: now,
			Deploys:  map[string][]AttemptInfo{host: {{At: now, Success: true, Iteration: 1}}},
			Verifies: map[string][]AttemptInfo{host: {{At: now, Success: true, Iteration: 1}}},
		}
	}
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "6.1 develop deployed unset close-mode (post-first-deploy review)",
			Description: "First deploy succeeded; close-mode still unset → review prompt should fire.",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Bootstrapped: true, Deployed: true,
					CloseDeployMode: topology.CloseModeUnset,
				}},
				WorkSession: successfulHistory("appdev"),
			},
		},
		{
			Section:     sec,
			Name:        "6.2 develop deployed CloseMode=auto (steady-state iteration)",
			Description: "Iteration after picking auto close-mode — strategy-review should NOT fire.",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Bootstrapped: true, Deployed: true,
					CloseDeployMode: topology.CloseModeAuto,
				}},
				WorkSession: successfulHistory("appdev"),
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 7 — Develop close-mode variants
// ----------------------------------------------------------------------------

func developCloseModeScenarios() []matrixScenario {
	const sec = "7. Develop — close-mode variants"
	now := time.Now().UTC()
	deployed := func(host string) *WorkSessionSummary {
		return &WorkSessionSummary{
			Intent: "feature-x", Services: []string{host}, CreatedAt: now,
			Deploys:  map[string][]AttemptInfo{host: {{At: now, Success: true, Iteration: 1}}},
			Verifies: map[string][]AttemptInfo{host: {{At: now, Success: true, Iteration: 1}}},
		}
	}
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "7.1 close-mode=auto + dev mode (container)",
			Description: "Default close path — auto = run zerops_deploy at close.",
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Bootstrapped: true, Deployed: true,
					CloseDeployMode: topology.CloseModeAuto,
				}},
				WorkSession: deployed("appdev"),
			},
		},
		{
			Section:     sec,
			Name:        "7.2 close-mode=git-push + GitPushState=configured + webhook",
			Description: "Full git-push setup with webhook integration.",
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					Bootstrapped: true, Deployed: true,
					CloseDeployMode:  topology.CloseModeGitPush,
					GitPushState:     topology.GitPushConfigured,
					BuildIntegration: topology.BuildIntegrationWebhook,
					RemoteURL:        "git@github.com:org/repo.git",
					StageHostname:    "appstage",
				}},
				WorkSession: deployed("appdev"),
			},
		},
		{
			Section:     sec,
			Name:        "7.3 close-mode=manual (yield to user)",
			Description: "Manual close — ZCP records evidence but user owns deploys.",
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Bootstrapped: true, Deployed: true,
					CloseDeployMode: topology.CloseModeManual,
				}},
				WorkSession: deployed("appdev"),
			},
		},
		{
			Section:     sec,
			Name:        "7.4 close-mode=git-push BUT FirstDeployedAt empty (D2a edge)",
			Description: "Agent set close-mode before first deploy — atoms must explain D2a (default self-deploy still applies).",
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					Bootstrapped: true, Deployed: false,
					CloseDeployMode: topology.CloseModeGitPush,
					GitPushState:    topology.GitPushUnconfigured,
					StageHostname:   "appstage",
				}},
				WorkSession: &WorkSessionSummary{Intent: "first deploy", Services: []string{"appdev"}, CreatedAt: now},
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 8 — Git-push capability matrix (close-mode × git-push state × build integration)
// ----------------------------------------------------------------------------

func developGitPushMatrixScenarios() []matrixScenario {
	const sec = "8. Develop — git-push capability matrix"
	now := time.Now().UTC()
	deployed := func(host string) *WorkSessionSummary {
		return &WorkSessionSummary{
			Intent: "ci/cd", Services: []string{host}, CreatedAt: now,
			Deploys:  map[string][]AttemptInfo{host: {{At: now, Success: true, Iteration: 1}}},
			Verifies: map[string][]AttemptInfo{host: {{At: now, Success: true, Iteration: 1}}},
		}
	}
	mk := func(name, desc string, closeMode topology.CloseDeployMode, push topology.GitPushState, integ topology.BuildIntegration) matrixScenario {
		return matrixScenario{
			Section:     sec,
			Name:        name,
			Description: desc,
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					Bootstrapped: true, Deployed: true,
					CloseDeployMode: closeMode, GitPushState: push, BuildIntegration: integ,
					RemoteURL: "git@github.com:org/repo.git", StageHostname: "appstage",
				}},
				WorkSession: deployed("appdev"),
			},
		}
	}
	return []matrixScenario{
		mk("8.1 auto / unconfigured / none", "Default — git push capability not provisioned.",
			topology.CloseModeAuto, topology.GitPushUnconfigured, topology.BuildIntegrationNone),
		mk("8.2 auto / configured / none", "Capability provisioned; close still does zcli (auto).",
			topology.CloseModeAuto, topology.GitPushConfigured, topology.BuildIntegrationNone),
		mk("8.3 git-push / unconfigured / none", "Mode flipped to git-push but capability missing — must chain to setup.",
			topology.CloseModeGitPush, topology.GitPushUnconfigured, topology.BuildIntegrationNone),
		mk("8.4 git-push / configured / webhook", "Full webhook CI.",
			topology.CloseModeGitPush, topology.GitPushConfigured, topology.BuildIntegrationWebhook),
		mk("8.5 git-push / configured / actions", "GitHub Actions CI.",
			topology.CloseModeGitPush, topology.GitPushConfigured, topology.BuildIntegrationActions),
		mk("8.6 git-push / broken / webhook", "Push capability previously broken; recovery atom expected.",
			topology.CloseModeGitPush, topology.GitPushBroken, topology.BuildIntegrationWebhook),
	}
}

// ----------------------------------------------------------------------------
// Section 9 — Failure tiers (iteration ladder)
// ----------------------------------------------------------------------------

func developFailureTierScenarios() []matrixScenario {
	const sec = "9. Develop — failure tiers"
	now := time.Now().UTC()
	failedDeploys := func(host string, n int) *WorkSessionSummary {
		attempts := make([]AttemptInfo, 0, n)
		for i := range n {
			attempts = append(attempts, AttemptInfo{
				At: now, Iteration: i + 1, Success: false,
				Reason:       "build failed: dependency conflict",
				FailureClass: topology.FailureClass("build"),
			})
		}
		return &WorkSessionSummary{
			Intent: "fix bug", Services: []string{host}, CreatedAt: now,
			Deploys: map[string][]AttemptInfo{host: attempts},
		}
	}
	mk := func(name, desc string, n int) matrixScenario {
		return matrixScenario{
			Section: sec, Name: name, Description: desc,
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Bootstrapped: true, Deployed: false,
					CloseDeployMode: topology.CloseModeAuto,
				}},
				WorkSession: failedDeploys("appdev", n),
			},
		}
	}
	return []matrixScenario{
		mk("9.1 iteration tier 1 (1 failed)", "First failure — DIAGNOSE tier.", 1),
		mk("9.2 iteration tier 3 (3 failed)", "After 3 failures — SYSTEMATIC tier kicks in.", 3),
		mk("9.3 iteration tier 5 (5 failed, STOP)", "Hit iteration cap — STOP tier should surface.", 5),
	}
}

// ----------------------------------------------------------------------------
// Section 10 — Multi-service orchestration
// ----------------------------------------------------------------------------

func developMultiServiceScenarios() []matrixScenario {
	const sec = "10. Develop — multi-service orchestration"
	now := time.Now().UTC()
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "10.1 standard mode dev+stage halves both never-deployed",
			Description: "Standard pair — atoms should fire per-half with correct hostnames.",
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
						Bootstrapped: true, Deployed: false, StageHostname: "appstage"},
					{Hostname: "appstage", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
						Bootstrapped: true, Deployed: false},
				},
				WorkSession: &WorkSessionSummary{
					Intent: "first deploy", Services: []string{"appdev", "appstage"}, CreatedAt: now,
				},
			},
		},
		{
			Section:     sec,
			Name:        "10.2 mixed runtimes (api + web + db)",
			Description: "Two runtimes + managed dep — per-service rendering correctness.",
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{Hostname: "apidev", TypeVersion: "go@1.23",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
						Bootstrapped: true, Deployed: true,
						CloseDeployMode: topology.CloseModeAuto},
					{Hostname: "webdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
						Bootstrapped: true, Deployed: true,
						CloseDeployMode: topology.CloseModeAuto},
					{Hostname: "db", TypeVersion: "postgresql@16",
						RuntimeClass: topology.RuntimeManaged},
				},
				WorkSession: &WorkSessionSummary{
					Intent: "feature", Services: []string{"apidev", "webdev"}, CreatedAt: now,
					Deploys: map[string][]AttemptInfo{
						"apidev": {{At: now, Success: true, Iteration: 1}},
						"webdev": {{At: now, Success: true, Iteration: 1}},
					},
					Verifies: map[string][]AttemptInfo{
						"apidev": {{At: now, Success: true, Iteration: 1}},
						"webdev": {{At: now, Success: true, Iteration: 1}},
					},
				},
			},
		},
		{
			// F9 Lever B (audit-prerelease-internal-testing-2026-04-29):
			// scope=[1 host] in a 4-service project. Pre-fix synthesizer
			// matched per-service atoms against ALL 4 services (3 dev
			// runtimes + 1 managed) regardless of work-session scope.
			// Post-fix it narrows to scope hostnames; out-of-scope dev
			// runtimes contribute zero per-service axis matches.
			Section:     sec,
			Name:        "10.3 four runtimes scope=1 (Lever B narrow)",
			Description: "Project has 3 dev runtimes + 1 managed; scope is just appdev. Per-service atoms must fire only for appdev.",
			Envelope: StateEnvelope{
				Phase: PhaseDevelopActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
						Bootstrapped: true, Deployed: true,
						CloseDeployMode: topology.CloseModeAuto},
					{Hostname: "apidev", TypeVersion: "go@1.23",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
						Bootstrapped: true, Deployed: true,
						CloseDeployMode: topology.CloseModeAuto},
					{Hostname: "webdev", TypeVersion: "php-nginx@8.4",
						RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeDev,
						Bootstrapped: true, Deployed: true,
						CloseDeployMode: topology.CloseModeAuto},
					{Hostname: "db", TypeVersion: "postgresql@16",
						RuntimeClass: topology.RuntimeManaged},
				},
				WorkSession: &WorkSessionSummary{
					Intent: "fix login appdev", Services: []string{"appdev"}, CreatedAt: now,
					Deploys: map[string][]AttemptInfo{
						"appdev": {{At: now, Success: true, Iteration: 1}},
					},
					Verifies: map[string][]AttemptInfo{
						"appdev": {{At: now, Success: true, Iteration: 1}},
					},
				},
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 11 — Strategy-setup phase (stateless synthesis)
// ----------------------------------------------------------------------------

func strategySetupScenarios() []matrixScenario {
	const sec = "11. Strategy-setup synthesis"
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "11.1 strategy-setup container (git-push setup)",
			Description: "action=git-push-setup synthesizes setup-git-push-container.",
			Envelope: StateEnvelope{
				Phase: PhaseStrategySetup, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					CloseDeployMode: topology.CloseModeGitPush,
					GitPushState:    topology.GitPushUnconfigured,
				}},
			},
		},
		{
			Section:     sec,
			Name:        "11.2 strategy-setup local",
			Description: "Local-env git-push setup atom.",
			Envelope: StateEnvelope{
				Phase: PhaseStrategySetup, Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeLocalStage,
					CloseDeployMode: topology.CloseModeGitPush,
					GitPushState:    topology.GitPushUnconfigured,
				}},
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 12 — Export workflow
// ----------------------------------------------------------------------------

func exportScenarios() []matrixScenario {
	const sec = "12. Export workflow"
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "12.1 export-active container",
			Description: "Export workflow synthesizes export-* atoms.",
			Envelope: StateEnvelope{
				Phase: PhaseExportActive, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Bootstrapped: true, Deployed: true,
					CloseDeployMode: topology.CloseModeAuto,
				}},
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Section 13 — Develop closed (auto)
// ----------------------------------------------------------------------------

func developClosedScenarios() []matrixScenario {
	const sec = "13. Develop closed (auto)"
	now := time.Now().UTC()
	return []matrixScenario{
		{
			Section:     sec,
			Name:        "13.1 develop-closed-auto after green run",
			Description: "All services deployed+verified, session auto-closed.",
			Envelope: StateEnvelope{
				Phase: PhaseDevelopClosed, Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Bootstrapped: true, Deployed: true,
					CloseDeployMode: topology.CloseModeAuto,
				}},
				WorkSession: &WorkSessionSummary{
					Intent: "feature", Services: []string{"appdev"},
					CreatedAt: now, ClosedAt: ptrTime(now), CloseReason: "auto-complete",
					Deploys:  map[string][]AttemptInfo{"appdev": {{At: now, Success: true, Iteration: 1}}},
					Verifies: map[string][]AttemptInfo{"appdev": {{At: now, Success: true, Iteration: 1}}},
				},
			},
		},
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func uniqueAtomIDs(matches []MatchedRender) []string {
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		seen[m.AtomID] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func totalBodyBytes(matches []MatchedRender) int {
	n := 0
	for _, m := range matches {
		n += len(m.Body)
	}
	return n
}

func firstChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// containsLegacyStrategyVocab flags atom bodies that still talk about
// the retired `strategy=push-dev/push-git` vocabulary instead of the
// current closeDeployMode + gitPushState axes.
func containsLegacyStrategyVocab(body string) bool {
	tokens := []string{
		"strategies={", "action=\"strategy\"", `action="strategy"`,
		"strategy=push-dev", "strategy=push-git",
		`"push-dev"`, `"push-git"`,
	}
	for _, tok := range tokens {
		if strings.Contains(body, tok) {
			return true
		}
	}
	return false
}

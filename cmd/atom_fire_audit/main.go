// Probe binary for atom-corpus-hygiene plan §6.3. Walks every plausible
// envelope shape, runs Synthesize, and tabulates per-atom fire-set
// (the set of envelope keys that match the atom's axes).
//
// Output: one row per atom_id with the count of distinct envelope keys
// it fires on plus an explicit `fires-on` column listing the keys.
// Atoms with count = 0 are DEAD candidates for Phase 1 deletion (still
// require ComputeEnvelope walk per Phase 1 Codex round to confirm —
// synthetic envelopes don't capture every real ComputeEnvelope output).
//
// Built and deleted as part of the hygiene plan; not shipped (Phase 8
// EXIT deletes this directory).
//
// Trade-off note: the binary does NOT walk
// `internal/workflow/corpus_coverage_test.go::coverageFixtures()` —
// _test.go files are not importable from `cmd/`. The synthetic Cartesian
// product + multi-service mixed envelope is comprehensive enough to
// cover every atom's potential fire territory for the purpose of
// detecting dead atoms (atoms whose axes can't be satisfied by any
// reasonable envelope shape). Fixtures contribute coverage anchors,
// not new fire-set territory. If a Phase 1 candidate looks DEAD here
// but the executor suspects fixture coverage, run the probe-equivalent
// `go test ./internal/workflow -run TestCorpusCoverage_RoundTrip -v`
// and grep for the candidate atom-ID in the matched output.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// statusKeyTag formats a service status for envelope key inclusion;
// empty status renders as "_" so keys remain greppable.
func statusKeyTag(s string) string {
	if s == "" {
		return "_"
	}
	return s
}

type plausibleEnvelope struct {
	key string
	env workflow.StateEnvelope
}

func main() {
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		panic(err)
	}

	fireMap := make(map[string]map[string]struct{})
	for _, atom := range corpus {
		fireMap[atom.ID] = make(map[string]struct{})
	}

	envelopes := generatePlausibleEnvelopes()
	totalRuns := 0
	errEnvelopes := make(map[string]string) // envelope key → error message
	for _, pe := range envelopes {
		matches, err := workflow.Synthesize(pe.env, corpus)
		if err != nil {
			errEnvelopes[pe.key] = err.Error()
			continue
		}
		totalRuns++
		for _, m := range matches {
			fireMap[m.AtomID][pe.key] = struct{}{}
		}
	}
	if len(errEnvelopes) > 0 {
		// Synthesize is all-or-nothing per envelope: any atom with a content
		// bug (e.g. unescaped placeholder like `{hostname:value}` literal in
		// a code-fence example) makes Synthesize error out for that envelope,
		// suppressing fire-set credit for EVERY atom that should match it.
		// We log to stderr so the production output stays a clean fire-set
		// table while Phase 1 / Phase 6 see the bug surface.
		fmt.Fprintf(os.Stderr, "# %d envelopes errored during Synthesize:\n", len(errEnvelopes))
		// Print at most 10 sample errors with their envelope keys.
		count := 0
		for k, msg := range errEnvelopes {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", k, msg)
			count++
			if count >= 10 {
				fmt.Fprintf(os.Stderr, "  ... +%d more\n", len(errEnvelopes)-count)
				break
			}
		}
	}

	type row struct {
		id    string
		fires []string
	}
	rows := make([]row, 0, len(fireMap))
	for id, set := range fireMap {
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rows = append(rows, row{id, keys})
	}
	sort.Slice(rows, func(i, j int) bool {
		if len(rows[i].fires) != len(rows[j].fires) {
			return len(rows[i].fires) < len(rows[j].fires)
		}
		return rows[i].id < rows[j].id
	})

	fmt.Printf("# atom_fire_audit — %d atoms × %d envelopes (%d successful Synthesize runs)\n",
		len(corpus), len(envelopes), totalRuns)
	fmt.Println()
	fmt.Printf("%-50s  %5s  %s\n", "atom_id", "count", "fires-on (sample / full)")
	for _, r := range rows {
		sample := r.fires
		if len(sample) > 5 {
			sample = append([]string{}, sample[:5]...)
			sample = append(sample, fmt.Sprintf("... +%d more", len(r.fires)-5))
		}
		fmt.Printf("%-50s  %5d  %v\n", r.id, len(r.fires), sample)
	}

	fmt.Println()
	fmt.Println("# DEAD candidates (fire-set = 0)")
	dead := 0
	for _, r := range rows {
		if len(r.fires) == 0 {
			fmt.Printf("  %s\n", r.id)
			dead++
		}
	}
	if dead == 0 {
		fmt.Println("  (none)")
	}
}

// generatePlausibleEnvelopes builds the Cartesian product of axis values
// described in §6.3 of the hygiene plan. Codex round 1 corrections
// applied (committed a0f19ece): bootstrap step set narrowed to
// {discover, provision, close}; bootstrap envelopes carry service
// snapshots; develop product expanded over Trigger axis; strategy-setup
// + export-active + develop-closed-auto generated; multi-service
// mixed-deploy-state added.
func generatePlausibleEnvelopes() []plausibleEnvelope {
	// Cap is intentionally over-sized; final length is ~4749 in the
	// 2026-04-26 corpus, growth-bounded by the axis enumerations below.
	out := make([]plausibleEnvelope, 0, 5000)

	envs := []workflow.Environment{workflow.EnvContainer, workflow.EnvLocal}
	runtimes := []topology.RuntimeClass{
		topology.RuntimeDynamic, topology.RuntimeStatic,
		topology.RuntimeImplicitWeb, topology.RuntimeManaged,
	}
	triggers := []topology.PushGitTrigger{
		topology.TriggerUnset, topology.TriggerActions, topology.TriggerWebhook,
	}

	// ── Idle — every (env × IdleScenario) pair.
	for _, env := range envs {
		for _, scen := range []workflow.IdleScenario{
			workflow.IdleEmpty, workflow.IdleBootstrapped, workflow.IdleAdopt,
			workflow.IdleIncomplete, workflow.IdleOrphan,
		} {
			out = append(out, plausibleEnvelope{
				key: fmt.Sprintf("idle/%s/%s", env, scen),
				env: workflow.StateEnvelope{
					Phase: workflow.PhaseIdle, Environment: env,
					IdleScenario: scen,
				},
			})
		}
	}

	// ── Bootstrap-active — every (env × route × step) with no-svc + per-runtime svc variants.
	routes := []workflow.BootstrapRoute{
		workflow.BootstrapRouteRecipe, workflow.BootstrapRouteClassic,
		workflow.BootstrapRouteAdopt, workflow.BootstrapRouteResume,
	}
	bootSteps := []string{"discover", "provision", "close"} // atom-valid set
	for _, env := range envs {
		for _, route := range routes {
			for _, step := range bootSteps {
				out = append(out, plausibleEnvelope{
					key: fmt.Sprintf("bootstrap/%s/%s/%s/no-svc", env, route, step),
					env: workflow.StateEnvelope{
						Phase: workflow.PhaseBootstrapActive, Environment: env,
						Bootstrap: &workflow.BootstrapSessionSummary{Route: route, Step: step},
					},
				})
				for _, rt := range runtimes {
					out = append(out, plausibleEnvelope{
						key: fmt.Sprintf("bootstrap/%s/%s/%s/svc-%s", env, route, step, rt),
						env: workflow.StateEnvelope{
							Phase: workflow.PhaseBootstrapActive, Environment: env,
							Bootstrap: &workflow.BootstrapSessionSummary{Route: route, Step: step},
							Services: []workflow.ServiceSnapshot{{
								Hostname: "app", TypeVersion: "nodejs@22",
								RuntimeClass: rt, Mode: topology.ModeStandard,
								Strategy:     topology.StrategyUnset,
								Bootstrapped: true, Deployed: false,
							}},
						},
					})
				}
			}
		}
	}

	// ── Develop-active — Cartesian over (env × mode × strategy × trigger × runtime × deployState).
	modes := []topology.Mode{
		topology.ModeDev, topology.ModeStage, topology.ModeStandard,
		topology.ModeSimple, topology.ModeLocalStage, topology.ModeLocalOnly,
	}
	strategies := []topology.DeployStrategy{
		topology.StrategyPushDev, topology.StrategyPushGit,
		topology.StrategyManual, topology.StrategyUnset,
	}
	deployStates := []bool{false, true}
	// ServiceStatus is service-scoped — only one atom in the corpus
	// (develop-ready-to-deploy) currently uses it, on READY_TO_DEPLOY only.
	// Generate one variant per status so the axis is exercised.
	statuses := []string{"", "READY_TO_DEPLOY", "ACTIVE", "DEPLOYING"}
	for _, env := range envs {
		for _, mode := range modes {
			for _, stratV := range strategies {
				for _, trig := range triggers {
					for _, rt := range runtimes {
						for _, deployed := range deployStates {
							for _, status := range statuses {
								out = append(out, plausibleEnvelope{
									key: fmt.Sprintf("develop/%s/%s/%s/%s/%s/dep=%v/st=%s",
										env, mode, stratV, trig, rt, deployed, statusKeyTag(status)),
									env: workflow.StateEnvelope{
										Phase: workflow.PhaseDevelopActive, Environment: env,
										Services: []workflow.ServiceSnapshot{{
											Hostname: "appdev", TypeVersion: "nodejs@22",
											RuntimeClass: rt, Mode: mode,
											Strategy: stratV, Trigger: trig,
											Bootstrapped: true, Deployed: deployed,
											Status: status,
										}},
									},
								})
							}
						}
					}
				}
			}
		}
	}
	// Multi-service mixed deploy-state — Axis J risk surface.
	for _, env := range envs {
		out = append(out, plausibleEnvelope{
			key: fmt.Sprintf("develop/%s/multi/mixed-deploy", env),
			env: workflow.StateEnvelope{
				Phase: workflow.PhaseDevelopActive, Environment: env,
				Services: []workflow.ServiceSnapshot{
					{Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						Strategy:     topology.StrategyPushDev,
						Bootstrapped: true, Deployed: true},
					{Hostname: "workerdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						Strategy:     topology.StrategyPushDev,
						Bootstrapped: true, Deployed: false},
				},
			},
		})
	}

	// ── Develop-closed-auto — single phase per env.
	for _, env := range envs {
		out = append(out, plausibleEnvelope{
			key: fmt.Sprintf("develop-closed-auto/%s", env),
			env: workflow.StateEnvelope{
				Phase: workflow.PhaseDevelopClosed, Environment: env,
			},
		})
	}

	// ── Strategy-setup — push-git × env × trigger.
	for _, env := range envs {
		for _, trig := range triggers {
			out = append(out, plausibleEnvelope{
				key: fmt.Sprintf("strategy-setup/%s/push-git/%s", env, trig),
				env: workflow.StateEnvelope{
					Phase: workflow.PhaseStrategySetup, Environment: env,
					Services: []workflow.ServiceSnapshot{{
						Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						Strategy:     topology.StrategyPushGit, Trigger: trig,
						Bootstrapped: true, Deployed: false,
					}},
				},
			})
		}
	}

	// ── Export-active — container only (export.md::environments: [container]).
	out = append(out, plausibleEnvelope{
		key: "export-active/container",
		env: workflow.StateEnvelope{
			Phase: workflow.PhaseExportActive, Environment: workflow.EnvContainer,
			Services: []workflow.ServiceSnapshot{{
				Hostname: "appdev", TypeVersion: "nodejs@22",
				RuntimeClass: topology.RuntimeDynamic,
				Mode:         topology.ModeStandard,
				Strategy:     topology.StrategyPushDev,
				Bootstrapped: true, Deployed: true,
			}},
		},
	})

	return out
}

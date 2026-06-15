package port

import (
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestPortHarden_PlanWhenNoRubric: with no rubric observations, harden returns
// the harden PLAN (the checkpoints the agent must run first).
func TestPortHarden_PlanWhenNoRubric(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv) // strapi: postgresql managed dep

	res := callPort(t, srv, map[string]any{
		"action": "harden",
	})
	if res.IsError {
		t.Fatalf("harden plan failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	if body["status"] != "port-harden-plan" {
		t.Errorf("status = %v, want port-harden-plan", body["status"])
	}
	if _, ok := body["hardenPlan"]; !ok {
		t.Errorf("expected a hardenPlan in the plan response")
	}
}

// TestPortHarden_ScoresFitCeilingAndPersists: a full-HA rubric report scores a
// feasible FitCeiling at the HA-prod ceiling and persists it on the PortSession.
func TestPortHarden_ScoresFitCeilingAndPersists(t *testing.T) {
	srv, dir := portTestServer(t)
	startPort(t, srv)

	res := callPort(t, srv, map[string]any{
		"action": "harden",
		"rubric": map[string]any{
			"buildSucceeded":      true,
			"reachedActive":       true,
			"stableAfterHold":     true,
			"httpRootPassed":      true,
			"coreFlowProbePassed": true,
			"harden": map[string]any{
				"sentinelSurvivedRedeploy": true,
				"sentinelOnDurableSurface": true,
				"appContainers":            2,
				"haDeps":                   []any{"postgresql"}, // measured-HA managed dep → C6 managed-HA gate met
				"haVerifyPassed":           true,
			},
		},
		"glueRepo": map[string]any{
			"url":               "github.com/zerops-recipe-apps/strapi",
			"committedSha":      "abc123",
			"buildFromGitReady": true,
		},
	})
	if res.IsError {
		t.Fatalf("harden score failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	if body["status"] != "port-harden" {
		t.Errorf("status = %v, want port-harden", body["status"])
	}
	fc, ok := body["fitCeiling"].(map[string]any)
	if !ok {
		t.Fatalf("expected fitCeiling object, got %T", body["fitCeiling"])
	}
	if fc["feasible"] != true {
		t.Errorf("full-HA rubric must be feasible, got %v", fc["feasible"])
	}
	if fc["measuredCeiling"].(float64) != float64(PortTierHAProd) {
		t.Errorf("measured ceiling = %v, want ha-prod (%d)", fc["measuredCeiling"], PortTierHAProd)
	}
	// First feasible score → progressRose true.
	if body["progressRose"] != true {
		t.Errorf("first feasible score should set progressRose=true, got %v", body["progressRose"])
	}

	// Persisted on the PortSession.
	ps, err := LoadPortSession(dir, os.Getpid())
	if err != nil || ps == nil {
		t.Fatalf("load port session: err=%v ps=%v", err, ps)
	}
	if ps.FitCeiling == nil || !ps.FitCeiling.Feasible {
		t.Fatalf("FitCeiling must be persisted feasible, got %+v", ps.FitCeiling)
	}
}

// TestPortHarden_PartialThroughputNotHA pins the rubric contract end-to-end: a
// port that persists (C5=2) but only scales for throughput (C6=1) scores a
// small-prod ceiling with tier 5 excluded.
func TestPortHarden_PartialThroughputNotHA(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	res := callPort(t, srv, map[string]any{
		"action": "harden",
		"rubric": map[string]any{
			"buildSucceeded":      true,
			"reachedActive":       true,
			"stableAfterHold":     true,
			"httpRootPassed":      true,
			"coreFlowProbePassed": true,
			"harden": map[string]any{
				"sentinelSurvivedRedeploy": true,
				"sentinelOnDurableSurface": true,
				"appContainers":            2,
				// No haDeps → no managed dep proven HA → C6 managed-HA gate UNMET →
				// throughput-only scaling (C6=1), tier 5 excluded.
				"haVerifyPassed": false,
			},
		},
	})
	if res.IsError {
		t.Fatalf("harden score failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	fc := body["fitCeiling"].(map[string]any)
	if fc["measuredCeiling"].(float64) != float64(PortTierSmallProd) {
		t.Errorf("measured ceiling = %v, want small-prod (%d)", fc["measuredCeiling"], PortTierSmallProd)
	}
}

// TestPortHarden_AttachedAtIterateStop: after scoring a FitCeiling, hitting the
// iteration cap surfaces the measured FitCeiling in the stop response.
func TestPortHarden_AttachedAtIterateStop(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	// Score a FitCeiling first.
	res := callPort(t, srv, map[string]any{
		"action": "harden",
		"rubric": map[string]any{
			"buildSucceeded":  true,
			"reachedActive":   true,
			"stableAfterHold": true,
			"httpRootPassed":  true,
		},
	})
	if res.IsError {
		t.Fatalf("harden score failed: %s", getTextContent(t, res))
	}

	// EASY cap = 4; the harden turn does not bump the iteration counter (no
	// attempt recorded), so 4 failing iterate turns trip the cap.
	var body map[string]any
	for range 4 {
		r := callPort(t, srv, map[string]any{
			"action":       "iterate",
			"failureClass": string(topology.FailureClassVerify),
		})
		if r.IsError {
			t.Fatalf("iterate failed: %s", getTextContent(t, r))
		}
		body = decodePortJSON(t, r)
	}
	if body["stop"] != true {
		t.Fatalf("expected stop at cap, got %v", body["stop"])
	}
	if _, ok := body["fitCeiling"]; !ok {
		t.Errorf("stop response must attach the measured FitCeiling, got keys %v", keysOf(body))
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

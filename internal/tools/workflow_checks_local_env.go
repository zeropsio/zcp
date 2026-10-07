package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// LocalDotenvStatus enumerates the freshness states a CWD's `.env`
// can be in relative to its sources. See docs/spec-env-handling.md
// §13 — pinned by TestCheckLocalDotenvFresh_*.
type LocalDotenvStatus string

const (
	// LocalDotenvFresh: existing .env matches the EnvPlan; no action.
	LocalDotenvFresh LocalDotenvStatus = "fresh"
	// LocalDotenvStale: plan diff has Added or Modified entries;
	// regenerate to bring .env back in sync with sources.
	LocalDotenvStale LocalDotenvStatus = "stale"
	// LocalDotenvUnownedEdits: existing .env has keys not produced
	// by any source (user edited .env directly). Default regen
	// refuses without force=true.
	LocalDotenvUnownedEdits LocalDotenvStatus = "unowned-edits"
	// LocalDotenvMissing: no .env on disk; first-time generation
	// needed.
	LocalDotenvMissing LocalDotenvStatus = "missing"
	// LocalDotenvVPNDown: ref resolution failed transiently
	// (likely VPN/API connectivity); existing .env is unchanged.
	LocalDotenvVPNDown LocalDotenvStatus = "vpn-down"
	// LocalDotenvSkipped: the check did not apply (no zerops.yaml,
	// not a local-mode context, etc.).
	LocalDotenvSkipped LocalDotenvStatus = "skipped"
)

// LocalDotenvCheckResult is the result of checkLocalDotenvFresh.
// Detail is a one-line human-readable summary; Diff is non-nil when
// Status is Stale or UnownedEdits and lets callers (status / atom)
// surface the specific keys at issue.
type LocalDotenvCheckResult struct {
	Status       LocalDotenvStatus
	Detail       string
	Setup        string
	Diff         *ops.EnvDiff
	RecoveryHint *LocalDotenvRecovery
}

// LocalDotenvRecovery names the canonical next call for an agent to
// fix a non-fresh state. The call is always
// `zerops_env action=generate-dotenv` with appropriate args; the
// hint just makes the args explicit so the agent doesn't guess.
type LocalDotenvRecovery struct {
	Tool    string            `json:"tool"`
	Action  string            `json:"action"`
	Args    map[string]string `json:"args"`
	Comment string            `json:"comment"`
}

// checkLocalDotenvFresh inspects the CWD's `.env` against the rendered
// EnvPlan for the given setup. It is the lifecycle status primitive
// for local-mode env state — surfaces "stale", "unowned-edits", or
// "missing" with a recovery hint pointing at the canonical regen.
//
// setup may be empty: a single-block zerops.yaml auto-picks; a
// multi-block yaml without setup returns Skipped (caller passes
// the setup explicitly when known).
func checkLocalDotenvFresh(
	ctx context.Context,
	client platform.Client,
	projectID string,
	setup string,
	cwd string,
) LocalDotenvCheckResult {
	if cwd == "" {
		cwd = "."
	}
	// No zerops.yaml → not a local-mode context for this primitive.
	if _, err := os.Stat(filepath.Join(cwd, "zerops.yaml")); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(cwd, "zerops.yml")); errors.Is(err, os.ErrNotExist) {
			return LocalDotenvCheckResult{
				Status: LocalDotenvSkipped,
				Detail: "no zerops.yaml in CWD — local-mode dotenv check does not apply",
			}
		}
	}

	plan, err := ops.BuildEnvPlan(ctx, client, projectID, setup, cwd)
	if err != nil {
		var setupReq *ops.SetupRequiredError
		if errors.As(err, &setupReq) {
			return LocalDotenvCheckResult{
				Status: LocalDotenvSkipped,
				Detail: "multiple setup blocks in zerops.yaml; pass setup parameter to status",
			}
		}
		var transient *ops.RefResolveTransientError
		if errors.As(err, &transient) {
			return LocalDotenvCheckResult{
				Status: LocalDotenvVPNDown,
				Detail: fmt.Sprintf("ref resolution failed transiently for service %q — run `zcli vpn up` and retry", transient.Service),
				RecoveryHint: &LocalDotenvRecovery{
					Tool:    "zerops_env",
					Action:  "generate-dotenv",
					Args:    map[string]string{"setup": setup, "preview": "true"},
					Comment: "After VPN is up, preview the diff before writing.",
				},
			}
		}
		return LocalDotenvCheckResult{
			Status: LocalDotenvSkipped,
			Detail: fmt.Sprintf("could not build env plan: %v", err),
		}
	}

	envPath := filepath.Join(cwd, ".env")
	if _, err := os.Stat(envPath); errors.Is(err, os.ErrNotExist) {
		return LocalDotenvCheckResult{
			Status: LocalDotenvMissing,
			Setup:  plan.Setup,
			Detail: ".env does not exist; first-time generation needed",
			RecoveryHint: &LocalDotenvRecovery{
				Tool:    "zerops_env",
				Action:  "generate-dotenv",
				Args:    map[string]string{"setup": plan.Setup},
				Comment: "Creates .env from the project vault + zerops.yaml + .env.local overlay.",
			},
		}
	}

	diff, err := plan.DiffAgainstExisting(envPath)
	if err != nil {
		return LocalDotenvCheckResult{
			Status: LocalDotenvSkipped,
			Detail: fmt.Sprintf("could not diff .env: %v", err),
		}
	}

	switch {
	case diff.HasUnowned():
		return LocalDotenvCheckResult{
			Status: LocalDotenvUnownedEdits,
			Setup:  plan.Setup,
			Detail: fmt.Sprintf(".env has %d key(s) not produced by any source — move to .env.local or pass force=true", len(diff.Unowned)),
			Diff:   diff,
			RecoveryHint: &LocalDotenvRecovery{
				Tool:    "zerops_env",
				Action:  "generate-dotenv",
				Args:    map[string]string{"setup": plan.Setup, "preview": "true"},
				Comment: "Preview the diff to see which keys are unowned, then either move to .env.local or pass force=true.",
			},
		}
	case !diff.IsClean():
		return LocalDotenvCheckResult{
			Status: LocalDotenvStale,
			Setup:  plan.Setup,
			Detail: fmt.Sprintf(".env is out of sync — %d added, %d modified", len(diff.Added), len(diff.Modified)),
			Diff:   diff,
			RecoveryHint: &LocalDotenvRecovery{
				Tool:    "zerops_env",
				Action:  "generate-dotenv",
				Args:    map[string]string{"setup": plan.Setup},
				Comment: "Regenerate .env to apply the source changes.",
			},
		}
	default:
		return LocalDotenvCheckResult{
			Status: LocalDotenvFresh,
			Setup:  plan.Setup,
			Detail: ".env matches sources; no action needed",
		}
	}
}

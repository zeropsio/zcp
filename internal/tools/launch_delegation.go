// Package tools — delegated launch-token acquisition for the new-project
// mutation path (plans/token-delegation-implementation-spec-2026-07-10.md
// §4.4). The delegated path replaces the manual dashboard-mint ASK with a
// platform-minted token, authorized by a one-time delegation attached to
// ZCP's own integration token — everything downstream of acquisition
// (staging, stage-first reads, physical window close) is unchanged
// (spec §0).
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// delegatedTokenNamePrefix + delegatedTokenNameMaxLen bound the
// dashboard-visible name minted for a delegated launch-window token.
const (
	delegatedTokenNamePrefix = "zcp-launch-"
	delegatedTokenNameMaxLen = 48
)

// delegatedTokenName derives the dashboard-visible name minted for the
// delegated launch-window token from the production project name — pure
// operator recognition (does NOT feed matchLaunchToken, which matches on
// access properties, not name). Sanitizes to [a-z0-9-], collapses
// repeated HYPHEN runs only (never repeated letters), and truncates the
// suffix so the total name never exceeds the platform's length ceiling.
// An empty or fully-punctuation suffix collapses to the bare prefix
// (trailing hyphen trimmed).
func delegatedTokenName(prodName string) string {
	suffix := sanitizeDelegatedTokenSuffix(prodName)
	maxSuffixLen := delegatedTokenNameMaxLen - len(delegatedTokenNamePrefix)
	if len(suffix) > maxSuffixLen {
		suffix = strings.Trim(suffix[:maxSuffixLen], "-")
	}
	if suffix == "" {
		return strings.TrimSuffix(delegatedTokenNamePrefix, "-")
	}
	return delegatedTokenNamePrefix + suffix
}

// sanitizeDelegatedTokenSuffix lowercases s, drops every byte outside
// [a-z0-9-], and collapses runs of hyphens to one. Repeated LETTERS are
// left untouched — only hyphen runs collapse.
func sanitizeDelegatedTokenSuffix(s string) string {
	lower := strings.ToLower(s)
	kept := make([]byte, 0, len(lower))
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			kept = append(kept, c)
		}
	}
	filtered := string(kept)
	for strings.Contains(filtered, "--") {
		filtered = strings.ReplaceAll(filtered, "--", "-")
	}
	return strings.Trim(filtered, "-")
}

// delegationConsumedNarrative is the D-7 honest mid-flight-failure
// message shared by every consumed-but-uncompleted delegated-mint
// outcome (empty token, admin-client construction failure, staging
// failure — the outcome table's row 3): the one-time delegation was
// spent, a token named mintedName now exists in the dashboard, ZCP no
// longer holds its value, and recovery is regenerate + re-call with
// launchKey. cause names what specifically failed after the mint
// succeeded.
func delegationConsumedNarrative(mintedName, cause string) string {
	return fmt.Sprintf(
		"The one-time delegation was consumed minting a token named %q, but %s. "+
			"ZCP no longer holds its value — the platform shows it exactly once at mint time. "+
			"Regenerate that token in the Zerops dashboard (Settings → Access Tokens Management) "+
			"and re-call this workflow with launchKey=<the regenerated value>.",
		mintedName, cause)
}

// delegationMintIndeterminateResponse renders the indeterminate-mint
// outcome (timeout, 5xx, transport error — anything other than a typed
// ErrDelegationUnavailable): the POST may have committed server-side
// even though this call errored, so this is NOT the D-6 manual fallback
// (the delegation may already be burned) — a distinct blocker directing
// the agent to check the dashboard rather than silently retry. Never
// serializes the raw SDK error body into the response (the caller logs
// it to stderr instead).
func delegationMintIndeterminateResponse(corpus []workflow.KnowledgeAtom, mintedName string) *mcp.CallToolResult {
	msg := fmt.Sprintf(
		"Minting the delegated launch token (requested name %q) did not return a clear success or failure — "+
			"the request may have committed server-side even though this call errored. NEVER auto-retry the mint. "+
			"Check the Zerops dashboard (Settings → Access Tokens Management) for a token named %q: if present, "+
			"regenerate it there and re-call this workflow with launchKey=<the regenerated value>; if absent, the "+
			"delegation may still be available — retry action=\"start\" workflow=\"launch-production\" confirmLaunch=true.",
		mintedName, mintedName)
	return launchFailedResponse(corpus, topology.BlockerCategoryAuth, "delegation-mint-indeterminate", msg)
}

// delegationUnavailableResponse renders the D-6 fallback when no usable
// delegation exists (empty list, or the mint raced to a typed
// ErrDelegationUnavailable): status STAYS "ready-to-launch" (not
// "failed") and the response is otherwise the same shape as a normal
// ready-to-launch — manual launchKey walkthrough text verbatim,
// sanitized inputs, bundle preview, readiness checks, source context
// when available — with delegatedLaunch.available=false and a blocker
// pointing back at the manual path. sourceCtx is best-effort (nil when
// the discovery read fails); the mutation path does not otherwise
// compute it the way the read-side ready-to-launch branch does.
func delegationUnavailableResponse(
	corpus []workflow.KnowledgeAtom,
	input WorkflowInput,
	sourceEnvs []platform.ProjectEnvVar,
	sourceCtx *launchSourceContext,
	launchBundle *ops.LaunchBundle,
	bundleInputs ops.LaunchBundleInputs,
	resolved []resolvedLaunchRuntime,
	stateDir string,
) *mcp.CallToolResult {
	evidenceSources := make([]readinessEvidenceSource, 0, len(resolved))
	for _, r := range resolved {
		evidenceSources = append(evidenceSources, readinessEvidenceSource{
			PushHostname: r.PushHostname,
			SetupName:    r.SetupName,
		})
	}
	payload := buildLaunchReadyToLaunchPayload(corpus, input, sourceEnvs, sourceCtx,
		runReadinessRubric(launchBundle, bundleInputs, readinessEvidenceInput{StateDir: stateDir, Sources: evidenceSources}),
		launchBundlePreviewFrom(launchBundle, bundleInputs))
	payload.DelegatedLaunch = &delegatedLaunchAvailability{Available: false}
	payload.Blockers = append(payload.Blockers, topology.Blocker{
		ID:       "delegation-unavailable",
		Severity: topology.BlockerSeverityBlock,
		Category: topology.BlockerCategoryAuth,
		Message: "This token has no unused delegation to mint a launch token from — never granted one, already " +
			"consumed, or revoked. Fall back to the manual path: generate a launch integration token in the dashboard " +
			"and re-call with launchKey set.",
		Recovery: &topology.Recovery{
			Tool:   "zerops_workflow",
			Action: "start",
			Args:   map[string]string{"workflow": workflowLaunchProduction},
		},
	})
	return jsonResult(payload)
}

// abortDelegatedMint marks an in-flight delegated-mint attempt as failed
// with an empty TargetProjectID, so the resume-gate's P0 concurrent-
// mutation lock (a genuinely in-flight topology.LaunchStatusLaunching)
// does not block an immediate retry — Failed+no-target-project is
// already the existing "safe to retry" resume branch (handleLaunchProduction).
// Best-effort: a write failure here just leaves the pre-mint Launching
// state to age out past launchMutationStaleAfter.
func abortDelegatedMint(stateDir, launchID, sourceProjectID, targetProjectName, reason string) {
	_ = writeLaunchState(stateDir, &launchState{
		LaunchID:          launchID,
		SourceProjectID:   sourceProjectID,
		TargetProjectName: targetProjectName,
		Status:            topology.LaunchStatusFailed,
		LastError:         reason,
	})
}

// resolveDelegatedLaunchToken runs the §4.4 acquisition sequence for the
// delegated new-project publish path (ConfirmLaunch=true, no explicit
// LaunchKey). The caller (executeLaunchMutation) invokes this
// immediately before staging, after every refusal gate has already
// passed (D-3) — a successful mint burns the one-time delegation even on
// a later failure, so nothing that can still legitimately refuse the
// launch may run after this point.
//
// Order: staged-secret probe (delegated retry — zero list/mint calls
// when a prior attempt already staged a token, §4.5) → list
// delegations (D-1, fresh read every call) → FATAL pre-mint state
// write → mint. Returns either a resolved token + the locally-
// retained requested name (recovery text uses this, never the platform
// DTO), or a non-nil terminal response per the mint-outcome table —
// the caller returns that response as-is without constructing an admin
// client.
func resolveDelegatedLaunchToken(
	ctx context.Context,
	client platform.Client,
	sourceProjectID string,
	corpus []workflow.KnowledgeAtom,
	input WorkflowInput,
	stateDir, launchID string,
	primaryRuntime resolvedLaunchRuntime,
	sourceEnvs []platform.ProjectEnvVar,
	launchBundle *ops.LaunchBundle,
	bundleInputs ops.LaunchBundleInputs,
	resolved []resolvedLaunchRuntime,
	rt runtime.Info,
) (token string, mintedName string, resp *mcp.CallToolResult) {
	auditReject := func(reason string) {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      reason,
		})
	}

	// Delegated retry (§4.5): a prior attempt may have staged the token
	// before failing later (create/poll/pipeline). Resolving the staged
	// value first means a retry costs ZERO delegation list/mint calls
	// — the delegation was already spent by the earlier attempt.
	stageProbe := &launchState{TargetServiceHostname: primaryRuntime.PushHostname}
	staged, stageErr := launchKeyFromStage(ctx, client, sourceProjectID, stageProbe)
	if stageErr != nil {
		auditReject("delegated retry: read staged token: " + stageErr.Error())
		return "", "", launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"delegation-stage-read-failed",
			fmt.Sprintf("Could not confirm whether a launch token from a prior attempt was already staged: %v. "+
				"Retrying now could mint a second token if one was already minted and staged. "+
				"Fix the read cause (service reachable?) and re-call with confirmLaunch=true once resolved.", stageErr))
	}
	if staged != "" {
		return staged, "", nil
	}

	delegations, listErr := client.ListOwnTokenDelegations(ctx)
	if listErr != nil {
		fmt.Fprintf(os.Stderr, "zcp: list own token delegations: %v\n", listErr)
	}
	if listErr != nil || !delegationsUsable(delegations) {
		auditReject("delegated mint: no usable delegation")
		sourceCtx := gatherLaunchSourceContext(ctx, client, sourceProjectID, stateDir, rt)
		return "", "", delegationUnavailableResponse(corpus, input, sourceEnvs, sourceCtx, launchBundle, bundleInputs, resolved, stateDir)
	}

	mintedName = delegatedTokenName(input.ProductionProjectName)

	// FATAL pre-mint state write (delegated-path-only gate, D-3): if
	// THIS write fails, abort BEFORE the mint — nothing was burned
	// yet. Reuses the Launching status so a genuinely-stuck attempt
	// still trips the existing P0 concurrent-mutation lock; success here
	// also carries the forensic trail (acquisition + requested name)
	// forward if the process dies between minting and the later full
	// state write in executeLaunchMutation.
	preMint := &launchState{
		LaunchID:          launchID,
		SourceProjectID:   sourceProjectID,
		TargetProjectName: input.ProductionProjectName,
		Status:            topology.LaunchStatusLaunching,
		TokenAcquisition:  "delegated",
		MintedTokenName:   mintedName,
	}
	if err := writeLaunchState(stateDir, preMint); err != nil {
		auditReject("write pre-mint launch state: " + err.Error())
		return "", "", launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"launch-state-write-failed",
			fmt.Sprintf("Recording the delegated launch attempt failed before minting: %v. "+
				"Nothing was created and no delegation was consumed — safe to retry.", err))
	}

	minted, mintErr := client.MintDelegatedLaunchToken(ctx, mintedName)
	if mintErr != nil {
		var pe *platform.PlatformError
		if errors.As(mintErr, &pe) && pe.Code == platform.ErrDelegationUnavailable {
			// Race: usable at list-time, consumed by the time the mint
			// landed. Nothing was created by THIS call — unblock
			// retries and fall back to the manual path (D-6).
			abortDelegatedMint(stateDir, launchID, sourceProjectID, input.ProductionProjectName, "delegated mint: no usable delegation (race)")
			auditReject("delegated mint: no usable delegation (race)")
			sourceCtx := gatherLaunchSourceContext(ctx, client, sourceProjectID, stateDir, rt)
			return "", "", delegationUnavailableResponse(corpus, input, sourceEnvs, sourceCtx, launchBundle, bundleInputs, resolved, stateDir)
		}
		// Indeterminate: never serialize the raw SDK error body into the
		// response — stderr only.
		fmt.Fprintf(os.Stderr, "zcp: delegated mint indeterminate error (requested name %q): %v\n", mintedName, mintErr)
		abortDelegatedMint(stateDir, launchID, sourceProjectID, input.ProductionProjectName, "delegated mint: indeterminate error")
		auditReject("delegated mint: indeterminate error")
		return "", "", delegationMintIndeterminateResponse(corpus, mintedName)
	}
	if minted.Token == "" {
		abortDelegatedMint(stateDir, launchID, sourceProjectID, input.ProductionProjectName, "delegated mint returned an empty token")
		auditReject("delegated mint returned an empty token")
		return "", "", launchFailedResponse(corpus, topology.BlockerCategoryAuth, "delegation-consumed",
			delegationConsumedNarrative(mintedName, "the platform returned an empty token value"))
	}
	return minted.Token, mintedName, nil
}

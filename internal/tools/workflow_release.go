package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// releaseTagRe matches the semver release tags the production pipeline's
// default tag regex (`^v\d+\.\d+\.\d+$`) consumes.
var releaseTagRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// handleRelease is the source-side release act (spec-git-delivery-target
// §7, Karel's "ten člověk řekne, že chce release"): verify the working
// tree is clean and HEAD is on the remote (the P-LP-11 read, reused),
// derive the next semver from the remote's existing v* tags, and — once
// the user confirmed a version — create the annotated tag at HEAD and
// push it. The tag fires whichever production pipeline exists (the
// launch-emitted Actions tag workflow or the dashboard TAG integration).
//
// This is a SOURCE-repo act: same trust surface as every other push —
// ZCP's zero-standing-prod-access model is untouched (no prod credential
// is involved; the platform/CI consumes the tag).
//
// Two-call narrowing: without releaseVersion the handler returns
// status="release-prompt" carrying the freshness evidence + suggested
// next version; the re-call with releaseVersion executes the tag push.
func handleRelease(
	ctx context.Context,
	sshDeployer ops.SSHDeployer,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	if input.Service == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"service is required for action=release",
			"Pass service=<push-source hostname> (the pair whose repo feeds production)."), WithRecoveryStatus()), nil, nil
	}
	meta, err := workflow.FindServiceMeta(stateDir, input.Service)
	if err != nil || meta == nil || !meta.IsComplete() {
		return convertError(platform.NewPlatformError(
			platform.ErrAdoptRequired,
			fmt.Sprintf("Service %q is not bootstrapped", input.Service),
			"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\""), WithRecoveryStatus()), nil, nil
	}
	if meta.GitPushState != topology.GitPushConfigured {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("release needs the repo as source of truth — git-push is not configured for %q", input.Service),
			fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q first; a release tags the PUSHED state.", meta.Hostname),
		), WithRecoveryStatus()), nil, nil
	}

	// Freshness evidence — the P-LP-11 read, reused verbatim: the tag must
	// name exactly the pushed state the production pipeline will build.
	proof, proofErr := launchPushProofReader(ctx, sshDeployer, rt, meta.Hostname, meta.RemoteURL)
	if proofErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("release: could not VERIFY source freshness on %q (read failed: %v) — this is a transport problem, not a state problem", meta.Hostname, proofErr),
			"Retry once SSH/exec is healthy; do not tag unverified state.",
		), WithRecoveryStatus()), nil, nil
	}
	switch {
	case proof.DirtyTree:
		return convertError(platform.NewPlatformError(
			platform.ErrPreflightFailed,
			fmt.Sprintf("release refused: %q has uncommitted changes — a release tags the pushed HEAD, and the tree differs from it", meta.Hostname),
			fmt.Sprintf("Commit, then deliver via push first: zerops_deploy targetService=%q strategy=\"git-push\". Re-call action=\"release\" once green.", meta.Hostname),
		), WithRecoveryStatus()), nil, nil
	case proof.LocalHead == "" || proof.RemoteHead == "" || proof.LocalHead != proof.RemoteHead:
		return convertError(platform.NewPlatformError(
			platform.ErrPreflightFailed,
			fmt.Sprintf("release refused: HEAD on %q is not the remote HEAD (local=%s remote=%s) — production would build different code than you release", meta.Hostname, shortSHA(proof.LocalHead), shortSHA(proof.RemoteHead)),
			fmt.Sprintf("Push first: zerops_deploy targetService=%q strategy=\"git-push\", then re-call action=\"release\".", meta.Hostname),
		), WithRecoveryStatus()), nil, nil
	}

	existing, suggestion := releaseTagSuggestion(ctx, sshDeployer, rt, meta)

	if input.ReleaseVersion == "" {
		return jsonResult(map[string]any{
			"status":           "release-prompt",
			"service":          meta.Hostname,
			"head":             shortSHA(proof.LocalHead),
			"existingTags":     existing,
			"suggestedVersion": suggestion,
			"pipeline":         releasePipelineNote(meta),
			"nextStep":         fmt.Sprintf("Confirm the version with the user (suggested %q), then re-call: zerops_workflow action=\"release\" service=%q releaseVersion=%q. The tag is created at the verified pushed HEAD and fires the production pipeline.", suggestion, meta.Hostname, suggestion),
		}), nil, nil
	}

	version := strings.TrimSpace(input.ReleaseVersion)
	if !releaseTagRe.MatchString(version) {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("releaseVersion %q does not match the production pipeline's tag shape (vMAJOR.MINOR.PATCH)", version),
			fmt.Sprintf("Pass e.g. releaseVersion=%q — the default pipeline tag regex is ^v\\d+\\.\\d+\\.\\d+$.", suggestion),
		), WithRecoveryStatus()), nil, nil
	}
	for _, tag := range existing {
		if tag == version {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("release tag %q already exists on the remote", version),
				fmt.Sprintf("Pick the next free version (suggested %q).", suggestion),
			), WithRecoveryStatus()), nil, nil
		}
	}

	tagCmd := ops.BuildGitTagPushCommand("/var/www", version)
	if !rt.InContainer {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"action=release runs the tag push on the container push source; in local mode tag from your own shell",
			fmt.Sprintf("Run locally: git tag -a %s -m \"release %s\" && git push origin %s", version, version, version),
		), WithRecoveryStatus()), nil, nil
	}
	if _, tagErr := sshDeployer.ExecSSH(ctx, meta.Hostname, tagCmd); tagErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			withSSHStderr(fmt.Sprintf("release: tag push %s from %q failed", version, meta.Hostname), tagErr),
			"Nothing fired — the tag did not reach the remote. Fix the named cause and re-call.",
		), WithRecoveryStatus()), nil, nil
	}

	return jsonResult(map[string]any{
		"status":   "released",
		"service":  meta.Hostname,
		"version":  version,
		"head":     shortSHA(proof.LocalHead),
		"pipeline": releasePipelineNote(meta),
		"nextStep": "The tag is on the remote and the production pipeline owns the rest. ZCP holds no production access — confirm the deploy in the production dashboard (or the repo's Actions runs for the tag workflow), then smoke-test.",
	}), nil, nil
}

// releaseTagSuggestion reads the remote's v* tags (authenticated — works
// for private repos) and suggests the next patch bump. Defaults to
// v1.0.0 on a tag-less repo. Best-effort: a read failure yields the
// default suggestion with no existing list.
func releaseTagSuggestion(ctx context.Context, sshDeployer ops.SSHDeployer, rt runtime.Info, meta *workflow.ServiceMeta) ([]string, string) {
	if !rt.InContainer || sshDeployer == nil {
		return nil, "v1.0.0"
	}
	out, err := sshDeployer.ExecSSH(ctx, meta.Hostname, "cd /var/www && "+ops.BuildGitTagListCommand(meta.RemoteURL))
	if err != nil {
		return nil, "v1.0.0"
	}
	var tags []string
	best := [3]int{}
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		idx := strings.LastIndex(line, "refs/tags/")
		if idx == -1 {
			continue
		}
		tag := strings.TrimSuffix(strings.TrimSpace(line[idx+len("refs/tags/"):]), "^{}")
		m := releaseTagRe.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		tags = append(tags, tag)
		maj, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		cur := [3]int{maj, minor, patch}
		if !found || semverGreater(cur, best) {
			best = cur
			found = true
		}
	}
	sort.Strings(tags)
	tags = dedupSortedStrings(tags)
	if !found {
		return tags, "v1.0.0"
	}
	return tags, fmt.Sprintf("v%d.%d.%d", best[0], best[1], best[2]+1)
}

func semverGreater(a, b [3]int) bool {
	if a[0] != b[0] {
		return a[0] > b[0]
	}
	if a[1] != b[1] {
		return a[1] > b[1]
	}
	return a[2] > b[2]
}

func dedupSortedStrings(in []string) []string {
	out := in[:0]
	var prev string
	for i, s := range in {
		if i == 0 || s != prev {
			out = append(out, s)
		}
		prev = s
	}
	return out
}

// releasePipelineNote names what the tag will fire, derived from the
// recorded state — never a guess presented as fact.
func releasePipelineNote(meta *workflow.ServiceMeta) string {
	if len(meta.ProdLaunches) == 0 {
		return "No production launch is recorded for this pair — the tag lands on the remote, but a production pipeline only exists after zerops_workflow workflow=\"launch-production\" (or your own CI consumes tags)."
	}
	return "Production pipeline fires on the tag: the dashboard TAG integration and/or the launch-emitted Actions tag workflow rebuild the production runtime from this exact HEAD."
}

// shortSHA renders the first 10 chars of a SHA for display.
func shortSHA(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	if sha == "" {
		return "<none>"
	}
	return sha
}

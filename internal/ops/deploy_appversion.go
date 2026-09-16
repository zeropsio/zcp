package ops

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// RedeployLastAppVersion re-deploys a service's most recent appVersion via
// PUT /app-version/{id}/deploy (docs/spec-workflows.md §8 R2) — the ONLY
// in-place recovery for a never-activated buildFromGit service: no
// rebuild, no re-import, the already-built artifact goes ACTIVE. Live-
// verified: the platform does NOT reuse the stored yaml, so the caller
// resends it verbatim.
//
// The newest appVersion (by Sequence, via the DIRECT ListServiceAppVersions
// — never the ES-backed SearchAppVersions, so a just-settled status is
// never missed) must be DEPLOY_FAILED (the artifact-redeploy shape R2
// names) or ACTIVE (a repeat call against an already-recovered service) —
// any other status errors naming it, since no other status names a built,
// redeployable artifact.
//
// zerops.yaml text cascade — first hit wins:
//  1. The appVersion's own ZEROPS_YAML user-data blob (GetAppVersionZeropsYaml).
//  2. The app-code archive (GetAppVersionAppCode → fetch → extract
//     zerops.yaml). archiveYamlFetcher is a package var so tests can stub
//     the HTTP+zip round trip (ops cannot import workflow, which owns the
//     production-grade equivalent — see archiveYamlFetcher's doc).
//  3. The public git source (publicGitSource url + branch → raw
//     zerops.yaml on github.com/gitlab.com; rawGitYamlFetcher is the
//     stubbable fetch). Live-verified 2026-09-14: for a buildFromGit
//     appVersion the platform stores no yaml and the app-code archive is
//     the build output, so this is the step that actually fires.
//  4. Nothing yields text → error naming every attempt.
//
// setup: the caller-supplied value when non-empty, else derived from the
// yaml (resolveRedeploySetup: the only setup, or the one named like the
// hostname); ambiguity is an error listing the candidates because the
// platform records no setup name for a GIT appVersion.
func RedeployLastAppVersion(
	ctx context.Context,
	client platform.Client,
	projectID, hostname, setup string,
) (*DeployResult, error) {
	svc, err := LookupService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, fmt.Errorf("redeploy last app version: %w", err)
	}

	versions, err := client.ListServiceAppVersions(ctx, svc.ID)
	if err != nil {
		return nil, fmt.Errorf("redeploy last app version: %w", err)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("redeploy last app version: service %s has no app-version history", hostname)
	}
	newest := versions[0]
	for _, v := range versions[1:] {
		if v.Sequence > newest.Sequence {
			newest = v
		}
	}
	// ACTIVE is deliberately NOT accepted here (live-verified 2026-09-14):
	// the platform 400s (appVersionInvalidStatus) a PUT
	// /app-version/{id}/deploy against the currently active version, so
	// treating it as a redeployable "newest" status would always fail
	// against the platform. Rolling an ACTIVE service's older BACKUP
	// appVersion back into place is ops.ReactivateAppVersion's job
	// (deploy_rollback.go), not this newest-only recovery path.
	switch newest.Status {
	case platform.BuildStatusDeployFailed:
	default:
		return nil, fmt.Errorf(
			"redeploy last app version: newest appVersion %s status is %s, want %s",
			newest.ID, newest.Status, platform.BuildStatusDeployFailed,
		)
	}

	yaml, err := resolveRedeployYaml(ctx, client, newest)
	if err != nil {
		return nil, fmt.Errorf("redeploy last app version: %w", err)
	}

	effectiveSetup, err := resolveRedeploySetup(setup, hostname, yaml)
	if err != nil {
		return nil, fmt.Errorf("redeploy last app version: %w", err)
	}

	proc, err := client.RedeployAppVersion(ctx, newest.ID, yaml, effectiveSetup)
	if err != nil {
		return nil, fmt.Errorf("redeploy last app version: %w", err)
	}

	result := &DeployResult{
		Mode:            "appversion",
		TargetService:   hostname,
		TargetServiceID: svc.ID,
	}

	final, err := PollProcess(ctx, client, proc.ID, nil)
	if err != nil {
		result.TimedOut = true
		return result, nil //nolint:nilerr // timeout/cancel is reported on the result, not as an error
	}

	if final.Status == platform.ProcessStatusFinished {
		result.Status = platform.BuildStatusDeployed
		result.Message = fmt.Sprintf("Successfully redeployed the built artifact to %s.", hostname)
		return result, nil
	}

	result.Status = platform.BuildStatusDeployFailed
	result.FailedPhase = "init"
	result.FailureClassification = ClassifyDeployFailure(FailureInput{
		Phase:  PhaseInit,
		Status: platform.BuildStatusDeployFailed,
	})
	if result.FailureClassification != nil && result.FailureClassification.SuggestedAction != "" {
		result.Suggestion = result.FailureClassification.SuggestedAction
	}
	return result, nil
}

// resolveRedeployYaml runs the yaml-source cascade described on
// RedeployLastAppVersion's doc comment.
func resolveRedeployYaml(ctx context.Context, client platform.Client, av platform.AppVersionEvent) (string, error) {
	yaml, err := client.GetAppVersionZeropsYaml(ctx, av.ID)
	if err == nil && yaml != "" {
		return yaml, nil
	}

	url, urlErr := client.GetAppVersionAppCode(ctx, av.ID)
	if urlErr == nil && url != "" {
		if archiveYaml, fetchErr := archiveYamlFetcher(ctx, url); fetchErr == nil && archiveYaml != "" {
			return archiveYaml, nil
		}
	}

	// Step 3: a GIT-sourced appVersion (buildFromGit) — live-verified
	// 2026-09-14: the platform stores neither the zerops.yaml text nor the
	// setup name for it, and the app-code archive is the build OUTPUT
	// (deployFiles), which normally omits zerops.yaml. The repo itself is
	// the only source: fetch the file raw from the public git host.
	if av.PublicGitSource != nil && av.PublicGitSource.GitURL != "" {
		rawURL, urlErr := rawZeropsYamlURL(av.PublicGitSource.GitURL, av.PublicGitSource.BranchName)
		if urlErr != nil {
			return "", fmt.Errorf("no zerops.yaml source available for appVersion %s: no ZEROPS_YAML user-data blob, the app-code archive yielded no zerops.yaml, and the git source cannot be fetched raw: %w", av.ID, urlErr)
		}
		gitYaml, fetchErr := rawGitYamlFetcher(ctx, rawURL)
		if fetchErr != nil {
			return "", fmt.Errorf("no zerops.yaml source available for appVersion %s: no ZEROPS_YAML user-data blob, the app-code archive yielded no zerops.yaml, and %s: %w", av.ID, rawURL, fetchErr)
		}
		if gitYaml != "" {
			return gitYaml, nil
		}
	}

	return "", fmt.Errorf(
		"no zerops.yaml source available for appVersion %s: no ZEROPS_YAML user-data blob, the app-code archive yielded no zerops.yaml, and the appVersion has no public git source",
		av.ID,
	)
}

// rawZeropsYamlURL maps a public git repo URL + branch to the raw
// zerops.yaml URL of the hosts buildFromGit supports. An empty branch is
// the platform's default branch name, `main`.
func rawZeropsYamlURL(gitURL, branch string) (string, error) {
	u, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(gitURL), ".git"))
	if err != nil {
		return "", fmt.Errorf("git source url %q: %w", gitURL, err)
	}
	if branch == "" {
		branch = "main"
	}
	repoPath := strings.Trim(u.Path, "/")
	switch strings.ToLower(u.Host) {
	case "github.com", "www.github.com":
		return "https://raw.githubusercontent.com/" + repoPath + "/" + branch + "/zerops.yaml", nil
	case "gitlab.com", "www.gitlab.com":
		return "https://gitlab.com/" + repoPath + "/-/raw/" + branch + "/zerops.yaml", nil
	}
	return "", fmt.Errorf("git host %q is not supported for a raw zerops.yaml fetch (github.com and gitlab.com are)", u.Host)
}

// rawGitYamlFetcher fetches the raw zerops.yaml text from a public git
// host (cascade step 3). Package var so tests stub the network.
var rawGitYamlFetcher = defaultRawGitYamlFetcher

func defaultRawGitYamlFetcher(ctx context.Context, rawURL string) (string, error) {
	const maxBytes = 1 << 20
	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("raw git fetch: new request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("raw git fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("raw git fetch: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", fmt.Errorf("raw git fetch: read body: %w", err)
	}
	return string(body), nil
}

// resolveRedeploySetup picks the zerops.yaml setup block for the redeploy:
// the caller's explicit choice; else the only setup the yaml declares; else
// the setup named like the hostname (the platform's own default); else an
// error listing the candidates — the platform records no setup name for a
// GIT appVersion, so there is nothing further to read.
func resolveRedeploySetup(setup, hostname, yamlText string) (string, error) {
	if setup != "" {
		return setup, nil
	}
	doc, err := ParseZeropsYmlContent([]byte(yamlText), "zerops.yaml")
	if err != nil {
		return "", err
	}
	names := doc.SetupNames()
	switch {
	case len(names) == 1:
		return names[0], nil
	case slices.Contains(names, hostname):
		return hostname, nil
	case len(names) == 0:
		return hostname, nil
	}
	return "", fmt.Errorf("zerops.yaml declares setups %v and none is named %q — pass setup=<name> to zerops_deploy", names, hostname)
}

// archiveYamlFetcher is the app-code archive fallback: HTTP GET the signed
// URL, decode as zip, extract zerops.yaml. A package var (not a
// workflow.ArchiveFetcher import — ops does not import workflow, see
// docs/spec-architecture.md dependency rule) so tests can stub the round
// trip; production always runs defaultArchiveYamlFetcher.
//
// Deliberately parallels workflow.DefaultArchiveFetcher (setup-name
// cascade step 4) rather than sharing it — the two packages cannot import
// each other, and this extraction is small/self-contained enough that
// duplicating it beats promoting it to a shared layer for one caller.
var archiveYamlFetcher = defaultArchiveYamlFetcher

func defaultArchiveYamlFetcher(ctx context.Context, url string) (string, error) {
	const maxBytes = 50 * 1024 * 1024
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("archive fetch: new request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("archive fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("archive fetch: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", fmt.Errorf("archive fetch: read body: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("archive fetch: zip open: %w", err)
	}
	for _, f := range zr.File {
		base := f.Name
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		if base != zeropsYmlPrimary && base != zeropsYmlFallback {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("archive fetch: open entry %q: %w", f.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxBytes))
		_ = rc.Close()
		if err != nil {
			return "", fmt.Errorf("archive fetch: read entry %q: %w", f.Name, err)
		}
		return string(content), nil
	}
	return "", nil // no zerops.yaml in archive
}

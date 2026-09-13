package ops

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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
//  3. Neither yields text → error naming both attempts.
//
// setup: the caller-supplied value when non-empty, else the hostname (the
// platform's own default) — matching the caller's fallback contract
// (ServiceMeta.PrimarySetupName/ProdSetupName resolution lives above this
// layer; this function takes whatever the caller already resolved).
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
	switch newest.Status {
	case platform.BuildStatusDeployFailed, platform.ServiceStatusActive:
	default:
		return nil, fmt.Errorf(
			"redeploy last app version: newest appVersion %s status is %s, want %s or %s",
			newest.ID, newest.Status, platform.BuildStatusDeployFailed, platform.ServiceStatusActive,
		)
	}

	yaml, err := resolveRedeployYaml(ctx, client, newest.ID)
	if err != nil {
		return nil, fmt.Errorf("redeploy last app version: %w", err)
	}

	effectiveSetup := setup
	if effectiveSetup == "" {
		effectiveSetup = hostname
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
func resolveRedeployYaml(ctx context.Context, client platform.Client, appVersionID string) (string, error) {
	yaml, err := client.GetAppVersionZeropsYaml(ctx, appVersionID)
	if err == nil && yaml != "" {
		return yaml, nil
	}

	url, urlErr := client.GetAppVersionAppCode(ctx, appVersionID)
	if urlErr == nil && url != "" {
		if archiveYaml, fetchErr := archiveYamlFetcher(ctx, url); fetchErr == nil && archiveYaml != "" {
			return archiveYaml, nil
		}
	}

	return "", fmt.Errorf(
		"no zerops.yaml source available for appVersion %s: no ZEROPS_YAML user-data blob, and the app-code archive yielded no zerops.yaml",
		appVersionID,
	)
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

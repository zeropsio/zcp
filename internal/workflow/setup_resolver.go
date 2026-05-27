package workflow

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// ErrRequiresSetupInput signals that ResolveCanonicalSetup exhausted its
// cascade without producing a deterministic setup name. The structured
// payload feeds the requiresSetupInput blocker — see plan
// `plans/setup-name-local-canonical-2026-05-27.md` §requiresSetupInput.
//
// AvailableSetups carries the setup-block names the cascade saw in any
// readable yaml source (archive extract OR LocalYAMLBody); empty when
// no yaml was inspected. Callers project this into the user-facing
// recovery response so the agent surfaces a concrete chooser, not a
// generic prompt.
//
//nolint:errname // domain blocker, not idiomatic XxxError suffix
type ErrRequiresSetupInput struct {
	Service         string
	TargetHostname  string
	AvailableSetups []string
	Reason          string
}

func (e *ErrRequiresSetupInput) Error() string {
	if len(e.AvailableSetups) > 0 {
		return fmt.Sprintf("requiresSetupInput: %s (available: %v)", e.Reason, e.AvailableSetups)
	}
	return fmt.Sprintf("requiresSetupInput: %s", e.Reason)
}

// ResolveCanonicalSetupInput bundles the cascade inputs. Each field is
// optional — empty values just skip the corresponding cascade step rather
// than hard-error. Pass everything you have access to; the cascade picks
// the first deterministic hit.
type ResolveCanonicalSetupInput struct {
	StateDir       string        // local meta dir (.zcp/state); empty → skip cache + write-back
	ServiceID      string        // platform service stack ID; empty → skip steps 1-3
	TargetHostname string        // REQUIRED — selects meta half + drives disambiguation candidates
	Mode           topology.Mode // hint for candidate cascade (step 5)
	LocalYAMLBody  string        // optional step 5 fallback (workingDir yaml / SSH cat content)

	// ArchiveFetcher is the step 4 fetch hook. Production uses
	// DefaultArchiveFetcher (HTTP GET + zip decode + extract zerops.yaml).
	// Tests inject a stub that returns canned body / error so cascade-step
	// logic can be exercised without an HTTP harness. Nil → use default.
	ArchiveFetcher ArchiveFetcher
}

// ArchiveFetcher downloads the source archive at url, extracts the
// zerops.yaml content, and returns the body. Empty body OR non-nil
// error → caller treats as cascade miss (step 4 short-circuits to
// step 5). Plan: §ResolveCanonicalSetup cascade.
type ArchiveFetcher func(ctx context.Context, url string) (yamlBody string, err error)

// ResolveCanonicalSetup runs the 6-step cascade and returns the canonical
// setup-block name for the (service, hostname) pair. On any cascade hit
// at steps 2-5, the resolver writes back to local ServiceMeta cache (no
// platform write of any kind — see plan §Architectural decision).
//
// Cascade — first hit wins:
//  1. Local cache (ServiceMeta.PrimarySetupName / StageSetupName via
//     SetupNameFor)
//  2. ServiceStack.GithubIntegration.ZeropsYamlSetup
//  3. ServiceStack.ActiveAppVersion.GithubIntegration.ZeropsYamlSetup
//  4. GetAppVersionAppCode → fetch archive → extract zerops.yaml → parse
//  5. LocalYAMLBody → PickSetupNameFromNames
//  6. nil + ErrRequiresSetupInput{AvailableSetups, Reason}
//
// Read-only callers (status, discover, checks, envelope-build) MUST NOT
// call this — use ServiceMeta.SetupNameFor directly and accept "" as a
// cache-miss signal. Write-back is reserved for mutation surfaces
// (deploy preflight, git-push validation, build-integration anticipation,
// launch composer, adoption gate).
func ResolveCanonicalSetup(ctx context.Context, client platform.Client, in ResolveCanonicalSetupInput) (string, error) {
	if in.TargetHostname == "" {
		return "", errors.New("ResolveCanonicalSetup: TargetHostname is required")
	}

	// Step 1 — local cache via SetupNameFor.
	if in.StateDir != "" {
		if meta, _ := FindServiceMeta(in.StateDir, in.TargetHostname); meta != nil {
			if cached := meta.SetupNameFor(in.TargetHostname); cached != "" {
				return cached, nil
			}
		}
	}

	// Steps 2-4 need platform client + service ID.
	var activeAppVersionID string
	if client != nil && in.ServiceID != "" {
		// Step 2 — ServiceStack.GithubIntegration.ZeropsYamlSetup.
		if status, err := client.GetServiceStackIntegrationStatus(ctx, in.ServiceID); err == nil {
			if status.State == platform.IntegrationConfigured && status.ZeropsYamlSetup != "" {
				_ = writeBackCache(in.StateDir, in.TargetHostname, status.ZeropsYamlSetup)
				return status.ZeropsYamlSetup, nil
			}
		}

		// Step 3 — ServiceStack.ActiveAppVersion.GithubIntegration.ZeropsYamlSetup.
		// Also captures activeAppVersionID for step 4.
		if svc, err := client.GetService(ctx, in.ServiceID); err == nil && svc != nil && svc.ActiveAppVersion != nil {
			activeAppVersionID = svc.ActiveAppVersion.ID
			if svc.ActiveAppVersion.GithubIntegrationSetup != "" {
				_ = writeBackCache(in.StateDir, in.TargetHostname, svc.ActiveAppVersion.GithubIntegrationSetup)
				return svc.ActiveAppVersion.GithubIntegrationSetup, nil
			}
		}

		// Step 4 — GetAppVersionAppCode → fetch archive → extract zerops.yaml.
		if activeAppVersionID != "" {
			if url, err := client.GetAppVersionAppCode(ctx, activeAppVersionID); err == nil && url != "" {
				fetch := in.ArchiveFetcher
				if fetch == nil {
					fetch = DefaultArchiveFetcher
				}
				if yamlBody, fetchErr := fetch(ctx, url); fetchErr == nil && yamlBody != "" {
					if names, parseErr := ListSetupNames(yamlBody); parseErr == nil {
						if picked, ok := PickSetupNameFromNames(names, in.TargetHostname, in.Mode); ok {
							_ = writeBackCache(in.StateDir, in.TargetHostname, picked)
							return picked, nil
						}
						if len(names) > 0 {
							return "", &ErrRequiresSetupInput{
								Service:         in.ServiceID,
								TargetHostname:  in.TargetHostname,
								AvailableSetups: names,
								Reason:          "archive zerops.yaml has multiple setups; no hostname/suffix match",
							}
						}
					}
				}
			}
		}
	}

	// Step 5 — LocalYAMLBody when supplied.
	if in.LocalYAMLBody != "" {
		names, err := ListSetupNames(in.LocalYAMLBody)
		if err != nil {
			return "", &ErrRequiresSetupInput{
				Service:        in.ServiceID,
				TargetHostname: in.TargetHostname,
				Reason:         fmt.Sprintf("local zerops.yaml unreadable: %v", err),
			}
		}
		if picked, ok := PickSetupNameFromNames(names, in.TargetHostname, in.Mode); ok {
			_ = writeBackCache(in.StateDir, in.TargetHostname, picked)
			return picked, nil
		}
		return "", &ErrRequiresSetupInput{
			Service:         in.ServiceID,
			TargetHostname:  in.TargetHostname,
			AvailableSetups: names,
			Reason:          "no setup matched hostname / suffix conventions and yaml has multiple blocks",
		}
	}

	// Step 6 — total miss.
	return "", &ErrRequiresSetupInput{
		Service:        in.ServiceID,
		TargetHostname: in.TargetHostname,
		Reason:         "no canonical setup-name in local meta or platform sources; no local yaml supplied",
	}
}

// WriteResolvedSetupName persists a freshly-discovered setup-block name
// into the local ServiceMeta cache. Exported so first-deploy paths
// (Gate B per plan §Gate B) can record the convention they just resolved
// from a local yaml parse without re-routing through the full cascade
// (they already have the answer; they just need the write-back side-effect).
//
// Pair-keyed: targetHostname == StageHostname writes StageSetupName;
// targetHostname == Hostname writes PrimarySetupName. Out-of-scope
// hostnames are no-ops.
//
// Best-effort: filesystem hiccups are returned but should be logged-
// and-continued rather than failing the parent operation — a missed
// cache write surfaces as a re-run cascade on the next call, not as a
// deploy failure.
func WriteResolvedSetupName(stateDir, targetHostname, value string) error {
	return writeBackCache(stateDir, targetHostname, value)
}

// writeBackCache persists a fresh resolved value into the local
// ServiceMeta cache (the canonical store per device — no platform write).
// Best-effort: a missing meta or filesystem hiccup is silently swallowed
// so cascade callers don't fail on a benign cache write.
//
// Pair-keyed: targetHostname == StageHostname writes StageSetupName;
// targetHostname == Hostname writes PrimarySetupName. Out-of-scope
// hostnames are no-ops (no field to update).
func writeBackCache(stateDir, targetHostname, value string) error {
	if stateDir == "" || targetHostname == "" || value == "" {
		return nil
	}
	meta, err := FindServiceMeta(stateDir, targetHostname)
	if err != nil || meta == nil {
		return err
	}
	switch {
	case meta.StageHostname != "" && targetHostname == meta.StageHostname:
		if meta.StageSetupName == value {
			return nil
		}
		meta.StageSetupName = value
	case targetHostname == meta.Hostname:
		if meta.PrimarySetupName == value {
			return nil
		}
		meta.PrimarySetupName = value
	default:
		return nil // out-of-scope; nothing to write
	}
	return WriteServiceMeta(stateDir, meta)
}

// DefaultArchiveFetcher is the production cascade step 4 fetcher:
// HTTP GET the signed archive URL, decode as zip, find the zerops.yaml
// entry (or zerops.yml fallback), return its body. Empty result OR
// error → cascade treats as miss and falls through to step 5.
//
// 30s timeout matches platform.DefaultAPITimeout. Body capped at 50 MiB
// to bound memory on unexpectedly-large archives; legitimate ZCP
// archives are well under that.
var DefaultArchiveFetcher ArchiveFetcher = func(ctx context.Context, url string) (string, error) {
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
		name := f.Name
		// Match `zerops.yaml` or `zerops.yml` at any depth — recipes
		// sometimes nest under a top-level dir.
		base := name
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			base = name[idx+1:]
		}
		if base != "zerops.yaml" && base != "zerops.yml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("archive fetch: open entry %q: %w", name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxBytes))
		_ = rc.Close()
		if err != nil {
			return "", fmt.Errorf("archive fetch: read entry %q: %w", name, err)
		}
		return string(content), nil
	}
	return "", nil // no zerops.yaml in archive — cascade falls through
}

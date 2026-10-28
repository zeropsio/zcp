package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A part whose pair is keyed by its dev pool (docs/spec-workflows.md §13
// PM-10) has no push-source checkout: the pool's hostname reaches a random
// one of its containers, possibly someone's lease (§8 PS-3). Launch and
// export read such a part from its origin instead (§10 P-LP-19, §9 E10): the
// pair's recorded origin at the ref production builds, fetched into a
// scratch repository of zcp's own — deterministic, and never a container.

// poolPartSource is what the origin of a pool-keyed part holds at the ref
// launch and export read.
type poolPartSource struct {
	// RepoURL is the pair's recorded origin, verbatim (meta.RemoteURL).
	RepoURL string
	// Ref is the ref read: the recorded tracked ref, else the origin's
	// default branch (launchPushProofRef).
	Ref string
	// CommitSHA is the tip the read fetched.
	CommitSHA string
	// ZeropsYAMLBody is the root zerops.yaml (else zerops.yml) CommitSHA
	// holds, "" for neither.
	ZeropsYAMLBody string
}

// poolPartOriginDeps is what the read needs besides the meta: the platform
// client and project the pool's GIT_TOKEN is read through, and the
// container's description.
type poolPartOriginDeps struct {
	client    platform.Client
	rt        runtime.Info
	projectID string
}

// poolPartOriginError is an origin the read could not read: the pool keying
// the pair, the origin and ref asked, what went wrong in git's own words and
// what to fix.
type poolPartOriginError struct {
	pool, origin, ref, said, fix string
}

func (e *poolPartOriginError) Error() string {
	origin := topology.RedactRepoURLCredentials(e.origin)
	if origin == "" {
		origin = "no origin"
	}
	return fmt.Sprintf("zcp could not read %s at %s — the origin of the part keyed by the dev pool %s, which holds no checkout of its own: %s. %s",
		origin, e.ref, e.pool, e.said, e.fix)
}

// readPoolPartOrigin reads the origin of the part whose pair meta is keyed by
// its dev pool. Nothing reaches the pool, a lease or the stage: the origin is
// fetched in zcp's own process (ops.ReadOriginTip), with the pool's GIT_TOKEN
// as the credential when zcp's token reads it (poolGitTokenFor). A failure is
// a *poolPartOriginError naming what to fix.
func readPoolPartOrigin(ctx context.Context, deps poolPartOriginDeps, meta *workflow.ServiceMeta) (*poolPartSource, error) {
	ref := launchPushProofRef(meta)
	remote := strings.TrimSpace(meta.RemoteURL)
	if remote == "" || meta.GitPushState != topology.GitPushConfigured {
		return nil, &poolPartOriginError{
			pool: meta.Hostname, origin: remote, ref: ref,
			said: fmt.Sprintf("the pair records no configured origin (git-push %q)", meta.GitPushState),
			fix:  "A pool-keyed pair keeps the origin its dev service pushed to, and git-push-setup refuses the pair: the part has no origin zcp can read until one is recorded.",
		}
	}
	token, how := poolGitTokenFor(ctx, deps, meta)
	tip, err := ops.ReadOriginTip(ctx, remote, ref, token)
	if err != nil {
		e := &poolPartOriginError{pool: meta.Hostname, origin: remote, ref: ref, said: fmt.Sprintf("%v (%s)", err, how)}
		if errors.Is(err, ops.ErrOriginRefMissing) {
			e.fix = fmt.Sprintf("Push %s to the origin from a lease of the pool (zerops_lease action=\"acquire\" pool=%q), then call again.", poolPartRefWords(ref), meta.Hostname)
		} else {
			e.fix = fmt.Sprintf("Make the origin readable from this container: reachable, and readable with the person's GIT_TOKEN on the pool %q — a credential the person gives, never one zcp writes or asks for in the conversation. Then call again.", meta.Hostname)
		}
		return nil, e
	}
	return &poolPartSource{RepoURL: remote, Ref: tip.Ref, CommitSHA: tip.CommitSHA, ZeropsYAMLBody: tip.ZeropsYAML}, nil
}

// poolPartRefWords names ref for a person: the default branch for HEAD.
func poolPartRefWords(ref string) string {
	if ref == symbolicHeadRef {
		return "a default branch"
	}
	return "the branch " + ref
}

// poolGitTokenFor is the pool's GIT_TOKEN as the env API reads it — the
// dev's, which the pool migration copied there (PM-4) — and how it was read,
// for a failure to name. "" when zcp's token reads it masked, the pool holds
// none, or the read failed: the origin is then read without a credential,
// which a public repository answers. The value goes to git's environment
// alone, never into a result, an error or a log.
func poolGitTokenFor(ctx context.Context, deps poolPartOriginDeps, meta *workflow.ServiceMeta) (token, how string) {
	if deps.client == nil {
		return "", "read without a credential: no platform client to read the pool's GIT_TOKEN"
	}
	serviceID := ""
	if meta.DevPool != nil {
		serviceID = meta.DevPool.ServiceID
	}
	if serviceID == "" {
		svc, err := ops.LookupService(ctx, deps.client, deps.projectID, meta.Hostname)
		if err != nil {
			return "", fmt.Sprintf("read without a credential: the pool was not found to read its GIT_TOKEN (%v)", err)
		}
		serviceID = svc.ID
	}
	envs, err := ops.FetchServiceEnv(ctx, deps.client, serviceID)
	if err != nil {
		return "", fmt.Sprintf("read without a credential: the pool's GIT_TOKEN could not be read (%v)", err)
	}
	for _, e := range envs {
		if e.Key != ops.GitTokenEnvKey {
			continue
		}
		if e.Masked || e.Content == "" {
			return "", "read without a credential: zcp's token reads the pool's GIT_TOKEN masked — a token that reads sensitive values does"
		}
		return e.Content, "read with the pool's GIT_TOKEN"
	}
	return "", "read without a credential: the pool holds no GIT_TOKEN"
}

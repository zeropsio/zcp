package eval

import (
	"context"
	"fmt"
	"net/http"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// FinalURLProbe captures the outcome of an end-to-end HTTP GET against a
// service's public subdomain URL. Runner populates it after the agent
// finishes; Grade then asserts Got == Expect.FinalURLStatus.
//
// Err is populated for any pre-HTTP failure (service not found, subdomain
// not enabled, URL unresolvable) or transport failure (dial refused, TLS
// error, context cancelled). Got stays 0 in those cases. When the HTTP
// layer returns a status, Got carries it (even for 5xx) and Err stays empty.
type FinalURLProbe struct {
	Hostname string `json:"hostname"`
	URL      string `json:"url,omitempty"`
	Got      int    `json:"got,omitempty"`
	Err      string `json:"err,omitempty"`
}

// ProbeFinalURL resolves the subdomain URL for a hostname and issues a single
// HTTP GET. This is the closing check that distinguishes "service looks
// healthy in the control plane" from "the app actually responds over the
// internet" — the gap eval scenarios have been silently missing.
func ProbeFinalURL(
	ctx context.Context,
	client platform.Client,
	doer ops.HTTPDoer,
	projectID string,
	hostname string,
) FinalURLProbe {
	probe := FinalURLProbe{Hostname: hostname}

	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		probe.Err = fmt.Sprintf("list services: %v", err)
		return probe
	}

	svc, lookupErr := ops.FindService(services, hostname)
	if lookupErr != nil {
		probe.Err = lookupErr.Error()
		return probe
	}

	url := ops.ResolveSubdomainURL(ctx, client, projectID, svc)
	if url == "" {
		probe.Err = fmt.Sprintf("service %q has no reachable subdomain URL (enable subdomain first)", hostname)
		return probe
	}
	probe.URL = url

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		probe.Err = fmt.Sprintf("build request: %v", err)
		return probe
	}

	resp, err := doer.Do(req)
	if err != nil {
		probe.Err = fmt.Sprintf("GET %s: %v", url, err)
		return probe
	}
	defer resp.Body.Close()
	probe.Got = resp.StatusCode
	return probe
}

// ResolveProbeHostname picks the single web-facing runtime in a project so
// greenfield scenarios don't need to hard-code a hostname the LLM is free to
// choose. Returns an error when 0 or >1 candidates are found — both cases
// mean the scenario author must set Expect.FinalURLHostname explicitly.
func ResolveProbeHostname(ctx context.Context, client platform.Client, projectID string) (string, error) {
	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return "", fmt.Errorf("list services: %w", err)
	}
	var candidates []string
	for _, svc := range services {
		if svc.SubdomainAccess && len(svc.Ports) > 0 {
			candidates = append(candidates, svc.Name)
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no web-facing service with subdomain enabled — set finalUrlHostname on the scenario")
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("multiple web-facing services (%v) — set finalUrlHostname on the scenario", candidates)
	}
}

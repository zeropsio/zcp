package ops

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// SubdomainURLForPort is the subdomain address of one port of svc: Zerops
// gives every HTTP port its own address, so a caller names the port it
// reports. "" when the project has no subdomain host or the address is not
// known yet.
func SubdomainURLForPort(ctx context.Context, client platform.Client, projectID string, svc *platform.ServiceStack, port int) string {
	proj, err := client.GetProject(ctx, projectID)
	if err != nil || proj.SubdomainHost == "" {
		return ""
	}
	return subdomainURLForPort(ctx, client, proj.SubdomainHost, svc, port, nil)
}

// subdomainURLForPort is the public-address projection for a selected port.
// Full project hosts can be built without reading envs. Bare hosts and env-only
// discovery use the same platform URL-list projection. A missing cached key
// is refetched because the platform injects zeropsSubdomain asynchronously.
func subdomainURLForPort(ctx context.Context, client platform.Client, subdomainHost string, svc *platform.ServiceStack, port int, rawEnvs []platform.ServiceEnvVar) string {
	if built := BuildSubdomainURL(svc.Name, subdomainHost, port); built != "" {
		return built
	}
	for _, env := range rawEnvs {
		if env.Key == envKeyZeropsSubdomain {
			return projectSubdomainURL(svc.Name, subdomainHost, port, env.Content)
		}
	}
	envs, err := client.GetServiceEnv(ctx, svc.ID)
	if err != nil {
		return ""
	}
	for _, env := range envs {
		if env.Key == envKeyZeropsSubdomain {
			return projectSubdomainURL(svc.Name, subdomainHost, port, env.Content)
		}
	}
	return ""
}

// projectSubdomainURL projects an advertised list onto the selected HTTP port.
// Port 80 omits its suffix. A bare project prefix can still reconstruct a
// declared port during propagation from the domain of an advertised URL.
func projectSubdomainURL(hostname, subdomainHost string, port int, content string) string {
	urls := parseSubdomainURLs(content)
	for _, u := range urls {
		label, _, _ := strings.Cut(u.Hostname(), ".")
		prefix, matches := strings.CutPrefix(label, hostname+"-")
		if !matches || hostname == "" {
			continue
		}
		if port == 80 && !strings.Contains(prefix, "-") || port != 0 && port != 80 && strings.HasSuffix(prefix, "-"+strconv.Itoa(port)) {
			return u.String()
		}
	}
	if subdomainHost != "" {
		if domain := ParseSubdomainDomain(content); domain != "" {
			return BuildSubdomainURL(hostname, subdomainHost+"."+domain, port)
		}
	}
	// An env-only caller without port metadata can use a single advertised URL;
	// a list cannot truthfully select an endpoint without that context.
	if port == 0 && len(urls) == 1 {
		return urls[0].String()
	}
	return ""
}

// parseSubdomainURLs validates each newline-delimited address independently.
// No consumer receives the raw list as a URI or a domain suffix.
func parseSubdomainURLs(content string) []*url.URL {
	var urls []*url.URL
	for line := range strings.SplitSeq(content, "\n") {
		u, err := url.Parse(strings.TrimSpace(line))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			continue
		}
		urls = append(urls, u)
	}
	return urls
}

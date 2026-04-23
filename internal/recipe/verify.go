package recipe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Verifier runs behavioral verification probes against a deployed service.
// Callers pass the verifier to scaffold or feature agents; the agent
// composes its own smoke test on top of these primitives.
type Verifier struct {
	HTTP    *http.Client
	Timeout time.Duration
}

// DefaultVerifier returns a verifier with a 20s HTTP timeout — long enough
// for a cold-start dev container, short enough to fail fast.
func DefaultVerifier() *Verifier {
	return &Verifier{
		HTTP:    &http.Client{Timeout: 20 * time.Second},
		Timeout: 20 * time.Second,
	}
}

// ProbeResult captures one verification probe's outcome.
type ProbeResult struct {
	Name       string
	OK         bool
	StatusCode int
	Body       string
	Err        string
}

// ReachableHTTP sends a GET and reports status + first 1 KB of body. OK
// iff status is 2xx.
func (v *Verifier) ReachableHTTP(ctx context.Context, url string) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, v.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ProbeResult{Name: "reachable-http", Err: err.Error()}
	}
	return v.runProbe(req, "reachable-http")
}

// ForwardedForEcho sends an X-Forwarded-For and reads the response body
// looking for the injected IP. OK iff status 2xx AND body contains the IP.
// Requires the app to expose a `/debug/remote-ip` (or similar) endpoint
// that echoes the trusted-proxy-resolved client IP.
func (v *Verifier) ForwardedForEcho(ctx context.Context, url, ip string) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, v.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ProbeResult{Name: "forwarded-for-echo", Err: err.Error()}
	}
	req.Header.Set("X-Forwarded-For", ip)
	res := v.runProbe(req, "forwarded-for-echo")
	if res.OK && !strings.Contains(res.Body, ip) {
		res.OK = false
		res.Err = fmt.Sprintf("body %q did not contain injected IP %q", res.Body, ip)
	}
	return res
}

// HealthEndpoint probes a `/health` path and treats empty body as a fail.
// OK iff status 2xx AND body is non-empty.
func (v *Verifier) HealthEndpoint(ctx context.Context, baseURL string) ProbeResult {
	return v.expectNonEmpty(ctx, strings.TrimRight(baseURL, "/")+"/health", "health-endpoint")
}

// runProbe executes an HTTP request and packages the result.
func (v *Verifier) runProbe(req *http.Request, name string) ProbeResult {
	resp, err := v.HTTP.Do(req)
	if err != nil {
		return ProbeResult{Name: name, Err: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return ProbeResult{
		Name:       name,
		OK:         resp.StatusCode >= 200 && resp.StatusCode < 300,
		StatusCode: resp.StatusCode,
		Body:       string(body),
	}
}

func (v *Verifier) expectNonEmpty(ctx context.Context, url, name string) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, v.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ProbeResult{Name: name, Err: err.Error()}
	}
	res := v.runProbe(req, name)
	if res.OK && strings.TrimSpace(res.Body) == "" {
		res.OK = false
		res.Err = "empty body"
	}
	return res
}

// SuiteResult aggregates probe results for agent-side report emission.
type SuiteResult struct {
	Results []ProbeResult
	Failed  int
}

// Summary returns a one-line summary for an agent response.
func (s SuiteResult) Summary() string {
	return fmt.Sprintf("%d/%d probes passed", len(s.Results)-s.Failed, len(s.Results))
}

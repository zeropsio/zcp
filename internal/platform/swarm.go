package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// swarmMaxBody bounds what is read back from an orchestrator.
const swarmMaxBody = 1 << 20

// swarmRolloutTimeout bounds a rollout, which returns only once the pool's
// outdated containers are removed and their replacements exist.
const swarmRolloutTimeout = 10 * time.Minute

// SwarmLease is the secret a Swarm orchestrator returns once, on acquire. It
// is the credential of one reserved container, so it formats and marshals as
// a placeholder: a stray %v, %+v or json.Marshal of a struct carrying it
// cannot leak it. Secret() is the single, explicit way to read it.
type SwarmLease string

// Secret returns the lease string. Call it only to write the lease where it
// is kept (the lease record) or to send it in the X-Swarm-Lease header.
func (l SwarmLease) Secret() string { return string(l) }

// String redacts the lease.
func (SwarmLease) String() string { return "[lease]" }

// GoString redacts the lease for %#v.
func (SwarmLease) GoString() string { return "SwarmLease([lease])" }

// MarshalJSON redacts the lease.
func (SwarmLease) MarshalJSON() ([]byte, error) { return []byte(`"[lease]"`), nil }

// SwarmContainer is one container of a pool, as the orchestrator lists it.
// It never carries the lease (acquire returns that apart, in SwarmAcquired).
type SwarmContainer struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Number           int64  `json:"number"`
	Hostname         string `json:"hostname"`
	Status           string `json:"status"`
	ServiceID        string `json:"serviceId"`
	StackID          string `json:"stackId"`
	AppVersionID     string `json:"appVersionId"`
	Current          bool   `json:"current"`
	LockState        string `json:"lockState"`
	LockSince        string `json:"lockSince,omitempty"`
	LeaseExpiresAt   string `json:"leaseExpiresAt,omitempty"`
	Dirty            bool   `json:"dirty"`
	UnreachableSince string `json:"unreachableSince,omitempty"`
	Created          string `json:"created,omitempty"`
}

// SwarmPool is the limits of a pool (GET /pool).
type SwarmPool struct {
	MinimumContainerCount int `json:"minimumContainerCount"`
	MaximumContainerCount int `json:"maximumContainerCount"`
}

// SwarmAcquireRequest is the body of POST /container/acquire.
type SwarmAcquireRequest struct {
	Start           bool  `json:"start"`
	Clean           bool  `json:"clean"`
	LeaseTTLSeconds int64 `json:"leaseTtlSeconds"`
}

// SwarmAcquired is the container an acquire reserved, with its lease.
type SwarmAcquired struct {
	Container SwarmContainer `json:"container"`
	Lease     SwarmLease     `json:"lease"`
}

// SwarmRollout is what POST /container/rollout did with each outdated
// container (current:false) of the pool.
type SwarmRollout struct {
	// ActiveAppVersionID is the app version every created container runs;
	// empty when the pool is empty.
	ActiveAppVersionID string `json:"activeAppVersionId"`
	// Replaced are the ids of the outdated containers removed.
	Replaced []string `json:"replaced"`
	// Created are the replacements.
	Created []SwarmContainer `json:"created"`
	// Retiring are the ids of outdated containers with work in progress; the
	// orchestrator replaces each once its work ends.
	Retiring []string `json:"retiring"`
	// Skipped are the outdated containers not replaced, each with its reason.
	Skipped []SwarmRolloutSkip `json:"skipped"`
	// CreateErrors say why replacements could not be created.
	CreateErrors []SwarmRolloutCreateError `json:"createErrors"`
}

// SwarmReasonPoolFixedSize is the SwarmError.Reason of a rollout refused
// because the pool's minimum container count equals its maximum: there is no
// room to replace a container.
const SwarmReasonPoolFixedSize = "pool-fixed-size"

// SwarmRolloutSkip is an outdated container a rollout did not replace.
// Reason is held-by-other (reserved, and no force was sent),
// container-deleting, containers-preparing, removal-failed or create-failed.
type SwarmRolloutSkip struct {
	ID      string `json:"id"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// SwarmRolloutCreateError is one replacement a rollout could not create; it
// names the new container only when the platform got far enough to name it.
type SwarmRolloutCreateError struct {
	ContainerID   string `json:"containerId,omitempty"`
	ContainerName string `json:"containerName,omitempty"`
	Message       string `json:"message"`
}

// SwarmClient talks to one pool's orchestrator (http://orch.<pool>.zerops,
// reachable only inside the project). Every call carries the pool service's
// API_TOKEN as a bearer token; the lease of a container travels ONLY in the
// X-Swarm-Lease header, never in a URL — Go's *url.Error prints the full URL
// on a transport failure, so a query parameter would leak the secret into
// errors and logs.
type SwarmClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewSwarmClient builds a client for the orchestrator at baseURL.
func NewSwarmClient(baseURL, token string, hc *http.Client) *SwarmClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &SwarmClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc}
}

// Acquire reserves a container (POST /container/acquire). There is no
// idempotency key: a lost response leaves the container reserved.
func (c *SwarmClient) Acquire(ctx context.Context, req SwarmAcquireRequest) (SwarmAcquired, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return SwarmAcquired{}, fmt.Errorf("swarm acquire: marshal: %w", err)
	}
	var wire struct {
		SwarmContainer
		Lease string `json:"lease"`
	}
	if err := c.do(ctx, http.MethodPost, "/container/acquire", body, "", &wire); err != nil {
		return SwarmAcquired{}, fmt.Errorf("swarm acquire: %w", err)
	}
	return SwarmAcquired{Container: wire.SwarmContainer, Lease: SwarmLease(wire.Lease)}, nil
}

// scrub removes the secrets this client holds (the bearer token and the
// lease of the call) from text that may reach an error, a log or a tool
// result: an orchestrator may echo what it was sent.
func (c *SwarmClient) scrub(text, lease string) string {
	for _, secret := range []string{lease, c.token} {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return text
}

// transportError maps a failure to reach the orchestrator (DNS, refused,
// timeout) to a NETWORK_ERROR. The text names the orchestrator's base URL and
// the transport cause only — never the request URL a *url.Error prints.
func (c *SwarmClient) transportError(ctx context.Context, err error, lease string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("request: %w", ctxErr)
	}
	cause := err
	var ue *url.Error
	if errors.As(err, &ue) {
		cause = ue.Err
	}
	pe := NewPlatformError(ErrNetworkError,
		c.scrub(fmt.Sprintf("the orchestrator at %s did not answer: %v", c.baseURL, cause), lease), "")
	pe.Cause = cause
	return pe
}

// Pool returns the pool limits (GET /pool).
func (c *SwarmClient) Pool(ctx context.Context) (SwarmPool, error) {
	var p SwarmPool
	if err := c.do(ctx, http.MethodGet, "/pool", nil, "", &p); err != nil {
		return SwarmPool{}, fmt.Errorf("swarm pool: %w", err)
	}
	return p, nil
}

// Containers lists the pool's containers (GET /container).
func (c *SwarmClient) Containers(ctx context.Context) ([]SwarmContainer, error) {
	var list []SwarmContainer
	if err := c.do(ctx, http.MethodGet, "/container", nil, "", &list); err != nil {
		return nil, fmt.Errorf("swarm containers: %w", err)
	}
	return list, nil
}

// Release returns a reserved container to the pool (POST
// /container/{id}/release). With reset the container is restored to its
// primary snapshot first (stopped, restored, started: its processes end).
func (c *SwarmClient) Release(ctx context.Context, id, lease string, reset bool) (SwarmContainer, error) {
	path := "/container/" + url.PathEscape(id) + "/release"
	if reset {
		path += "?reset=true"
	}
	var out SwarmContainer
	if err := c.do(ctx, http.MethodPost, path, nil, lease, &out); err != nil {
		return SwarmContainer{}, fmt.Errorf("swarm release: %w", err)
	}
	return out, nil
}

// Renew extends a reservation by the TTL it was acquired with (POST
// /container/{id}/renew).
func (c *SwarmClient) Renew(ctx context.Context, id, lease string) (SwarmContainer, error) {
	var out SwarmContainer
	if err := c.do(ctx, http.MethodPost, "/container/"+url.PathEscape(id)+"/renew", nil, lease, &out); err != nil {
		return SwarmContainer{}, fmt.Errorf("swarm renew: %w", err)
	}
	return out, nil
}

// Delete removes a container its holder leases (DELETE /container/{id}); the
// orchestrator answers 202 and removes it in the background. Removals run
// one at a time per service: a concurrent one is refused as retryable.
func (c *SwarmClient) Delete(ctx context.Context, id, lease string) error {
	if err := c.do(ctx, http.MethodDelete, "/container/"+url.PathEscape(id), nil, lease, nil); err != nil {
		return fmt.Errorf("swarm delete: %w", err)
	}
	return nil
}

// Rollout replaces the pool's outdated containers with ones built for its
// current app version (POST /container/rollout). It never sends force: force
// needs the admin token and would take a container from its holder, so every
// reserved container is skipped as held-by-other and keeps its version. The
// call carries no lease.
//
// The orchestrator answers once its removals and creates are done, which can
// take minutes for a VM pool, so the call is bounded by swarmRolloutTimeout
// instead of the client's own (short) budget; a rollout continues on the
// orchestrator if the call is cut.
func (c *SwarmClient) Rollout(ctx context.Context) (SwarmRollout, error) {
	ctx, cancel := context.WithTimeout(ctx, swarmRolloutTimeout)
	defer cancel()
	hc := *c.http
	hc.Timeout = 0 // the context bounds the call
	long := *c
	long.http = &hc
	var out SwarmRollout
	if err := long.do(ctx, http.MethodPost, "/container/rollout", nil, "", &out); err != nil {
		return SwarmRollout{}, fmt.Errorf("swarm rollout: %w", err)
	}
	return out, nil
}

// do sends one request and decodes a JSON answer into out (nil: ignore the
// body). lease, when non-empty, goes in the X-Swarm-Lease header.
func (c *SwarmClient) do(ctx context.Context, method, path string, body []byte, lease string, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if lease != "" {
		req.Header.Set("X-Swarm-Lease", lease)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.transportError(ctx, err, lease)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, swarmMaxBody))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		se := parseSwarmError(resp.StatusCode, raw)
		se.Message = c.scrub(se.Message, lease)
		se.Cause = c.scrub(se.Cause, lease)
		return se
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// SwarmError is an orchestrator refusal: its error envelope
// {"error":{"code","message","meta":[{"metadata":{"reason","retryable","message"}}]}}.
// Reason and Retryable come from the orchestrator's own metadata (an error
// passed on from the platform carries neither, and reads Reason "" and
// Retryable false); match on Reason, never on Message.
type SwarmError struct {
	Status    int    // HTTP status
	Code      string // containerAction, notAuthorized, ...
	Message   string
	Reason    string // unknown-lease, all-reserved, ...; empty when the orchestrator named none
	Retryable bool
	// Cause is the platform's own refusal the orchestrator passed on
	// (metadata.message), verbatim — e.g. why no container could be created
	// for an all-reserved acquire. Empty when the orchestrator passed none.
	Cause string
}

// SwarmReasonAllReserved is the SwarmError.Reason of an acquire refused
// because every container that could serve it is reserved and another could
// not be created. The orchestrator queues nothing: it answers at once.
const SwarmReasonAllReserved = "all-reserved"

// swarmCauseServiceMaximum is how the platform words a refusal to create a
// container for a service that already holds its maximum (live, 2026-10-23:
// "[containerAction] Service is already at maximum number of containers").
const swarmCauseServiceMaximum = "maximum number of containers"

// AtServiceMaximum reports a refusal whose platform cause is the pool service
// holding its maximum number of containers — the one cause raising the
// service's maximum can lift. Any other cause (or none) reads false.
func (e *SwarmError) AtServiceMaximum() bool {
	return strings.Contains(strings.ToLower(e.Cause), swarmCauseServiceMaximum)
}

// Error implements error. It carries no request URL and no lease.
func (e *SwarmError) Error() string {
	var b strings.Builder
	b.WriteString("orchestrator refused")
	if e.Code != "" {
		b.WriteString(" (" + e.Code)
		if e.Reason != "" {
			b.WriteString(": " + e.Reason)
		}
		b.WriteString(")")
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	if e.Code == "" && e.Message == "" {
		fmt.Fprintf(&b, ": status %d", e.Status)
	}
	return b.String()
}

// swarmErrorEnvelope is the orchestrator's error body.
type swarmErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Meta    []struct {
			Metadata map[string][]string `json:"metadata"`
		} `json:"meta"`
	} `json:"error"`
}

// parseSwarmError turns a non-2xx answer into a *SwarmError. A body that is
// not the envelope still yields one, carrying only the status.
func parseSwarmError(status int, raw []byte) *SwarmError {
	se := &SwarmError{Status: status}
	var env swarmErrorEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return se
	}
	se.Code, se.Message = env.Error.Code, env.Error.Message
	for _, m := range env.Error.Meta {
		if r := m.Metadata["reason"]; len(r) > 0 && se.Reason == "" {
			se.Reason = r[0]
		}
		if r := m.Metadata["retryable"]; len(r) > 0 && r[0] == "true" {
			se.Retryable = true
		}
		if c := m.Metadata["message"]; len(c) > 0 && se.Cause == "" {
			se.Cause = c[0]
		}
	}
	return se
}

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
	"slices"
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
	// Snapshots is how many snapshots the container has. Only a read by id
	// (Container) counts them: a list omits the field and a create answers it
	// null, so both read 0 here (live, 2026-10-23).
	Snapshots int `json:"snapshots,omitempty"`
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

// Container reads one container by id (GET /container/{id}) — the one read
// that counts its snapshots.
func (c *SwarmClient) Container(ctx context.Context, id string) (SwarmContainer, error) {
	var out SwarmContainer
	if err := c.do(ctx, http.MethodGet, "/container/"+url.PathEscape(id), nil, "", &out); err != nil {
		return SwarmContainer{}, fmt.Errorf("swarm container: %w", err)
	}
	return out, nil
}

// Create creates count containers (POST /container {"count":n}) and answers
// once they exist — about 16 s each on a container pool (live, 2026-10-23),
// minutes on a VM pool — each with its zerops-primary snapshot. Meanwhile an
// acquire that finds no other free container is refused containers-preparing,
// and one can take a container this call creates before the call answers: a
// container this returns is not the caller's. More than fits under the pool's
// maximum is refused pool-capacity-exceeded. The call carries no lease.
func (c *SwarmClient) Create(ctx context.Context, count int) ([]SwarmContainer, error) {
	body, err := json.Marshal(struct {
		Count int `json:"count"`
	}{count})
	if err != nil {
		return nil, fmt.Errorf("swarm create: marshal: %w", err)
	}
	var out []SwarmContainer
	if err := c.do(ctx, http.MethodPost, "/container", body, "", &out); err != nil {
		return nil, fmt.Errorf("swarm create: %w", err)
	}
	return out, nil
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
// /container/{id}/renew). For a lease with no expiry (TTL 0, every lease zcp
// takes) it changes nothing — not lockSince, dirty, the status, processes or
// files; it never starts a stopped container — and answers the container as it
// stands: the holder's own lease on its reserved container 200 (running or
// stopped, and lockState deleting while the holder's delete runs), any other
// lease, or any lease on a free container, 400 unknown-lease, and a container
// that is gone container-not-found (live, 2026-10-24: ~36 ms). So it is the
// exact "does this lease still hold this container" question.
func (c *SwarmClient) Renew(ctx context.Context, id, lease string) (SwarmContainer, error) {
	var out SwarmContainer
	if err := c.do(ctx, http.MethodPost, "/container/"+url.PathEscape(id)+"/renew", nil, lease, &out); err != nil {
		return SwarmContainer{}, fmt.Errorf("swarm renew: %w", err)
	}
	return out, nil
}

// Stop stops a container its holder leases (POST /container/{id}/stop) and
// answers it as it is after the stop: STOPPED and still reserved, so the
// holder can start it again or release it (live, 2026-10-23: 9.2–9.6 s on a
// container never reset, whose run.start runs, 4.6–4.7 s on a reset one;
// /var/www, $HOME and /tmp kept, processes ended). It never sends force,
// which would clear the reservation.
func (c *SwarmClient) Stop(ctx context.Context, id, lease string) (SwarmContainer, error) {
	var out SwarmContainer
	if err := c.do(ctx, http.MethodPost, "/container/"+url.PathEscape(id)+"/stop", nil, lease, &out); err != nil {
		return SwarmContainer{}, fmt.Errorf("swarm stop: %w", err)
	}
	return out, nil
}

// Start starts a stopped container its holder leases (POST
// /container/{id}/start) and answers it as it is after the start; the
// reservation does not change (live, 2026-10-23: 3.3–4.4 s, ssh answering
// ~65 ms after). It never sends force.
func (c *SwarmClient) Start(ctx context.Context, id, lease string) (SwarmContainer, error) {
	var out SwarmContainer
	if err := c.do(ctx, http.MethodPost, "/container/"+url.PathEscape(id)+"/start", nil, lease, &out); err != nil {
		return SwarmContainer{}, fmt.Errorf("swarm start: %w", err)
	}
	return out, nil
}

// Delete removes a container its holder leases (DELETE /container/{id}); the
// orchestrator answers 202 and removes it in the background (gone in 7–24 s,
// listed as deleting meanwhile). It never sends force. The platform runs one
// removal per pool at a time and keeps the pool at its minimum: it refuses a
// second removal, and one below the minimum, with no reason (live,
// 2026-10-23: 400, metadata null, only the message tells them apart).
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
		for i := range se.Details {
			se.Details[i] = c.scrub(se.Details[i], lease)
		}
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
	// Details is the rest of what meta[] says — each item's code and error
	// and every metadata key — that the message, the reason, retryable and
	// Cause do not already say, scrubbed of the client's secrets. A refusal
	// whose message is the platform's placeholder ("See metadata") says why
	// only here.
	Details []string
}

// SwarmReasonAllReserved is the SwarmError.Reason of an acquire refused
// because every container that could serve it is reserved and another could
// not be created. The orchestrator queues nothing: it answers at once.
const SwarmReasonAllReserved = "all-reserved"

// SwarmReasonUnknownLease and SwarmReasonContainerNotFound are the
// SwarmError.Reason of a call whose lease the orchestrator no longer holds, or
// whose container is gone: nothing of the lease is left to release. A renew
// answers unknown-lease for any lease that does not hold the reserved
// container and for any lease on a free container (live, 2026-10-24), where a
// release with the same lease says held-by-other: the vocabulary is per
// endpoint.
const (
	SwarmReasonUnknownLease      = "unknown-lease"
	SwarmReasonContainerNotFound = "container-not-found"
)

// SwarmReasonHeldByOther is the SwarmError.Reason of a call whose lease does
// not hold the reserved container it names: another holder reserves it. The
// orchestrator answers it at once (400 containerAction, retryable:"true",
// "container is reserved by another consumer - pass its lease, or ?force=true
// to take it over") and changes nothing (live, 2026-10-24). A free container
// is another matter: the orchestrator checks no lease on it, so a release of
// one resets it whatever lease it carries, a lease never issued included
// (live, 2026-10-24: 200 after ~9.5 s, the container reset).
const SwarmReasonHeldByOther = "held-by-other"

// SwarmContainerActive is the status of a running container
// (SwarmContainer.Status), the platform's own ACTIVE.
const SwarmContainerActive = ServiceStatusActive

// SwarmContainerStopped is the status of a stopped container, the platform's
// own STOPPED: with lockState reserved it is a container its holder stopped
// and keeps.
const SwarmContainerStopped = ServiceStatusStopped

// SwarmReasonContainersPreparing is the SwarmError.Reason of an acquire
// refused because free containers are still being created (a create in
// progress): retryable, answered at once, and it reserved nothing (live,
// 2026-10-23).
const SwarmReasonContainersPreparing = "containers-preparing"

// SwarmReasonPoolCapacityExceeded is the SwarmError.Reason of a create asking
// for more containers than fit under the pool's maximum. Not retryable.
const SwarmReasonPoolCapacityExceeded = "pool-capacity-exceeded"

// SwarmReasonNoPrimarySnapshot is the SwarmError.Reason of a release with
// reset refused because the container has no primary snapshot — the ones the
// platform creates for the pool's minimum (live, 2026-10-23); the reservation
// stays. Not retryable.
const SwarmReasonNoPrimarySnapshot = "no-primary-snapshot"

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
	if said := e.Said(); said != "" {
		b.WriteString(": " + said)
	} else if e.Code == "" {
		fmt.Fprintf(&b, ": status %d", e.Status)
	}
	return b.String()
}

// Said is everything the refusal says in words: its message — left out when
// it is the platform's placeholder, which points at the metadata — the
// platform's cause when it differs from the message, and the rest of what
// the metadata says (Details), as one text.
func (e *SwarmError) Said() string {
	var parts []string
	if msg := strings.TrimSpace(e.Message); msg != "" && !isMetadataPlaceholder(msg) {
		parts = append(parts, msg)
	}
	if e.Cause != "" && e.Cause != e.Message {
		parts = append(parts, "platform: "+e.Cause)
	}
	parts = append(parts, e.Details...)
	return strings.Join(parts, "; ")
}

// swarmMetadataPlaceholder is the message the platform answers when what it
// has to say is in meta[] only.
const swarmMetadataPlaceholder = "see metadata"

func isMetadataPlaceholder(msg string) bool {
	return strings.EqualFold(strings.TrimSpace(msg), swarmMetadataPlaceholder)
}

// swarmErrorEnvelope is the orchestrator's error body. Meta is kept raw: a
// metadata value that is not a list of strings must not make the whole
// envelope unreadable (swarmMeta reads it).
type swarmErrorEnvelope struct {
	Error struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Meta    json.RawMessage `json:"meta"`
	} `json:"error"`
}

// swarmMetaItem is one element of the envelope's meta[], each metadata value
// as the strings it holds (swarmMetaStrings).
type swarmMetaItem struct {
	code, err string
	metadata  map[string][]string
	keys      []string // metadata's keys, sorted
}

// swarmMeta reads meta[]; anything that is not a list of objects reads as
// none, and an item that is not an object is skipped.
func swarmMeta(raw json.RawMessage) []swarmMetaItem {
	var items []map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]swarmMetaItem, 0, len(items))
	for _, m := range items {
		if m == nil {
			continue
		}
		item := swarmMetaItem{metadata: map[string][]string{}}
		item.code, _ = m["code"].(string)
		item.err, _ = m["error"].(string)
		if md, ok := m["metadata"].(map[string]any); ok {
			for k, v := range md {
				if vals := swarmMetaStrings(v); len(vals) > 0 {
					item.metadata[k] = vals
					item.keys = append(item.keys, k)
				}
			}
			slices.Sort(item.keys)
		}
		out = append(out, item)
	}
	return out
}

// swarmMetaStrings is a metadata value as strings: a list's elements, or the
// value itself; an object or null holds none.
func swarmMetaStrings(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			out = append(out, swarmMetaStrings(x)...)
		}
		return out
	case string:
		return []string{t}
	case float64, bool:
		return []string{fmt.Sprint(t)}
	}
	return nil
}

// swarmMetaKnown are the metadata keys a SwarmError reads into fields of its
// own (Reason, Retryable, Cause), never repeated in Details.
var swarmMetaKnown = map[string]bool{"reason": true, "retryable": true, "message": true}

// parseSwarmError turns a non-2xx answer into a *SwarmError. A body that is
// not the envelope still yields one, carrying only the status.
func parseSwarmError(status int, raw []byte) *SwarmError {
	se := &SwarmError{Status: status}
	var env swarmErrorEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return se
	}
	se.Code, se.Message = env.Error.Code, env.Error.Message
	meta := swarmMeta(env.Error.Meta)
	for _, m := range meta {
		if r := m.metadata["reason"]; len(r) > 0 && se.Reason == "" {
			se.Reason = r[0]
		}
		if r := m.metadata["retryable"]; len(r) > 0 && r[0] == "true" {
			se.Retryable = true
		}
		if c := m.metadata["message"]; len(c) > 0 && se.Cause == "" {
			se.Cause = c[0]
		}
	}
	se.Details = swarmDetails(se, meta)
	if se.Reason == "" && status == http.StatusBadRequest && se.Code == swarmCodeContainerAction && strings.EqualFold(strings.TrimSpace(se.Message), swarmMessageContainerNotFound) {
		// A container deleted a moment ago: until a listing catches up, the
		// orchestrator passes the platform's bare refusal through as a 400,
		// with no reason (live, 2026-10-24); it is the same fact as the
		// container-not-found it names afterwards. Only that 400 was seen:
		// the same words with any other status say nothing of the container.
		se.Reason = SwarmReasonContainerNotFound
	}
	return se
}

// swarmDetails is what meta[] says that se's own fields do not: an item's
// error unless it repeats the message or the cause (with the item's code
// when it is not the envelope's), and every metadata key but the ones se
// reads into fields, each once.
func swarmDetails(se *SwarmError, meta []swarmMetaItem) []string {
	var out []string
	seen := map[string]bool{}
	add := func(d string) {
		if d = strings.TrimSpace(d); d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, m := range meta {
		switch {
		case m.err != "" && m.err != se.Message && m.err != se.Cause && m.code != "" && m.code != se.Code:
			add(m.code + ": " + m.err)
		case m.err != "" && m.err != se.Message && m.err != se.Cause:
			add(m.err)
		case m.code != "" && m.code != se.Code:
			add(m.code)
		}
		for _, k := range m.keys {
			if !swarmMetaKnown[k] {
				add(k + ": " + strings.Join(m.metadata[k], ", "))
			}
		}
	}
	return out
}

// swarmCodeContainerAction is the orchestrator's error code of a refused
// container call; swarmMessageContainerNotFound the platform's bare message
// for a container that is gone, which the orchestrator passes through without
// a reason right after a delete.
const (
	swarmCodeContainerAction      = "containerAction"
	swarmMessageContainerNotFound = "Container not found"
)

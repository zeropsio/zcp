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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zerops-go/sdkBase"
)

// boxMaxBody bounds what is read back from a Box workspace API.
const boxMaxBody = 4 << 20

// boxAPIPrefix is where every route of a Box workspace API lives, under the
// access's urlApi.
const boxAPIPrefix = "/api/rest"

// boxOperationListLimit is how many recent operations of a kind a listing
// reads (the API answers newest first).
const boxOperationListLimit = 20

// BoxAccessToken is a zproxy access token: a bearer to one Box workspace's
// whole API, valid for an hour after its last use (live, 2026-10-24). It
// formats and marshals as a placeholder, so a stray %v, %+v or json.Marshal
// of a struct carrying it cannot leak it; Secret() is the one way to read it.
type BoxAccessToken string

// Secret returns the token. Call it only to put the token in an
// Authorization header.
func (t BoxAccessToken) Secret() string { return string(t) }

// String redacts the token.
func (BoxAccessToken) String() string { return "[box-access]" }

// GoString redacts the token for %#v.
func (BoxAccessToken) GoString() string { return "BoxAccessToken([box-access])" }

// MarshalJSON redacts the token.
func (BoxAccessToken) MarshalJSON() ([]byte, error) { return []byte(`"[box-access]"`), nil }

// BoxAccess is a zproxy access to one Box workspace's API: the answer of
// POST /service-stack/{id}/ui-access. URLAPI is the shared zproxy API host
// (`https://proxy.<domain>/service-api`, live 2026-10-24); the token selects
// the workspace. The answer's urlUi carries the token in its path, so it is
// never kept.
type BoxAccess struct {
	Token      BoxAccessToken `json:"-"`
	URLAPI     string         `json:"urlApi"`
	Expiration string         `json:"expiration,omitempty"`
}

// BoxMinter mints a zproxy access to one Box.
type BoxMinter func(ctx context.Context) (BoxAccess, error)

// boxUIAccessAnswer is the part of the ui-access answer zcp reads; urlUi is
// deliberately absent.
type boxUIAccessAnswer struct {
	AccessToken string `json:"accessToken"`
	Expiration  string `json:"expiration"`
	URLAPI      string `json:"urlApi"`
}

// MintBoxAccess mints a zproxy access to the Box service serviceID with this
// client's own Zerops token: POST /service-stack/{id}/ui-access, posted by
// hand on the SDK's authorized transport (the route is not in the SDK).
// Minting needs BasicUser or above on the project, which a Mate's
// project-scoped token has (live, 2026-10-24). A refusal keeps the
// platform's code (serviceStackNoActiveContainer right after a create or on a
// stopped Box, insufficientPermissions, serviceStackNotFound) in APICode. An
// answer zcp cannot use is refused without its body, which carries the token.
func (z *ZeropsClient) MintBoxAccess(ctx context.Context, serviceID string) (BoxAccess, error) {
	u := "/api/rest/public/service-stack/" + url.PathEscape(serviceID) + "/ui-access"
	resp := sdkBase.Post(ctx, z.env, u, struct{}{})
	if resp.Err != nil {
		return BoxAccess{}, fmt.Errorf("mint box access: %w", mapSDKError(resp.Err, "service"))
	}
	status := resp.HttpResponse.StatusCode
	raw := resp.ResponseData.Bytes()
	if status >= http.StatusMultipleChoices {
		var apiErrResp directProcessErrorResponse
		if err := json.Unmarshal(raw, &apiErrResp); err != nil {
			pe := NewPlatformError(ErrAPIError, fmt.Sprintf("mint box access: malformed %d answer", status),
				"Retry; if it persists the platform API changed — report it")
			pe.serverSide = status >= http.StatusInternalServerError
			return BoxAccess{}, pe
		}
		apiErrResp.Error.HttpStatusCode = status
		return BoxAccess{}, fmt.Errorf("mint box access: %w", mapSDKError(apiErrResp.Error, "service"))
	}
	var out boxUIAccessAnswer
	if err := json.Unmarshal(raw, &out); err != nil {
		return BoxAccess{}, NewPlatformError(ErrAPIError, "mint box access: the ui-access answer is not JSON",
			"Retry; if it persists the platform API changed — report it")
	}
	switch {
	case out.AccessToken == "":
		return BoxAccess{}, NewPlatformError(ErrAPIError, "mint box access: the ui-access answer carries no access token",
			"Retry; if it persists the platform API changed — report it")
	case out.URLAPI == "":
		return BoxAccess{}, NewPlatformError(ErrAPIError, "mint box access: the ui-access answer carries no urlApi",
			"Retry; if it persists the platform API changed — report it")
	}
	return BoxAccess{Token: BoxAccessToken(out.AccessToken), URLAPI: strings.TrimRight(out.URLAPI, "/"), Expiration: out.Expiration}, nil
}

// Box workspace API vocabulary (the workspace's OpenAPI document, live
// 2026-10-24).
const (
	// BoxDomainFormDash puts a sandbox port in front of a Zerops subdomain:
	// `{port}-{id}-<domain>`, one label under the wildcard certificate.
	BoxDomainFormDash = "DASH"

	BoxBaseStatusReady      = "READY"
	BoxBaseStatusPulling    = "PULLING"
	BoxBaseStatusFlattening = "FLATTENING"
	BoxBaseStatusFailed     = "FAILED"

	BoxOperationKindPullBase = "PULL_BASE"

	BoxOperationPending  = "PENDING"
	BoxOperationRunning  = "RUNNING"
	BoxOperationFinished = "FINISHED"
	BoxOperationFailed   = "FAILED"

	// BoxErrorWorkspaceFull is a request the workspace has no room for
	// (409); its metadata names the resource, requested, free and usable.
	BoxErrorWorkspaceFull = "workspaceFull"
	// BoxErrorUnauthorized is a missing or wrong bearer (401).
	BoxErrorUnauthorized = "unauthorized"
)

// BoxPing is GET /api/rest/ping.
type BoxPing struct {
	Time    string `json:"time"`
	Version string `json:"version"`
}

// BoxDomain is one host suffix sandbox ports are published under.
type BoxDomain struct {
	Domain string `json:"domain"`
	Form   string `json:"form"`
}

// BoxWorkspace is the part of GET /api/rest/workspace zcp reads.
type BoxWorkspace struct {
	Domains []BoxDomain `json:"domains"`
	Version string      `json:"version"`
}

// BoxBase is one base of the workspace (GET /api/rest/base-list). Name is
// empty for a base whose name another pull took over.
type BoxBase struct {
	Digest       string `json:"digest"`
	ImageRef     string `json:"imageRef"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	FailedReason string `json:"failedReason"`
	SizeBytes    int64  `json:"sizeBytes"`
}

// BoxOperation is one long-running piece of work (a pull, a create, ...).
// A replayed Idempotency-Key answers the FIRST answer verbatim, its stale
// status included (live, 2026-10-24): only a read of the operation tells
// where it is.
type BoxOperation struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Status   string          `json:"status"`
	Error    string          `json:"error"`
	Created  string          `json:"created"`
	Finished string          `json:"finished"`
	Result   json.RawMessage `json:"result"`
}

// boxBaseResult is a finished pull's or build's result.
type boxBaseResult struct {
	Digest string `json:"digest"`
	Name   string `json:"name"`
}

func (o BoxOperation) baseResult() boxBaseResult {
	var r boxBaseResult
	if len(o.Result) == 0 || json.Unmarshal(o.Result, &r) != nil {
		return boxBaseResult{}
	}
	return r
}

// BaseDigest is the digest a finished pull's result names; "" for any other
// operation.
func (o BoxOperation) BaseDigest() string { return o.baseResult().Digest }

// BaseName is the name a finished pull's result names; "" for an unnamed
// base or any other operation.
func (o BoxOperation) BaseName() string { return o.baseResult().Name }

// BoxPullBaseRequest is the body of POST /api/rest/base.
type BoxPullBaseRequest struct {
	ImageRef string `json:"imageRef"`
	Name     string `json:"name,omitempty"`
}

// BoxError is a Box workspace API refusal: its error envelope
// {"error":{"code","message","meta":[{"metadata":{k:[v]}}]}}. Match on Code,
// never on Message. Metadata keeps each key's first value.
type BoxError struct {
	Status   int
	Code     string
	Message  string
	Metadata map[string]string
}

// Error implements error. It carries no URL and no token.
func (e *BoxError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Box API refused (%d", e.Status)
	if e.Code != "" {
		b.WriteString(" " + e.Code)
	}
	b.WriteString(")")
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	return b.String()
}

// boxErrorEnvelope is the Box API's error body.
type boxErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Meta    []struct {
			Metadata map[string][]string `json:"metadata"`
		} `json:"meta"`
	} `json:"error"`
}

// parseBoxError turns a non-2xx answer into a *BoxError; a body that is not
// the envelope yields one carrying only the status.
func parseBoxError(status int, raw []byte) *BoxError {
	be := &BoxError{Status: status}
	var env boxErrorEnvelope
	if json.Unmarshal(raw, &env) != nil {
		return be
	}
	be.Code, be.Message = env.Error.Code, env.Error.Message
	for _, m := range env.Error.Meta {
		for k, v := range m.Metadata {
			if len(v) == 0 {
				continue
			}
			if be.Metadata == nil {
				be.Metadata = map[string]string{}
			}
			if _, seen := be.Metadata[k]; !seen {
				be.Metadata[k] = v[0]
			}
		}
	}
	return be
}

// BoxClient talks to one Box workspace's API through zproxy: every call
// carries the access token as its bearer in the Authorization header and
// nowhere else — never in a URL (Go's *url.Error prints the full URL on a
// transport failure), a body, an error or a log. The access is minted on the
// first call, kept for the client's later calls, and minted again once when
// the API answers 401 (an access expired, or the workspace key changed).
type BoxClient struct {
	mint BoxMinter
	http *http.Client

	mu     sync.Mutex // guards access; never held during I/O
	access *BoxAccess
}

// NewBoxClient builds a client that reaches its Box with the accesses mint
// hands out.
func NewBoxClient(mint BoxMinter, hc *http.Client) *BoxClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &BoxClient{mint: mint, http: hc}
}

// Ping answers GET /api/rest/ping. Through zproxy even ping needs the access
// token (live, 2026-10-24), so an answer proves the access and the
// orchestrator both.
func (c *BoxClient) Ping(ctx context.Context) (BoxPing, error) {
	var out BoxPing
	if err := c.call(ctx, http.MethodGet, "/ping", nil, nil, "", &out); err != nil {
		return BoxPing{}, fmt.Errorf("box ping: %w", err)
	}
	return out, nil
}

// Workspace reads the workspace (GET /api/rest/workspace).
func (c *BoxClient) Workspace(ctx context.Context) (BoxWorkspace, error) {
	var out BoxWorkspace
	if err := c.call(ctx, http.MethodGet, "/workspace", nil, nil, "", &out); err != nil {
		return BoxWorkspace{}, fmt.Errorf("box workspace: %w", err)
	}
	return out, nil
}

// Bases lists the workspace's bases (GET /api/rest/base-list).
func (c *BoxClient) Bases(ctx context.Context) ([]BoxBase, error) {
	var out struct {
		Items []BoxBase `json:"items"`
	}
	if err := c.call(ctx, http.MethodGet, "/base-list", nil, nil, "", &out); err != nil {
		return nil, fmt.Errorf("box base list: %w", err)
	}
	return out.Items, nil
}

// Operations lists the recent operations of kind, newest first (GET
// /api/rest/operation-list).
func (c *BoxClient) Operations(ctx context.Context, kind string) ([]BoxOperation, error) {
	q := url.Values{"kind": {kind}, "limit": {strconv.Itoa(boxOperationListLimit)}}
	var out struct {
		Items []BoxOperation `json:"items"`
	}
	if err := c.call(ctx, http.MethodGet, "/operation-list", q, nil, "", &out); err != nil {
		return nil, fmt.Errorf("box operation list: %w", err)
	}
	return out.Items, nil
}

// PullBase starts a base pull (POST /api/rest/base) and answers its
// operation. The Idempotency-Key is required: the same key with the same
// request within 24 h replays the first answer and runs nothing twice.
func (c *BoxClient) PullBase(ctx context.Context, req BoxPullBaseRequest, idempotencyKey string) (BoxOperation, error) {
	var out BoxOperation
	if err := c.call(ctx, http.MethodPost, "/base", nil, req, idempotencyKey, &out); err != nil {
		return BoxOperation{}, fmt.Errorf("box base pull: %w", err)
	}
	return out, nil
}

// Operation reads an operation (GET /api/rest/operation/{id}); a positive
// wait long-polls it (waitSeconds, at most 30 s server-side) until it
// finishes or the time is up.
func (c *BoxClient) Operation(ctx context.Context, id string, wait time.Duration) (BoxOperation, error) {
	var q url.Values
	if s := int(wait / time.Second); s > 0 {
		q = url.Values{"waitSeconds": {strconv.Itoa(s)}}
	}
	var out BoxOperation
	if err := c.call(ctx, http.MethodGet, "/operation/"+url.PathEscape(id), q, nil, "", &out); err != nil {
		return BoxOperation{}, fmt.Errorf("box operation: %w", err)
	}
	return out, nil
}

// PutDomains replaces the workspace's sandbox domains with domains (PUT
// /api/rest/workspace/domains): the WHOLE list, as given — a caller adding
// one sends every domain it keeps (live, 2026-10-24).
func (c *BoxClient) PutDomains(ctx context.Context, domains []BoxDomain) (BoxWorkspace, error) {
	if domains == nil {
		domains = []BoxDomain{}
	}
	body := struct {
		Domains []BoxDomain `json:"domains"`
	}{domains}
	var out BoxWorkspace
	if err := c.call(ctx, http.MethodPut, "/workspace/domains", nil, body, "", &out); err != nil {
		return BoxWorkspace{}, fmt.Errorf("box domains: %w", err)
	}
	return out, nil
}

// current answers the kept access, minting one when there is none.
func (c *BoxClient) current(ctx context.Context) (BoxAccess, error) {
	c.mu.Lock()
	a := c.access
	c.mu.Unlock()
	if a != nil {
		return *a, nil
	}
	return c.remint(ctx)
}

// remint mints a new access and keeps it.
func (c *BoxClient) remint(ctx context.Context) (BoxAccess, error) {
	a, err := c.mint(ctx)
	if err != nil {
		return BoxAccess{}, fmt.Errorf("box access: %w", err)
	}
	if a.Token == "" || a.URLAPI == "" {
		return BoxAccess{}, NewPlatformError(ErrAPIError, "box access: the minted access is incomplete", "Retry the call")
	}
	c.mu.Lock()
	c.access = &a
	c.mu.Unlock()
	return a, nil
}

// call sends one request with the kept access and decodes a JSON answer into
// out; a 401 mints a new access and sends the request once more.
func (c *BoxClient) call(ctx context.Context, method, path string, query url.Values, body any, idempotencyKey string, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		raw = b
	}
	access, err := c.current(ctx)
	if err != nil {
		return err
	}
	err = c.do(ctx, access, method, path, query, raw, idempotencyKey, out)
	var be *BoxError
	if !errors.As(err, &be) || be.Status != http.StatusUnauthorized {
		return err
	}
	if access, err = c.remint(ctx); err != nil {
		return err
	}
	return c.do(ctx, access, method, path, query, raw, idempotencyKey, out)
}

// do sends one request with access.
func (c *BoxClient) do(ctx context.Context, access BoxAccess, method, path string, query url.Values, body []byte, idempotencyKey string, out any) error {
	u := access.URLAPI + boxAPIPrefix + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+access.Token.Secret())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return boxTransportError(ctx, access, err)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, boxMaxBody))
	if err != nil {
		// An answer the connection cut short is the network failing, never
		// the API refusing: a POST /base whose answer was lost so is sent
		// again with its Idempotency-Key (IsTransient).
		return boxCutShortError(ctx, access, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		be := parseBoxError(resp.StatusCode, answer)
		scrubBoxError(be, access.Token.Secret())
		return be
	}
	if out == nil || len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// redactToken removes token from text.
func redactToken(text, token string) string {
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "[redacted]")
}

// scrubBoxError removes the token from what a refusal echoed back.
func scrubBoxError(be *BoxError, token string) {
	be.Code, be.Message = redactToken(be.Code, token), redactToken(be.Message, token)
	for k, v := range be.Metadata {
		be.Metadata[k] = redactToken(v, token)
	}
}

// boxCutShortError maps an answer whose body the connection cut short to a
// NETWORK_ERROR naming the API's base URL and the cause only.
func boxCutShortError(ctx context.Context, access BoxAccess, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("read response: %w", ctxErr)
	}
	pe := NewPlatformError(ErrNetworkError, redactToken(fmt.Sprintf("the Box API at %s cut its answer short: %v", access.URLAPI, err), access.Token.Secret()), "")
	pe.Cause = err
	return pe
}

// boxTransportError maps a failure to reach the Box API (DNS, refused,
// timeout) to a NETWORK_ERROR naming the API's base URL and the transport
// cause only — never the request URL a *url.Error prints.
func boxTransportError(ctx context.Context, access BoxAccess, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("request: %w", ctxErr)
	}
	cause := err
	var ue *url.Error
	if errors.As(err, &ue) {
		cause = ue.Err
	}
	text := redactToken(fmt.Sprintf("the Box API at %s did not answer: %v", access.URLAPI, cause), access.Token.Secret())
	pe := NewPlatformError(ErrNetworkError, text, "")
	pe.Cause = cause
	return pe
}

package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const documentHTTPTimeout = 20 * time.Second

// seedElastic indexes a few product/article documents into namespace-
// derived indices and forces a refresh so they are immediately searchable
// (elasticsearch documents are not visible to reads until the next refresh
// cycle or an explicit one). Index names are namespace-derived via
// DocumentName; document ids stay the engine's original static ids — they
// only need to be unique WITHIN an index, and each namespace gets its own
// index. Unlike typesense/qdrant below, there is no separate create-
// collection call to tolerate: PUT <index>/_doc/<id> auto-creates the index
// on first write and replaces-in-place on every write after, so a re-run
// against an already-seeded index has no "already exists" class to hit.
func seedElastic(ctx context.Context, conn provider.DocumentConn, opts Options) error {
	products := DocumentName(opts.Namespace, "products")
	articles := DocumentName(opts.Namespace, "articles")
	docs := []struct{ idx, id, body string }{
		{products, "1", `{"name":"Widget","price":9.99,"tags":["tools","new"]}`},
		{products, "2", `{"name":"Gadget","price":19.50,"tags":["tools"]}`},
		{products, "3", `{"name":"Gizmo","price":4.25,"tags":["misc"]}`},
		{articles, "a1", `{"title":"Hello Zerops","author":"alice","words":420}`},
	}
	for _, d := range docs {
		if _, err := httpDo(ctx, http.MethodPut, conn.BaseURL+"/"+d.idx+"/_doc/"+d.id, []byte(d.body), basicAuth(conn.User, conn.APIKey), ""); err != nil {
			return fmt.Errorf("seed elasticsearch: index %s/%s: %w", d.idx, d.id, err)
		}
	}
	if _, err := httpDo(ctx, http.MethodPost, conn.BaseURL+"/_refresh", nil, basicAuth(conn.User, conn.APIKey), ""); err != nil {
		return fmt.Errorf("seed elasticsearch: refresh: %w", err)
	}
	return nil
}

// seedMeili indexes a few product documents into a namespace-derived index.
// Like elasticsearch, there is no separate create-index call to tolerate:
// "add or replace documents" lazily creates the index on first write and
// upserts by primary key on every write after, so a re-run has no "already
// exists" class to hit.
func seedMeili(ctx context.Context, conn provider.DocumentConn, opts Options) error {
	idx := DocumentName(opts.Namespace, "products")
	body := `[{"id":1,"title":"Widget","price":9.99},{"id":2,"title":"Gadget","price":19.5},{"id":3,"title":"Gizmo","price":4.25}]`
	if _, err := httpDo(ctx, http.MethodPost, conn.BaseURL+"/indexes/"+idx+"/documents", []byte(body), bearer(conn.APIKey), ""); err != nil {
		return fmt.Errorf("seed meilisearch: index %s: %w", idx, err)
	}
	return nil
}

// seedTypesense creates a namespace-derived collection and imports a couple
// of documents into it. Typesense's documented "resource already exists"
// status is 409 (api-errors.html); create-collection returning 409 means the
// collection already exists from a prior seed run under this namespace (or
// the static dataset on a re-run of dcseed) — that is success-proceed, not a
// fixture failure. Any other status IS a real failure and must not be
// swallowed (the pre-S10b dcseed ignored the create-collection error
// unconditionally, which would also have hidden a 500/auth failure here).
func seedTypesense(ctx context.Context, conn provider.DocumentConn, opts Options) error {
	coll := DocumentName(opts.Namespace, "products")
	schema := fmt.Sprintf(`{"name":%q,"fields":[{"name":"title","type":"string"},{"name":"price","type":"float"}]}`, coll)
	if _, err := httpDo(ctx, http.MethodPost, conn.BaseURL+"/collections", []byte(schema), tsKey(conn.APIKey), ""); err != nil && !isStatus(err, http.StatusConflict) {
		return fmt.Errorf("seed typesense: create collection %s: %w", coll, err)
	}
	docs := "{\"id\":\"1\",\"title\":\"Widget\",\"price\":9.99}\n{\"id\":\"2\",\"title\":\"Gadget\",\"price\":19.5}\n"
	if _, err := httpDo(ctx, http.MethodPost, conn.BaseURL+"/collections/"+coll+"/documents/import?action=upsert", []byte(docs), tsKey(conn.APIKey), "text/plain"); err != nil {
		return fmt.Errorf("seed typesense: import into %s: %w", coll, err)
	}
	return nil
}

// seedQdrant creates a namespace-derived collection with a small vector
// dimension and upserts a few points. Qdrant returns 409 Conflict on
// create-collection when the collection already exists (live-verified in
// the 2026-07-16 shakeout: "PUT .../collections/items -> 409 ... Collection
// `items` already exists!") — that means a prior seed run already created
// it, which is success-proceed, not a fixture failure. Any other status IS
// a real failure and must not be swallowed.
func seedQdrant(ctx context.Context, conn provider.DocumentConn, opts Options) error {
	coll := DocumentName(opts.Namespace, "items")
	hdr := qdrantAuth(conn)
	if _, err := httpDo(ctx, http.MethodPut, conn.BaseURL+"/collections/"+coll, []byte(`{"vectors":{"size":4,"distance":"Cosine"}}`), hdr, ""); err != nil && !isStatus(err, http.StatusConflict) {
		return fmt.Errorf("seed qdrant: create collection %s: %w", coll, err)
	}
	points := `{"points":[{"id":1,"vector":[0.1,0.2,0.3,0.4],"payload":{"name":"alpha"}},{"id":2,"vector":[0.2,0.1,0.4,0.3],"payload":{"name":"beta"}},{"id":3,"vector":[0.9,0.8,0.7,0.6],"payload":{"name":"gamma"}}]}`
	if _, err := httpDo(ctx, http.MethodPut, conn.BaseURL+"/collections/"+coll+"/points?wait=true", []byte(points), hdr, ""); err != nil {
		return fmt.Errorf("seed qdrant: upsert points into %s: %w", coll, err)
	}
	return nil
}

func qdrantAuth(conn provider.DocumentConn) func(*http.Request) {
	return func(r *http.Request) {
		if conn.APIKey != "" {
			r.Header.Set("api-key", conn.APIKey)
		}
	}
}

// cleanupElastic deletes every index _cat/indices reports as owned by
// namespace (DocumentOwns).
func cleanupElastic(ctx context.Context, conn provider.DocumentConn, namespace string) error {
	body, err := httpDo(ctx, http.MethodGet, conn.BaseURL+"/_cat/indices?format=json", nil, basicAuth(conn.User, conn.APIKey), "")
	if err != nil {
		return fmt.Errorf("seed cleanup elasticsearch: list indices: %w", err)
	}
	var rows []struct {
		Index string `json:"index"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return fmt.Errorf("seed cleanup elasticsearch: parse indices: %w", err)
	}
	for _, row := range rows {
		if !DocumentOwns(namespace, row.Index) {
			continue
		}
		if _, err := httpDo(ctx, http.MethodDelete, conn.BaseURL+"/"+row.Index, nil, basicAuth(conn.User, conn.APIKey), ""); err != nil {
			return fmt.Errorf("seed cleanup elasticsearch: delete %s: %w", row.Index, err)
		}
	}
	return nil
}

// cleanupMeili deletes every index /indexes reports as owned by namespace.
func cleanupMeili(ctx context.Context, conn provider.DocumentConn, namespace string) error {
	body, err := httpDo(ctx, http.MethodGet, conn.BaseURL+"/indexes", nil, bearer(conn.APIKey), "")
	if err != nil {
		return fmt.Errorf("seed cleanup meilisearch: list indexes: %w", err)
	}
	var payload struct {
		Results []struct {
			UID string `json:"uid"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("seed cleanup meilisearch: parse indexes: %w", err)
	}
	for _, r := range payload.Results {
		if !DocumentOwns(namespace, r.UID) {
			continue
		}
		if _, err := httpDo(ctx, http.MethodDelete, conn.BaseURL+"/indexes/"+r.UID, nil, bearer(conn.APIKey), ""); err != nil {
			return fmt.Errorf("seed cleanup meilisearch: delete %s: %w", r.UID, err)
		}
	}
	return nil
}

// cleanupTypesense deletes every collection /collections reports as owned
// by namespace.
func cleanupTypesense(ctx context.Context, conn provider.DocumentConn, namespace string) error {
	body, err := httpDo(ctx, http.MethodGet, conn.BaseURL+"/collections", nil, tsKey(conn.APIKey), "")
	if err != nil {
		return fmt.Errorf("seed cleanup typesense: list collections: %w", err)
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return fmt.Errorf("seed cleanup typesense: parse collections: %w", err)
	}
	for _, row := range rows {
		if !DocumentOwns(namespace, row.Name) {
			continue
		}
		if _, err := httpDo(ctx, http.MethodDelete, conn.BaseURL+"/collections/"+row.Name, nil, tsKey(conn.APIKey), ""); err != nil {
			return fmt.Errorf("seed cleanup typesense: delete %s: %w", row.Name, err)
		}
	}
	return nil
}

// cleanupQdrant deletes every collection /collections reports as owned by
// namespace.
func cleanupQdrant(ctx context.Context, conn provider.DocumentConn, namespace string) error {
	hdr := qdrantAuth(conn)
	body, err := httpDo(ctx, http.MethodGet, conn.BaseURL+"/collections", nil, hdr, "")
	if err != nil {
		return fmt.Errorf("seed cleanup qdrant: list collections: %w", err)
	}
	var payload struct {
		Result struct {
			Collections []struct {
				Name string `json:"name"`
			} `json:"collections"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("seed cleanup qdrant: parse collections: %w", err)
	}
	for _, c := range payload.Result.Collections {
		if !DocumentOwns(namespace, c.Name) {
			continue
		}
		if _, err := httpDo(ctx, http.MethodDelete, conn.BaseURL+"/collections/"+c.Name, nil, hdr, ""); err != nil {
			return fmt.Errorf("seed cleanup qdrant: delete %s: %w", c.Name, err)
		}
	}
	return nil
}

// ---------- shared HTTP helpers (document engines only — kv/object/stream
// speak their own native protocol/SDK) ----------

// statusError is httpDo's error for any non-2xx response: a bounded body
// snippet for diagnosis (Error), plus the numeric status so a caller can
// explicitly tolerate exactly one class (isStatus) instead of swallowing
// every failure the way the pre-S10b dcseed swallowed typesense/qdrant's
// create-collection errors unconditionally — see seedTypesense/seedQdrant.
type statusError struct {
	method string
	url    string
	status int
	body   []byte
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s %s -> %d: %.160s", e.method, e.url, e.status, e.body)
}

// isStatus reports whether err is a statusError carrying exactly code.
func isStatus(err error, code int) bool {
	var se *statusError
	return errors.As(err, &se) && se.status == code
}

// httpDo issues one request and returns the response body. A >=300 status
// is an error (statusError, with a bounded body snippet for diagnosis), but
// the body bytes are still returned alongside it since some callers (none
// today, but the shape stays honest) may want to inspect a non-2xx body.
func httpDo(ctx context.Context, method, u string, body []byte, hdr func(*http.Request), ctype string) ([]byte, error) {
	cx, cancel := context.WithTimeout(ctx, documentHTTPTimeout)
	defer cancel()
	var r *http.Request
	var err error
	if body != nil {
		r, err = http.NewRequestWithContext(cx, method, u, bytes.NewReader(body))
	} else {
		r, err = http.NewRequestWithContext(cx, method, u, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	if ctype == "" {
		ctype = "application/json"
	}
	r.Header.Set("Content-Type", ctype)
	if hdr != nil {
		hdr(r)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return respBody, &statusError{method: method, url: u, status: resp.StatusCode, body: respBody}
	}
	return respBody, nil
}

func basicAuth(u, p string) func(*http.Request) {
	return func(r *http.Request) {
		if u != "" || p != "" {
			r.SetBasicAuth(u, p)
		}
	}
}

func bearer(k string) func(*http.Request) {
	return func(r *http.Request) {
		if k != "" {
			r.Header.Set("Authorization", "Bearer "+k)
		}
	}
}

func tsKey(k string) func(*http.Request) {
	return func(r *http.Request) {
		if k != "" {
			r.Header.Set("X-TYPESENSE-API-KEY", k)
		}
	}
}

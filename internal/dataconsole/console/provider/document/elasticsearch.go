package document

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// esEngine speaks the Elasticsearch REST dialect with HTTP basic auth
// (user:apiKey). The cursor is the search `from` offset rendered as a string.
type esEngine struct{ t *transport }

func newESEngine(base, user, key string, client *http.Client) *esEngine {
	return &esEngine{t: &transport{
		base:   base,
		client: client,
		setAuth: func(r *http.Request) {
			r.SetBasicAuth(user, key)
		},
	}}
}

func (e *esEngine) name() string { return "elasticsearch" }

func (e *esEngine) containers(ctx context.Context) ([]string, error) {
	var out []struct {
		Index string `json:"index"`
	}
	if err := e.t.requestJSON(ctx, http.MethodGet, "/_cat/indices?format=json&h=index", nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out))
	for _, idx := range out {
		if strings.HasPrefix(idx.Index, ".") {
			continue // hide system indices
		}
		names = append(names, idx.Index)
	}
	return names, nil
}

func (e *esEngine) docs(ctx context.Context, container, cursor string, limit int) ([]string, string, error) {
	from := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, "", invalidCursor()
		}
		from = n
	}
	body, err := jsonBody(map[string]any{"from": from, "size": limit, "_source": false})
	if err != nil {
		return nil, "", err
	}
	var out struct {
		Hits struct {
			Hits []struct {
				ID string `json:"_id"` //nolint:tagliatelle // Elasticsearch wire field
			} `json:"hits"`
		} `json:"hits"`
	}
	path := "/" + url.PathEscape(container) + "/_search"
	if err := e.t.requestJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(out.Hits.Hits))
	for _, h := range out.Hits.Hits {
		ids = append(ids, h.ID)
	}
	next := ""
	if len(ids) == limit {
		next = strconv.Itoa(from + limit)
	}
	return ids, next, nil
}

func (e *esEngine) getDoc(ctx context.Context, container, id string) ([]byte, error) {
	var out struct {
		Source json.RawMessage `json:"_source"` //nolint:tagliatelle // Elasticsearch wire field
	}
	path := "/" + url.PathEscape(container) + "/_doc/" + url.PathEscape(id)
	if err := e.t.requestJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if len(out.Source) == 0 {
		return nil, provider.ErrNotFound
	}
	return out.Source, nil
}

func (e *esEngine) putDoc(ctx context.Context, container, id string, body []byte) error {
	if err := esIdentityGuard(body, id); err != nil {
		return err
	}
	path := "/" + url.PathEscape(container) + "/_doc/" + url.PathEscape(id)
	_, err := e.t.request(ctx, http.MethodPut, path, body)
	return err
}

// esIdentityGuard enforces DD-6 for Elasticsearch: the document is addressed by
// the path id (the URL's _doc/{id}). A body carrying a reserved `_id` that
// DIFFERS from the path would be rejected upstream as a flattened 502 (audit
// D-05) — refuse it here as a clear ErrInvalid instead. An ordinary `id` field
// is inert data in ES (never the identity), so it passes through untouched.
func esIdentityGuard(body []byte, id string) error {
	bid, present, err := bodyField(body, "_id")
	if err != nil {
		return err
	}
	if present && bid != id {
		return fmt.Errorf("doc: elasticsearch: %w: body _id %q does not match path id %q", provider.ErrInvalid, bid, id)
	}
	return nil
}

// search runs a bounded simple_query_string over all fields, returning matching
// _ids only (_source:false), never a highlighted snippet — no engine markup
// crosses the wire (S16).
func (e *esEngine) search(ctx context.Context, container, q, cursor string, limit int) ([]string, string, error) {
	from := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, "", invalidCursor()
		}
		from = n
	}
	body, err := jsonBody(map[string]any{
		"from": from, "size": limit, "_source": false,
		"query": map[string]any{"simple_query_string": map[string]any{"query": q}},
	})
	if err != nil {
		return nil, "", err
	}
	var out struct {
		Hits struct {
			Hits []struct {
				ID string `json:"_id"` //nolint:tagliatelle // Elasticsearch wire field
			} `json:"hits"`
		} `json:"hits"`
	}
	path := "/" + url.PathEscape(container) + "/_search"
	if err := e.t.requestJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(out.Hits.Hits))
	for _, h := range out.Hits.Hits {
		ids = append(ids, h.ID)
	}
	next := ""
	if len(ids) == limit {
		next = strconv.Itoa(from + limit)
	}
	return ids, next, nil
}

// createDoc creates a NEW document. With an explicit id it uses ES's create-only
// endpoint (_create), which returns 409 on an existing id → ErrConflict; with no
// id it POSTs /_doc so ES mints an id, returned as _id.
func (e *esEngine) createDoc(ctx context.Context, container, id string, body []byte) (string, error) {
	if id != "" {
		if err := esIdentityGuard(body, id); err != nil {
			return "", err
		}
		path := "/" + url.PathEscape(container) + "/_create/" + url.PathEscape(id)
		if _, err := e.t.request(ctx, http.MethodPut, path, body); err != nil {
			return "", err
		}
		return id, nil
	}
	var out struct {
		ID string `json:"_id"` //nolint:tagliatelle // Elasticsearch wire field
	}
	path := "/" + url.PathEscape(container) + "/_doc"
	if err := e.t.requestJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (e *esEngine) deleteDoc(ctx context.Context, container, id string) error {
	path := "/" + url.PathEscape(container) + "/_doc/" + url.PathEscape(id)
	_, err := e.t.request(ctx, http.MethodDelete, path, nil)
	return err
}

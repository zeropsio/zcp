package document

import (
	"context"
	"encoding/json"
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
	path := "/" + url.PathEscape(container) + "/_doc/" + url.PathEscape(id)
	_, err := e.t.request(ctx, http.MethodPut, path, body)
	return err
}

func (e *esEngine) deleteDoc(ctx context.Context, container, id string) error {
	path := "/" + url.PathEscape(container) + "/_doc/" + url.PathEscape(id)
	_, err := e.t.request(ctx, http.MethodDelete, path, nil)
	return err
}

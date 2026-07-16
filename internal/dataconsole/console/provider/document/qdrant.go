package document

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// qdrantEngine speaks the Qdrant REST dialect (api-key header). Points carry
// vectors, so the engine is READ-ONLY in v1: putDoc/deleteDoc reject. The cursor
// is the JSON-encoded scroll offset (an int or a uuid string).
type qdrantEngine struct{ t *transport }

func newQdrantEngine(base, key string, client *http.Client) *qdrantEngine {
	return &qdrantEngine{t: &transport{
		base:   base,
		client: client,
		setAuth: func(r *http.Request) {
			r.Header.Set("api-key", key)
		},
	}}
}

func (q *qdrantEngine) name() string { return "qdrant" }

func (q *qdrantEngine) containers(ctx context.Context) ([]string, error) {
	var out struct {
		Result struct {
			Collections []struct {
				Name string `json:"name"`
			} `json:"collections"`
		} `json:"result"`
	}
	if err := q.t.requestJSON(ctx, http.MethodGet, "/collections", nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Result.Collections))
	for _, c := range out.Result.Collections {
		names = append(names, c.Name)
	}
	return names, nil
}

func (q *qdrantEngine) docs(ctx context.Context, container, cursor string, limit int) ([]string, string, error) {
	// The scroll offset is an opaque JSON value (int or uuid); null starts a scan.
	offset := json.RawMessage("null")
	if cursor != "" {
		offset = json.RawMessage(cursor)
	}
	body, err := jsonBody(map[string]any{
		"limit":        limit,
		"offset":       offset,
		"with_payload": true,
		"with_vector":  false,
	})
	if err != nil {
		return nil, "", err
	}
	var out struct {
		Result struct {
			Points []struct {
				ID json.RawMessage `json:"id"`
			} `json:"points"`
			NextPageOffset json.RawMessage `json:"next_page_offset"` //nolint:tagliatelle // Qdrant wire field
		} `json:"result"`
	}
	path := "/collections/" + url.PathEscape(container) + "/points/scroll"
	if err := q.t.requestJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(out.Result.Points))
	for _, p := range out.Result.Points {
		ids = append(ids, rawToID(p.ID))
	}
	next := ""
	if n := out.Result.NextPageOffset; len(n) > 0 && string(n) != "null" {
		next = string(n)
	}
	return ids, next, nil
}

func (q *qdrantEngine) getDoc(ctx context.Context, container, id string) ([]byte, error) {
	var out struct {
		Result json.RawMessage `json:"result"`
	}
	path := "/collections/" + url.PathEscape(container) + "/points/" + url.PathEscape(id)
	if err := q.t.requestJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if len(out.Result) == 0 || string(out.Result) == "null" {
		return nil, provider.ErrNotFound
	}
	return out.Result, nil
}

// putDoc/deleteDoc reject: Qdrant points are vector-bearing and not
// human-editable, so the engine is read-only regardless of the configured flag.
func (q *qdrantEngine) putDoc(ctx context.Context, container, id string, body []byte) error {
	return provider.ErrReadOnly
}

func (q *qdrantEngine) deleteDoc(ctx context.Context, container, id string) error {
	return provider.ErrReadOnly
}

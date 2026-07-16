package document

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// meiliEngine speaks the Meilisearch REST dialect (Bearer auth). Document ids are
// keyed by the index's primaryKey field; the cursor is the document `offset`.
type meiliEngine struct{ t *transport }

func newMeiliEngine(base, key string, client *http.Client) *meiliEngine {
	return &meiliEngine{t: &transport{
		base:   base,
		client: client,
		setAuth: func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+key)
		},
	}}
}

func (m *meiliEngine) name() string { return "meilisearch" }

func (m *meiliEngine) containers(ctx context.Context) ([]string, error) {
	var out struct {
		Results []struct {
			UID string `json:"uid"`
		} `json:"results"`
	}
	if err := m.t.requestJSON(ctx, http.MethodGet, "/indexes?limit=1000", nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Results))
	for _, r := range out.Results {
		names = append(names, r.UID)
	}
	return names, nil
}

// primaryKey resolves the id field name for an index ("id" when the index has
// not yet inferred one — Meilisearch's conventional default).
func (m *meiliEngine) primaryKey(ctx context.Context, uid string) (string, error) {
	var out struct {
		PrimaryKey string `json:"primaryKey"`
	}
	if err := m.t.requestJSON(ctx, http.MethodGet, "/indexes/"+url.PathEscape(uid), nil, &out); err != nil {
		return "", err
	}
	if out.PrimaryKey == "" {
		return "id", nil
	}
	return out.PrimaryKey, nil
}

func (m *meiliEngine) docs(ctx context.Context, container, cursor string, limit int) ([]string, string, error) {
	pk, err := m.primaryKey(ctx, container)
	if err != nil {
		return nil, "", err
	}
	off := 0
	if cursor != "" {
		n, perr := strconv.Atoi(cursor)
		if perr != nil {
			return nil, "", invalidCursor()
		}
		off = n
	}
	q := url.Values{}
	q.Set("offset", strconv.Itoa(off))
	q.Set("limit", strconv.Itoa(limit))
	var out struct {
		Results []map[string]json.RawMessage `json:"results"`
	}
	path := "/indexes/" + url.PathEscape(container) + "/documents?" + q.Encode()
	if err := m.t.requestJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(out.Results))
	for _, doc := range out.Results {
		ids = append(ids, rawToID(doc[pk]))
	}
	next := ""
	if len(out.Results) == limit {
		next = strconv.Itoa(off + limit)
	}
	return ids, next, nil
}

func (m *meiliEngine) getDoc(ctx context.Context, container, id string) ([]byte, error) {
	path := "/indexes/" + url.PathEscape(container) + "/documents/" + url.PathEscape(id)
	return m.t.request(ctx, http.MethodGet, path, nil)
}

func (m *meiliEngine) putDoc(ctx context.Context, container, id string, body []byte) error {
	// Meilisearch upserts an ARRAY of documents, keyed by primaryKey; the edited
	// body already carries it, so id is implicit in the payload.
	wrapped := make([]byte, 0, len(body)+2)
	wrapped = append(wrapped, '[')
	wrapped = append(wrapped, body...)
	wrapped = append(wrapped, ']')
	path := "/indexes/" + url.PathEscape(container) + "/documents"
	_, err := m.t.request(ctx, http.MethodPut, path, wrapped)
	return err
}

func (m *meiliEngine) deleteDoc(ctx context.Context, container, id string) error {
	path := "/indexes/" + url.PathEscape(container) + "/documents/" + url.PathEscape(id)
	_, err := m.t.request(ctx, http.MethodDelete, path, nil)
	return err
}

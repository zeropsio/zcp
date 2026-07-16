package document

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// typesenseMaxPerPage is Typesense's hard cap on documents/search per_page.
const typesenseMaxPerPage = 250

// typesenseEngine speaks the Typesense REST dialect (X-TYPESENSE-API-KEY). The
// cursor is the 1-based search page number.
type typesenseEngine struct{ t *transport }

func newTypesenseEngine(base, key string, client *http.Client) *typesenseEngine {
	return &typesenseEngine{t: &transport{
		base:   base,
		client: client,
		setAuth: func(r *http.Request) {
			r.Header.Set("X-TYPESENSE-API-KEY", key)
		},
	}}
}

func (ts *typesenseEngine) name() string { return "typesense" }

func (ts *typesenseEngine) containers(ctx context.Context) ([]string, error) {
	var out []struct {
		Name string `json:"name"`
	}
	if err := ts.t.requestJSON(ctx, http.MethodGet, "/collections", nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out))
	for _, c := range out {
		names = append(names, c.Name)
	}
	return names, nil
}

func (ts *typesenseEngine) docs(ctx context.Context, container, cursor string, limit int) ([]string, string, error) {
	if limit > typesenseMaxPerPage {
		limit = typesenseMaxPerPage
	}
	page := 1
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, "", invalidCursor()
		}
		page = n
	}
	q := url.Values{}
	q.Set("q", "*")
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", strconv.Itoa(page))
	var out struct {
		Hits []struct {
			Document struct {
				ID string `json:"id"`
			} `json:"document"`
		} `json:"hits"`
	}
	path := "/collections/" + url.PathEscape(container) + "/documents/search?" + q.Encode()
	if err := ts.t.requestJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(out.Hits))
	for _, h := range out.Hits {
		ids = append(ids, h.Document.ID)
	}
	next := ""
	if len(out.Hits) == limit {
		next = strconv.Itoa(page + 1)
	}
	return ids, next, nil
}

func (ts *typesenseEngine) getDoc(ctx context.Context, container, id string) ([]byte, error) {
	path := "/collections/" + url.PathEscape(container) + "/documents/" + url.PathEscape(id)
	return ts.t.request(ctx, http.MethodGet, path, nil)
}

func (ts *typesenseEngine) putDoc(ctx context.Context, container, id string, body []byte) error {
	// Typesense upserts a single document (the body must carry "id").
	path := "/collections/" + url.PathEscape(container) + "/documents?action=upsert"
	_, err := ts.t.request(ctx, http.MethodPost, path, body)
	return err
}

func (ts *typesenseEngine) deleteDoc(ctx context.Context, container, id string) error {
	path := "/collections/" + url.PathEscape(container) + "/documents/" + url.PathEscape(id)
	_, err := ts.t.request(ctx, http.MethodDelete, path, nil)
	return err
}

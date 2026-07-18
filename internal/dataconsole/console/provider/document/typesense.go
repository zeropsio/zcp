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
	// Identity guard (DD-6): typesense routes a document by the body's "id"
	// field, ignoring the URL — a body id that differs misroutes and an omitted
	// id makes typesense auto-assign one (audit D-05, both silent). A
	// path-addressed upsert MUST carry the matching id.
	if err := typesenseRequireBodyID(body, id); err != nil {
		return err
	}
	// Typesense upserts a single document (the body must carry "id").
	path := "/collections/" + url.PathEscape(container) + "/documents?action=upsert"
	_, err := ts.t.request(ctx, http.MethodPost, path, body)
	return err
}

// typesenseRequireBodyID enforces that the body's "id" equals the path id.
func typesenseRequireBodyID(body []byte, id string) error {
	bid, present, err := bodyField(body, "id")
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("doc: typesense: %w: document is missing its id", provider.ErrInvalid)
	}
	if bid != id {
		return fmt.Errorf("doc: typesense: %w: document id %q does not match path id %q", provider.ErrInvalid, bid, id)
	}
	return nil
}

// search runs a bounded query over a collection. Typesense requires an explicit
// query_by field list, so the collection schema's string fields are resolved
// first; only matching document ids are returned (never the per-hit
// `highlights`, which are not read — no engine markup crosses the wire, S16).
func (ts *typesenseEngine) search(ctx context.Context, container, q, cursor string, limit int) ([]string, string, error) {
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
	queryBy, err := ts.queryByFields(ctx, container)
	if err != nil {
		return nil, "", err
	}
	qParam := q
	if qParam == "" {
		qParam = "*"
	}
	qv := url.Values{}
	qv.Set("q", qParam)
	qv.Set("query_by", queryBy)
	qv.Set("per_page", strconv.Itoa(limit))
	qv.Set("page", strconv.Itoa(page))
	var out struct {
		Hits []struct {
			Document struct {
				ID string `json:"id"`
			} `json:"document"`
		} `json:"hits"`
	}
	path := "/collections/" + url.PathEscape(container) + "/documents/search?" + qv.Encode()
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

// queryByFields resolves a collection's searchable string fields for query_by.
// A collection with no string field cannot be text-searched (ErrUnsupported).
func (ts *typesenseEngine) queryByFields(ctx context.Context, container string) (string, error) {
	var out struct {
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	}
	path := "/collections/" + url.PathEscape(container)
	if err := ts.t.requestJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	fields := make([]string, 0, len(out.Fields))
	for _, f := range out.Fields {
		if f.Type == "string" || f.Type == "string[]" {
			fields = append(fields, f.Name)
		}
	}
	if len(fields) == 0 {
		return "", fmt.Errorf("doc: typesense: %w: collection has no string field to search", provider.ErrUnsupported)
	}
	return strings.Join(fields, ","), nil
}

// createDoc creates a NEW document. action=create refuses a duplicate id with
// 409 → ErrConflict (unlike putDoc's action=upsert). With an explicit id the
// body must match it (identity guard); with no id typesense mints one, echoed
// back from the created-document response (audit D-05: the auto-assigned id was
// previously invisible to the caller).
func (ts *typesenseEngine) createDoc(ctx context.Context, container, id string, body []byte) (string, error) {
	if id != "" {
		if err := typesenseRequireBodyID(body, id); err != nil {
			return "", err
		}
	}
	path := "/collections/" + url.PathEscape(container) + "/documents?action=create"
	resp, err := ts.t.request(ctx, http.MethodPost, path, body)
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if uerr := json.Unmarshal(resp, &out); uerr != nil {
		if id != "" {
			return id, nil
		}
		return "", fmt.Errorf("doc: typesense: decode create response: %w", provider.ErrUpstream)
	}
	if out.ID != "" {
		return out.ID, nil
	}
	return id, nil
}

func (ts *typesenseEngine) deleteDoc(ctx context.Context, container, id string) error {
	path := "/collections/" + url.PathEscape(container) + "/documents/" + url.PathEscape(id)
	_, err := ts.t.request(ctx, http.MethodDelete, path, nil)
	return err
}

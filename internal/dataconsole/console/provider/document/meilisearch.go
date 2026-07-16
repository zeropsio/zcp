package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	// meiliTaskPollInterval/meiliTaskPollTimeout bound putDoc's wait for a
	// write's real outcome (see waitTask) — production defaults; tests shrink
	// both via a direct struct literal for determinism/speed.
	meiliTaskPollInterval = 150 * time.Millisecond
	meiliTaskPollTimeout  = 10 * time.Second
	// maxTaskReasonLen bounds the sanitized failure reason wrapped into a
	// returned error (defense-in-depth against an unexpectedly large upstream
	// message ending up in logs).
	maxTaskReasonLen = 200
)

// meiliEngine speaks the Meilisearch REST dialect (Bearer auth). Document ids are
// keyed by the index's primaryKey field; the cursor is the document `offset`.
type meiliEngine struct {
	t            *transport
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func newMeiliEngine(base, key string, client *http.Client) *meiliEngine {
	return &meiliEngine{
		t: &transport{
			base:   base,
			client: client,
			setAuth: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+key)
			},
		},
		pollInterval: meiliTaskPollInterval,
		pollTimeout:  meiliTaskPollTimeout,
	}
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

// putDoc upserts a document, then waits for Meilisearch's async task queue to
// actually apply it (waitTask) before reporting success — a bare 2xx from the
// write below only means "enqueued", never "applied" (DOC-AUD-01).
func (m *meiliEngine) putDoc(ctx context.Context, container, id string, body []byte) error {
	// Identity guard (DD-6): meilisearch routes a document by its primaryKey
	// field in the body, IGNORING any path id — so a body whose primaryKey
	// DIFFERS from the path silently lands at the wrong id (audit D-05). Resolve
	// the index's primaryKey and refuse a mismatch here. A body that OMITS the
	// primaryKey is left to meili's async validation, which the task poll below
	// now surfaces honestly (a missing-pk write no longer vanishes — DOC-AUD-01).
	pk, err := m.primaryKey(ctx, container)
	if err != nil {
		return err
	}
	if bid, present, berr := bodyField(body, pk); berr != nil {
		return berr
	} else if present && bid != id {
		return fmt.Errorf("doc: meilisearch: %w: document %s %q does not match path id %q", provider.ErrInvalid, pk, bid, id)
	}
	// Meilisearch upserts an ARRAY of documents, keyed by primaryKey; the edited
	// body already carries it, so id is implicit in the payload.
	wrapped := make([]byte, 0, len(body)+2)
	wrapped = append(wrapped, '[')
	wrapped = append(wrapped, body...)
	wrapped = append(wrapped, ']')
	path := "/indexes/" + url.PathEscape(container) + "/documents"
	resp, err := m.t.request(ctx, http.MethodPut, path, wrapped)
	if err != nil {
		return err
	}
	var enqueued struct {
		TaskUID int64 `json:"taskUid"`
	}
	if err := json.Unmarshal(resp, &enqueued); err != nil {
		return fmt.Errorf("doc: meilisearch: decode enqueue response: %w", provider.ErrUpstream)
	}
	return m.waitTask(ctx, enqueued.TaskUID)
}

// waitTask polls a Meilisearch task (GET /tasks/{uid}) until it reaches a
// terminal status, so putDoc never reports a write as applied while it is
// only enqueued or still processing (DOC-AUD-01). A task that fails
// Meilisearch's own async per-document validation returns a typed error
// carrying a sanitized version of meili's failure reason (ErrInvalid — the
// submitted document was rejected); a task still non-terminal once
// pollTimeout elapses returns a distinct "accepted, not confirmed" error
// (ErrTimeout) — the write may or may not still apply, so it must not be
// folded into either success or the failed-validation case. Never returns nil
// without having observed status "succeeded".
func (m *meiliEngine) waitTask(ctx context.Context, taskUID int64) error {
	deadline := time.Now().Add(m.pollTimeout)
	for {
		var task struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if err := m.t.requestJSON(ctx, http.MethodGet, fmt.Sprintf("/tasks/%d", taskUID), nil, &task); err != nil {
			return err
		}
		switch task.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			reason := "task " + task.Status
			if task.Error != nil {
				reason = sanitizeTaskReason(task.Error.Code, task.Error.Message)
			}
			return fmt.Errorf("doc: meilisearch: task %d %s: %s: %w", taskUID, task.Status, reason, provider.ErrInvalid)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("doc: meilisearch: task %d not confirmed within %s: %w", taskUID, m.pollTimeout, provider.ErrTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.pollInterval):
		}
	}
}

// sanitizeTaskReason renders a bounded, control-character-free summary of a
// meili task's failure for inclusion in the returned error's message (stderr
// diagnostic sink only — the client-facing envelope stays the generic
// per-sentinel message, see server.publicErrorMessage).
func sanitizeTaskReason(code, message string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, message)
	if len(clean) > maxTaskReasonLen {
		clean = clean[:maxTaskReasonLen] + "..."
	}
	if code != "" {
		return code + ": " + clean
	}
	return clean
}

func (m *meiliEngine) deleteDoc(ctx context.Context, container, id string) error {
	path := "/indexes/" + url.PathEscape(container) + "/documents/" + url.PathEscape(id)
	_, err := m.t.request(ctx, http.MethodDelete, path, nil)
	return err
}

// search runs a bounded query over an index, returning matching document ids
// (the primaryKey field of each hit) — never the `_formatted`/highlight fields,
// which are not requested, so no engine markup crosses the wire (S16).
func (m *meiliEngine) search(ctx context.Context, container, q, cursor string, limit int) ([]string, string, error) {
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
	body, err := jsonBody(map[string]any{"q": q, "offset": off, "limit": limit})
	if err != nil {
		return nil, "", err
	}
	var out struct {
		Hits []map[string]json.RawMessage `json:"hits"`
	}
	path := "/indexes/" + url.PathEscape(container) + "/search"
	if err := m.t.requestJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(out.Hits))
	for _, doc := range out.Hits {
		ids = append(ids, rawToID(doc[pk]))
	}
	next := ""
	if len(out.Hits) == limit {
		next = strconv.Itoa(off + limit)
	}
	return ids, next, nil
}

// createDoc creates a NEW document at an explicit id (meilisearch has no
// engine-assigned id — the primaryKey field must be present, so an empty id is
// refused). Collision is refused by a pre-check (meili has no create-only
// endpoint); the write itself reuses putDoc, so it is identity-guarded AND
// task-confirmed (never a false success on enqueue — DOC-AUD-01).
func (m *meiliEngine) createDoc(ctx context.Context, container, id string, body []byte) (string, error) {
	if id == "" {
		return "", fmt.Errorf("doc: meilisearch: %w: create requires an explicit id", provider.ErrInvalid)
	}
	if _, err := m.getDoc(ctx, container, id); err == nil {
		return "", fmt.Errorf("doc: meilisearch: %w: document %q already exists", provider.ErrConflict, id)
	} else if !errors.Is(err, provider.ErrNotFound) {
		return "", err
	}
	if err := m.putDoc(ctx, container, id, body); err != nil {
		return "", err
	}
	return id, nil
}

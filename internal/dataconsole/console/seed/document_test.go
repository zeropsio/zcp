package seed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// TestSeedQdrant_CreateCollectionConflict_ProceedsToUpsert pins the
// live-shakeout fix (2026-07-16, VPN run against zcp-eval-clean): the live
// failure was "create collection items: PUT .../collections/items -> 409
// ... Collection `items` already exists!" — a re-run against an engine that
// already has the collection from a prior seed. A 409 on create-collection
// must be treated as success-proceed (the collection is already there) and
// the seeder must still perform the points upsert; any other status must
// still fail.
func TestSeedQdrant_CreateCollectionConflict_ProceedsToUpsert(t *testing.T) {
	t.Parallel()
	var pointsPUTArrived atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/collections/items":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"status":{"error":"Collection ` + "`items`" + ` already exists!"},"time":0}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/items/points":
			pointsPUTArrived.Store(true)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":{"status":"completed"},"status":"ok","time":0}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	desc := provider.DocumentConn{Engine: "qdrant", BaseURL: srv.URL}
	if err := Service(context.Background(), "qdrant", desc, Options{}); err != nil {
		t.Fatalf("Service (seed qdrant, create-collection 409): %v", err)
	}
	if !pointsPUTArrived.Load() {
		t.Error("points PUT never arrived — seeder must proceed to upsert after a 409 on create-collection")
	}
}

// TestSeedQdrant_CreateCollectionServerError_Fails pins the "only 409 is
// tolerated" half of the same fix: a 500 on create-collection is a real
// failure and must not be swallowed the way the pre-S10b dcseed swallowed
// every create-collection error unconditionally.
func TestSeedQdrant_CreateCollectionServerError_Fails(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/collections/items" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":{"error":"internal error"},"time":0}`))
			return
		}
		t.Errorf("unexpected request reached the server after create-collection should have failed: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	desc := provider.DocumentConn{Engine: "qdrant", BaseURL: srv.URL}
	if err := Service(context.Background(), "qdrant", desc, Options{}); err == nil {
		t.Fatal("Service (seed qdrant, create-collection 500): want error, got nil")
	}
}

// TestSeedTypesense_CreateCollectionConflict_ProceedsToImport mirrors the
// qdrant case for typesense: pre-S10b dcseed unconditionally swallowed the
// create-collection error (`_ = httpReq(...)`), which would also swallow a
// real failure (auth, 500, network) and let a confusing downstream import
// error stand in for it. The fix must tolerate EXACTLY 409 (Typesense's
// documented "resource already exists" status) and still let other statuses
// fail loudly.
func TestSeedTypesense_CreateCollectionConflict_ProceedsToImport(t *testing.T) {
	t.Parallel()
	var importArrived atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/collections":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"A collection with name ` + "`products`" + ` already exists."}`))
		case r.Method == http.MethodPost && r.URL.Path == "/collections/products/documents/import":
			importArrived.Store(true)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}` + "\n" + `{"success":true}` + "\n"))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	desc := provider.DocumentConn{Engine: "typesense", BaseURL: srv.URL}
	if err := Service(context.Background(), "typesense", desc, Options{}); err != nil {
		t.Fatalf("Service (seed typesense, create-collection 409): %v", err)
	}
	if !importArrived.Load() {
		t.Error("document import never arrived — seeder must proceed after a 409 on create-collection")
	}
}

// TestSeedTypesense_CreateCollectionServerError_Fails pins that a 500 on
// create-collection is a real failure — the pre-fix code swallowed this
// unconditionally (`_ = httpReq(...)`), which this test would not have
// caught; the fix must not regress back to that.
func TestSeedTypesense_CreateCollectionServerError_Fails(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/collections" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal error"}`))
			return
		}
		t.Errorf("unexpected request reached the server after create-collection should have failed: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	desc := provider.DocumentConn{Engine: "typesense", BaseURL: srv.URL}
	if err := Service(context.Background(), "typesense", desc, Options{}); err == nil {
		t.Fatal("Service (seed typesense, create-collection 500): want error, got nil")
	}
}

// TestSeedElastic_IndexesAndRefreshes is a control case for the sweep: the
// elasticsearch seeder has no create-style call at all (PUT .../_doc/<id> is
// an upsert — index auto-create + replace-if-present — so there is no
// "already exists" class to tolerate), pinned here with a fake server so a
// future change that adds one doesn't silently reintroduce the swallowed-
// error pattern without a matching test.
func TestSeedElastic_IndexesAndRefreshes(t *testing.T) {
	t.Parallel()
	var docPUTs, refreshes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/products/_doc/1":
			docPUTs.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"result":"created"}`))
		case r.Method == http.MethodPut:
			docPUTs.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"result":"created"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/_refresh":
			refreshes.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"_shards":{"total":1,"successful":1,"failed":0}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	desc := provider.DocumentConn{Engine: "elasticsearch", BaseURL: srv.URL}
	if err := Service(context.Background(), "elasticsearch", desc, Options{}); err != nil {
		t.Fatalf("Service (seed elasticsearch): %v", err)
	}
	if docPUTs.Load() != 4 {
		t.Errorf("doc PUTs = %d, want 4 (3 products + 1 article)", docPUTs.Load())
	}
	if refreshes.Load() != 1 {
		t.Errorf("refreshes = %d, want 1", refreshes.Load())
	}
}

// TestSeedMeili_AddsDocuments is a control case for the sweep: meilisearch's
// "add or replace documents" endpoint lazily creates the index and upserts
// by primary key, so there is no separate create-style call to tolerate.
// The endpoint is TASK-ASYNC (202 + taskUid): the seed must not return until
// the task reaches a terminal succeeded status — an unconfirmed seed leaves
// the index nonexistent under queue backlog and every later case read fails
// (spec-dataconsole §7.1 I-1 applied to the fixture itself).
func TestSeedMeili_AddsDocuments(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var got []map[string]any
	polls := 0
	succeededServed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/indexes/products/documents" {
			mu.Lock()
			defer mu.Unlock()
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Errorf("decode body: %v", err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"taskUid":7,"status":"enqueued"}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/tasks/7" {
			mu.Lock()
			defer mu.Unlock()
			polls++
			if polls == 1 {
				_, _ = w.Write([]byte(`{"status":"processing"}`))
				return
			}
			succeededServed = true
			_, _ = w.Write([]byte(`{"status":"succeeded"}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	desc := provider.DocumentConn{Engine: "meilisearch", BaseURL: srv.URL}
	if err := Service(context.Background(), "meilisearch", desc, Options{}); err != nil {
		t.Fatalf("Service (seed meilisearch): %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Errorf("documents posted = %d, want 3", len(got))
	}
	if !succeededServed {
		t.Errorf("seed returned before the task reached a terminal succeeded status (polls=%d)", polls)
	}
}

// TestSeedMeili_FailedTask_Errors pins the other terminal: a task that ends
// failed/canceled is a fixture FAILURE the seed must surface, never swallow —
// returning nil here would hand the cases a nonexistent index with a green
// setup.
func TestSeedMeili_FailedTask_Errors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/indexes/products/documents" {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"taskUid":9,"status":"enqueued"}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/tasks/9" {
			_, _ = w.Write([]byte(`{"status":"failed","error":{"message":"invalid document id"}}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	desc := provider.DocumentConn{Engine: "meilisearch", BaseURL: srv.URL}
	err := Service(context.Background(), "meilisearch", desc, Options{})
	if err == nil {
		t.Fatal("Service (seed meilisearch) = nil error, want failed-task error")
	}
	if !strings.Contains(err.Error(), "failed") || !strings.Contains(err.Error(), "invalid document id") {
		t.Errorf("error %q does not name the failed task and its message", err)
	}
}

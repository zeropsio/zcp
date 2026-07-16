package document

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// ---- S16 document search ----

// TestSearch_ES_ReturnsMatchingDocs pins the bounded, read-only search: a query
// over an index returns matching document ids as blob nodes.
func TestSearch_ES_ReturnsMatchingDocs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/_search") {
			_, _ = w.Write([]byte(`{"hits":{"hits":[{"_id":"a"},{"_id":"b"}]}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	p := mustProvider(t, "elasticsearch", srv.URL)
	assertSearchIDs(t, p, provider.Path{Segments: []string{"idx"}}, "hello", []string{"a", "b"})
}

// TestSearch_Meili_ReturnsMatchingDocs pins meili search (primaryKey-keyed hits).
func TestSearch_Meili_ReturnsMatchingDocs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(`{"hits":[{"id":"a"},{"id":"b"}]}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/indexes/") && !strings.Contains(r.URL.Path, "/documents") && !strings.Contains(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(`{"primaryKey":"id"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := mustProvider(t, "meilisearch", srv.URL)
	assertSearchIDs(t, p, provider.Path{Segments: []string{"products"}}, "hello", []string{"a", "b"})
}

// TestSearch_Typesense_ReturnsMatchingDocs pins typesense search — the schema is
// resolved for query_by, then matching document ids returned.
func TestSearch_Typesense_ReturnsMatchingDocs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/documents/search"):
			_, _ = w.Write([]byte(`{"hits":[{"document":{"id":"a"}},{"document":{"id":"b"}}]}`))
		case r.URL.Path == "/collections/products":
			_, _ = w.Write([]byte(`{"fields":[{"name":"title","type":"string"},{"name":"price","type":"int32"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := mustProvider(t, "typesense", srv.URL)
	assertSearchIDs(t, p, provider.Path{Segments: []string{"products"}}, "hello", []string{"a", "b"})
}

// TestSearch_TooLongQuery_Rejected pins the input bound: an over-length query is
// refused (ErrInvalid) before any upstream call.
func TestSearch_TooLongQuery_Rejected(t *testing.T) {
	t.Parallel()
	p := mustProvider(t, "elasticsearch", "http://es:9200")
	long := strings.Repeat("a", maxSearchQueryLen+1)
	if _, _, err := p.Search(context.Background(), provider.Path{Segments: []string{"idx"}}, long, provider.Page{}); !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("over-long query = %v, want ErrInvalid", err)
	}
}

// TestSearch_Qdrant_Unsupported pins that qdrant (vector/payload search,
// deferred) does not advertise the searcher capability.
func TestSearch_Qdrant_Unsupported(t *testing.T) {
	t.Parallel()
	p := mustProvider(t, "qdrant", "http://q:6333")
	if _, _, err := p.Search(context.Background(), provider.Path{Segments: []string{"col"}}, "x", provider.Page{}); !errors.Is(err, provider.ErrUnsupported) {
		t.Fatalf("qdrant search = %v, want ErrUnsupported", err)
	}
}

// TestSearch_WrongArity_Rejected pins that search addresses exactly one container.
func TestSearch_WrongArity_Rejected(t *testing.T) {
	t.Parallel()
	p := mustProvider(t, "elasticsearch", "http://es:9200")
	if _, _, err := p.Search(context.Background(), provider.Path{Segments: []string{"idx", "doc"}}, "x", provider.Page{}); !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("2-seg search = %v, want ErrInvalid", err)
	}
}

// ---- S19 identity guard (DD-6) ----

// TestWriteBlob_ES_IdentityMismatch_Refused pins that a body _id differing from
// the path id is refused, and no write ever reaches the engine (no duplicate).
func TestWriteBlob_ES_IdentityMismatch_Refused(t *testing.T) {
	t.Parallel()
	rec := &writeRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	p := mustWritable(t, "elasticsearch", srv.URL)
	err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"idx", "1"}}, []byte(`{"_id":"different","title":"x"}`), "")
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("es id mismatch = %v, want ErrInvalid", err)
	}
	if n := rec.writeCount(); n != 0 {
		t.Fatalf("es id mismatch triggered %d writes, want 0 (no duplicate)", n)
	}
}

// TestWriteBlob_Meili_IdentityMismatch_Refused pins the meili misroute guard: a
// body primaryKey differing from the path is refused before enqueue.
func TestWriteBlob_Meili_IdentityMismatch_Refused(t *testing.T) {
	t.Parallel()
	rec := &writeRecorder{pk: "id"}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	p := mustWritable(t, "meilisearch", srv.URL)
	err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"products", "1"}}, []byte(`{"id":"BODYID","title":"x"}`), "")
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("meili id mismatch = %v, want ErrInvalid", err)
	}
	if n := rec.writeCount(); n != 0 {
		t.Fatalf("meili id mismatch triggered %d writes, want 0 (no misroute)", n)
	}
}

// TestWriteBlob_Typesense_IdentityMismatchOrMissing_Refused pins that a body id
// that differs from — or is absent from — the path is refused (audit D-05:
// typesense silently misroutes on mismatch and auto-assigns on omit).
func TestWriteBlob_Typesense_IdentityMismatchOrMissing_Refused(t *testing.T) {
	t.Parallel()
	rec := &writeRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	p := mustWritable(t, "typesense", srv.URL)
	if err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"products", "1"}}, []byte(`{"id":"BODYID"}`), ""); !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("typesense id mismatch = %v, want ErrInvalid", err)
	}
	if err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"products", "1"}}, []byte(`{"title":"x"}`), ""); !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("typesense missing id = %v, want ErrInvalid", err)
	}
	if n := rec.writeCount(); n != 0 {
		t.Fatalf("typesense id violation triggered %d writes, want 0", n)
	}
}

// ---- S17 document create ----

// TestCreateDoc_Meili_TaskConfirmed pins the RED: a create with an explicit id
// exists after the write, and is confirmed via the async task poll (never a
// false success on enqueue).
func TestCreateDoc_Meili_TaskConfirmed(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeMeiliServer(t, 5, []string{"succeeded"}, "", "")
	m := fastPollEngine(srv.URL)
	id, err := m.createDoc(context.Background(), "products", "newid", []byte(`{"id":"newid","title":"x"}`))
	if err != nil {
		t.Fatalf("meili createDoc = %v, want nil", err)
	}
	if id != "newid" {
		t.Fatalf("created id = %q, want newid", id)
	}
}

// TestCreateDoc_ES_CollisionRefused pins collision-refusing create: ES's
// create-only endpoint answers an existing id with 409 → ErrConflict.
func TestCreateDoc_ES_CollisionRefused(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/_create/") {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	e := &esEngine{t: &transport{base: srv.URL, client: http.DefaultClient, setAuth: func(*http.Request) {}}}
	if _, err := e.createDoc(context.Background(), "idx", "1", []byte(`{"title":"x"}`)); !errors.Is(err, provider.ErrConflict) {
		t.Fatalf("es create collision = %v, want ErrConflict", err)
	}
}

// TestCreateDoc_Typesense_EngineAssignedIDEchoed pins the auto-id echo (audit
// D-05: the auto-assigned id was previously invisible to the caller).
func TestCreateDoc_Typesense_EngineAssignedIDEchoed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "action=create") {
			_, _ = w.Write([]byte(`{"id":"5","title":"x"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	ts := &typesenseEngine{t: &transport{base: srv.URL, client: http.DefaultClient, setAuth: func(*http.Request) {}}}
	id, err := ts.createDoc(context.Background(), "products", "", []byte(`{"title":"x"}`))
	if err != nil {
		t.Fatalf("typesense auto-id create = %v", err)
	}
	if id != "5" {
		t.Fatalf("echoed id = %q, want 5 (engine-assigned)", id)
	}
}

// TestCreateDoc_Qdrant_ReadOnly pins that a view-only engine refuses create at
// the capability gate.
func TestCreateDoc_Qdrant_ReadOnly(t *testing.T) {
	t.Parallel()
	p := mustProvider(t, "qdrant", "http://q:6333")
	if _, err := p.CreateDoc(context.Background(), provider.Path{Segments: []string{"col", "1"}}, []byte(`{}`)); !errors.Is(err, provider.ErrReadOnly) {
		t.Fatalf("qdrant create = %v, want ErrReadOnly", err)
	}
}

// TestCreateDoc_WrongArity_Rejected pins the [container] or [container,id] shape.
func TestCreateDoc_WrongArity_Rejected(t *testing.T) {
	t.Parallel()
	p := mustWritable(t, "elasticsearch", "http://es:9200")
	if _, err := p.CreateDoc(context.Background(), provider.Path{Segments: []string{"a", "b", "c"}}, []byte(`{}`)); !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("3-seg create = %v, want ErrInvalid", err)
	}
}

// ---- D-06 truncation ----

// TestReadBlob_Truncation_IsValidJSON pins D-06: an over-cap document read must
// return VALID JSON with an honest marker (not a mangled mid-token byte-slice),
// and carry the TRUE size + Truncated flag.
func TestReadBlob_Truncation_IsValidJSON(t *testing.T) {
	t.Parallel()
	full := `{"_source":{"title":"` + strings.Repeat("x", 200) + `"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(full))
	}))
	defer srv.Close()
	p := mustWritable(t, "elasticsearch", srv.URL)
	p.caps.MaxInlineBytes = 40 // force truncation

	data, meta, err := p.ReadBlob(context.Background(), provider.Path{Segments: []string{"idx", "1"}})
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if !meta.Truncated {
		t.Fatalf("meta.Truncated = false, want true")
	}
	if !json.Valid(data) {
		t.Fatalf("truncated body is not valid JSON: %s", data)
	}
	if meta.Size <= p.caps.MaxInlineBytes {
		t.Fatalf("meta.Size = %d, want the TRUE (pre-truncation) size > cap %d", meta.Size, p.caps.MaxInlineBytes)
	}
}

// ---- helpers ----

func mustProvider(t *testing.T, engine, base string) *Provider {
	t.Helper()
	p, err := New(Config{Engine: engine, BaseURL: base})
	if err != nil {
		t.Fatalf("New(%s): %v", engine, err)
	}
	return p
}

func mustWritable(t *testing.T, engine, base string) *Provider {
	t.Helper()
	p, err := New(Config{Engine: engine, BaseURL: base, ReadOnly: false})
	if err != nil {
		t.Fatalf("New(%s): %v", engine, err)
	}
	return p
}

func assertSearchIDs(t *testing.T, p *Provider, path provider.Path, q string, want []string) {
	t.Helper()
	nodes, _, err := p.Search(context.Background(), path, q, provider.Page{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := make([]string, len(nodes))
	for i, n := range nodes {
		if n.Kind != provider.KindBlob {
			t.Fatalf("search node %d kind = %q, want blob", i, n.Kind)
		}
		got[i] = n.Name
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("search ids = %v, want %v", got, want)
	}
}

// writeRecorder is a fake engine server that answers the meili primaryKey lookup
// (when pk is set) and 404s everything else, while counting any write-shaped
// request (a document/create write) so a test can prove the identity guard
// rejected BEFORE any write reached the wire.
type writeRecorder struct {
	pk string
	mu sync.Mutex
	n  int
}

func (rec *writeRecorder) writeCount() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.n
}

func (rec *writeRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// meili primaryKey lookup — a read, never counted as a write.
	if rec.pk != "" && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/indexes/") && !strings.Contains(r.URL.Path, "/documents") {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"primaryKey":"` + rec.pk + `"}`))
		return
	}
	if r.Method == http.MethodPut || r.Method == http.MethodPost || r.Method == http.MethodDelete {
		rec.mu.Lock()
		rec.n++
		rec.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

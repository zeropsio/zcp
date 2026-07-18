package document

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

func TestNew_RejectsEmptyBaseURL(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{Engine: "elasticsearch"}); !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestNew_RejectsUnknownEngine(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{Engine: "mongo", BaseURL: "http://x:1"}); !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestNew_EngineCapsAndKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		engine       string
		readOnly     bool
		wantKind     string
		wantEdit     bool
		wantReadOnly bool
	}{
		{"elasticsearch", false, "elasticsearch", true, false},
		{"elasticsearch", true, "elasticsearch", false, true},
		{"meilisearch", false, "meilisearch", true, false},
		{"typesense", false, "typesense", true, false},
		{"qdrant", false, "qdrant", false, true}, // forced read-only
		{"qdrant", true, "qdrant", false, true},  // stays read-only
	}
	for _, c := range cases {
		p, err := New(Config{Engine: c.engine, BaseURL: "http://host:1", ReadOnly: c.readOnly})
		if err != nil {
			t.Fatalf("%s: New: %v", c.engine, err)
		}
		if p.Kind() != c.wantKind {
			t.Errorf("%s: Kind=%q want %q", c.engine, p.Kind(), c.wantKind)
		}
		caps := p.Caps()
		if caps.Family != provider.FamilyDocument {
			t.Errorf("%s: Family=%q", c.engine, caps.Family)
		}
		// Support must track the read-only gate (consistent with family.go): a
		// read-only engine is view-only, never "supported".
		wantSupport := provider.SupportFull
		if c.wantReadOnly {
			wantSupport = provider.SupportViewOnly
		}
		if caps.Support != wantSupport {
			t.Errorf("%s(ro=%v): Support=%q want %q", c.engine, c.readOnly, caps.Support, wantSupport)
		}
		if caps.MaxInlineBytes != maxInlineDefault {
			t.Errorf("%s: MaxInlineBytes=%d", c.engine, caps.MaxInlineBytes)
		}
		if caps.EditBlob != c.wantEdit {
			t.Errorf("%s: EditBlob=%v want %v", c.engine, caps.EditBlob, c.wantEdit)
		}
		if caps.ReadOnly != c.wantReadOnly {
			t.Errorf("%s: ReadOnly=%v want %v", c.engine, caps.ReadOnly, c.wantReadOnly)
		}
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: "elasticsearch", BaseURL: "http://es:9200/"})
	if err != nil {
		t.Fatal(err)
	}
	es, ok := p.eng.(*esEngine)
	if !ok {
		t.Fatalf("engine type = %T", p.eng)
	}
	if es.t.base != "http://es:9200" {
		t.Errorf("base=%q, want trailing slash trimmed", es.t.base)
	}
}

func TestBlobAddr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		segs    []string
		wantC   string
		wantID  string
		wantErr bool
	}{
		{nil, "", "", true},
		{[]string{"idx"}, "", "", true},
		{[]string{"idx", "42"}, "idx", "42", false},
		{[]string{"idx", "42", "extra"}, "", "", true},
	}
	for _, c := range cases {
		gotC, gotID, err := blobAddr(provider.Path{Segments: c.segs})
		if c.wantErr {
			if !errors.Is(err, provider.ErrInvalid) {
				t.Errorf("%v: want ErrInvalid, got %v", c.segs, err)
			}
			continue
		}
		if err != nil || gotC != c.wantC || gotID != c.wantID {
			t.Errorf("blobAddr(%v) = (%q,%q,%v)", c.segs, gotC, gotID, err)
		}
	}
}

func TestChild_NoAlias(t *testing.T) {
	t.Parallel()
	parent := provider.Path{Service: "es", Segments: []string{"idx"}}
	c := child(parent, "42")
	if c.Service != "es" || len(c.Segments) != 2 || c.Segments[0] != "idx" || c.Segments[1] != "42" {
		t.Fatalf("child = %+v", c)
	}
	if len(parent.Segments) != 1 {
		t.Fatalf("parent mutated: %+v", parent)
	}
}

func TestRawToID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
	}{
		{`"abc"`, "abc"},
		{`123`, "123"},
		{`"550e8400-e29b-41d4-a716-446655440000"`, "550e8400-e29b-41d4-a716-446655440000"},
		{``, ""},
	}
	for _, c := range cases {
		if got := rawToID(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("rawToID(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestPrettyJSON(t *testing.T) {
	t.Parallel()
	in := []byte(`{"a":1}`)
	out := prettyJSON(in)
	if string(out) != "{\n  \"a\": 1\n}" {
		t.Errorf("pretty = %q", out)
	}
	// non-JSON passes through unchanged
	bad := []byte("not json")
	if string(prettyJSON(bad)) != "not json" {
		t.Errorf("non-JSON should pass through")
	}
}

func TestStatusErr(t *testing.T) {
	t.Parallel()
	if !errors.Is(statusErr(http.StatusNotFound), provider.ErrNotFound) {
		t.Error("404 -> ErrNotFound")
	}
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusBadGateway, 500} {
		if !errors.Is(statusErr(code), provider.ErrUpstream) {
			t.Errorf("%d -> ErrUpstream", code)
		}
	}
}

// TestReadBlob_NonexistentIndexOrDoc_ReturnsNotFound locks in the document
// family's half of DOC-AUD-02/T-2's "not-found vs outage" split: unlike
// tabular's relation-not-found (which needed an actual fix — see
// provider/tabular's T-AUD-06 regression test), a document read against a
// nonexistent index/container already comes back as a clean 404 today —
// Elasticsearch answers a GET against a missing index with its own 404,
// which transport.statusErr already maps to ErrNotFound end to end.
// TestStatusErr above pins the mapping in isolation; this pins the same
// behavior through the real Provider (List/ReadBlob), not just the helper —
// no production code changes for this half, this is the regression lock the
// S27 finding calls for.
func TestReadBlob_NonexistentIndexOrDoc_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"index_not_found_exception"}`))
	}))
	defer srv.Close()

	p, err := New(Config{Engine: "elasticsearch", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := p.ReadBlob(context.Background(), provider.Path{Service: "es", Segments: []string{"does-not-exist", "1"}}); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("ReadBlob(nonexistent index) = %v, want ErrNotFound", err)
	}
	if _, _, err := p.List(context.Background(), provider.Path{Segments: []string{"does-not-exist"}}, provider.Page{}); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("List(nonexistent index) = %v, want ErrNotFound", err)
	}
}

// readOnlyGate proves the write methods reject before touching the network.
func TestWriteAndDelete_ReadOnlyGate(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: "elasticsearch", BaseURL: "http://es:9200", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	path := provider.Path{Service: "es", Segments: []string{"idx", "1"}}
	if err := p.WriteBlob(context.Background(), path, []byte(`{}`), "application/json"); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("WriteBlob: want ErrReadOnly, got %v", err)
	}
	if err := p.Delete(context.Background(), path); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("Delete: want ErrReadOnly, got %v", err)
	}
}

func TestQdrant_WriteRejects(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: "qdrant", BaseURL: "http://q:6333"})
	if err != nil {
		t.Fatal(err)
	}
	path := provider.Path{Service: "q", Segments: []string{"col", "1"}}
	if err := p.WriteBlob(context.Background(), path, []byte(`{}`), "application/json"); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("qdrant WriteBlob: want ErrReadOnly, got %v", err)
	}
	// engine-level guard rejects even if the cap gate were bypassed
	if err := p.eng.putDoc(context.Background(), "col", "1", []byte(`{}`)); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("qdrant putDoc: want ErrReadOnly, got %v", err)
	}
}

// TestStat_ReportsTypedNodeMeta pins the presentation contract's typed
// NodeMeta (DD-4): Stat's Node.Meta must carry Size + ContentType, not the
// old untyped map[string]any.
func TestStat_ReportsTypedNodeMeta(t *testing.T) {
	t.Parallel()
	source := `{"a":1}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"_source":` + source + `}`))
	}))
	defer srv.Close()

	p, err := New(Config{Engine: "elasticsearch", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	node, err := p.Stat(context.Background(), provider.Path{Segments: []string{"idx", "1"}})
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if node.Meta == nil || node.Meta.Size == nil || *node.Meta.Size != int64(len(source)) {
		t.Errorf("Meta.Size = %v, want %d", node.Meta, len(source))
	}
	if node.Meta.ContentType != "application/json" {
		t.Errorf("Meta.ContentType = %q, want application/json", node.Meta.ContentType)
	}
}

// TestReadBlob_Qdrant_SignalsVector pins UI-AUD-03's minimal server-side
// signal: a qdrant point's raw JSON embeds a full embedding array (fine at 4
// dims, a wall of numbers at a real 384/768/1536-dim embedding), so
// BlobMeta.Vector must be true on every qdrant ReadBlob — the exact field the
// SPA (S15) keys on to collapse it into a summary instead of rendering the
// array inline.
func TestReadBlob_Qdrant_SignalsVector(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"id":1,"payload":{},"vector":[0.1,0.2,0.3,0.4]}}`))
	}))
	defer srv.Close()

	p, err := New(Config{Engine: "qdrant", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, meta, err := p.ReadBlob(context.Background(), provider.Path{Segments: []string{"col", "1"}})
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if !meta.Vector {
		t.Fatalf("qdrant ReadBlob BlobMeta.Vector = false, want true (UI-AUD-03)")
	}
}

// TestReadBlob_Elasticsearch_NotVector is the companion GREEN case: a
// non-qdrant engine must never carry the vector signal.
func TestReadBlob_Elasticsearch_NotVector(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"_source":{"a":1}}`))
	}))
	defer srv.Close()

	p, err := New(Config{Engine: "elasticsearch", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, meta, err := p.ReadBlob(context.Background(), provider.Path{Segments: []string{"idx", "1"}})
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if meta.Vector {
		t.Fatalf("elasticsearch ReadBlob BlobMeta.Vector = true, want false")
	}
}

// Arity guards reject before any network call.
func TestList_RejectsTooDeepPath(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: "meilisearch", BaseURL: "http://m:7700"})
	if err != nil {
		t.Fatal(err)
	}
	path := provider.Path{Segments: []string{"idx", "doc", "deep"}}
	if _, _, err := p.List(context.Background(), path, provider.Page{}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("List 3-seg: want ErrInvalid, got %v", err)
	}
}

func TestStat_RejectsWrongArity(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: "typesense", BaseURL: "http://t:8108"})
	if err != nil {
		t.Fatal(err)
	}
	path := provider.Path{Segments: []string{"only-container"}}
	if _, err := p.Stat(context.Background(), path); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("Stat 1-seg: want ErrInvalid, got %v", err)
	}
}

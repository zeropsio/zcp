package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// TestServer_Search_WorksInReadOnlySession is the S16 security-acceptance pin:
// /api/search is a READ — it is behind the bearer (matrix covers 401), is
// non-mutating (coherence + drift guard cover mutating:false), and needs NO
// write token, so it returns results even in a read-only session (a standalone
// SPA can search). The response is the nodes shape the tree/list uses.
func TestServer_Search_WorksInReadOnlySession(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "elasticsearch", provider.FamilyDocument, false) // read-only session
	defer ts.Close()
	r := do(t, ts, "GET", "/api/search?service=svc&q=hello", tok, "", false)
	if r.code != http.StatusOK {
		t.Fatalf("search in read-only session = %d, want 200 (search is a read)", r.code)
	}
	if rec.lastOp != "search" {
		t.Fatalf("Search not reached: lastOp=%q", rec.lastOp)
	}
}

// TestServer_Search_ResponseShape pins the wire shape: {nodes, nextCursor}.
func TestServer_Search_ResponseShape(t *testing.T) {
	t.Parallel()
	ts, tok, _, _ := newRouteServer(t, "elasticsearch", provider.FamilyDocument, true)
	defer ts.Close()
	req := newRequest(t, http.MethodGet, "/api/search?service=svc&q=hello", nil)
	resp := doReq(t, ts, req, tok, false)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Nodes []provider.Node `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Nodes) != 1 || out.Nodes[0].Name != "hit-hello" {
		t.Fatalf("search nodes = %+v, want one hit-hello node", out.Nodes)
	}
}

// TestServer_Search_NonDocumentFamily_Unsupported pins that a family without a
// Search method gets a clean 422, not a 500.
func TestServer_Search_NonDocumentFamily_Unsupported(t *testing.T) {
	t.Parallel()
	// object fake has no Search method.
	ts, tok := newTestServer(t, true)
	defer ts.Close()
	if r := do(t, ts, "GET", "/api/search?service=store&q=x", tok, "", false); r.code != http.StatusUnprocessableEntity {
		t.Fatalf("search on object family = %d, want 422", r.code)
	}
}

// TestServer_KVCreate_ReachesProvider pins that a valid create body reaches the
// KV provider's CreateKey under the write posture.
func TestServer_KVCreate_ReachesProvider(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "valkey@7", provider.FamilyKV, true)
	defer ts.Close()
	body := `{"path":{"service":"svc","segments":["h"]},"type":"hash","field":"a","value":"MQ=="}`
	if r := do(t, ts, "POST", "/api/kv/create", tok, body, true); r.code != http.StatusOK {
		t.Fatalf("kv create = %d, want 200", r.code)
	}
	if rec.lastOp != "createkey" {
		t.Fatalf("CreateKey not reached: lastOp=%q", rec.lastOp)
	}
}

// TestServer_KVCreate_WriteGate pins that create is write-token gated (a
// read-only session refuses before the provider).
func TestServer_KVCreate_WriteGate(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "valkey@7", provider.FamilyKV, false)
	defer ts.Close()
	body := `{"path":{"service":"svc","segments":["h"]},"type":"hash","field":"a","value":"MQ=="}`
	if r := do(t, ts, "POST", "/api/kv/create", tok, body, true); r.code != http.StatusForbidden {
		t.Fatalf("read-only kv create = %d, want 403", r.code)
	}
	if rec.lastOp != "" {
		t.Fatalf("reached provider before write gate: lastOp=%q", rec.lastOp)
	}
}

// TestServer_DocCreate_ReachesProvider_EchoesID pins that a document create
// reaches CreateDoc and echoes the stored id.
func TestServer_DocCreate_ReachesProvider_EchoesID(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "elasticsearch", provider.FamilyDocument, true)
	defer ts.Close()
	body := `{"path":{"service":"svc","segments":["idx"]},"data":"e30="}` // data = {}
	req := newRequest(t, http.MethodPost, "/api/document/create", strings.NewReader(body))
	resp := doReq(t, ts, req, tok, true)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("document create = %d, want 200", resp.StatusCode)
	}
	var out struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.OK || out.ID != "assigned-1" {
		t.Fatalf("document create body = %+v, want ok+id=assigned-1", out)
	}
	if rec.lastOp != "createdoc" {
		t.Fatalf("CreateDoc not reached: lastOp=%q", rec.lastOp)
	}
}

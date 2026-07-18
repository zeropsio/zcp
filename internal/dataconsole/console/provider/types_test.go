package provider

import (
	"encoding/json"
	"testing"
	"time"
)

// TestNodeMeta_JSONShape pins the exact wire shape of the typed, discriminated
// NodeMeta (DD-4) — the presentation canon's single source for tree-node
// metadata. S15 (the SPA that renders it) depends on this exact field set and
// these exact JSON keys.
func TestNodeMeta_JSONShape(t *testing.T) {
	t.Parallel()
	size := int64(42)
	count := int64(7)
	ttl := int64(120)
	modified := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	meta := NodeMeta{
		Size:        &size,
		Modified:    &modified,
		ContentType: "application/json",
		ETag:        "abc123",
		EntryType:   "hash",
		Count:       &count,
		TTLSeconds:  &ttl,
	}
	got, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"size":42,"modified":"2026-07-16T12:00:00Z","contentType":"application/json","etag":"abc123","entryType":"hash","count":7,"ttlSeconds":120}`
	if string(got) != want {
		t.Fatalf("NodeMeta JSON =\n%s\nwant\n%s", got, want)
	}
}

// TestNodeMeta_OmitsAbsentFields pins the other half of "only populate what a
// provider actually knows": every unset field must vanish from the wire, not
// serialize as a zero value the SPA could mistake for a real fact (a bare
// "ttlSeconds":0 reads as "expires in 0s", not "unknown" — the exact KV-AUD-02
// class of bug this typed struct exists to prevent from recurring elsewhere).
func TestNodeMeta_OmitsAbsentFields(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(NodeMeta{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != "{}" {
		t.Fatalf("empty NodeMeta JSON = %s, want {}", got)
	}
}

// TestNode_MetaOmittedWhenNil pins that a Node with no known metadata at all
// (Meta left nil) drops the "meta" key entirely rather than serializing
// "meta":{} or "meta":null — the SPA can treat key-absence as the single
// "nothing known" signal.
func TestNode_MetaOmittedWhenNil(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(Node{Name: "n", Kind: KindBlob, Path: Path{Service: "s"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"name":"n","kind":"blob","path":{"service":"s","segments":null},"hasChildren":false}`
	if string(got) != want {
		t.Fatalf("Node JSON =\n%s\nwant\n%s", got, want)
	}
}

// TestColumn_JSONShape pins the per-column editability + reason wire shape
// (DD-4): Editable/Reason ride alongside Name/DataType/PK so the SPA renders
// edit affordances from server truth, never client guessing.
func TestColumn_JSONShape(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(Column{Name: "id", DataType: "integer", PK: true, Editable: false, Reason: "primary key"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"name":"id","dataType":"integer","pk":true,"editable":false,"reason":"primary key"}`
	if string(got) != want {
		t.Fatalf("Column JSON =\n%s\nwant\n%s", got, want)
	}
}

// TestColumn_ReasonOmittedWhenEditable pins that an editable column's Reason
// (always "" in that case) drops off the wire rather than serializing an
// empty string key — Reason only ever carries information when Editable is
// false ("why not"), so its absence IS the "no reason needed" signal.
func TestColumn_ReasonOmittedWhenEditable(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(Column{Name: "name", DataType: "text", Editable: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"name":"name","dataType":"text","pk":false,"editable":true}`
	if string(got) != want {
		t.Fatalf("Column JSON =\n%s\nwant\n%s", got, want)
	}
}

// TestColumnEditability pins the shared PK-based editability rule every
// family's Column-producing code applies (resolution 6): a primary-key / row-
// identity column is never editable in v1 — editing it is a delete+recreate
// relational operation, out of scope — so this is the ONE place that decision
// lives, not reinvented per family.
func TestColumnEditability(t *testing.T) {
	t.Parallel()
	if editable, reason := ColumnEditability(true); editable || reason != "primary key" {
		t.Fatalf("ColumnEditability(pk=true) = (%v,%q), want (false,\"primary key\")", editable, reason)
	}
	if editable, reason := ColumnEditability(false); !editable || reason != "" {
		t.Fatalf("ColumnEditability(pk=false) = (%v,%q), want (true,\"\")", editable, reason)
	}
}

// TestBlobMeta_JSONShape pins BlobMeta's wire shape including the new Vector
// signal (UI-AUD-03): a qdrant point's raw JSON embeds a full embedding
// array, and Vector is the minimal server-side flag the SPA (S15) keys on to
// collapse it rather than rendering a wall of floats inline.
func TestBlobMeta_JSONShape(t *testing.T) {
	t.Parallel()
	ttl := int64(60)
	got, err := json.Marshal(BlobMeta{ContentType: "application/json", Size: 10, TTLSeconds: &ttl, Truncated: true, Vector: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"contentType":"application/json","size":10,"ttlSeconds":60,"truncated":true,"vector":true}`
	if string(got) != want {
		t.Fatalf("BlobMeta JSON =\n%s\nwant\n%s", got, want)
	}
}

// TestBlobMeta_VectorOmittedWhenFalse pins that the common case (not a
// vector) drops the key entirely, consistent with every other BlobMeta flag's
// omitempty discipline.
func TestBlobMeta_VectorOmittedWhenFalse(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(BlobMeta{ContentType: "text/plain", Size: 5})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"contentType":"text/plain","size":5,"truncated":false}`
	if string(got) != want {
		t.Fatalf("BlobMeta JSON =\n%s\nwant\n%s", got, want)
	}
}

package kv

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

func createPath(name string) provider.Path {
	return provider.Path{Service: "cache", Segments: []string{name}}
}

// TestCreateKey_NewHash_Exists pins KV-AUD-03's core: a brand-new hash can be
// created in one op and reads back through the provider's own Stat/ReadTable.
func TestCreateKey_NewHash_Exists(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, false)
	applied, err := p.CreateKey(context.Background(), provider.KVCreate{
		Path: createPath("h"), Type: "hash", Field: "a", Value: []byte("1"),
	})
	if err != nil {
		t.Fatalf("CreateKey(hash) = %v, want nil", err)
	}
	if applied.Statement != "HSET" {
		t.Errorf("Applied.Statement = %q, want HSET", applied.Statement)
	}
	node, err := p.Stat(context.Background(), createPath("h"))
	if err != nil {
		t.Fatalf("Stat after create: %v", err)
	}
	if node.Meta == nil || node.Meta.EntryType != "hash" {
		t.Fatalf("created key type = %+v, want hash", node.Meta)
	}
	tp, err := p.ReadTable(context.Background(), createPath("h"), provider.Page{})
	if err != nil {
		t.Fatalf("ReadTable after create: %v", err)
	}
	if !hasRow(tp.Rows, "a", "1") {
		t.Fatalf("hash rows = %v, want field a=1", tp.Rows)
	}
}

// TestCreateKey_CollisionRefused_OriginalUntouched pins the resolution-5
// contract: a create against an existing name is refused (ErrConflict) and the
// original value is never overwritten (preserves the KV-AUD-01 clobber guard).
func TestCreateKey_CollisionRefused_OriginalUntouched(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, false)
	if _, err := p.CreateKey(context.Background(), provider.KVCreate{
		Path: createPath("h"), Type: "hash", Field: "a", Value: []byte("1"),
	}); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	// A colliding create with a DIFFERENT type (string) must be refused and must
	// not clobber the existing hash.
	if _, cerr := p.CreateKey(context.Background(), provider.KVCreate{
		Path: createPath("h"), Type: "string", Value: []byte("clobber"),
	}); !errors.Is(cerr, provider.ErrConflict) {
		t.Fatalf("colliding create = %v, want ErrConflict", cerr)
	}
	node, serr := p.Stat(context.Background(), createPath("h"))
	if serr != nil {
		t.Fatalf("Stat after collision: %v", serr)
	}
	if node.Meta == nil || node.Meta.EntryType != "hash" {
		t.Fatalf("after collision key type = %+v, want still hash (untouched)", node.Meta)
	}
	tp, terr := p.ReadTable(context.Background(), createPath("h"), provider.Page{})
	if terr != nil {
		t.Fatalf("ReadTable after collision: %v", terr)
	}
	if !hasRow(tp.Rows, "a", "1") {
		t.Fatalf("after collision hash rows = %v, want original field a=1 intact", tp.Rows)
	}
}

// TestCreateKey_AllTypes pins that every redis collection type + string is
// creatable through the one op.
func TestCreateKey_AllTypes(t *testing.T) {
	t.Parallel()
	score := 3.5
	cases := []struct {
		name     string
		req      provider.KVCreate
		wantType string
	}{
		{"string", provider.KVCreate{Path: createPath("s"), Type: "string", Value: []byte("v")}, "string"},
		{"list", provider.KVCreate{Path: createPath("l"), Type: "list", Value: []byte("e0")}, "list"},
		{"set", provider.KVCreate{Path: createPath("se"), Type: "set", Value: []byte("m0")}, "set"},
		{"zset", provider.KVCreate{Path: createPath("z"), Type: "zset", Field: "m0", Score: &score}, "zset"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p, _ := newTestProvider(t, false)
			if _, err := p.CreateKey(context.Background(), c.req); err != nil {
				t.Fatalf("CreateKey(%s) = %v", c.name, err)
			}
			node, err := p.Stat(context.Background(), c.req.Path)
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if node.Meta == nil || node.Meta.EntryType != c.wantType {
				t.Fatalf("created %s type = %+v, want %s", c.name, node.Meta, c.wantType)
			}
		})
	}
}

// TestCreateKey_MissingEntry_Rejected pins that a collection type that redis
// cannot represent empty is refused when its required first entry is absent.
func TestCreateKey_MissingEntry_Rejected(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, false)
	if _, err := p.CreateKey(context.Background(), provider.KVCreate{
		Path: createPath("h"), Type: "hash",
	}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("hash create without field = %v, want ErrInvalid", err)
	}
	if _, err := p.CreateKey(context.Background(), provider.KVCreate{
		Path: createPath("z"), Type: "zset", Field: "m",
	}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("zset create without score = %v, want ErrInvalid", err)
	}
}

// TestCreateKey_UnknownType_Rejected and empty-key guard.
func TestCreateKey_UnknownType_Rejected(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, false)
	if _, err := p.CreateKey(context.Background(), provider.KVCreate{
		Path: createPath("x"), Type: "stream", Value: []byte("v"),
	}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("unknown type = %v, want ErrInvalid", err)
	}
	if _, err := p.CreateKey(context.Background(), provider.KVCreate{
		Path: provider.Path{Service: "cache"}, Type: "string",
	}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("empty key = %v, want ErrInvalid", err)
	}
}

// TestCreateKey_ReadOnlyGate proves the write gate rejects before any command.
func TestCreateKey_ReadOnlyGate(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, true) // read-only
	if _, err := p.CreateKey(context.Background(), provider.KVCreate{
		Path: createPath("h"), Type: "hash", Field: "a", Value: []byte("1"),
	}); !errors.Is(err, provider.ErrReadOnly) {
		t.Fatalf("read-only create = %v, want ErrReadOnly", err)
	}
}

func hasRow(rows [][]any, col0, col1 string) bool {
	for _, r := range rows {
		if len(r) >= 2 && toStr(r[0]) == col0 && toStr(r[1]) == col1 {
			return true
		}
	}
	return false
}

func toStr(v any) string {
	s, _ := v.(string)
	return s
}

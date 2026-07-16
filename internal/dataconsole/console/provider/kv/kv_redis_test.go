package kv

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// newTestProvider spins a hermetic in-memory Valkey (miniredis) and returns a
// writable provider pointed at it. Encodes live redis SCAN/HSET/etc. semantics
// without a network service.
func newTestProvider(t *testing.T, readOnly bool) (*Provider, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	p, err := New(Config{Addr: mr.Addr(), ReadOnly: readOnly})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, mr
}

// TestList_NoKeyLoss_AcrossScanBatches pins the SCAN key-loss bug: a single SCAN
// batch (scanCount=400) larger than the UI limit must not be truncated with the
// batch's next cursor advertised — paging would skip the un-rendered keys. The
// fix processes whole batches and only advances the returned cursor past keys it
// actually returned, so paging the whole keyspace returns every key exactly once.
func TestList_NoKeyLoss_AcrossScanBatches(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, true)
	const total = 1000
	want := map[string]bool{}
	for i := range total {
		k := fmt.Sprintf("flat-%04d", i) // flat keys (no ':') -> all leaves at root
		_ = mr.Set(k, "v")
		want[k] = true
	}
	got := map[string]bool{}
	cursor := ""
	for range 10000 {
		nodes, next, err := p.List(context.Background(), provider.Path{}, provider.Page{Limit: 100, Cursor: cursor})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, n := range nodes {
			if got[n.Name] {
				t.Fatalf("duplicate key returned: %q", n.Name)
			}
			got[n.Name] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(got) != total {
		t.Fatalf("key loss: returned %d distinct keys, want %d", len(got), total)
	}
}

func TestList_HonorsCursorPagination(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, true)
	want := numbered("flat")
	for _, k := range want {
		_ = mr.Set(k, "v")
	}

	nodes, _, err := p.List(context.Background(), provider.Path{}, provider.Page{})
	if err != nil {
		t.Fatalf("List default page: %v", err)
	}
	if len(nodes) > defaultLimit {
		t.Fatalf("default List returned %d nodes, want at most %d", len(nodes), defaultLimit)
	}

	walkKVPage(t, want, 37, func(cursor string, limit int) ([]string, string, error) {
		nodes, next, err := p.List(context.Background(), provider.Path{}, provider.Page{Cursor: cursor, Limit: limit})
		if err != nil {
			return nil, "", err
		}
		names := make([]string, len(nodes))
		for i, n := range nodes {
			names[i] = n.Name
		}
		return names, next, nil
	})
}

func TestReadTable_HonorsCursorPagination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		key  string
		seed func(*miniredis.Miniredis, []string)
		want []string
	}{
		{
			name: "hash",
			key:  "h",
			want: numbered("field"),
			seed: func(mr *miniredis.Miniredis, fields []string) {
				for _, f := range fields {
					mr.HSet("h", f, "v-"+f)
				}
			},
		},
		{
			name: "list",
			key:  "l",
			want: numericStrings(defaultLimit + 5),
			seed: func(mr *miniredis.Miniredis, vals []string) {
				_, _ = mr.Push("l", vals...)
			},
		},
		{
			name: "set",
			key:  "s",
			want: numbered("member"),
			seed: func(mr *miniredis.Miniredis, members []string) {
				_, _ = mr.SetAdd("s", members...)
			},
		},
		{
			name: "zset",
			key:  "z",
			want: numbered("member"),
			seed: func(mr *miniredis.Miniredis, members []string) {
				for i, m := range members {
					_, _ = mr.ZAdd("z", float64(i), m)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, mr := newTestProvider(t, true)
			tc.seed(mr, tc.want)
			path := provider.Path{Segments: []string{tc.key}}

			first, err := p.ReadTable(context.Background(), path, provider.Page{})
			if err != nil {
				t.Fatalf("ReadTable default page: %v", err)
			}
			if len(first.Rows) > defaultLimit {
				t.Fatalf("default ReadTable returned %d rows, want at most %d", len(first.Rows), defaultLimit)
			}

			walkKVPage(t, tc.want, 41, func(cursor string, limit int) ([]string, string, error) {
				tp, err := p.ReadTable(context.Background(), path, provider.Page{Cursor: cursor, Limit: limit})
				if err != nil {
					return nil, "", err
				}
				return rowKeys(tp.Rows), tp.NextCursor, nil
			})
		})
	}
}

func walkKVPage(t *testing.T, want []string, limit int, fetch func(cursor string, limit int) ([]string, string, error)) {
	t.Helper()
	wantSet := map[string]bool{}
	for _, item := range want {
		wantSet[item] = true
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 1; page <= len(want)+2; page++ {
		items, next, err := fetch(cursor, limit)
		if err != nil {
			t.Fatalf("fetch page %d: %v", page, err)
		}
		if len(items) > limit {
			t.Fatalf("page %d returned %d items, want at most limit %d", page, len(items), limit)
		}
		for _, item := range items {
			if !wantSet[item] {
				t.Fatalf("page %d returned unexpected item %q", page, item)
			}
			if seen[item] {
				t.Fatalf("duplicate item across pages: %q", item)
			}
			seen[item] = true
		}
		if len(seen) < len(want) && next == "" {
			t.Fatalf("page %d exhausted cursor after %d/%d items", page, len(seen), len(want))
		}
		if len(seen) == len(want) && next != "" {
			t.Fatalf("page %d returned cursor %q after the final item", page, next)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != len(want) {
		t.Fatalf("pagination returned %d distinct items, want %d", len(seen), len(want))
	}
}

func rowKeys(rows [][]any) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = fmt.Sprint(row[0])
	}
	return out
}

func numbered(prefix string) []string {
	out := make([]string, defaultLimit+5)
	for i := range out {
		out[i] = fmt.Sprintf("%s-%03d", prefix, i)
	}
	return out
}

func numericStrings(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strconv.Itoa(i)
	}
	return out
}

func TestSetEntry_HashField(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	mr.HSet("h", "a", "1")
	if _, err := p.SetEntry(context.Background(), provider.KVEntryEdit{
		Path: provider.Path{Segments: []string{"h"}}, Field: "a", Value: []byte("2"),
	}); err != nil {
		t.Fatalf("SetEntry hash: %v", err)
	}
	if v := mr.HGet("h", "a"); v != "2" {
		t.Fatalf("hash field = %q, want 2", v)
	}
}

func TestDeleteEntry_HashField(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	mr.HSet("h", "a", "1")
	mr.HSet("h", "b", "2")
	if _, err := p.DeleteEntry(context.Background(), provider.Path{Segments: []string{"h"}}, "a"); err != nil {
		t.Fatalf("DeleteEntry hash: %v", err)
	}
	if mr.HGet("h", "a") != "" {
		t.Fatalf("hash field a not deleted")
	}
}

func TestSetEntry_ZsetMemberScore(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	_, _ = mr.ZAdd("z", 1, "m")
	score := 5.0
	if _, err := p.SetEntry(context.Background(), provider.KVEntryEdit{
		Path: provider.Path{Segments: []string{"z"}}, Field: "m", Score: &score,
	}); err != nil {
		t.Fatalf("SetEntry zset: %v", err)
	}
	if got, _ := mr.ZScore("z", "m"); got != 5.0 {
		t.Fatalf("zset score = %v, want 5", got)
	}
}

func TestSetEntry_SetMemberRename(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	_, _ = mr.SetAdd("s", "old")
	if _, err := p.SetEntry(context.Background(), provider.KVEntryEdit{
		Path: provider.Path{Segments: []string{"s"}}, Field: "old", Value: []byte("new"),
	}); err != nil {
		t.Fatalf("SetEntry set: %v", err)
	}
	members, _ := mr.SMembers("s")
	if slices.Contains(members, "old") || !slices.Contains(members, "new") {
		t.Fatalf("set members = %v, want [new] (old renamed)", members)
	}
}

func TestSetEntry_ListIndex(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	_, _ = mr.Push("l", "a", "b", "c")
	if _, err := p.SetEntry(context.Background(), provider.KVEntryEdit{
		Path: provider.Path{Segments: []string{"l"}}, Field: "1", Value: []byte("B"),
	}); err != nil {
		t.Fatalf("SetEntry list: %v", err)
	}
	vals, _ := mr.List("l")
	if len(vals) != 3 || vals[1] != "B" {
		t.Fatalf("list = %v, want [a B c]", vals)
	}
}

func TestSetEntry_ReadOnly_Rejected(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, true)
	mr.HSet("h", "a", "1")
	if _, err := p.SetEntry(context.Background(), provider.KVEntryEdit{
		Path: provider.Path{Segments: []string{"h"}}, Field: "a", Value: []byte("2"),
	}); err != provider.ErrReadOnly {
		t.Fatalf("read-only SetEntry = %v, want ErrReadOnly", err)
	}
	if _, err := p.DeleteEntry(context.Background(), provider.Path{Segments: []string{"h"}}, "a"); err != provider.ErrReadOnly {
		t.Fatalf("read-only DeleteEntry = %v, want ErrReadOnly", err)
	}
}

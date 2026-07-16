package kv

import (
	"context"
	"errors"
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

// TestWriteBlob_RefusesCrossTypeClobber pins KV-AUD-01 (CRITICAL, live-audit
// confirmed): WriteBlob (the string-value write path, PUT /api/blob) used to
// issue an unconditional SET with no type check, so writing a string over an
// existing collection key silently destroyed it — reproduced live against a
// zset named "leaderboard" (plans/dataconsole-audit/kv.md KV-AUD-01). The fix
// must refuse before the SET reaches Redis, and the zset must survive intact.
func TestWriteBlob_RefusesCrossTypeClobber(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	_, _ = mr.ZAdd("leaderboard", 100, "alice")
	_, _ = mr.ZAdd("leaderboard", 250, "bob")
	_, _ = mr.ZAdd("leaderboard", 175, "carol")

	err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"leaderboard"}}, []byte("oops"))
	if !errors.Is(err, provider.ErrWrongType) {
		t.Fatalf("WriteBlob over zset = %v, want ErrWrongType", err)
	}

	// The collection must be untouched — this is the load-bearing assertion:
	// the audit's actual failure mode was silent data loss, not just a wrong
	// error code.
	if typ := mr.Type("leaderboard"); typ != "zset" {
		t.Fatalf("leaderboard type = %q after refused WriteBlob, want zset (untouched)", typ)
	}
	for member, want := range map[string]float64{"alice": 100, "bob": 250, "carol": 175} {
		if got, err := mr.ZScore("leaderboard", member); err != nil || got != want {
			t.Fatalf("leaderboard[%s] = %v, %v; want %v, nil (untouched)", member, got, err, want)
		}
	}
}

// TestWriteBlob_ProceedsOnNonexistentOrStringKey pins the two cases that MUST
// keep working after the KV-AUD-01 fix: a brand-new key (Redis TYPE "none")
// and an existing plain string both proceed with the ordinary SET — only an
// existing collection is refused.
func TestWriteBlob_ProceedsOnNonexistentOrStringKey(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	_ = mr.Set("existing-string", "old")

	if err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"brand-new-key"}}, []byte("v1")); err != nil {
		t.Fatalf("WriteBlob(nonexistent key) = %v, want nil", err)
	}
	if got, gerr := mr.Get("existing-string"); gerr != nil || got != "old" {
		t.Fatalf("sanity: existing-string = %q, %v before overwrite, want old, nil", got, gerr)
	}
	if err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"existing-string"}}, []byte("new")); err != nil {
		t.Fatalf("WriteBlob(existing string key) = %v, want nil", err)
	}
	if got, gerr := mr.Get("existing-string"); gerr != nil || got != "new" {
		t.Fatalf("existing-string = %q, %v after WriteBlob, want new, nil", got, gerr)
	}
}

// TestSetEntry_Zset_MissingScore_Refused pins KV-AUD-10 (HIGH, same class as
// KV-AUD-01): SetEntry against a zset with a value but no score used to
// silently ZADD the member at score 0, discarding the member's real score
// with no error and no signal (plans/dataconsole-audit/kv.md KV-AUD-10). The
// fix refuses instead of defaulting; the member's existing score must be
// unchanged. SetEntry with an explicit score (the SPA's only shape, fixed by
// D-01) keeps working — see TestSetEntry_ZsetMemberScore.
func TestSetEntry_Zset_MissingScore_Refused(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	_, _ = mr.ZAdd("z", 42, "m")

	_, err := p.SetEntry(context.Background(), provider.KVEntryEdit{
		Path: provider.Path{Segments: []string{"z"}}, Field: "m", Value: []byte("ignored-text"),
		// Score deliberately omitted (nil).
	})
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("SetEntry zset without score = %v, want ErrInvalid", err)
	}
	if got, zerr := mr.ZScore("z", "m"); zerr != nil || got != 42 {
		t.Fatalf("z[m] score = %v, %v after refused SetEntry, want 42, nil (unchanged)", got, zerr)
	}
}

// TestDeleteEntry_Affected_ReflectsRealCount pins KV-AUD-06 (LOW, live-audit
// confirmed): DeleteEntry used to hardcode Applied{Affected:1} regardless of
// whether Redis actually removed anything — live-confirmed on a hash field
// that didn't exist, which still reported affected:1
// (plans/dataconsole-audit/kv.md KV-AUD-06). The fix must return the real
// HDEL/SREM/ZREM reply count: 1 when the entry existed and was removed, 0
// when it did not.
func TestDeleteEntry_Affected_ReflectsRealCount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		key    string
		seed   func(*miniredis.Miniredis)
		field  string
		want   int64
		verify func(*testing.T, *miniredis.Miniredis)
	}{
		{
			name: "hash field exists", key: "h",
			seed:  func(mr *miniredis.Miniredis) { mr.HSet("h", "a", "1"); mr.HSet("h", "b", "2") },
			field: "a", want: 1,
			verify: func(t *testing.T, mr *miniredis.Miniredis) {
				t.Helper()
				if mr.HGet("h", "a") != "" {
					t.Fatalf("hash field a still present after delete")
				}
			},
		},
		{
			name: "hash field missing", key: "h",
			seed:  func(mr *miniredis.Miniredis) { mr.HSet("h", "b", "2") },
			field: "a", want: 0,
			verify: func(t *testing.T, mr *miniredis.Miniredis) {
				t.Helper()
				if mr.HGet("h", "b") != "2" {
					t.Fatalf("unrelated hash field b was disturbed by a no-op delete")
				}
			},
		},
		{
			name: "set member exists", key: "s",
			seed:  func(mr *miniredis.Miniredis) { _, _ = mr.SetAdd("s", "x", "y") },
			field: "x", want: 1,
		},
		{
			name: "set member missing", key: "s",
			seed:  func(mr *miniredis.Miniredis) { _, _ = mr.SetAdd("s", "y") },
			field: "x", want: 0,
		},
		{
			name: "zset member exists", key: "z",
			seed:  func(mr *miniredis.Miniredis) { _, _ = mr.ZAdd("z", 1, "m") },
			field: "m", want: 1,
		},
		{
			name: "zset member missing", key: "z",
			seed:  func(mr *miniredis.Miniredis) { _, _ = mr.ZAdd("z", 1, "other") },
			field: "m", want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, mr := newTestProvider(t, false)
			tc.seed(mr)
			applied, err := p.DeleteEntry(context.Background(), provider.Path{Segments: []string{tc.key}}, tc.field)
			if err != nil {
				t.Fatalf("DeleteEntry: %v", err)
			}
			if applied.Affected != tc.want {
				t.Fatalf("Affected = %d, want %d", applied.Affected, tc.want)
			}
			if tc.verify != nil {
				tc.verify(t, mr)
			}
		})
	}
}

// TestSetEntry_SetMemberRename_Affected_ReflectsSourceExistence extends the
// KV-AUD-06 honesty fix to the set "rename" path (SREM+SADD): renaming a
// member that no longer exists still creates the new member (SADD has no
// existence precondition) but must report Affected:0, not the previously
// hardcoded 1 — the old member was never actually there to rename.
func TestSetEntry_SetMemberRename_Affected_ReflectsSourceExistence(t *testing.T) {
	t.Parallel()
	p, mr := newTestProvider(t, false)
	_, _ = mr.SetAdd("s", "old")

	applied, err := p.SetEntry(context.Background(), provider.KVEntryEdit{
		Path: provider.Path{Segments: []string{"s"}}, Field: "old", Value: []byte("new"),
	})
	if err != nil {
		t.Fatalf("SetEntry rename existing: %v", err)
	}
	if applied.Affected != 1 {
		t.Fatalf("Affected = %d, want 1 (source existed)", applied.Affected)
	}

	applied, err = p.SetEntry(context.Background(), provider.KVEntryEdit{
		Path: provider.Path{Segments: []string{"s"}}, Field: "never-existed", Value: []byte("fresh"),
	})
	if err != nil {
		t.Fatalf("SetEntry rename missing source: %v", err)
	}
	if applied.Affected != 0 {
		t.Fatalf("Affected = %d, want 0 (source never existed)", applied.Affected)
	}
	members, _ := mr.SMembers("s")
	if !slices.Contains(members, "fresh") {
		t.Fatalf("set members = %v, want fresh present (SADD still applies)", members)
	}
}

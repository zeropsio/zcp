package kv

import (
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

func TestKeyOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		segs []string
		want string
	}{
		{nil, ""},
		{[]string{"user"}, "user"},
		{[]string{"user", "1"}, "user:1"},
		{[]string{"user", "1", "name"}, "user:1:name"},
	}
	for _, c := range cases {
		if got := keyOf(provider.Path{Segments: c.segs}); got != c.want {
			t.Errorf("keyOf(%v) = %q, want %q", c.segs, got, c.want)
		}
	}
}

func TestChild(t *testing.T) {
	t.Parallel()
	p := provider.Path{Service: "cache", Segments: []string{"user"}}
	c := child(p, "1")
	if c.Service != "cache" || len(c.Segments) != 2 || c.Segments[1] != "1" {
		t.Fatalf("child = %+v", c)
	}
	// must not alias the parent slice
	if len(p.Segments) != 1 {
		t.Fatalf("parent mutated: %+v", p)
	}
}

func TestCursorStr(t *testing.T) {
	t.Parallel()
	if cursorStr(0) != "" {
		t.Error("cursor 0 means done -> empty")
	}
	if cursorStr(42) != "42" {
		t.Error("cursor 42")
	}
}

func TestNew_RejectsEmptyAddr(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Error("expected error on empty addr")
	}
}

// ---- cols1/cols2: per-column editability (DD-4) ----
//
// The address column of a hash/list/zset grid (field/index/member — cols2's
// first column, PK:true) is locked via the shared ColumnEditability rule: it
// is the entry's identity, not its payload, so an "edit" there is a
// SetEntry-address change, never a value write (D-02: hash field locked; the
// same address/payload split makes list's index locked too — D-03's "no
// editable key column"). A redis SET's sole column (cols1) is the opposite
// case: the member itself IS what SetEntry rewrites (SREM+SADD rename), so it
// stays editable, PK:false.

func TestCols1_MemberIsEditable(t *testing.T) {
	t.Parallel()
	cols := cols1("member")
	if len(cols) != 1 {
		t.Fatalf("cols1 = %+v, want 1 column", cols)
	}
	c := cols[0]
	if c.PK {
		t.Errorf("member PK = true, want false (set member is itself the editable rename target)")
	}
	if !c.Editable || c.Reason != "" {
		t.Errorf("member Editable/Reason = %v/%q, want true/\"\"", c.Editable, c.Reason)
	}
}

func TestCols2_FirstColumnLockedSecondEditable(t *testing.T) {
	t.Parallel()
	cols := cols2("field", "value")
	if len(cols) != 2 {
		t.Fatalf("cols2 = %+v, want 2 columns", cols)
	}
	key, val := cols[0], cols[1]
	if !key.PK {
		t.Errorf("key column PK = false, want true")
	}
	if key.Editable || key.Reason != "primary key" {
		t.Errorf("key column Editable/Reason = %v/%q, want false/\"primary key\"", key.Editable, key.Reason)
	}
	if val.PK {
		t.Errorf("value column PK = true, want false")
	}
	if !val.Editable || val.Reason != "" {
		t.Errorf("value column Editable/Reason = %v/%q, want true/\"\"", val.Editable, val.Reason)
	}
}

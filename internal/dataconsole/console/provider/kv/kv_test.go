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

package main

import "testing"

func TestServeMode_ManagedInvocationRequiresExplicitPrivateStdioMode(t *testing.T) {
	for _, tt := range []struct {
		name          string
		args          []string
		managed, fail bool
	}{
		{name: "ordinary"}, {name: "explicit ordinary", args: []string{"serve"}},
		{name: "owned", args: []string{"serve", "--mate-invocation"}, managed: true},
		{name: "unknown", args: []string{"serve", "--other"}, fail: true},
		{name: "mixed", args: []string{"serve", "--mate-invocation", "--http"}, fail: true},
		{name: "value", args: []string{"serve", "--mate-invocation=false"}, fail: true},
		{name: "duplicated", args: []string{"serve", "--mate-invocation", "--mate-invocation"}, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := serveMode(tt.args)
			if got != tt.managed || (err != nil) != tt.fail {
				t.Fatalf("got(%v,%v),want(%v,fail%v)", got, err, tt.managed, tt.fail)
			}
		})
	}
}

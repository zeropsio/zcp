package topology

import (
	"strings"
	"testing"
)

func TestInvocation_ExactOriginIdentity_Validated(t *testing.T) {
	t.Parallel()
	const task = "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name       string
		invocation Invocation
		valid      bool
	}{
		{"ordinary manual", Invocation{Scope: InvocationScope{Kind: "manual"}}, true},
		{"bound manual", Invocation{ID: strings.Repeat("a", 64), Scope: InvocationScope{Kind: "manual"}}, true},
		{"exact task", Invocation{ID: strings.Repeat("a", 64), Scope: InvocationScope{Kind: "task", TaskID: task, Assignment: "9007199254740993"}}, true},
		{"max task", Invocation{ID: strings.Repeat("a", 64), Scope: InvocationScope{Kind: "task", TaskID: task, Assignment: "9223372036854775807"}}, true},
		{"unknown", Invocation{}, false},
		{"task without original id", Invocation{Scope: InvocationScope{Kind: "task", TaskID: task, Assignment: "1"}}, false},
		{"noncanonical grant", Invocation{ID: strings.Repeat("a", 64), Scope: InvocationScope{Kind: "task", TaskID: task, Assignment: "01"}}, false},
		{"rounded numeric overflow", Invocation{ID: strings.Repeat("a", 64), Scope: InvocationScope{Kind: "task", TaskID: task, Assignment: "9223372036854775808"}}, false},
		{"manual carrying task", Invocation{Scope: InvocationScope{Kind: "manual", TaskID: task, Assignment: "1"}}, false},
		{"binding id is not canonical", Invocation{ID: strings.Repeat("A", 64), Scope: InvocationScope{Kind: "manual"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.invocation.Validate() == nil; got != tc.valid {
				t.Fatalf("valid=%v want %v", got, tc.valid)
			}
		})
	}
}

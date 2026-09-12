package console

import "testing"

func TestSafeFragment_DistinguishesNormalizedCollisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		identity string
		want     string
	}{
		{identity: "", want: "x-"},
		{identity: "a-b_C9", want: "x-a-b_C9"},
		{identity: "a-b", want: "x-a-b"},
		{identity: "a/b", want: "x-a-b~YS9i"},
		{identity: "a.b", want: "x-a-b~YS5i"},
		{identity: "a//b", want: "x-a--b~YS8vYg"},
		{identity: "žluť", want: "x--lu-~xb5sdcWl"},
		// This unchanged identity collided with a/b under the former
		// truncated-hash scheme. The disjoint marker makes that impossible.
		{identity: "a-b-c14cddc033f6", want: "x-a-b-c14cddc033f6"},
	}

	seen := make(map[string]string, len(tests))
	for _, test := range tests {
		got := safeFragment("x-", test.identity)
		if got != test.want {
			t.Errorf("safeFragment(%q) = %q, want %q", test.identity, got, test.want)
		}
		if prior, exists := seen[got]; exists {
			t.Errorf("safeFragment aliases %q and %q as %q", prior, test.identity, got)
		}
		seen[got] = test.identity
	}
}

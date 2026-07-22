package topology

import "testing"

func TestIsClassifyInfrastructure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key  string
		want bool
	}{
		{"ZCP_API_KEY", true},
		{"ZCP_AGENT_TYPE", true},
		{"ZCP_AGENT_TYPES", true},
		{"ZCP_AGENTS", true},
		{"GIT_TOKEN", true},
		{"ZCP_LAUNCH_TOKEN", true},
		{"ZCP_CUSTOM_USER_THING", false}, // prefix-only ZCP_* stays out of the closed set
		{"APP_KEY", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsClassifyInfrastructure(c.key); got != c.want {
			t.Errorf("IsClassifyInfrastructure(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

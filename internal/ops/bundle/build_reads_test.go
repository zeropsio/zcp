package bundle

import (
	"slices"
	"testing"
)

// A setup's build reads what its build.envVariables name — directly, through
// a runtime variable the build lifts (${RUNTIME_X}), or through a project
// variable — and never what only its run reads.
func TestBuildReads_OnlyWhatTheBuildReads(t *testing.T) {
	t.Parallel()
	const body = `zerops:
  - setup: storedev
    build:
      envVariables:
        NODE_ENV: development
    run:
      envVariables:
        API_URL: ${apidev_zeropsSubdomain}
  - setup: storeprod
    build:
      envVariables:
        PUBLISHABLE_KEY: ${api_CHANNEL_KEY}
        BACKEND: ${RUNTIME_BACKEND_URL}
        SEARCH: ${SEARCH_URL}
    run:
      envVariables:
        BACKEND_URL: https://${apistage_zeropsSubdomain}
        ADMIN_URL: ${adminstage_zeropsSubdomain}
`
	hosts := []string{"apidev", "apistage", "api", "adminstage", "api-v2", "storedev", "storestage"}
	project := map[string]string{"SEARCH_URL": "${SEARCH_INTERNAL}", "SEARCH_INTERNAL": "http://${api_v2_hostname}:7700"}
	tests := []struct {
		name  string
		setup string
		want  []string
	}{
		{name: "a dev setup whose build names nothing reads nothing, whatever its run reads", setup: "storedev"},
		{name: "a stage setup: directly, through a lifted runtime variable and through project variables", setup: "storeprod",
			want: []string{"api", "api-v2", "apistage"}},
		{name: "every setup", setup: "", want: []string{"api", "api-v2", "apistage"}},
		{name: "a setup the file does not have", setup: "nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ReferencedHosts(BuildReadValues(body, tt.setup, nil), hosts, project)
			if !slices.Equal(got, tt.want) {
				t.Errorf("build reads = %v, want %v", got, tt.want)
			}
		})
	}
}

// A lifted runtime variable the setup's zerops.yaml does not set is the
// service's own variable.
func TestBuildReadValues_ALiftedVariableFallsBackToTheServices(t *testing.T) {
	t.Parallel()
	const body = "zerops:\n  - setup: web\n    build:\n      envVariables:\n        API: ${RUNTIME_API_URL}\n"
	got := ReferencedHosts(BuildReadValues(body, "web", map[string]string{"API_URL": "${apistage_zeropsSubdomain}"}), []string{"apistage"}, nil)
	if !slices.Equal(got, []string{"apistage"}) {
		t.Errorf("build reads = %v, want [apistage]", got)
	}
}

// A setup that extends another reads what the setup it extends builds with,
// its own variables winning a key both set.
func TestBuildReadValues_FollowsExtends(t *testing.T) {
	t.Parallel()
	const body = `zerops:
  - setup: base
    build:
      envVariables:
        API: https://${apistage_zeropsSubdomain}
        SEARCH: ${searchstage_hostname}
    run:
      envVariables:
        AUTH_URL: ${authstage_zeropsSubdomain}
  - setup: storeprod
    extends: base
    build:
      envVariables:
        SEARCH: none
        AUTH: ${RUNTIME_AUTH_URL}
  - setup: listed
    extends: [base]
`
	hosts := []string{"apistage", "searchstage", "authstage"}
	for _, tt := range []struct {
		setup string
		want  []string
	}{
		{"storeprod", []string{"apistage", "authstage"}},
		{"listed", []string{"apistage", "searchstage"}},
		{"base", []string{"apistage", "searchstage"}},
		// Every setup, as the recipe writer reads a pair: a lift through an
		// inherited run variable counts too.
		{"", []string{"apistage", "authstage", "searchstage"}},
	} {
		if got := ReferencedHosts(BuildReadValues(body, tt.setup, nil), hosts, nil); !slices.Equal(got, tt.want) {
			t.Errorf("%s reads %v, want %v", tt.setup, got, tt.want)
		}
	}
}

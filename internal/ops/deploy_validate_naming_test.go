package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A validate message names the repo's own config file; a zerops.yaml repo
// reads as before.
func TestValidateZeropsYml_NamesTheReposConfigFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"no_setup_entries", "zerops: []\n", " has no setup entries under 'zerops:' key"},
		{"no_entry_for_hostname", "zerops:\n  - setup: other\n    run:\n      start: x\n", "no setup entry for hostname \"appdev\" in %s"},
		{"reserved_env_key", "zerops:\n  - setup: appdev\n    run:\n      envVariables:\n        HOSTNAME: x\n", "reserved env-var key(s) in %s: run.envVariables.HOSTNAME"},
	}
	for _, file := range []string{"zerops.yaml", "zerops.yml"} {
		for _, tt := range tests {
			t.Run(tt.name+"/"+file, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, file), []byte(tt.body), 0o600); err != nil {
					t.Fatal(err)
				}
				warnings, err := ValidateZeropsYml(dir, "appdev", "nodejs@22", DeployClassCross)
				text := strings.Join(warnings, "\n")
				if err != nil {
					text += "\n" + err.Error()
				}
				want := tt.want
				if strings.Contains(want, "%s") {
					want = strings.Replace(want, "%s", file, 1)
				} else {
					want = file + want
				}
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in %q", want, text)
				}
				other := "zerops.yml"
				if file == other {
					other = "zerops.yaml"
				}
				if strings.Contains(text, other) {
					t.Errorf("message names %s for a %s repo: %q", other, file, text)
				}
			})
		}
	}
}

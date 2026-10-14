package bundle

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// HeldRuntimes reads what a tier file holds of the app's parts: each runtime
// built from a repository with a named setup, under the hostname the tier
// gives it. A file that does not parse holds nothing it can be read back by.
func HeldRuntimes(body string) []HeldRuntime {
	var doc struct {
		Services []struct {
			Hostname     string `yaml:"hostname"`
			BuildFromGit string `yaml:"buildFromGit"`
			ZeropsSetup  string `yaml:"zeropsSetup"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil
	}
	var held []HeldRuntime
	for _, svc := range doc.Services {
		host, repo, setup := strings.TrimSpace(svc.Hostname), strings.TrimSpace(svc.BuildFromGit), strings.TrimSpace(svc.ZeropsSetup)
		if host == "" || repo == "" || setup == "" {
			continue
		}
		held = append(held, HeldRuntime{Hostname: host, RepoURL: repo, SetupName: setup})
	}
	return held
}

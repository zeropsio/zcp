package content

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// legacyUnpinned lists fixture files whose buildFromGit URL(s) predate
// FM-62's pinning requirement (docs/spec-eval-farm.md §4.1 FM-62) — carried
// here so TestEvalScenarioFixtures_BuildFromGitPinned can pin the corpus's
// current state without failing the build on it today. This allowlist may
// only SHRINK: an entry is removed only once that fixture's buildFromGit
// URLs are pinned (a 7-40 hex sha or vX.Y.Z tag suffix, or a sibling `ref:`
// field in the same service block) — promoted as S5/S6 re-prepare each
// matrix cell (docs/spec-scenarios.md §9.3).
var legacyUnpinned = map[string]bool{
	"eval/behavioral/scenarios/fixtures/acceptance-node-postgres-record.yaml": true,
	"eval/behavioral/scenarios/fixtures/dev-only-node-postgres-deployed.yaml": true,
	"eval/behavioral/scenarios/fixtures/laravel-dev-deployed.yaml":            true,
	"eval/behavioral/scenarios/fixtures/laravel-showcase-deployed.yaml":       true,
	"eval/behavioral/scenarios/fixtures/nodejs-simple-deployed.yaml":          true,
	"eval/behavioral/scenarios/fixtures/nodejs-standard-deployed.yaml":        true,
	"eval/behavioral/scenarios/fixtures/python-simple-failed-no-db.yaml":      true,
	"eval/behavioral/scenarios/fixtures/nodejs-prod-failed-no-db.yaml":        true,
}

// fixtureService is the minimal shape TestEvalScenarioFixtures_BuildFromGitPinned
// needs from one entry of a fixture YAML's `services:` list — every other
// field is ignored (yaml.Unmarshal is lenient by default).
type fixtureService struct {
	Hostname     string `yaml:"hostname"`
	BuildFromGit string `yaml:"buildFromGit"`
	Ref          string `yaml:"ref"`
}

type fixtureDocument struct {
	Services []fixtureService `yaml:"services"`
}

// pinBranch matches the only ref shape the platform honours AND that does
// not move: a branch named `pin/<label>` on a repository this project
// controls. Live-verified 2026-09-13 (project eval-x): `@<sha>`, `@<short-sha>`
// and an unknown `@<ref>` are silently ignored — the platform clones the
// default branch and records publicGitSource.branchName = "main"; a sibling
// `ref:` key is ignored too, never rejected. So a sha suffix is a FALSE pin
// and is rejected here on purpose (docs/spec-eval-farm.md §4.2 FM-62).
var pinBranch = regexp.MustCompile(`^pin/[A-Za-z0-9._-]+$`)

// buildFromGitPinViolations returns one message per service entry in data
// (a fixture YAML's raw bytes) whose buildFromGit repository is not pinned
// (docs/spec-eval-farm.md §4.2 FM-62): the URL must carry a trailing
// `@pin/<label>` branch; a bare URL, a default branch (`@main`), a sha or tag
// suffix, or a sibling `ref:` key is a violation. A fixture with no
// `buildFromGit` at all yields no violations.
func buildFromGitPinViolations(data []byte) ([]string, error) {
	var doc fixtureDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse fixture yaml: %w", err)
	}
	var violations []string
	for _, svc := range doc.Services {
		if svc.BuildFromGit == "" {
			continue
		}
		if svc.Ref != "" {
			violations = append(violations, fmt.Sprintf("service %q: sibling ref: %q is ignored by the import API — pin with a trailing @pin/<label> branch instead", svc.Hostname, svc.Ref))
		}
		url, urlRef, hasURLRef := strings.Cut(svc.BuildFromGit, "@")
		switch {
		case !hasURLRef:
			violations = append(violations, fmt.Sprintf("service %q: buildFromGit %q has no pinned ref (want a trailing @pin/<label> branch)", svc.Hostname, url))
		case !pinBranch.MatchString(urlRef):
			violations = append(violations, fmt.Sprintf("service %q: buildFromGit %q suffix %q is not a pin/<label> branch (a sha, tag or default branch is not honoured as a pin by the platform)", svc.Hostname, svc.BuildFromGit, urlRef))
		}
	}
	return violations, nil
}

// fixtureDirs are the behavioral-scenario fixture directories, scanned the
// same way eval_scenario_manifest_test.go scans scenario files.
func fixtureDirs(repoRoot string) []string {
	return []string{
		filepath.Join(repoRoot, "eval", "behavioral", "scenarios", "fixtures"),
		filepath.Join(repoRoot, "eval", "behavioral", "scenarios-local", "fixtures"),
	}
}

// TestEvalScenarioFixtures_BuildFromGitPinned pins docs/spec-eval-farm.md
// §4.2 FM-62: a fixture that references a repository via buildFromGit
// carries a trailing `@pin/<label>` branch — never a bare URL, a default
// branch, a sha or tag suffix (not honoured by the platform), or a sibling
// `ref:` key (ignored by the platform). The current corpus is allowed
// through via the shrinking legacyUnpinned allowlist.
func TestEvalScenarioFixtures_BuildFromGitPinned(t *testing.T) {
	t.Parallel()
	t.Run("unit cases", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name      string
			content   string
			wantFail  bool
			wantMatch string
		}{
			{
				name:     "bare URL — unpinned",
				content:  "services:\n  - hostname: x\n    buildFromGit: https://github.com/x/y\n",
				wantFail: true,
			},
			{
				name:    "pin branch suffix — the only honoured pin",
				content: "services:\n  - hostname: x\n    buildFromGit: https://github.com/x/y@pin/2026-09-13\n",
			},
			{
				name:      "sha suffix — a false pin, platform builds the default branch",
				content:   "services:\n  - hostname: x\n    buildFromGit: https://github.com/x/y@3f2a9c1\n",
				wantFail:  true,
				wantMatch: "not a pin/<label> branch",
			},
			{
				name:     "tag suffix — unproven, rejected",
				content:  "services:\n  - hostname: x\n    buildFromGit: https://github.com/x/y@v1.2.3\n",
				wantFail: true,
			},
			{
				name:      "default branch suffix — rejected",
				content:   "services:\n  - hostname: x\n    buildFromGit: https://github.com/x/y@main\n",
				wantFail:  true,
				wantMatch: "not a pin/<label> branch",
			},
			{
				name:      "sibling ref key — ignored by the API, rejected",
				content:   "services:\n  - hostname: x\n    buildFromGit: https://github.com/x/y@pin/a\n    ref: 3f2a9c1\n",
				wantFail:  true,
				wantMatch: "sibling ref:",
			},
			{
				name:    "no buildFromGit at all — not applicable",
				content: "services:\n  - hostname: x\n    type: postgresql@18\n",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				violations, err := buildFromGitPinViolations([]byte(tc.content))
				if err != nil {
					t.Fatalf("buildFromGitPinViolations: %v", err)
				}
				if tc.wantFail && len(violations) == 0 {
					t.Fatalf("expected a pin violation, got none")
				}
				if !tc.wantFail && len(violations) != 0 {
					t.Fatalf("expected no violation, got %v", violations)
				}
				if tc.wantMatch != "" && (len(violations) == 0 || !strings.Contains(violations[0], tc.wantMatch)) {
					t.Errorf("violations = %v, want one containing %q", violations, tc.wantMatch)
				}
			})
		}
	})

	t.Run("corpus", func(t *testing.T) {
		t.Parallel()
		repoRoot := findRepoRoot(t)
		var checked int
		var failures []string
		for _, dir := range fixtureDirs(repoRoot) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue // scenarios-local/fixtures may not exist on every checkout
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
					continue
				}
				full := filepath.Join(dir, e.Name())
				rel, relErr := filepath.Rel(repoRoot, full)
				if relErr != nil {
					rel = full
				}
				rel = filepath.ToSlash(rel)
				checked++
				if legacyUnpinned[rel] {
					continue
				}
				data, readErr := os.ReadFile(full)
				if readErr != nil {
					failures = append(failures, fmt.Sprintf("%s: unreadable: %v", rel, readErr))
					continue
				}
				violations, err := buildFromGitPinViolations(data)
				if err != nil {
					failures = append(failures, fmt.Sprintf("%s: %v", rel, err))
					continue
				}
				for _, v := range violations {
					failures = append(failures, rel+": "+v)
				}
			}
		}
		if checked == 0 {
			t.Fatal("no fixture files found under eval/behavioral/**/fixtures — the corpus scan is broken")
		}
		if len(failures) > 0 {
			t.Errorf("fixture(s) reference an unpinned buildFromGit repository (docs/spec-eval-farm.md §4.1 FM-62):\n%s", strings.Join(failures, "\n"))
		}
	})
}

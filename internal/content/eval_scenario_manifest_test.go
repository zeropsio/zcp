package content

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// requiredScenarioManifestFields are the front-matter keys EVERY behavioral
// scenario must carry — and ONLY those the flow-eval harness (flow-eval.sh +
// `cmd/zcp eval behavioral {list,run,all}`, parsed by internal/eval/scenario.go)
// actually RELIES ON. The lint enforces the harness's own contract; it invents
// no field. Evidence per key:
//
//   - id            internal/eval/scenario.go::validate() errors when empty;
//     flow-eval.sh addresses a scenario as `<id>.md` (the filename
//     MUST equal id — also pinned below); list prints sc.ID.
//   - description   cmd/zcp/eval_behavioral.go::printScenarioListEntry prints it,
//     and the flow-eval run-by-descriptor match reads it.
//   - seed          validate() requires a valid enum (empty|imported|deployed|
//     settled) — it drives fixture seeding before the agent runs.
//   - tags          printScenarioListEntry prints them; descriptor-match reads them.
//   - area          printScenarioListEntry prints it; descriptor-match reads it.
//   - retrospective IsBehavioral() gate — loadBehavioralScenarios SKIPS any
//     scenario lacking it, so a scenario without retrospective is
//     INVISIBLE to list/run/all (it would never run as a flow-eval).
//
// The richer curation manifest the 2026-06-16 audit proposed (canonical /
// overlaps / last-reviewed, docs/spec-testing-architecture.md §7) is DEFERRED —
// a forward practice added as scenarios are touched, NOT retrofitted en masse —
// so it is deliberately absent from the enforced set until that curation pass
// makes those keys universal too.
var requiredScenarioManifestFields = []string{
	"id",
	"description",
	"seed",
	"tags",
	"area",
	"retrospective",
}

// scenarioManifestDirs are the two behavioral-scenario directories, scanned the
// same way the drift guards scan them (eval_scenario_drift_test.go). scenarios-local/
// may be absent on a partial checkout — a missing dir is not a failure.
func scenarioManifestDirs(repoRoot string) []string {
	return []string{
		filepath.Join(repoRoot, "eval", "behavioral", "scenarios"),
		filepath.Join(repoRoot, "eval", "behavioral", "scenarios-local"),
	}
}

// frontmatterHasTopLevelKey reports whether body's YAML front-matter (the first
// `---`…`---` block) declares the given top-level key. It scans only inside the
// front-matter and only matches keys at column 0 (`key:`), so a nested key or a
// mention of the word in the prose body never counts as present.
func frontmatterHasTopLevelKey(body []byte, key string) bool {
	want := regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `:`)
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	fences := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			fences++
			if fences == 2 {
				break // past the front-matter
			}
			continue
		}
		if fences != 1 {
			continue // outside the front-matter block
		}
		if want.MatchString(line) {
			return true
		}
	}
	return false
}

// TestEvalScenarioManifest_RequiredFrontmatter turns the implicit scenario-manifest
// convention into an enforced one: every behavioral scenario must carry the
// front-matter fields the flow-eval harness relies on (see
// requiredScenarioManifestFields). It also pins id == filename, because
// flow-eval.sh resolves a scenario by `<id>.md` — a divergence would make the
// scenario unrunnable by id. No new per-file data is introduced; the corpus
// already satisfies this, so the lint locks the seam against future drift the
// way docs/spec-testing-architecture.md §5/§7 describe.
func TestEvalScenarioManifest_RequiredFrontmatter(t *testing.T) {
	repoRoot := findRepoRoot(t)

	type violation struct {
		file   string
		reason string
	}
	var violations []violation

	for _, dir := range scenarioManifestDirs(repoRoot) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// scenarios-local may not exist on every checkout — not a failure.
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			full := filepath.Join(dir, e.Name())
			rel, relErr := filepath.Rel(repoRoot, full)
			if relErr != nil {
				rel = full
			}
			body, readErr := os.ReadFile(full)
			if readErr != nil {
				violations = append(violations, violation{file: rel, reason: "unreadable: " + readErr.Error()})
				continue
			}
			for _, field := range requiredScenarioManifestFields {
				if !frontmatterHasTopLevelKey(body, field) {
					violations = append(violations, violation{
						file:   rel,
						reason: "missing required front-matter field `" + field + "`",
					})
				}
			}
			// id MUST equal the filename — flow-eval.sh runs `<id>.md`.
			base := strings.TrimSuffix(e.Name(), ".md")
			if id := frontmatterTopLevelValue(body, "id"); id != "" && id != base {
				violations = append(violations, violation{
					file:   rel,
					reason: "id `" + id + "` != filename `" + base + "` (flow-eval.sh addresses scenarios as `<id>.md`)",
				})
			}
		}
	}

	if len(violations) == 0 {
		return
	}
	var msg strings.Builder
	msg.WriteString("behavioral eval scenario(s) violate the manifest convention " +
		"(docs/spec-testing-architecture.md §7) — every scenario must carry the " +
		"front-matter fields flow-eval relies on, and id must equal the filename:\n")
	for _, v := range violations {
		msg.WriteString("  " + v.file + " — " + v.reason + "\n")
	}
	t.Error(msg.String())
}

// frontmatterTopLevelValue returns the scalar value of a top-level front-matter
// key (e.g. `id: foo` → "foo"), trimmed; "" when absent or when the value spans
// multiple lines (block scalar). Used only for the id == filename pin, where the
// value is always a single-line scalar.
func frontmatterTopLevelValue(body []byte, key string) string {
	want := regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `:[ \t]*(.*)$`)
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	fences := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			fences++
			if fences == 2 {
				break
			}
			continue
		}
		if fences != 1 {
			continue
		}
		if m := want.FindStringSubmatch(line); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

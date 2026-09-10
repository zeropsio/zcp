package farm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/zeropsio/zcp/internal/eval"
)

// Verdicts over a pulled bundle (docs/spec-eval-farm.md §5.1). VerdictPassed/
// VerdictFailed mirror the single-run task result; VerdictBlocked and
// VerdictUnpinned are farm-specific outcomes the bundle's own files decide.
const (
	VerdictPassed   = "passed"
	VerdictFailed   = "failed"
	VerdictBlocked  = "blocked"
	VerdictNotRun   = "not-run"
	VerdictUnpinned = "unpinned"
)

// DonePart is one part's own claimed digest (docs/spec-eval-farm.md §1.2
// FM-4).
type DonePart struct {
	TreeDigest string `json:"treeDigest"`
}

// DoneDocument is done.json's shape (docs/spec-eval-farm.md §1.2 FM-4). A
// legacy bundle (docs/spec-eval-farm.md §1.2 FM-6) decodes into a document
// with no Parts and no digest fields — IsLegacy reports that state.
type DoneDocument struct {
	RunID            string              `json:"runId"`
	ScenarioID       string              `json:"scenarioId"`
	RunnerDimensions map[string]string   `json:"runnerDimensions,omitempty"`
	Parts            map[string]DonePart `json:"parts,omitempty"`
	EvaluatorSha256  string              `json:"evaluatorSha256,omitempty"`
	CandidateSha256  string              `json:"candidateSha256,omitempty"`
}

// IsLegacy reports whether d predates FM-4's digest fields
// (docs/spec-eval-farm.md §1.2 FM-6): no evaluator/candidate digest and no
// part digests at all.
func (d DoneDocument) IsLegacy() bool {
	return d.EvaluatorSha256 == "" && d.CandidateSha256 == "" && len(d.Parts) == 0
}

// RunOutcome is one run's graded outcome over a pulled bundle
// (docs/spec-eval-farm.md §5.2 FM-37).
type RunOutcome struct {
	RunID      string
	ScenarioID string
	Verdict    string
	Reason     string // populated for blocked/no-bundle/unpinned outcomes
	ReportText string // the single-run report (spec-testing-architecture.md §10.5), when it could be built
	Done       *DoneDocument
}

// ReportRun grades one pulled run bundle at runDir (docs/spec-eval-farm.md
// §5.1/§5.2). evaluatorPin is the farm's current evaluator digest
// (docs/spec-eval-farm.md §1.2 FM-2); pass "" to skip the pin check (no
// current pin configured).
func ReportRun(runDir, evaluatorPin string) RunOutcome {
	runID := filepath.Base(runDir)
	donePath := filepath.Join(runDir, "done.json")
	doneBytes, err := os.ReadFile(donePath)
	if err != nil {
		return RunOutcome{RunID: runID, Verdict: VerdictBlocked, Reason: "blocked: no bundle (no done.json — docs/spec-eval-farm.md §1.2 FM-3)"}
	}
	var done DoneDocument
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		return RunOutcome{RunID: runID, Verdict: VerdictBlocked, Reason: fmt.Sprintf("blocked: no bundle (unreadable done.json: %v)", err)}
	}
	if done.RunID == "" {
		done.RunID = runID
	}

	legacy := done.IsLegacy()
	if !legacy {
		if reason := checkPartDigests(runDir, done); reason != "" {
			return RunOutcome{RunID: done.RunID, ScenarioID: done.ScenarioID, Verdict: VerdictBlocked, Reason: reason, Done: &done}
		}
		if evaluatorPin != "" && done.EvaluatorSha256 != evaluatorPin {
			return RunOutcome{
				RunID: done.RunID, ScenarioID: done.ScenarioID, Verdict: VerdictBlocked, Done: &done,
				Reason: fmt.Sprintf("blocked: evaluatorSha256 %s does not match the farm's current pin %s (docs/spec-eval-farm.md §1.2 FM-35)", done.EvaluatorSha256, evaluatorPin),
			}
		}
	}

	outcome := RunOutcome{RunID: done.RunID, ScenarioID: done.ScenarioID, Done: &done}
	sessionDir := filepath.Join(runDir, "capture")
	evalRunID, scenarioRunID, scopeErr := discoverEvalScope(sessionDir)
	if scopeErr == nil && metaRecordsAnthropicAPIKey(sessionDir, evalRunID, scenarioRunID) {
		return RunOutcome{RunID: done.RunID, ScenarioID: done.ScenarioID, Verdict: VerdictBlocked, Done: &done,
			Reason: "blocked: api key present; farm runs are oauth-only (docs/spec-eval-farm.md FM-16, owner decision 79ced2cc)"}
	}
	if scopeErr == nil {
		if report, _, buildErr := eval.BuildBehavioralReport(sessionDir, evalRunID, scenarioRunID); buildErr == nil {
			outcome.ReportText = eval.RenderBehavioralReportText(report)
			outcome.Verdict = report.Result.Task.Result
			if outcome.Verdict == "" {
				outcome.Verdict = VerdictNotRun
			}
		} else {
			scopeErr = buildErr
		}
	}
	if scopeErr != nil && !legacy {
		return RunOutcome{RunID: done.RunID, ScenarioID: done.ScenarioID, Verdict: VerdictBlocked, Done: &done,
			Reason: fmt.Sprintf("blocked: could not build the single-run report: %v", scopeErr)}
	}

	if legacy {
		// FM-6: unpinned means "cannot be checked against the
		// evaluator/candidate pin" — the bundle's own verdict label, not a
		// claim about whether the underlying rows passed or failed. Rows
		// still grade normally above (when the capture window let the
		// report build at all); the roll-up prints "unpinned" for this
		// bundle regardless.
		outcome.Verdict = VerdictUnpinned
		if scopeErr != nil {
			outcome.Reason = fmt.Sprintf("unpinned (legacy bundle, no evaluator/candidate digest); report could not be built: %v", scopeErr)
		} else {
			outcome.Reason = "unpinned (legacy bundle, no evaluator/candidate digest)"
		}
	}
	return outcome
}

// checkPartDigests recomputes every part done.json lists (docs/spec-eval-farm.md
// §1.2 FM-4/FM-5) from what actually landed under runDir and compares it
// against done.json's own claim. It returns a non-empty blocked reason on
// the first missing part or digest mismatch (FM-36/FM-38), "" when every
// listed part checks out.
func checkPartDigests(runDir string, done DoneDocument) string {
	names := make([]string, 0, len(done.Parts))
	for name := range done.Parts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		part := done.Parts[name]
		dir := filepath.Join(runDir, name)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return fmt.Sprintf("blocked: part %q done.json lists a digest for is missing (docs/spec-eval-farm.md §1.2 FM-36)", name)
		}
		digest, err := TreeDigest(dir)
		if err != nil {
			return fmt.Sprintf("blocked: could not recompute part %q's tree digest: %v", name, err)
		}
		if digest != part.TreeDigest {
			return fmt.Sprintf("blocked: part %q tree digest %s does not match done.json's claim %s (docs/spec-eval-farm.md §1.2 FM-5/FM-38)", name, digest, part.TreeDigest)
		}
	}
	return ""
}

// BundleNoKnownSecretValue proves docs/spec-eval-farm.md §1.3 FM-7's
// redaction promise on a bundle already on disk (a pulled run/batch dir, or
// a testdata fixture): no regular file under bundleDir contains any of
// secretValues verbatim. It never re-derives what a secret is (that is the
// wrapper's job, before upload) — it only checks the values it is given
// never appear, and names the first file/value pair it finds.
func BundleNoKnownSecretValue(bundleDir string, secretValues []string) error {
	var needles [][]byte
	for _, v := range secretValues {
		if v != "" {
			needles = append(needles, []byte(v))
		}
	}
	return filepath.WalkDir(bundleDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for i, needle := range needles {
			if bytes.Contains(content, needle) {
				rel, _ := filepath.Rel(bundleDir, path)
				return fmt.Errorf("known secret value %d found in %s (docs/spec-eval-farm.md §1.3 FM-7)", i, rel)
			}
		}
		return nil
	})
}

// metaRecordsAnthropicAPIKey reports whether the run's meta.json
// (docs/spec-eval-farm.md §2.4 FM-16) recorded anthropicApiKeyPresent=true
// — read directly rather than through eval.BuildBehavioralReport/
// BehavioralReport, which does not project that field. A missing or
// unreadable meta.json is not this check's concern (the report build below
// surfaces that); it reports false rather than erroring.
func metaRecordsAnthropicAPIKey(sessionDir, evalRunID, scenarioRunID string) bool {
	path := filepath.Join(sessionDir, "eval", evalRunID, scenarioRunID, "meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var meta struct {
		AnthropicAPIKeyPresent bool `json:"anthropicApiKeyPresent"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return false
	}
	return meta.AnthropicAPIKeyPresent
}

// discoverEvalScope finds the single (evalRunID, scenarioRunID) pair a farm
// run's capture window carries — a run project executes exactly one
// scenario (docs/spec-eval-farm.md §2.1 FM-10), so sessionDir/eval/ holds
// exactly one run id directory, itself holding exactly one scenario id
// directory.
func discoverEvalScope(sessionDir string) (evalRunID, scenarioRunID string, err error) {
	evalDir := filepath.Join(sessionDir, "eval")
	runID, err := singleSubdir(evalDir)
	if err != nil {
		return "", "", fmt.Errorf("eval run directory under %s: %w", evalDir, err)
	}
	scenarioID, err := singleSubdir(filepath.Join(evalDir, runID))
	if err != nil {
		return "", "", fmt.Errorf("scenario directory under %s/%s: %w", evalDir, runID, err)
	}
	return runID, scenarioID, nil
}

func singleSubdir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	if len(dirs) != 1 {
		return "", fmt.Errorf("expected exactly one directory, found %d", len(dirs))
	}
	return dirs[0], nil
}

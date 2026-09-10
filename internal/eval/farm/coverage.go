package farm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

// Cell is one (scenario, workflow step, tool decision) triple derived from
// captured MCP traffic (docs/spec-eval-farm.md §5.3 FM-39): the workflow
// step is the envelope phase observed on a call's result, the tool decision
// is "<tool>" or "<tool>:<action>". Count is how many runs exercised it.
type Cell struct {
	ScenarioID string
	Step       string
	Decision   string
	Count      int
}

// CoverageReport is the result of Coverage: the derived cells (sorted by
// scenario, then step, then decision, per FM-39/FM-40 — descriptive only,
// no verdict), the runs whose bundle carried no captured MCP stream, and the
// scenarios whose cells are all also produced by some other scenario
// (candidates for pruning, descriptive only).
type CoverageReport struct {
	Cells           []Cell
	NoStreamRuns    []string
	PruneCandidates []string
}

// runMeta is the subset of results/meta.json (spec-testing-architecture.md
// §10.1) this reads: the scenario id a run's bundle belongs to.
type runMeta struct {
	ScenarioID string `json:"scenarioId"`
}

// batchManifest is the subset of batches/<batch>/manifest.json this reads
// for --since (docs/spec-eval-farm.md §1.4 FM-9 names batch id, scenario
// set, digests, credential mode and run ids, but not a timestamp field;
// "startedAt" is this slice's working assumption, matching the brief — the
// slice that first writes manifest.json for real is the one that fixes this
// shape, and this reader adjusts to match it then).
type batchManifest struct {
	StartedAt string `json:"startedAt"`
}

// Coverage derives a CoverageReport from pulled bundles under dir, with no
// network access (docs/spec-eval-farm.md §5.3). Without since, dir's
// immediate subdirectories are treated as run dirs (<dir>/<runId>/). With
// since, dir's immediate subdirectories are treated as batch dirs, each
// holding its own manifest.json plus its run dirs
// (<dir>/<batch>/manifest.json, <dir>/<batch>/<runId>/); only batches whose
// manifest.json startedAt is greater than or equal to the named batch's are
// included.
func Coverage(dir string, since string) (CoverageReport, error) {
	runDirs, err := listRunDirs(dir, since)
	if err != nil {
		return CoverageReport{}, err
	}

	// counts[scenario][step][decision] = number of runs that exercised it.
	counts := make(map[string]map[string]map[string]int)
	var noStream []string
	scenarioSteps := make(map[string]map[[2]string]bool) // scenario -> set of (step,decision)
	stepScenarios := make(map[[2]string]map[string]bool) // (step,decision) -> set of scenarios

	for _, runDir := range runDirs {
		scenarioID, err := readRunScenarioID(runDir)
		if err != nil {
			return CoverageReport{}, err
		}
		if scenarioID == "" {
			// No done.json and no results/**/meta.json to fall back to —
			// nothing to attribute cells to (docs/spec-eval-farm.md §2.3
			// FM-13 has every real bundle carry done.json, so this is a
			// belt-and-braces case, not the live shape).
			noStream = append(noStream, filepath.Base(runDir))
			continue
		}

		sessionDir, err := resolveCaptureWindowDir(filepath.Join(runDir, "capture"))
		if err != nil {
			// A run that died before (or during) capture — e.g. the
			// binding preflight — has no capture window at all. Listed,
			// never fatal (docs/spec-eval-farm.md §5.3 FM-39).
			noStream = append(noStream, filepath.Base(runDir))
			continue
		}

		matches, err := filepath.Glob(filepath.Join(sessionDir, "mcp", "zcp-*.jsonl"))
		if err != nil {
			return CoverageReport{}, fmt.Errorf("farm: glob capture for %s: %w", runDir, err)
		}
		if len(matches) == 0 {
			noStream = append(noStream, filepath.Base(runDir))
			continue
		}

		for _, streamPath := range matches {
			calls, err := capture.ReadMCPStream(streamPath)
			if err != nil {
				return CoverageReport{}, fmt.Errorf("farm: coverage: %w", err)
			}
			for _, call := range calls {
				decision := call.Tool
				if call.Action != "" {
					decision = call.Tool + ":" + call.Action
				}
				step := call.EnvelopePhase

				if counts[scenarioID] == nil {
					counts[scenarioID] = make(map[string]map[string]int)
				}
				if counts[scenarioID][step] == nil {
					counts[scenarioID][step] = make(map[string]int)
				}
				counts[scenarioID][step][decision]++

				key := [2]string{step, decision}
				if scenarioSteps[scenarioID] == nil {
					scenarioSteps[scenarioID] = make(map[[2]string]bool)
				}
				scenarioSteps[scenarioID][key] = true
				if stepScenarios[key] == nil {
					stepScenarios[key] = make(map[string]bool)
				}
				stepScenarios[key][scenarioID] = true
			}
		}
	}

	var cells []Cell
	for scenarioID, steps := range counts {
		for step, decisions := range steps {
			for decision, count := range decisions {
				cells = append(cells, Cell{ScenarioID: scenarioID, Step: step, Decision: decision, Count: count})
			}
		}
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].ScenarioID != cells[j].ScenarioID {
			return cells[i].ScenarioID < cells[j].ScenarioID
		}
		if cells[i].Step != cells[j].Step {
			return cells[i].Step < cells[j].Step
		}
		return cells[i].Decision < cells[j].Decision
	})

	var pruneCandidates []string
	for scenarioID, keys := range scenarioSteps {
		unique := false
		for key := range keys {
			if len(stepScenarios[key]) == 1 {
				unique = true
				break
			}
		}
		if !unique {
			pruneCandidates = append(pruneCandidates, scenarioID)
		}
	}
	sort.Strings(pruneCandidates)
	sort.Strings(noStream)

	return CoverageReport{Cells: cells, NoStreamRuns: noStream, PruneCandidates: pruneCandidates}, nil
}

// readRunScenarioID resolves runDir's scenario id: done.json's scenarioId
// field (present in every bundle, written by the wrapper — docs/spec-eval-
// farm.md §2.3 FM-13) when done.json exists, falling back to the single
// results/**/meta.json (docs/spec-testing-architecture.md §10.1) only when
// done.json is absent. Returns "" (not an error) when neither resolves —
// the caller lists such a run as having no stream rather than aborting.
func readRunScenarioID(runDir string) (string, error) {
	doneBody, err := os.ReadFile(filepath.Join(runDir, "done.json"))
	if err == nil {
		var done DoneDocument
		if err := json.Unmarshal(doneBody, &done); err != nil {
			return "", fmt.Errorf("farm: parse done.json for %s: %w", runDir, err)
		}
		return done.ScenarioID, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("farm: read done.json for %s: %w", runDir, err)
	}

	matches, err := filepath.Glob(filepath.Join(runDir, "results", "*", "*", "meta.json"))
	if err != nil {
		return "", fmt.Errorf("farm: glob meta.json for %s: %w", runDir, err)
	}
	if len(matches) != 1 {
		return "", nil
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		return "", fmt.Errorf("farm: read meta.json for %s: %w", runDir, err)
	}
	var meta runMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("farm: parse meta.json for %s: %w", runDir, err)
	}
	return meta.ScenarioID, nil
}

// listRunDirs resolves dir (and --since) to the run dirs Coverage should
// read, per Coverage's doc comment.
func listRunDirs(dir string, since string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("farm: read dir %s: %w", dir, err)
	}

	if since == "" {
		var runDirs []string
		for _, entry := range entries {
			if entry.IsDir() {
				runDirs = append(runDirs, filepath.Join(dir, entry.Name()))
			}
		}
		sort.Strings(runDirs)
		return runDirs, nil
	}

	threshold, err := readBatchStartedAt(filepath.Join(dir, since))
	if err != nil {
		return nil, err
	}

	var batchDirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		batchDir := filepath.Join(dir, entry.Name())
		startedAt, err := readBatchStartedAt(batchDir)
		if err != nil {
			return nil, err
		}
		if compareTimestamps(startedAt, threshold) >= 0 {
			batchDirs = append(batchDirs, batchDir)
		}
	}
	sort.Strings(batchDirs)

	var runDirs []string
	for _, batchDir := range batchDirs {
		batchEntries, err := os.ReadDir(batchDir)
		if err != nil {
			return nil, fmt.Errorf("farm: read batch dir %s: %w", batchDir, err)
		}
		for _, entry := range batchEntries {
			if entry.IsDir() {
				runDirs = append(runDirs, filepath.Join(batchDir, entry.Name()))
			}
		}
	}
	sort.Strings(runDirs)
	return runDirs, nil
}

func readBatchStartedAt(batchDir string) (string, error) {
	body, err := os.ReadFile(filepath.Join(batchDir, "manifest.json"))
	if err != nil {
		return "", fmt.Errorf("farm: read manifest.json for %s: %w", batchDir, err)
	}
	var manifest batchManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", fmt.Errorf("farm: parse manifest.json for %s: %w", batchDir, err)
	}
	return manifest.StartedAt, nil
}

// compareTimestamps compares two RFC3339 timestamps, falling back to a
// plain string compare when either fails to parse (so a non-RFC3339
// "startedAt" still sorts, deterministically if not meaningfully).
func compareTimestamps(a, b string) int {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA == nil && errB == nil {
		switch {
		case ta.Before(tb):
			return -1
		case ta.After(tb):
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

// Markdown renders the report as the deterministic table + summary that
// both `zcp eval farm coverage`'s stdout and coverage.md carry, byte
// identical (docs/spec-eval-farm.md §5.3): per row scenario/step/decision/
// count, then a summary of cells total, no-stream runs, and scenarios with
// no unique cell (pruning candidates, descriptive only — FM-40).
func (r CoverageReport) Markdown() string {
	var b strings.Builder
	b.WriteString("| Scenario | Step | Decision | Runs |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, cell := range r.Cells {
		fmt.Fprintf(&b, "| %s | %s | %s | %d |\n", cell.ScenarioID, cell.Step, cell.Decision, cell.Count)
	}
	fmt.Fprintf(&b, "\nCells: %d\n", len(r.Cells))
	if len(r.NoStreamRuns) == 0 {
		b.WriteString("No-stream runs: (none)\n")
	} else {
		fmt.Fprintf(&b, "No-stream runs: %s\n", strings.Join(r.NoStreamRuns, ", "))
	}
	if len(r.PruneCandidates) == 0 {
		b.WriteString("Scenarios with no unique cell: (none)\n")
	} else {
		fmt.Fprintf(&b, "Scenarios with no unique cell: %s\n", strings.Join(r.PruneCandidates, ", "))
	}
	return b.String()
}

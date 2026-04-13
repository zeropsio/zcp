package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Dispatch-artifact floor for the feature sub-agent substep.
//
// v13 showed that an attestation-only gate is cosmetic: the main agent can
// type a glowing "single author across three codebases" description into the
// substep=subagent completion call without ever invoking the Agent tool. The
// feature code ends up interleaved with the main agent's debugging loops and
// the single-author contract discipline never actually runs.
//
// This check is the structural half of the gate. It walks the codebase
// mounts for files newer than the deploy-init-commands baseline and rejects
// completion when:
//
//  1. Too few feature files exist (agent never wrote anything)
//  2. The mtime spread of those files is wider than a single-burst dispatch
//     would produce (agent wrote them inline across many minutes instead of
//     dispatching a sub-agent that writes in one coherent tool call)
//
// The numbers are chosen so a real feature sub-agent clears the floor
// comfortably and an inline main-agent author cannot. They are tunable
// constants, not config — tune by adjusting the floor after observing
// real v14+ sub-agent runs.
const (
	// minFeatureArtifacts is the minimum number of post-baseline source
	// files required under the codebase mounts. A five-section showcase
	// feature sub-agent writes ~10 files (items.service.ts, items.controller.ts,
	// items.module.ts, dto/, nats.module.ts, ItemsPanel.svelte, updates to
	// App.svelte / app.module.ts / jobs.controller.ts). Six is the floor
	// that blocks the "wrote one file and attested done" case without
	// spuriously rejecting a simpler but legitimate feature set.
	minFeatureArtifacts = 6

	// maxFeatureMTimeSpread bounds the wall-clock window across all feature
	// files' mtimes. A real sub-agent dispatch writes every feature file
	// inside one Agent tool call — the sub-agent executes many Writes but
	// returns one result to the main agent, and the Writes run in a single
	// tight burst (under a minute in practice). The main agent inlining the
	// same code interleaved with Edit, Bash, Read, and MCP tool calls over
	// the course of deploy debugging produces a spread measured in tens of
	// minutes. Five minutes is wide enough for a slow sub-agent and tight
	// enough to block any inline pattern we have seen.
	maxFeatureMTimeSpread = 5 * time.Minute
)

// featureSourceExts are the source file extensions counted toward the
// dispatch-artifact floor. zerops.yaml, package-lock.json, README.md, and
// other config/meta files do not count — they are scaffold artifacts and
// may legitimately be edited by the main agent during deploy.
var featureSourceExts = map[string]struct{}{
	".ts":     {},
	".tsx":    {},
	".js":     {},
	".jsx":    {},
	".svelte": {},
	".vue":    {},
	".py":     {},
	".php":    {},
	".go":     {},
	".rb":     {},
}

// featureSkipDirs are directory name fragments that short-circuit the walk.
// node_modules and dist can contain thousands of files with post-baseline
// mtimes from npm install / nest build during deploy debugging — counting
// them would mask the real signal.
var featureSkipDirs = []string{
	"node_modules",
	"dist",
	"build",
	".git",
	"vendor",
	".next",
	".nuxt",
	".svelte-kit",
	"__pycache__",
	".venv",
}

// dispatchArtifactResult summarizes the walk for error reporting.
type dispatchArtifactResult struct {
	count     int
	oldest    time.Time
	newest    time.Time
	samples   []string // up to 5 example paths, relative to mount base
	mountBase string
}

// walkDispatchArtifacts collects source files newer than baseline under the
// codebase mounts for every runtime target in the plan. Returns zero-value
// result when the mount base does not exist (test environments, recipes
// running outside the mount harness) — callers should treat that as skip.
func walkDispatchArtifacts(plan *RecipePlan, baseline time.Time) dispatchArtifactResult {
	base := recipeMountBase
	if recipeMountBaseOverride != "" {
		base = recipeMountBaseOverride
	}
	res := dispatchArtifactResult{mountBase: base}
	if plan == nil {
		return res
	}
	if _, err := os.Stat(base); err != nil {
		return res
	}

	for _, t := range plan.Targets {
		if !IsRuntimeType(t.Type) {
			continue
		}
		if t.IsWorker && t.SharesCodebaseWith != "" {
			continue
		}
		mount := filepath.Join(base, t.Hostname+"dev")
		walkMountForArtifacts(mount, baseline, &res)
	}
	return res
}

// walkMountForArtifacts walks a single codebase mount, updating res in place.
func walkMountForArtifacts(mount string, baseline time.Time, res *dispatchArtifactResult) {
	_ = filepath.WalkDir(mount, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Permission/missing dir — skip the branch, continue the walk.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return filepath.SkipDir
		}
		if d.IsDir() {
			if slices.Contains(featureSkipDirs, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := featureSourceExts[ext]; !ok {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			// File vanished mid-walk (concurrent edit, race with npm
			// install) — skip this entry but continue the walk. Not an
			// error we want to propagate; the walk is best-effort.
			return nil //nolint:nilerr // intentional skip, see comment
		}
		mtime := info.ModTime()
		if !mtime.After(baseline) {
			return nil
		}
		res.count++
		if res.oldest.IsZero() || mtime.Before(res.oldest) {
			res.oldest = mtime
		}
		if mtime.After(res.newest) {
			res.newest = mtime
		}
		if len(res.samples) < 5 {
			if rel, err := filepath.Rel(res.mountBase, path); err == nil {
				res.samples = append(res.samples, rel)
			} else {
				res.samples = append(res.samples, path)
			}
		}
		return nil
	})
}

// featureDispatchBaseline returns the timestamp feature work should be
// measured against: the most recent of the scaffold-phase step completions
// (generate step CompletedAt) and the init-commands substep CompletedAt.
// Any feature file must be written after this moment.
//
// Returns the zero time when no usable baseline exists — callers should
// treat that as "cannot enforce, skip the check" rather than fail-closed,
// because fail-closed on missing state would block every replay of a
// session loaded from an older state file.
func featureDispatchBaseline(state *RecipeState) time.Time {
	if state == nil {
		return time.Time{}
	}
	var baseline time.Time
	for i := range state.Steps {
		step := &state.Steps[i]
		// Generate step completion — anything after this is post-scaffold.
		if step.Name == RecipeStepGenerate && step.CompletedAt != "" {
			if t, err := time.Parse(time.RFC3339, step.CompletedAt); err == nil && t.After(baseline) {
				baseline = t
			}
		}
		// Deploy step init-commands substep — tighter baseline that rules
		// out migration/seed edits from counting toward feature work.
		if step.Name == RecipeStepDeploy {
			for j := range step.SubSteps {
				ss := &step.SubSteps[j]
				if ss.Name == SubStepInitCommands && ss.CompletedAt != "" {
					if t, err := time.Parse(time.RFC3339, ss.CompletedAt); err == nil && t.After(baseline) {
						baseline = t
					}
				}
			}
		}
	}
	return baseline
}

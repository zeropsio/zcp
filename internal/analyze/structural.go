package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// CanonicalEnvFolders names the six tier folder names the recipe engine
// emits deterministically. Any directory under environments/ not on
// this list is a ghost and evidence of F-9 (main agent invented a slug
// from an un-populated `.EnvFolders` template variable).
//
// Kept verbatim here rather than imported from internal/workflow to
// keep the analyze package dependency-light and allow external
// verification tools to depend on it without pulling the whole
// workflow stack.
var CanonicalEnvFolders = []string{
	"0 \u2014 AI Agent",
	"1 \u2014 Remote (CDE)",
	"2 \u2014 Local",
	"3 \u2014 Stage",
	"4 \u2014 Small Production",
	"5 \u2014 Highly-available Production",
}

// DefaultAllowedAtomFields names every template field the Go render
// path populates (or will populate after Cx-ENVFOLDERS-WIRED). The
// B-22 lint only validates that atom references use a recognized
// field NAME; whether a field actually reaches dispatch is proven
// by the Cx-ENVFOLDERS-WIRED commit's unit tests for
// LoadAtomBodyRendered. The two gates work together:
//
//   - B-22 (this list): catches typos + net-new unknown fields at
//     build time so atom edits can't silently introduce an F-9-class
//     defect.
//   - LoadAtomBodyRendered test: catches regressions in the render
//     path so a field on this list actually gets populated.
//
// Field rationale:
//   - ProjectRoot, Hostnames (slice): plan-wide context for brief
//     atoms shared across sub-agents.
//   - Hostname (singular): per-codebase dispatch context for scaffold
//     atoms that target one codebase at a time.
//   - EnvFolders: canonical env folder list used by writer
//     canonical-output-tree.md; Cx-ENVFOLDERS-WIRED populates it.
//   - Framework, Slug, Tier: plan metadata.
//   - FactsLogPath, ManifestPath, PlanPath: per-run artifact paths
//     used by editorial-review + writer atoms.
var DefaultAllowedAtomFields = map[string]bool{
	"ProjectRoot":  true,
	"Hostnames":    true,
	"Hostname":     true,
	"EnvFolders":   true,
	"Framework":    true,
	"Slug":         true,
	"Tier":         true,
	"FactsLogPath": true,
	"ManifestPath": true,
	"PlanPath":     true,
}

// CheckGhostEnvDirs implements B-15. Walks {deliverable}/environments/,
// flags every directory name not in CanonicalEnvFolders. Returns Skip
// when the environments/ directory is absent (e.g. pre-finalize state).
func CheckGhostEnvDirs(deliverableDir string) BarResult {
	envsDir := filepath.Join(deliverableDir, "environments")
	entries, err := os.ReadDir(envsDir)
	if err != nil {
		return BarResult{
			Description: "ghost (non-canonical) directories under environments/",
			Measurement: "readdir environments/ ∉ CanonicalEnvFolders",
			Threshold:   0,
			Status:      StatusSkip,
			Reason:      fmt.Sprintf("cannot read %s: %v", envsDir, err),
		}
	}
	canonical := make(map[string]bool, len(CanonicalEnvFolders))
	for _, n := range CanonicalEnvFolders {
		canonical[n] = true
	}
	var ghosts []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !canonical[e.Name()] {
			ghosts = append(ghosts, e.Name())
		}
	}
	sort.Strings(ghosts)
	return BarResult{
		Description:   "ghost (non-canonical) directories under environments/",
		Measurement:   "readdir environments/ ∉ CanonicalEnvFolders",
		Threshold:     0,
		Observed:      len(ghosts),
		Status:        PassOrFail(len(ghosts) == 0),
		EvidenceFiles: ghosts,
	}
}

// CheckPerCodebaseMarkdown implements B-16. Counts README.md + CLAUDE.md
// present under each named codebase directory under the deliverable.
// Expected = 2 × len(codebases). v36 retrospective: observed 0 of 6
// because close-step never staged writer output (F-10).
//
// When codebases is empty the bar skips — the harness caller is
// responsible for naming them (CLI flag or plan-driven). This avoids
// guessing codebase names from arbitrary subdirectories and mis-
// classifying e.g. `src/` as a codebase.
func CheckPerCodebaseMarkdown(deliverableDir string, codebases []string) BarResult {
	if len(codebases) == 0 {
		return BarResult{
			Description: "per-codebase README.md + CLAUDE.md staged into deliverable",
			Measurement: "stat {deliverable}/{codebase}/{README.md,CLAUDE.md} for each codebase",
			Threshold:   0,
			Status:      StatusSkip,
			Reason:      "no codebases supplied (pass --app-codebases or plan-derived list)",
		}
	}
	expected := 2 * len(codebases)
	var observedFiles []string
	var missing []string
	for _, cb := range codebases {
		for _, name := range []string{"README.md", "CLAUDE.md"} {
			p := filepath.Join(deliverableDir, cb, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				rel, _ := filepath.Rel(deliverableDir, p)
				observedFiles = append(observedFiles, rel)
			} else {
				rel, _ := filepath.Rel(deliverableDir, p)
				missing = append(missing, rel)
			}
		}
	}
	status := PassOrFail(len(observedFiles) == expected)
	return BarResult{
		Description:   "per-codebase README.md + CLAUDE.md staged into deliverable",
		Measurement:   "stat {deliverable}/{codebase}/{README.md,CLAUDE.md}",
		Threshold:     expected,
		Observed:      len(observedFiles),
		Status:        status,
		EvidenceFiles: missing,
	}
}

// markerBrokenRe catches any ZEROPS_EXTRACT marker for the three known
// surface keys where the trailing `#` is missing. The literal `#` in
// the HTML comment wraps the key on BOTH sides of the colon — the
// keys `intro`, `integration-guide`, `knowledge-base` are all expected
// to carry `#` after the key inside the comment.
//
// Form expected by canonical scaffold:
//
//	<!-- #ZEROPS_EXTRACT_START:intro# -->
//
// Form observed in v36 writer dispatch (F-12):
//
//	<!-- #ZEROPS_EXTRACT_START:intro -->
var markerBrokenRe = regexp.MustCompile(
	`<!--\s*#ZEROPS_EXTRACT_(START|END):(intro|integration-guide|knowledge-base)\s*-->`,
)

// CheckMarkerExactForm implements B-17 over the deliverable tree. Each
// .md file with at least one broken marker counts once. Returns Skip
// when no markdown files are discoverable (unusual, would indicate a
// malformed deliverable).
func CheckMarkerExactForm(deliverableDir string) BarResult {
	var scanned int
	var offenders []string
	err := filepath.Walk(deliverableDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		scanned++
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil //nolint:nilerr // unreadable files are environment noise, not deliverable defects
		}
		if markerBrokenRe.Find(data) != nil {
			rel, _ := filepath.Rel(deliverableDir, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		return BarResult{
			Description: "ZEROPS_EXTRACT markers carry trailing '#' sentinel",
			Threshold:   0,
			Status:      StatusSkip,
			Reason:      fmt.Sprintf("walk: %v", err),
		}
	}
	if scanned == 0 {
		return BarResult{
			Description: "ZEROPS_EXTRACT markers carry trailing '#' sentinel",
			Threshold:   0,
			Status:      StatusSkip,
			Reason:      "no markdown files under deliverable",
		}
	}
	sort.Strings(offenders)
	return BarResult{
		Description:   "ZEROPS_EXTRACT markers carry trailing '#' sentinel",
		Measurement:   "regex <!-- #ZEROPS_EXTRACT_(START|END):(key) --> without trailing '#'",
		Threshold:     0,
		Observed:      len(offenders),
		Status:        PassOrFail(len(offenders) == 0),
		EvidenceFiles: offenders,
	}
}

// forbiddenStandaloneNames names the two standalone markdown files F-13
// flags as dead weight. Fragments live inside per-codebase README.md;
// any standalone file with these names is evidence the writer followed
// the pre-Cx-STANDALONE-FILES-REMOVED atom spec.
var forbiddenStandaloneNames = map[string]bool{
	"INTEGRATION-GUIDE.md": true,
	"GOTCHAS.md":           true,
}

// CheckStandaloneDuplicateFiles implements B-18 over the deliverable
// tree. Every path matching INTEGRATION-GUIDE.md or GOTCHAS.md counts.
// Threshold zero; any hit is a fail.
func CheckStandaloneDuplicateFiles(deliverableDir string) BarResult {
	var offenders []string
	err := filepath.Walk(deliverableDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if forbiddenStandaloneNames[info.Name()] {
			rel, _ := filepath.Rel(deliverableDir, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		return BarResult{
			Description: "standalone INTEGRATION-GUIDE.md / GOTCHAS.md absent",
			Threshold:   0,
			Status:      StatusSkip,
			Reason:      fmt.Sprintf("walk: %v", err),
		}
	}
	sort.Strings(offenders)
	return BarResult{
		Description:   "standalone INTEGRATION-GUIDE.md / GOTCHAS.md absent",
		Measurement:   "walk deliverable; flag paths named INTEGRATION-GUIDE.md or GOTCHAS.md",
		Threshold:     0,
		Observed:      len(offenders),
		Status:        PassOrFail(len(offenders) == 0),
		EvidenceFiles: offenders,
	}
}

// templateBlockRe matches every `{{...}}` template block. The inner
// pattern is lazy so adjacent blocks on the same line don't merge. The
// block body is then scanned for `.[A-Z]\w+` sequences via fieldRe.
var templateBlockRe = regexp.MustCompile(`\{\{[^{}]*?\}\}`)

// fieldRe pulls every `.Field` token out of a template block body.
// Used inside extractTemplateRefs to handle `.X`, `index .X`,
// `range .X`, and similar constructs uniformly.
var fieldRe = regexp.MustCompile(`\.([A-Z]\w+)`)

// extractTemplateRefs returns the distinct field names referenced in
// template blocks within body. Order preserved by first occurrence so
// callers can cite the first-sighting line.
func extractTemplateRefs(body string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, block := range templateBlockRe.FindAllString(body, -1) {
		for _, m := range fieldRe.FindAllStringSubmatch(block, -1) {
			field := m[1]
			if seen[field] {
				continue
			}
			seen[field] = true
			out = append(out, field)
		}
	}
	return out
}

// CheckAtomTemplateVarsBound implements B-22 over the named atom
// corpus. Every `{{.Field}}` reference must name an allowed field.
// `allowedFields` carries the canonical set (see
// DefaultAllowedAtomFields) — passed in so tests + non-prod scans can
// swap it.
//
// The bar is structural (not behavioral): it flags fields the render
// path would not populate. It does NOT prove those fields actually
// reach the dispatch prompt — that's F-9's root-cause dimension,
// verified downstream by the Cx-ENVFOLDERS-WIRED commit's unit tests.
func CheckAtomTemplateVarsBound(atomRoot string, allowedFields map[string]bool) BarResult {
	type offense struct {
		atom  string
		field string
	}
	var offenses []offense
	err := filepath.Walk(atomRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil //nolint:nilerr // unreadable atom file is environment noise
		}
		for _, field := range extractTemplateRefs(string(data)) {
			if !allowedFields[field] {
				rel, _ := filepath.Rel(atomRoot, path)
				offenses = append(offenses, offense{atom: rel, field: field})
			}
		}
		return nil
	})
	if err != nil {
		return BarResult{
			Description: "atom template variables bind to populated Go render fields",
			Threshold:   0,
			Status:      StatusSkip,
			Reason:      fmt.Sprintf("walk: %v", err),
		}
	}
	sort.Slice(offenses, func(i, j int) bool {
		if offenses[i].atom == offenses[j].atom {
			return offenses[i].field < offenses[j].field
		}
		return offenses[i].atom < offenses[j].atom
	})
	var evidence []string
	for _, o := range offenses {
		evidence = append(evidence, fmt.Sprintf("%s: .%s", o.atom, o.field))
	}
	return BarResult{
		Description:   "atom template variables bind to populated Go render fields",
		Measurement:   "walk atom corpus; extract {{.Field}}; fail when Field ∉ allowed set",
		Threshold:     0,
		Observed:      len(offenses),
		Status:        PassOrFail(len(offenses) == 0),
		EvidenceFiles: evidence,
	}
}

// FormatEvidencePaths joins a slice of evidence paths for inline
// display in a verdict body. Empty list becomes `"<none>"` so the
// output is unambiguous even when a bar passes.
func FormatEvidencePaths(files []string) string {
	if len(files) == 0 {
		return "<none>"
	}
	return strings.Join(files, ", ")
}

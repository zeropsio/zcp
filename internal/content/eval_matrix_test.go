// Package content_test (external test package, not `content`): internal/eval
// imports internal/content, so a same-package test importing internal/eval
// back would be an import cycle. content_test avoids that the normal Go way.
package content_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/eval"
)

// evalMatrixRepoRoot walks up from the test binary's working directory
// until it finds `go.mod` — the repo root. Duplicated (not reused) from
// repo_drift_test.go's findRepoRoot: that helper is unexported in package
// content, unreachable from this external test package.
func evalMatrixRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 8 {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repo root (go.mod) not found from " + dir)
	return ""
}

// This file pins docs/spec-scenarios.md §9.3 (the eval core matrix) as the
// single source for the eval farm's gate set. It parses every markdown table
// under §9.3 whose header carries both a "scenario id" and a "status"
// column (the F-table has no "scenario id" column and is deliberately
// ignored — it names gaps, not runnable cells).
//
// docs/spec-eval-farm.md §4.3 FM-33: the oracle-family vocabulary is derived
// by reflection over internal/eval.VerificationConfig, never hand-listed —
// see reflectionExistingFamilies/seedExpectExists below.

// matrixRow is one parsed row from a §9.3 table.
type matrixRow struct {
	id            string
	statusRaw     string
	families      []string // family tokens named in the row, qualifiers stripped
	isGate        bool
	wantsFamily   string // non-empty only for "gate · wants: <family>"
	isPending     bool
	pendingFamily string // non-empty only for "pending: <family>" (not "pending: scenario"/"pending: local mode in farm")
}

var backtickTokenRe = regexp.MustCompile("`([^`]+)`")
var separatorRowRe = regexp.MustCompile(`^[\s|:-]+$`)

// splitMarkdownRow splits one "| a | b | c |" line into trimmed cells,
// dropping the empty leading/trailing elements the surrounding pipes leave.
func splitMarkdownRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// tokenizeFamilies splits an "oracle families" cell on spaces, EXCEPT inside
// parens — a qualifier such as `toolArg(workingDir ∈ /var/www/appdev; Bash
// ln -s never)` must stay one token so the word "never" inside it is never
// mistaken for a second, independent family. The qualifier is then stripped:
// "mustOffer(classic)" -> "mustOffer".
func tokenizeFamilies(cell string) []string {
	var tokens []string
	var buf strings.Builder
	depth := 0
	flush := func() {
		if buf.Len() > 0 {
			tokens = append(tokens, buf.String())
			buf.Reset()
		}
	}
	for _, r := range cell {
		switch {
		case r == '(':
			depth++
			buf.WriteRune(r)
		case r == ')':
			if depth > 0 {
				depth--
			}
			buf.WriteRune(r)
		case r == ' ' && depth == 0:
			flush()
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	for i, tok := range tokens {
		if before, _, ok := strings.Cut(tok, "("); ok {
			tokens[i] = before
		}
	}
	return tokens
}

// parseRowStatus fills in isGate/wantsFamily/isPending/pendingFamily from the
// row's raw status cell. Grammar (docs/spec-scenarios.md §9.3): `gate`,
// optionally followed by `· wants: <family>`; or `pending: <family>`; or
// `pending: scenario` / `pending: local mode in farm` (no family).
func parseRowStatus(row *matrixRow) {
	s := row.statusRaw
	switch {
	case strings.HasPrefix(s, "gate"):
		row.isGate = true
		if _, after, ok := strings.Cut(s, "wants:"); ok {
			rest := strings.TrimSpace(after)
			if pi := strings.IndexAny(rest, " ("); pi >= 0 {
				rest = rest[:pi]
			}
			row.wantsFamily = strings.TrimSpace(rest)
		}
	case strings.HasPrefix(s, "pending:"):
		row.isPending = true
		rest := strings.TrimSpace(strings.TrimPrefix(s, "pending:"))
		if pi := strings.Index(rest, "("); pi >= 0 {
			rest = strings.TrimSpace(rest[:pi])
		}
		if rest != "scenario" && rest != "local mode in farm" {
			row.pendingFamily = rest
		}
	}
}

// parseMatrixRows reads docs/spec-scenarios.md, isolates §9.3 ("### 9.3 The
// matrix" up to the next "### " heading), and parses every markdown table
// block whose header contains both a "scenario id" and a "status" column.
// Column positions are read from each table's own header, not hardcoded, so
// tables A-E (which don't share a column layout) all parse correctly, and
// the F-table (no "scenario id" column) and the §9.1 axes table are skipped.
func parseMatrixRows(t *testing.T, repoRoot string) []matrixRow {
	t.Helper()
	path := filepath.Join(repoRoot, "docs", "spec-scenarios.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(body)

	start := strings.Index(text, "### 9.3 The matrix")
	if start < 0 {
		t.Fatalf("docs/spec-scenarios.md: §9.3 'The matrix' heading not found")
	}
	section := text[start:]
	if end := strings.Index(section, "\n### 9.4"); end >= 0 {
		section = section[:end]
	}

	lines := strings.Split(section, "\n")
	var rows []matrixRow
	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "|") {
			i++
			continue
		}
		if i+1 >= len(lines) || !separatorRowRe.MatchString(lines[i+1]) {
			i++
			continue
		}
		header := splitMarkdownRow(lines[i])
		idIdx, statusIdx, famIdx := -1, -1, -1
		for ci, cell := range header {
			switch strings.ToLower(cell) {
			case "scenario id":
				idIdx = ci
			case "status":
				statusIdx = ci
			case "oracle families":
				famIdx = ci
			}
		}
		i += 2 // consume header + separator
		if idIdx < 0 || statusIdx < 0 {
			// Not a scenario/status table (§9.1 axes table, F-table) —
			// skip its body without recording rows.
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				i++
			}
			continue
		}
		for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			cells := splitMarkdownRow(lines[i])
			i++
			if idIdx >= len(cells) || statusIdx >= len(cells) {
				continue
			}
			m := backtickTokenRe.FindStringSubmatch(cells[idIdx])
			if m == nil {
				continue // cell row (e.g. a continuation) with no scenario id
			}
			row := matrixRow{id: m[1], statusRaw: strings.TrimSpace(cells[statusIdx])}
			if famIdx >= 0 && famIdx < len(cells) {
				row.families = tokenizeFamilies(cells[famIdx])
			}
			parseRowStatus(&row)
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("docs/spec-scenarios.md §9.3: parsed zero rows — table format changed?")
	}
	return rows
}

// gateSetIDsFromSpec returns the ids of every `gate` row (with or without a
// `wants:` suffix), sorted.
func gateSetIDsFromSpec(rows []matrixRow) []string {
	var ids []string
	for _, r := range rows {
		if r.isGate {
			ids = append(ids, r.id)
		}
	}
	sort.Strings(ids)
	return ids
}

// readGateSetFile reads eval/farm/gate-set.txt: one scenario id per line,
// blank lines and `#`-comment lines ignored.
func readGateSetFile(t *testing.T, repoRoot string) []string {
	t.Helper()
	path := filepath.Join(repoRoot, "eval", "farm", "gate-set.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var ids []string
	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ids = append(ids, line)
	}
	sort.Strings(ids)
	return ids
}

// TestEvalMatrix_GateSetMatchesSpec_ExactIDs pins eval/farm/gate-set.txt to
// exactly the `gate` rows of docs/spec-scenarios.md §9.3 — a hand edit to
// either side must fail this test (§9's own opening paragraph).
func TestEvalMatrix_GateSetMatchesSpec_ExactIDs(t *testing.T) {
	repoRoot := evalMatrixRepoRoot(t)
	rows := parseMatrixRows(t, repoRoot)

	specIDs := gateSetIDsFromSpec(rows)
	fileIDs := readGateSetFile(t, repoRoot)

	if reflect.DeepEqual(specIDs, fileIDs) {
		return
	}

	specSet := make(map[string]bool, len(specIDs))
	for _, id := range specIDs {
		specSet[id] = true
	}
	fileSet := make(map[string]bool, len(fileIDs))
	for _, id := range fileIDs {
		fileSet[id] = true
	}

	var missingFromFile, extraInFile []string
	for _, id := range specIDs {
		if !fileSet[id] {
			missingFromFile = append(missingFromFile, id)
		}
	}
	for _, id := range fileIDs {
		if !specSet[id] {
			extraInFile = append(extraInFile, id)
		}
	}

	t.Errorf("eval/farm/gate-set.txt does not match the `gate` rows of docs/spec-scenarios.md §9.3\n"+
		"  in spec, missing from gate-set.txt: %v\n"+
		"  in gate-set.txt, not a `gate` row in spec: %v",
		missingFromFile, extraInFile)
}

// requiredFamilies returns the row's family tokens minus any `wants:`
// family — a `wants:` family is an oracle the row must adopt once it
// exists, not one the scenario carries today.
func (r matrixRow) requiredFamilies() []string {
	if r.wantsFamily == "" {
		return r.families
	}
	out := make([]string, 0, len(r.families))
	for _, f := range r.families {
		if f != r.wantsFamily {
			out = append(out, f)
		}
	}
	return out
}

// familyPresent reports whether family is asserted in the parsed
// verification block: either a direct VerificationConfig field (found by
// its yaml tag) that is non-zero, or — for `subdomainProbe`, which is not a
// VerificationConfig field but a nested ExpectedService one
// (docs/spec-scenarios.md §9.3 lists it as an existing "today" family) — any
// expectedServices entry carrying a subdomainProbe.
func familyPresent(v *eval.VerificationConfig, family string) bool {
	rv := reflect.ValueOf(*v)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("yaml")
		name := strings.Split(tag, ",")[0]
		if name == family {
			return !rv.Field(i).IsZero()
		}
	}
	if family == "subdomainProbe" {
		for _, es := range v.ExpectedServices {
			if es.SubdomainProbe != nil {
				return true
			}
		}
		return false
	}
	return false
}

// TestEvalMatrix_GateRows_ScenarioRequiredAndCarriesFamilies proves every
// `gate` row is actually runnable today (docs/spec-scenarios.md §9.3's own
// definition of `gate`): the scenario file exists, its front-matter marks
// verification required with a spec pointer, and every family the row
// claims (minus a `wants:` promise) is a family the scenario's verification
// block actually carries.
func TestEvalMatrix_GateRows_ScenarioRequiredAndCarriesFamilies(t *testing.T) {
	repoRoot := evalMatrixRepoRoot(t)
	rows := parseMatrixRows(t, repoRoot)

	var gateRows []matrixRow
	for _, r := range rows {
		if r.isGate {
			gateRows = append(gateRows, r)
		}
	}
	if len(gateRows) == 0 {
		t.Fatal("no `gate` rows parsed from docs/spec-scenarios.md §9.3")
	}

	for _, row := range gateRows {
		t.Run(row.id, func(t *testing.T) {
			scPath := filepath.Join(repoRoot, "eval", "behavioral", "scenarios", row.id+".md")
			if _, err := os.Stat(scPath); err != nil {
				t.Fatalf("scenario file for gate row %q: %v", row.id, err)
			}
			sc, err := eval.ParseScenario(scPath)
			if err != nil {
				t.Fatalf("ParseScenario(%s): %v", scPath, err)
			}
			if sc.Verification == nil {
				t.Fatalf("scenario %q: verification block missing entirely", row.id)
			}
			if sc.Verification.Mode != eval.VerificationRequired {
				t.Errorf("scenario %q: verification.mode = %q, want %q", row.id, sc.Verification.Mode, eval.VerificationRequired)
			}
			if sc.Verification.Spec == "" {
				t.Errorf("scenario %q: verification.spec missing", row.id)
			}
			for _, family := range row.requiredFamilies() {
				if !familyPresent(sc.Verification, family) {
					t.Errorf("scenario %q: row names family %q but verification.%s is absent", row.id, family, family)
				}
			}
		})
	}
}

// reflectionExistingFamilies derives, by reflection over
// internal/eval.VerificationConfig's yaml tags, the set of oracle families
// the runner evaluates today (docs/spec-eval-farm.md §4.3 FM-33: never
// hand-listed). `subdomainProbe` is the one documented exception: it is not
// a VerificationConfig field but a nested ExpectedService one, and §9.3's
// own prose lists it as an existing "today" family, so the reflection walk
// extends one level into ExpectedService to find it.
func reflectionExistingFamilies() map[string]bool {
	set := map[string]bool{}
	vt := reflect.TypeFor[eval.VerificationConfig]()
	for i := 0; i < vt.NumField(); i++ {
		tag := vt.Field(i).Tag.Get("yaml")
		name := strings.Split(tag, ",")[0]
		if name != "" && name != "-" {
			set[name] = true
		}
	}
	est := reflect.TypeFor[eval.ExpectedService]()
	for i := 0; i < est.NumField(); i++ {
		tag := est.Field(i).Tag.Get("yaml")
		name := strings.Split(tag, ",")[0]
		if name == "subdomainProbe" {
			set[name] = true
		}
	}
	return set
}

// seedExpectExists reports whether `seedExpect` exists as a runner field
// today: iff internal/eval.Scenario has a yaml-tagged `seed` field whose
// type itself has an `Expect` field. Scenario.Seed is currently a bare
// SeedMode string alias, so this is false — derived, not assumed.
func seedExpectExists() bool {
	st := reflect.TypeFor[eval.Scenario]()
	f, ok := st.FieldByName("Seed")
	if !ok {
		return false
	}
	name := strings.Split(f.Tag.Get("yaml"), ",")[0]
	if name != "seed" {
		return false
	}
	if f.Type.Kind() != reflect.Struct {
		return false
	}
	_, hasExpect := f.Type.FieldByName("Expect")
	return hasExpect
}

// familyExistsAsRunnerField reports whether family is a family the runner
// evaluates today.
func familyExistsAsRunnerField(family string) bool {
	if family == "seedExpect" {
		return seedExpectExists()
	}
	return reflectionExistingFamilies()[family]
}

// TestEvalMatrix_PendingOrWantsFamily_NotYetARunnerField proves every
// `pending: <family>` / `wants: <family>` marker in §9.3 still points at an
// oracle the runner does NOT evaluate today. The moment a marked family
// becomes a runner field, this test fails — the row must be promoted (its
// status/family list updated) or the table corrected; the marker can never
// silently rot (docs/spec-scenarios.md §9.3).
func TestEvalMatrix_PendingOrWantsFamily_NotYetARunnerField(t *testing.T) {
	repoRoot := evalMatrixRepoRoot(t)
	rows := parseMatrixRows(t, repoRoot)

	seen := map[string]bool{}
	for _, row := range rows {
		for _, family := range []string{row.wantsFamily, row.pendingFamily} {
			if family == "" || seen[row.id+"/"+family] {
				continue
			}
			seen[row.id+"/"+family] = true
			if familyExistsAsRunnerField(family) {
				t.Errorf("row %q (%s): family %q already exists as a runner field — "+
					"promote the row: its oracle now exists", row.id, row.statusRaw, family)
			}
		}
	}
}

// oracleFamiliesProseRe extracts the two backtick-fenced family lists out of
// §9.3's own "Oracle families: `...` (today) · `...` (added by the manifest
// v2 slices)." sentence — the "added" list is the independent, spec-sourced
// literal for the not-yet-existing families (never hand-typed here).
var oracleFamiliesProseRe = regexp.MustCompile(
	"(?s)Oracle families:\\s*`([^`]+)`\\s*\\(today\\)\\s*·\\s*`([^`]+)`\\s*\\(added",
)

// futureFamiliesFromProse parses the "(added by the manifest v2 slices)"
// backtick-fenced list out of §9.3's prose.
func futureFamiliesFromProse(t *testing.T, repoRoot string) map[string]bool {
	t.Helper()
	path := filepath.Join(repoRoot, "docs", "spec-scenarios.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := oracleFamiliesProseRe.FindStringSubmatch(string(body))
	if m == nil {
		t.Fatal("docs/spec-scenarios.md §9.3: 'Oracle families: ... (today) · ... (added...)' sentence not found")
	}
	set := map[string]bool{}
	for tok := range strings.FieldsSeq(m[2]) {
		set[tok] = true
	}
	return set
}

// TestEvalMatrix_FamilyNames_AreKnown proves every family token used
// anywhere in §9.3 is either a family the runner evaluates today
// (reflection-derived) or one named in §9.3's own "added by the manifest v2
// slices" future-family list — so a typo in the table (a family name that
// is neither) fails loudly instead of silently never gating anything.
func TestEvalMatrix_FamilyNames_AreKnown(t *testing.T) {
	repoRoot := evalMatrixRepoRoot(t)
	rows := parseMatrixRows(t, repoRoot)

	known := reflectionExistingFamilies()
	for fam := range futureFamiliesFromProse(t, repoRoot) {
		known[fam] = true
	}

	for _, row := range rows {
		for _, family := range row.families {
			if !known[family] {
				t.Errorf("row %q: family %q is neither an existing runner field nor in "+
					"§9.3's future-family list — typo, or the list needs updating", row.id, family)
			}
		}
	}
}

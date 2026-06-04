package content

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSingleOwnerFactsNoDrift enforces the single-owner tripwire registry
// (SingleOwnerFacts). For each registered fact it scans the fact's scope and
// asserts the forbidden fingerprint appears no more than MaxMatches times
// (outside the AllowedLines exemptions). This is the cross-source analogue of
// the atom-tier cross-ref contract (TestAtomCrossRefContract) and the
// architecture layer rule (architecture_test.go): a duplicate / drifted
// authoring of a registered fact becomes a build failure.
//
// Reuses the repo-drift scanner helpers (findRepoRoot / gitTrackedFiles /
// skipForDriftScan / looksLikeText) so the two cross-source lints share one
// file-enumeration contract.
func TestSingleOwnerFactsNoDrift(t *testing.T) {
	t.Parallel()
	repoRoot := findRepoRoot(t)
	for _, fact := range SingleOwnerFacts {
		t.Run(fact.ID, func(t *testing.T) {
			t.Parallel()
			hits := factFingerprintHits(t, repoRoot, fact)
			if len(hits) > fact.MaxMatches {
				t.Errorf("single-owner fact %q drifted: forbidden fingerprint appears %d× (max %d).\n  Owner: %s\n  %s\n  Occurrences:\n    %s",
					fact.ID, len(hits), fact.MaxMatches, fact.Owner, fact.Why, strings.Join(hits, "\n    "))
			}
		})
	}
}

// factFingerprintHits returns the "<rel>:<line>: <snippet>" occurrences of a
// fact's forbidden fingerprint within its scan scope, excluding AllowedLines
// exemptions. Used by both the registry lint and its teeth test.
func factFingerprintHits(t *testing.T, repoRoot string, fact SingleOwnerFact) []string {
	t.Helper()
	var files []string
	if len(fact.ScopeFiles) > 0 {
		files = fact.ScopeFiles
	} else {
		for _, rel := range gitTrackedFiles(t, repoRoot) {
			if !skipForDriftScan(rel) {
				files = append(files, rel)
			}
		}
	}
	allowed := make(map[string]bool, len(fact.AllowedLines))
	for _, a := range fact.AllowedLines {
		allowed[a] = true
	}
	var hits []string
	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			// A ScopeFiles entry that does not exist is a registry bug — surface it.
			if len(fact.ScopeFiles) > 0 {
				hits = append(hits, fmt.Sprintf("%s: <unreadable: %v>", rel, err))
			}
			continue
		}
		if !looksLikeText(body) {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			matched := false
			for _, re := range fact.Forbidden {
				if re.MatchString(line) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if allowed[rel+"::"+strings.TrimSpace(line)] {
				continue
			}
			hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, lineNo, trim(strings.TrimSpace(line), 120)))
		}
	}
	return hits
}

// TestSingleOwnerFactsLintHasTeeth proves the drift lint actually fires — that
// factFingerprintHits matches a fingerprint that is genuinely present, and that
// an AllowedLines exemption clears it. Without this a registry regex that never
// matches (or a broken scanner) would let the lint silently degrade to a no-op
// and report green forever.
func TestSingleOwnerFactsLintHasTeeth(t *testing.T) {
	t.Parallel()
	repoRoot := findRepoRoot(t)
	// A fingerprint guaranteed present in a stable committed file: the package
	// clause of the registry's own source.
	const probeFile = "internal/content/fact_owners.go"
	probe := SingleOwnerFact{
		ID:         "teeth-probe",
		Forbidden:  []*regexp.Regexp{regexp.MustCompile(`^package content$`)},
		ScopeFiles: []string{probeFile},
		MaxMatches: 0,
	}
	hits := factFingerprintHits(t, repoRoot, probe)
	if len(hits) == 0 {
		t.Fatalf("teeth check failed: probe fingerprint `^package content$` matched 0 lines in %s — the scanner is not matching, so the lint is a silent no-op", probeFile)
	}
	// AllowedLines must exempt the real occurrence.
	probe.AllowedLines = []string{probeFile + "::package content"}
	if got := factFingerprintHits(t, repoRoot, probe); len(got) != 0 {
		t.Errorf("teeth check failed: AllowedLines did not exempt the occurrence; got %d residual hits: %v", len(got), got)
	}
}

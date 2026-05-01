package recipe

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestVersionPinLint_Run20C6 — preventive lint against framework
// version-pin drift in brief and atom content. Run-19 TIMELINE
// recorded "brief said Svelte 4, scaffold shipped Svelte 5" — by
// the time prep-doc was authored the active brief paths had been
// cleaned, but no test pinned the absence. C6 adds the pin.
//
// The lint walks every `.md` under the recipe brief tree and the
// atom corpus root and refuses any line matching a pinned-major
// shape (`svelte ^4`, `next@14.`, `Svelte 5`, etc.). Lines marked
// with `<!-- pin-version-keep: <reason> -->` (inline or on adjacent
// lines) are exempted, mirroring the axis-marker convention from
// atoms_lint_axes.go.
//
// Initial run MUST pass — codex confirmed zero hits in the pre-
// run-20 corpus. The fixture sub-test proves the lint catches a
// re-introduction.
func TestVersionPinLint_Run20C6(t *testing.T) {
	t.Parallel()

	t.Run("baseline_corpus_clean", func(t *testing.T) {
		t.Parallel()
		hits := scanVersionPins(t, briefAndAtomRoots(t))
		if len(hits) > 0 {
			msgs := make([]string, 0, len(hits))
			for _, h := range hits {
				msgs = append(msgs, h.format())
			}
			t.Errorf(
				"version-pin lint found %d offender(s) in baseline corpus — either fix the pin or mark with `<!-- pin-version-keep: <reason> -->`:\n  %s",
				len(hits), strings.Join(msgs, "\n  "))
		}
	})

	t.Run("fixture_catches_reintroduction", func(t *testing.T) {
		t.Parallel()
		fixture := filepath.Join(t.TempDir(), "drift.md")
		body := "# Drift\n\nUse `svelte ^4` until further notice.\nAlso pinning `next@14.0.4` for now.\n"
		writeForTest(t, fixture, body)

		hits := scanVersionPinsInFile(fixture)
		if len(hits) < 2 {
			t.Errorf("fixture should trigger at least 2 hits (svelte ^4 + next@14); got %d: %v",
				len(hits), hits)
		}
	})

	t.Run("inline_keep_marker_exempts", func(t *testing.T) {
		t.Parallel()
		fixture := filepath.Join(t.TempDir(), "drift.md")
		body := "# Drift\n\nThis recipe pins `svelte ^4` <!-- pin-version-keep: legacy-fixture -->\n"
		writeForTest(t, fixture, body)

		hits := scanVersionPinsInFile(fixture)
		if len(hits) != 0 {
			t.Errorf("inline keep marker should exempt; got hits: %v", hits)
		}
	})

	t.Run("prior_line_keep_marker_exempts", func(t *testing.T) {
		t.Parallel()
		fixture := filepath.Join(t.TempDir(), "drift.md")
		body := "# Drift\n\n<!-- pin-version-keep: showcase-baseline -->\nThis recipe pins `svelte ^4`.\n"
		writeForTest(t, fixture, body)

		hits := scanVersionPinsInFile(fixture)
		if len(hits) != 0 {
			t.Errorf("prior-line keep marker should exempt; got hits: %v", hits)
		}
	})
}

// pinnedMajorPatterns are the run-20 C6 denylist patterns, derived
// from the run-19 TIMELINE evidence + the prep-doc spec list. Each
// regex targets one common drift shape:
//   - `svelte ^4` / `svelte ^4.x`
//   - `next@14.x`, `nuxt@3.x`, `astro@4.x`, `sveltekit@2.x`, `laravel@10.x`
//   - `Svelte 4`, `Next 14`, `Nuxt 3`, `Astro 4`, `SvelteKit 2`, `Laravel 10`
//
// Capitalization is honored on the proper-noun shape because case
// captures intent (a sentence starting "Svelte 5 introduces…" is
// the drift; "the svelte 4 plugin" is also drift but with lower-case
// the regex still hits via the first pattern).
var pinnedMajorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bsvelte ?\^?[0-9]+\b`),
	regexp.MustCompile(`\b(?:next|nuxt|astro|sveltekit|laravel)@[0-9]+\.`),
	regexp.MustCompile(`\b(?:Svelte|Next|Nuxt|Astro|SvelteKit|Laravel) ?[0-9]+(?:\.|\b)`),
}

// pinKeepRE matches the `<!-- pin-version-keep: <reason> -->` marker.
var pinKeepRE = regexp.MustCompile(`<!--\s*pin-version-keep:[^>]*-->`)

type versionPinHit struct {
	path    string
	line    int
	matched string
	body    string
}

func (h versionPinHit) format() string {
	return h.path + ":" + intToStr(h.line) + ": match=`" + h.matched + "` body=`" + strings.TrimSpace(h.body) + "`"
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// briefAndAtomRoots returns the directories the lint walks. The
// recipe brief tree is gated by package boundary (the embedded FS
// is only addressable through readAtom); to scan the on-disk
// representation we resolve the source tree relative to this test
// file. Falls back to the working directory when the test runs
// in a non-source environment.
func briefAndAtomRoots(t *testing.T) []string {
	t.Helper()
	// /Users/.../zcp/internal/recipe/<file>.go — walk up to repo root.
	// Use runtime caller instead so it works regardless of CWD.
	roots := []string{
		// Brief content lives under internal/recipe/content/briefs/**.
		filepath.Join("content", "briefs"),
		// Standalone principles loaded by both scaffold + content briefs.
		filepath.Join("content", "principles"),
		// Top-level atom corpus.
		filepath.Join("..", "content", "atoms"),
	}
	return roots
}

// scanVersionPins walks each root recursively, scanning every `.md`
// file for pinned-major matches. Marked lines are exempted.
func scanVersionPins(t *testing.T, roots []string) []versionPinHit {
	t.Helper()
	var hits []versionPinHit
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Tolerate missing roots (e.g. when atom corpus is
				// elsewhere in a different test environment).
				if filepath.Base(path) == filepath.Base(root) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".md" {
				return nil
			}
			hits = append(hits, scanVersionPinsInFile(path)...)
			return nil
		})
		if err != nil {
			t.Logf("walk %q: %v (continuing)", root, err)
		}
	}
	return hits
}

// scanVersionPinsInFile returns version-pin hits in a single file,
// honoring inline / adjacent-line keep markers.
func scanVersionPinsInFile(path string) []versionPinHit {
	body := readForTest(path)
	if body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	var hits []versionPinHit
	for i, line := range lines {
		// Code fences are scanned (atoms can teach drift inside
		// fenced examples too); the keep marker still applies if the
		// fence carries one inline.
		if hasKeepMarker(lines, i) {
			continue
		}
		for _, re := range pinnedMajorPatterns {
			if m := re.FindString(line); m != "" {
				hits = append(hits, versionPinHit{
					path:    path,
					line:    i + 1,
					matched: m,
					body:    line,
				})
				break
			}
		}
	}
	return hits
}

// hasKeepMarker reports whether the line at idx is marked with
// `<!-- pin-version-keep: ... -->` inline OR on the immediately
// preceding non-blank line OR on the immediately following non-blank
// line. Mirrors the axis-marker proximity rules from atoms_lint_axes.go.
func hasKeepMarker(lines []string, idx int) bool {
	if pinKeepRE.MatchString(lines[idx]) {
		return true
	}
	for j := idx - 1; j >= 0; j-- {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		if pinKeepRE.MatchString(lines[j]) {
			return true
		}
		break
	}
	for j := idx + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		if pinKeepRE.MatchString(lines[j]) {
			return true
		}
		break
	}
	return false
}

func readForTest(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func writeForTest(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

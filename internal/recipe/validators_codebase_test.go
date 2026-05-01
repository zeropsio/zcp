package recipe

import (
	"strings"
	"testing"
)

// TestScanAuthoringToolLeaks_ZSCInYamlFenceAllowed — run-23 fix-4. The
// `zsc execOnce …` / `zsc noop --silent` directive verbatim inside a
// fenced ```yaml block is the literal start command from zerops.yaml,
// not authoring-tool prose. Validator must NOT flag it as a leak.
func TestScanAuthoringToolLeaks_ZSCInYamlFenceAllowed(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		"## Step 1 — boot",
		"",
		"```yaml",
		"run:",
		"  start: zsc noop --silent",
		"  initCommands:",
		"    - zsc execOnce migrate-once -- npm run migrate",
		"```",
		"",
		"After boot you SSH in and run `npm run start:dev`.",
	}, "\n")
	vs := scanAuthoringToolLeaks("path", body, "codebase IG")
	for _, v := range vs {
		if strings.Contains(v.Message, "zsc") {
			t.Errorf("zsc inside yaml fence must not be flagged; got %+v", v)
		}
	}
}

// TestScanAuthoringToolLeaks_ZSCInProseRefuses — run-23 fix-4. When
// `zsc noop` appears in markdown PROSE (outside any yaml fence), it's
// authoring perspective leaking to the porter. Validator must flag it.
func TestScanAuthoringToolLeaks_ZSCInProseRefuses(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		"## Why this works",
		"",
		"The agent uses `zsc noop --silent` to keep the container alive",
		"so SSH stays open during dev.",
	}, "\n")
	vs := scanAuthoringToolLeaks("path", body, "codebase IG")
	found := false
	for _, v := range vs {
		if strings.Contains(v.Message, "zsc") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected zsc-leak violation for prose mention; got %+v", vs)
	}
}

// TestScanAuthoringToolLeaks_ZeropsToolStillFlaggedInsideFence —
// existing zerops_* / zcli / zcp tokens stay flagged everywhere, fence
// or not. The fence carve-out is `zsc`-only — tool-name leaks like
// `zerops_browser` inside a yaml block still indicate authoring drift
// (they aren't legitimate yaml syntax).
func TestScanAuthoringToolLeaks_ZeropsToolStillFlaggedInsideFence(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		"```yaml",
		"# tested via zerops_browser",
		"run:",
		"  start: node server.js",
		"```",
	}, "\n")
	vs := scanAuthoringToolLeaks("path", body, "codebase IG")
	found := false
	for _, v := range vs {
		if strings.Contains(v.Message, "zerops_browser") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("zerops_browser leak inside fence must still flag; got %+v", vs)
	}
}

// TestScanAuthoringToolLeaks_ZeropsToolInProseStillFlagged ensures the
// pre-existing behavior (any `zerops_*` mention is a leak) survives the
// fence-aware refactor.
func TestScanAuthoringToolLeaks_ZeropsToolInProseStillFlagged(t *testing.T) {
	t.Parallel()
	body := "Run `zerops_subdomain action=enable` after deploy."
	vs := scanAuthoringToolLeaks("path", body, "codebase IG")
	found := false
	for _, v := range vs {
		if strings.Contains(v.Message, "zerops_subdomain") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected zerops_subdomain leak in prose; got %+v", vs)
	}
}

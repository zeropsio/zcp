package tools

import (
	"strings"
	"testing"
)

func TestCheckClaudeMdNoBurnTrapFolk_CleanContent_Passes(t *testing.T) {
	t.Parallel()

	clean := []string{
		"",
		"### Gotchas\n\nNothing to see here.",
		"execOnce is keyed on appVersionId; each deploy gets a fresh one.",
		// Mentions burn but nowhere near execOnce → not folk doctrine
		"The container's fuel budget is burned by long builds.",
		// execOnce with clarifying text, no burn talk
		"execOnce runs once per deploy (per appVersionId) — idempotent by key.",
	}
	for _, body := range clean {
		got := checkClaudeMdNoBurnTrapFolk(body, "apidev")
		for _, c := range got {
			if c.Status == statusFail {
				t.Errorf("expected pass for %q, got fail: %+v", body, c)
			}
		}
	}
}

func TestCheckClaudeMdNoBurnTrapFolk_BurnNearExecOnce_Fails(t *testing.T) {
	t.Parallel()

	folk := []string{
		"execOnce burn trap: the first deploy burns the lock",
		"Burn trap — execOnce has already burned when you retry",
		"avoid the execOnce burn-trap by clearing state first",
		"the execOnce key was burned by a prior deploy",
		// Reversed order still counts
		"burn trap for execOnce scripts",
	}
	for _, body := range folk {
		got := checkClaudeMdNoBurnTrapFolk(body, "apidev")
		if len(got) == 0 {
			t.Errorf("expected fail for %q, got no checks", body)
			continue
		}
		if got[0].Status != statusFail {
			t.Errorf("expected fail for %q, got %s", body, got[0].Status)
		}
		if !strings.Contains(got[0].Name, "no_burn_trap_folk") {
			t.Errorf("unexpected name %q", got[0].Name)
		}
	}
}

func TestCheckClaudeMdNoBurnTrapFolk_BurnFarFromExecOnce_Passes(t *testing.T) {
	t.Parallel()

	// Burn and execOnce both present but >100 chars apart, split by many
	// unrelated paragraphs — should not trigger folk doctrine.
	body := "The CI fuel burned fast this sprint.\n\n" +
		strings.Repeat("Another paragraph about something unrelated. ", 10) +
		"\n\nexecOnce is the Zerops initCommands idempotency primitive."
	got := checkClaudeMdNoBurnTrapFolk(body, "apidev")
	for _, c := range got {
		if c.Status == statusFail {
			t.Errorf("expected pass for far-apart mentions, got fail: %+v", c)
		}
	}
}

func TestCheckClaudeMdNoBurnTrapFolk_NameIncludesHostname(t *testing.T) {
	t.Parallel()
	got := checkClaudeMdNoBurnTrapFolk("execOnce burn trap explained", "workerdev")
	if len(got) == 0 {
		t.Fatal("expected check result")
	}
	if !strings.HasPrefix(got[0].Name, "workerdev_") {
		t.Errorf("expected hostname-prefixed name, got %q", got[0].Name)
	}
}

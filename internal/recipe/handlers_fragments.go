package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// Fragment-recording logic lives here (split from handlers.go at
// run-9-readiness commit 10 / J). The handler dispatcher calls
// recordFragment; this file owns the validation + storage machinery.

// recordFragment validates the fragment id against the plan, applies
// append-or-overwrite semantics, and stores the body on plan.Fragments
// (or on the typed EnvComments for env/*/import-comments/* ids).
// Returns the post-write body size and whether append fired — run-9-
// readiness §2.J so the caller sees which fragment landed.
//
// mode is "" or "append" (default; codebase IG/KB/claude-md ids
// concatenate) or "replace" (overwrite prior body even on append-class
// ids). Run-12 §R — sub-agent uses replace to correct its own fragment
// after a complete-phase validator violation.
func recordFragment(sess *Session, id, body, mode string) (int, bool, error) {
	switch mode {
	case "", "append", "replace":
	default:
		return 0, false, fmt.Errorf("record-fragment: mode must be 'append' or 'replace', got %q", mode)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.Plan == nil {
		return 0, false, errors.New("record-fragment: plan not initialized — call update-plan first")
	}
	if err := validateFragmentID(sess.Plan, id); err != nil {
		return 0, false, fmt.Errorf("record-fragment: %w", err)
	}
	if strings.HasPrefix(id, "env/") && strings.Contains(id, "/import-comments/") {
		if err := applyEnvComment(sess.Plan, id, body); err != nil {
			return 0, false, err
		}
		return len(body), false, nil
	}
	if sess.Plan.Fragments == nil {
		sess.Plan.Fragments = map[string]string{}
	}
	if isAppendFragmentID(id) && mode != "replace" {
		existing := sess.Plan.Fragments[id]
		if existing == "" {
			sess.Plan.Fragments[id] = body
			return len(body), false, nil
		}
		combined := existing + "\n\n" + body
		sess.Plan.Fragments[id] = combined
		return len(combined), true, nil
	}
	sess.Plan.Fragments[id] = body
	return len(body), false, nil
}

// applyEnvComment routes env/<N>/import-comments/<target> into the
// typed plan.EnvComments map so the yaml emitter reads writer-authored
// comments without a separate fragment-consumption layer.
func applyEnvComment(plan *Plan, id, body string) error {
	// id = "env/<N>/import-comments/<target>"
	rest := strings.TrimPrefix(id, "env/")
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return fmt.Errorf("record-fragment: malformed env id %q", id)
	}
	tierKey := rest[:slash]
	target := strings.TrimPrefix(rest[slash+1:], "import-comments/")
	if plan.EnvComments == nil {
		plan.EnvComments = map[string]EnvComments{}
	}
	ec := plan.EnvComments[tierKey]
	if target == "project" {
		ec.Project = body
	} else {
		if ec.Service == nil {
			ec.Service = map[string]string{}
		}
		ec.Service[target] = body
	}
	plan.EnvComments[tierKey] = ec
	return nil
}

// isAppendFragmentID reports whether an id uses append-on-extend
// semantics. Per plan §2.A.4: feature sub-agent extends IG, KB, and
// CLAUDE.md sections; root and env overwrite (main agent authors once).
func isAppendFragmentID(id string) bool {
	if !strings.HasPrefix(id, "codebase/") {
		return false
	}
	switch {
	case strings.HasSuffix(id, "/integration-guide"):
		return true
	case strings.HasSuffix(id, "/knowledge-base"):
		return true
	case strings.Contains(id, "/claude-md/"):
		return true
	}
	return false
}

// fragmentIDRoot is the only root-scoped fragment id. Constants prevent
// a typo here from silently diverging from the assembler's marker id.
const fragmentIDRoot = "root/intro"

// validateFragmentID returns nil for a recognized fragment id, an
// actionable error otherwise. Wraps isValidFragmentID so the codebase/
// case can surface the slot-vs-codebase distinction (run-11 gap N-1).
func validateFragmentID(plan *Plan, id string) error {
	if rest, ok := strings.CutPrefix(id, "codebase/"); ok {
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			return fmt.Errorf("malformed codebase fragmentId %q (expected codebase/<hostname>/<name>)", id)
		}
		host := rest[:slash]
		if err := validateCodebaseHostname(plan, host); err != nil {
			return fmt.Errorf("fragmentId %q: %w", id, err)
		}
	}
	if isValidFragmentID(plan, id) {
		return nil
	}
	return fmt.Errorf("unknown fragmentId %q", id)
}

// isValidFragmentID reports whether id matches one of the declared
// fragment shapes given the plan's codebases. Covers root/, env/<N>/,
// env/<N>/import-comments/<hostname|project>, codebase/<hostname>/...
func isValidFragmentID(plan *Plan, id string) bool {
	if id == fragmentIDRoot {
		return true
	}
	if rest, ok := strings.CutPrefix(id, "env/"); ok {
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			return false
		}
		tierIdx, err := parseTierIndex(rest[:slash])
		if err != nil {
			return false
		}
		if _, ok := TierAt(tierIdx); !ok {
			return false
		}
		tail := rest[slash+1:]
		switch {
		case tail == "intro":
			return true
		case tail == "import-comments/project":
			return true
		case strings.HasPrefix(tail, "import-comments/"):
			host := strings.TrimPrefix(tail, "import-comments/")
			return codebaseKnown(plan, host) || serviceKnown(plan, host)
		}
		return false
	}
	if rest, ok := strings.CutPrefix(id, "codebase/"); ok {
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			return false
		}
		host := rest[:slash]
		if !codebaseKnown(plan, host) {
			return false
		}
		tail := rest[slash+1:]
		switch tail {
		case "intro", "integration-guide", "knowledge-base",
			"claude-md/service-facts", "claude-md/notes":
			return true
		}
		return false
	}
	return false
}

// parseTierIndex returns the numeric tier index parsed from a string
// key; returns an error on any non-numeric input.
func parseTierIndex(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

// serviceKnown reports whether a hostname matches one of the plan's
// managed services. Env import-comments may address a managed service
// block (db, cache, storage) in addition to runtime codebases.
func serviceKnown(plan *Plan, hostname string) bool {
	if plan == nil {
		return false
	}
	for _, s := range plan.Services {
		if s.Hostname == hostname {
			return true
		}
	}
	return false
}

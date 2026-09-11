package observer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
)

// NotRecorded is what an absent optional input file renders as, wherever it
// would otherwise appear (§7.2).
// NotRecorded is what every surface prints for an optional bundle file
// the run did not produce (§7.2).
const NotRecorded = "(not recorded)"

// firstN returns the first n runes of s (or all of s when shorter).
// Operates on runes, not bytes, so it never splits a multi-byte UTF-8
// sequence.
func firstN(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// truncateHead keeps s's first head characters, marking the cut with the
// omitted char count, when s is longer than head. Used for a thinking block
// or a tool input over 2,000 characters (§7.3). Operates on runes, so a
// multi-byte character is never split.
func truncateHead(s string, head int) string {
	r := []rune(s)
	if len(r) <= head {
		return s
	}
	omitted := len(r) - head
	return string(r[:head]) + omittedMarker(omitted)
}

// truncateHeadTail keeps s's first head and last tail characters, marking
// the cut with the omitted char count, when s is longer than head+tail.
// Used for a tool result over 6,000 (or, for an error result, 12,000)
// characters (§7.3). Operates on runes, so a multi-byte character is never
// split.
func truncateHeadTail(s string, head, tail int) string {
	r := []rune(s)
	if len(r) <= head+tail {
		return s
	}
	omitted := len(r) - head - tail
	return string(r[:head]) + omittedMarker(omitted) + string(r[len(r)-tail:])
}

func omittedMarker(n int) string {
	return fmt.Sprintf("[… %d chars omitted …]", n)
}

// collapseWhitespace collapses every whitespace run to one space and trims
// the ends (docs/spec-eval-farm.md §7.5 FM-46's quote-check normalization,
// observation.go's verifyQuote).
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// digestBudget is the digest's overall character budget (§7.3): once the
// assembled digest would exceed it, whole steps are dropped from the middle
// of STEPS.
const digestBudget = 400000

// DigestInput is everything BuildDigest needs to assemble one run's digest
// (§7.3). Zero-valued ScenarioMD/FinalState/SelfReview render as
// NotRecorded — the caller is responsible for resolving each optional
// input file to either its content or that zero value (§7.2).
type DigestInput struct {
	RunID      string
	ScenarioID string
	Verdict    string
	Duration   time.Duration
	CostUsd    float64
	Model      string

	TaskPrompt string
	ScenarioMD string // "" -> NotRecorded

	Checks []eval.RequiredCheck

	Steps []Step

	FinalState *eval.PlatformSnapshot // nil -> NotRecorded
	SelfReview string                 // "" -> NotRecorded
}

// BuildDigest assembles the observer's plain-text digest (§7.3): fixed
// sections in order RUN, TASK, SCENARIO, CHECKS, STEPS, FINAL STATE,
// SELF-REVIEW, each step per-line truncated, the whole bounded to
// digestBudget by dropping middle steps. Returns the digest text and its
// sha256 (lowercase hex) — the observation's recorded digestSha256 (§7.5).
func BuildDigest(in DigestInput) (text string, sha256Hex string) {
	head := runSection(in) + "\n\n" +
		taskSection(in) + "\n\n" +
		scenarioSection(in) + "\n\n" +
		checksSection(in) + "\n\n"
	tail := "\n\n" + finalStateSection(in) + "\n\n" + selfReviewSection(in) + "\n"

	stepsBudget := max(digestBudget-len(head)-len(tail)-len("=== STEPS ===\n"), 0)
	stepsBody := buildStepsSection(in.Steps, stepsBudget)

	full := head + "=== STEPS ===\n" + stepsBody + tail
	sum := sha256.Sum256([]byte(full))
	return full, hex.EncodeToString(sum[:])
}

func runSection(in DigestInput) string {
	return fmt.Sprintf(
		"=== RUN ===\nrunId: %s\nscenarioId: %s\nverdict: %s\nduration: %s\ncost: $%.4f\nmodel: %s",
		in.RunID, in.ScenarioID, in.Verdict, in.Duration, in.CostUsd, in.Model,
	)
}

func taskSection(in DigestInput) string {
	return "=== TASK ===\n" + in.TaskPrompt
}

func scenarioSection(in DigestInput) string {
	body := in.ScenarioMD
	if body == "" {
		body = NotRecorded
	}
	return "=== SCENARIO (the agent never saw this file) ===\n" + body
}

func checksSection(in DigestInput) string {
	return "=== CHECKS ===\n" + ChecksBody(in.Checks)
}

// ChecksBody renders the CHECKS section's body — every verification.json
// row: id, result, expected, observed, source — without the section
// header. Absent (nil/empty) checks render NotRecorded, never a fatal
// observation: a run that died before the verdict freeze has no
// verification.json, and those are exactly the runs worth observing.
// Exported so the quote check (§7.5 FM-46) can verify a step-0 citation
// against exactly the text the model saw under CHECKS.
func ChecksBody(checks []eval.RequiredCheck) string {
	if len(checks) == 0 {
		return NotRecorded
	}
	var b strings.Builder
	for i, c := range checks {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s %s expected=%q observed=%q source=%s", c.ID, c.Result, c.Expected, c.Observed, c.Source)
	}
	return b.String()
}

func finalStateSection(in DigestInput) string {
	if in.FinalState == nil || len(in.FinalState.Services) == 0 {
		return "=== FINAL STATE ===\n" + NotRecorded
	}
	var b strings.Builder
	b.WriteString("=== FINAL STATE ===\n")
	for i, s := range in.FinalState.Services {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s %s", s.Hostname, s.Status)
	}
	return b.String()
}

func selfReviewSection(in DigestInput) string {
	body := in.SelfReview
	if body == "" {
		body = NotRecorded
	}
	return "=== SELF-REVIEW (written by the agent after the run, from its own memory) ===\n" + body
}

// stepLine renders one step, per-line truncated per §7.3's numbers.
func stepLine(s Step) string {
	switch s.Kind {
	case StepTool:
		input := truncateHead(s.ToolInputJSON, 2000)
		line := fmt.Sprintf("#%d tool %s %s", s.N, s.ToolName, input)
		switch {
		case !s.ToolHasResult:
			line += "\n  → " + s.ToolResultText
		case s.ToolIsError:
			line += "\n  → ERROR " + truncateHeadTail(s.ToolResultText, 8000, 4000)
		default:
			line += "\n  → " + truncateHeadTail(s.ToolResultText, 4000, 2000)
		}
		return line
	case StepThinking:
		return fmt.Sprintf("#%d thinking %s", s.N, truncateHead(s.Text, 2000))
	case StepUser, StepAgent:
		return fmt.Sprintf("#%d %s %s", s.N, s.Kind, s.Text)
	default:
		return fmt.Sprintf("#%d %s %s", s.N, s.Kind, s.Text)
	}
}

// buildStepsSection joins every step's rendered line, dropping whole steps
// from the middle (never the head or tail) when the joined text would
// exceed budget (§7.3). budget may be negative (impossibly tight
// non-STEPS sections); in that case every step is dropped down to just the
// first and last.
func buildStepsSection(steps []Step, budget int) string {
	if len(steps) == 0 {
		return ""
	}
	lines := make([]string, len(steps))
	for i, s := range steps {
		lines[i] = stepLine(s)
	}

	const sep = 2 // len("\n\n") between joined lines
	full := 0
	for i, l := range lines {
		full += len(l)
		if i > 0 {
			full += sep
		}
	}
	if full <= budget {
		return strings.Join(lines, "\n\n")
	}

	n := len(lines)
	lo, hi := n/2, n/2 // dropped range [lo,hi), empty to start
	for {
		kept := 0
		first := true
		for i, l := range lines {
			if i >= lo && i < hi {
				continue
			}
			if !first {
				kept += sep
			}
			kept += len(l)
			first = false
		}
		var marker string
		if hi > lo {
			marker = fmt.Sprintf("[… steps #%d–#%d omitted …]", steps[lo].N, steps[hi-1].N)
			if !first {
				kept += sep
			}
			kept += len(marker)
		}
		if kept <= budget || (lo == 0 && hi == n) {
			out := make([]string, 0, n-((hi-lo)-1))
			out = append(out, lines[:lo]...)
			if hi > lo {
				out = append(out, marker)
			}
			out = append(out, lines[hi:]...)
			return strings.Join(out, "\n\n")
		}
		if lo > 0 {
			lo--
		}
		if hi < n {
			hi++
		}
	}
}

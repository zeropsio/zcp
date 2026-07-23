package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/skillpacks"
)

// skillsCommandTimeout bounds one whole `zcp skills` invocation (clone,
// discovery, copy, publish) — generous for a shallow clone of a curated
// skills repo, short enough that a hung network call cannot wedge the CLI
// forever.
const skillsCommandTimeout = 5 * time.Minute

// mutationJSON is the exact --json shape for pack-add/pack-remove, per
// docs/spec-welcome-mode.md's CLI contract: exactly one bounded object on
// stdout, on success or failure alike.
type mutationJSON struct {
	Version    int      `json:"version"`
	OK         bool     `json:"ok"`
	Operation  string   `json:"operation"`
	PackID     string   `json:"packId"`
	Code       string   `json:"code,omitempty"`
	Message    string   `json:"message"`
	State      string   `json:"state"`
	Commit     string   `json:"commit"`
	SkillCount int      `json:"skillCount"`
	Changed    bool     `json:"changed"`
	Warnings   []string `json:"warnings"`
}

// statusEntryJSON is one pack's entry in `pack-status --json`.
type statusEntryJSON struct {
	ID         string   `json:"id"`
	State      string   `json:"state"`
	Managed    bool     `json:"managed"`
	Retired    bool     `json:"retired"`
	Commit     string   `json:"commit"`
	SkillCount int      `json:"skillCount"`
	Warnings   []string `json:"warnings"`
}

type statusJSON struct {
	Version int               `json:"version"`
	Packs   []statusEntryJSON `json:"packs"`
}

// runSkills handles `zcp skills pack-add|pack-remove|pack-status`, run with
// cwd set to the project workspace. Human-mode progress goes to stderr; the
// result summary (human mode) or the single JSON object (--json mode) goes
// to stdout — cmd/ is exempt from the JSON-only-stdout rule that binds the
// MCP STDIO path.
func runSkills(args []string) int {
	if len(args) < 1 || isHelpArg(args[0]) {
		printSkillsUsage()
		if len(args) == 0 {
			return 1
		}
		return 0
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "pack-add":
		return runSkillsPackMutate("add", rest)
	case "pack-remove":
		return runSkillsPackMutate("remove", rest)
	case "pack-status":
		return runSkillsPackStatus(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown skills subcommand: %s\n", sub)
		printSkillsUsage()
		return 1
	}
}

// parseSkillsArgs splits rest into a single optional/required positional id
// plus the --json flag, rejecting any unknown flag or extra positional
// argument outright rather than silently ignoring it.
func parseSkillsArgs(rest []string, idRequired bool) (id string, jsonMode bool, err error) {
	var positionals []string
	for _, a := range rest {
		switch {
		case a == "--json":
			jsonMode = true
		case strings.HasPrefix(a, "-"):
			return "", false, fmt.Errorf("unknown flag %q", a)
		default:
			positionals = append(positionals, a)
		}
	}
	switch {
	case len(positionals) == 0 && idRequired:
		return "", jsonMode, fmt.Errorf("missing <id>")
	case len(positionals) == 0:
		return "", jsonMode, nil
	case len(positionals) == 1:
		return positionals[0], jsonMode, nil
	default:
		return "", false, fmt.Errorf("unexpected extra argument(s): %s", strings.Join(positionals[1:], " "))
	}
}

func runSkillsPackMutate(op string, rest []string) int {
	id, jsonMode, err := parseSkillsArgs(rest, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp skills pack-%s: %v\n", op, err)
		fmt.Fprintf(os.Stderr, "usage: zcp skills pack-%s <id> [--json]\n", op)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), skillsCommandTimeout)
	defer cancel()

	var result skillpacks.Result
	var opErr error
	switch op {
	case "add":
		if !jsonMode {
			fmt.Fprintf(os.Stderr, "installing skill pack %q...\n", id)
		}
		result, opErr = skillpacks.Add(ctx, ".", id)
	case "remove":
		if !jsonMode {
			fmt.Fprintf(os.Stderr, "removing skill pack %q...\n", id)
		}
		result, opErr = skillpacks.Remove(ctx, ".", id)
	}

	if jsonMode {
		printMutationJSON(result)
	} else {
		printMutationHuman(op, result, opErr)
	}
	if !result.OK() {
		return 1
	}
	return 0
}

func printMutationJSON(result skillpacks.Result) {
	warnings := result.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	out := mutationJSON{
		Version:    1,
		OK:         result.OK(),
		Operation:  result.Operation,
		PackID:     result.PackID,
		Code:       result.Code,
		Message:    result.Message,
		State:      string(result.State),
		Commit:     result.Commit,
		SkillCount: result.SkillCount,
		Changed:    result.Changed,
		Warnings:   warnings,
	}
	data, err := json.Marshal(out)
	if err != nil {
		// Marshaling a fixed, all-primitive struct cannot fail; if it
		// somehow does, still emit exactly one bounded JSON object.
		fmt.Fprintf(os.Stdout, `{"version":1,"ok":false,"code":"internal","message":%q}`+"\n", err.Error())
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

func printMutationHuman(op string, result skillpacks.Result, err error) {
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp skills pack-%s: %v\n", op, err)
		return
	}
	if result.Message != "" {
		fmt.Fprintln(os.Stdout, result.Message)
		return
	}
	if !result.Changed {
		fmt.Fprintf(os.Stdout, "%s: no changes (already %s)\n", result.PackID, result.State)
		return
	}
	fmt.Fprintf(os.Stdout, "%s: %d skill(s) %sed from %s (%s)\n", result.PackID, result.SkillCount, op, result.PackID, result.Commit)
}

func runSkillsPackStatus(rest []string) int {
	id, jsonMode, err := parseSkillsArgs(rest, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp skills pack-status: %v\n", err)
		fmt.Fprintln(os.Stderr, "usage: zcp skills pack-status [<id>] [--json]")
		return 1
	}

	var statuses []skillpacks.PackStatus
	if id == "" {
		statuses, err = skillpacks.StatusAll(".")
	} else {
		var st skillpacks.PackStatus
		st, err = skillpacks.Status(".", id)
		statuses = []skillpacks.PackStatus{st}
	}
	if err != nil {
		if jsonMode {
			code, message := skillsStatusErrorCode(err)
			fmt.Fprintf(os.Stdout, `{"version":1,"code":%q,"message":%q}`+"\n", code, message)
		} else {
			fmt.Fprintf(os.Stderr, "zcp skills pack-status: %v\n", err)
		}
		return 1
	}

	if jsonMode {
		printStatusJSON(statuses)
		return 0
	}
	printStatusHuman(statuses)
	return 0
}

// skillsStatusErrorCode extracts a stable code for a pack-status failure —
// almost always an unexpected filesystem/workspace error, since Status
// itself has no collision/local-changes-style mutation outcomes.
func skillsStatusErrorCode(err error) (code, message string) {
	var ce *skillpacks.CodedError
	if errors.As(err, &ce) {
		return ce.Code, ce.Message
	}
	return "internal", err.Error()
}

func printStatusJSON(statuses []skillpacks.PackStatus) {
	entries := make([]statusEntryJSON, 0, len(statuses))
	for _, s := range statuses {
		warnings := s.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		entries = append(entries, statusEntryJSON{
			ID: s.ID, State: string(s.State), Managed: s.Managed, Retired: s.Retired,
			Commit: s.Commit, SkillCount: s.SkillCount, Warnings: warnings,
		})
	}
	data, err := json.Marshal(statusJSON{Version: 1, Packs: entries})
	if err != nil {
		fmt.Fprintf(os.Stdout, `{"version":1,"packs":[]}`+"\n")
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

func printStatusHuman(statuses []skillpacks.PackStatus) {
	for _, s := range statuses {
		retired := ""
		if s.Retired {
			retired = " (retired)"
		}
		fmt.Fprintf(os.Stdout, "%s%s: %s, %d skill(s)\n", s.ID, retired, s.State, s.SkillCount)
		for _, w := range s.Warnings {
			fmt.Fprintf(os.Stdout, "  - %s\n", w)
		}
	}
}

func printSkillsUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp skills <subcommand> [<id>] [--json]

Subcommands:
  pack-add    <id>   Install a curated community skill pack into .agents/skills/ and .claude/skills/
  pack-remove <id>   Uninstall a previously installed skill pack (preserves local edits)
  pack-status [<id>] Report the install state of one or every known skill pack

Valid ids:
  `+strings.Join(skillpacks.ValidIDs(), ", ")+`

Examples:
  zcp skills pack-add superpowers
  zcp skills pack-status --json
  zcp skills pack-remove superpowers`)
}

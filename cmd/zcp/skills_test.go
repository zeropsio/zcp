package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Non-parallel throughout: t.Chdir rebases the process cwd runSkills
// resolves "." against, and captureOutput swaps the process-global
// os.Stdout/os.Stderr — neither is safe under t.Parallel().
//
// Every case here is reachable without any network access: an unknown pack
// id fails before Add ever clones, and Remove/Status on an absent pack
// never touch git at all — the actual clone/discover/publish/remove
// behavior is exhaustively covered by internal/skillpacks's own test suite
// against local git fixtures. These tests exercise only the CLI adapter:
// argument parsing, usage/help text, exit codes, and the --json envelope
// shape.

func TestRunSkills_NoArgs_PrintsUsageAndErrors(t *testing.T) {
	var code int
	_, stderr := captureOutput(t, func() { code = runSkills(nil) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Usage: zcp skills") {
		t.Errorf("stderr = %q, want it to contain usage text", stderr)
	}
}

func TestRunSkills_HelpArg_PrintsUsageExitsZero(t *testing.T) {
	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"--help"}) })
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "Usage: zcp skills") {
		t.Errorf("stderr = %q, want it to contain usage text", stderr)
	}
}

func TestRunSkills_UnknownSubcommand_Errors(t *testing.T) {
	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"pack-frobnicate", "superpowers"}) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "unknown skills subcommand") {
		t.Errorf("stderr = %q, want it to name the unknown subcommand", stderr)
	}
}

func TestRunSkills_MissingID_Errors(t *testing.T) {
	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"pack-add"}) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "usage: zcp skills pack-add") {
		t.Errorf("stderr = %q, want a pack-add usage line", stderr)
	}
}

func TestRunSkills_PackAdd_ExtraArgument_RejectedWithoutNetwork(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"pack-add", "superpowers", "extra-arg"}) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "extra-arg") {
		t.Errorf("stderr = %q, want it to name the rejected extra argument", stderr)
	}
}

func TestRunSkills_PackAdd_UnknownFlag_RejectedWithoutNetwork(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"pack-add", "superpowers", "--bogus"}) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "--bogus") {
		t.Errorf("stderr = %q, want it to name the unknown flag", stderr)
	}
}

func TestRunSkills_PackAdd_UnknownID_ErrorsWithoutNetwork(t *testing.T) {
	t.Chdir(t.TempDir())

	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"pack-add", "not-a-real-pack"}) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "not-a-real-pack") {
		t.Errorf("stderr = %q, want it to name the unknown id", stderr)
	}
}

func TestRunSkills_PackAdd_UnknownID_JSONMode(t *testing.T) {
	t.Chdir(t.TempDir())

	var code int
	stdout, _ := captureOutput(t, func() { code = runSkills([]string{"pack-add", "not-a-real-pack", "--json"}) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	var got mutationJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\nstdout: %s", err, stdout)
	}
	if got.OK {
		t.Error("ok = true, want false")
	}
	if got.Code != "unknown-pack" {
		t.Errorf("code = %q, want %q", got.Code, "unknown-pack")
	}
	if got.Operation != "add" || got.PackID != "not-a-real-pack" {
		t.Errorf("operation/packId = %q/%q, want add/not-a-real-pack", got.Operation, got.PackID)
	}
	if got.Warnings == nil {
		t.Error("warnings should be an empty array, not null")
	}
}

func TestRunSkills_PackRemove_AbsentPack_NoopSuccess(t *testing.T) {
	t.Chdir(t.TempDir())

	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"pack-remove", "superpowers"}) })
	if code != 0 {
		t.Fatalf("code = %d, want 0, stderr: %s", code, stderr)
	}
}

func TestRunSkills_PackRemove_AbsentPack_JSONMode(t *testing.T) {
	t.Chdir(t.TempDir())

	var code int
	stdout, _ := captureOutput(t, func() { code = runSkills([]string{"pack-remove", "superpowers", "--json"}) })
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	var got mutationJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\nstdout: %s", err, stdout)
	}
	if !got.OK {
		t.Error("ok = false, want true (absent pack-remove is a successful no-op)")
	}
	if got.Changed {
		t.Error("changed = true, want false (nothing was installed)")
	}
	if got.State != "absent" {
		t.Errorf("state = %q, want %q", got.State, "absent")
	}
}

func TestRunSkills_PackRemove_ExtraArgument_Rejected(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"pack-remove", "superpowers", "extra"}) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "extra") {
		t.Errorf("stderr = %q, want it to name the rejected extra argument", stderr)
	}
}

func TestRunSkills_PackStatus_NoArgs_ListsEveryCatalogPack(t *testing.T) {
	t.Chdir(t.TempDir())

	var code int
	stdout, _ := captureOutput(t, func() { code = runSkills([]string{"pack-status", "--json"}) })
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	var got statusJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\nstdout: %s", err, stdout)
	}
	if len(got.Packs) < 3 {
		t.Fatalf("packs = %v, want at least the 3 catalog packs", got.Packs)
	}
	for _, p := range got.Packs {
		if p.State != "absent" {
			t.Errorf("pack %q state = %q, want absent (nothing installed)", p.ID, p.State)
		}
		if p.Managed {
			t.Errorf("pack %q managed = true, want false", p.ID)
		}
	}
}

func TestRunSkills_PackStatus_SingleUnknownID(t *testing.T) {
	t.Chdir(t.TempDir())

	var code int
	stdout, _ := captureOutput(t, func() { code = runSkills([]string{"pack-status", "totally-unknown-id", "--json"}) })
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	var got statusJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\nstdout: %s", err, stdout)
	}
	if len(got.Packs) != 1 {
		t.Fatalf("packs = %v, want exactly 1", got.Packs)
	}
	if got.Packs[0].State != "absent" {
		t.Errorf("state = %q, want absent", got.Packs[0].State)
	}
}

// TestSkillsPackSet_MissingExpectedRevision_UsageError proves
// --expected-revision is mandatory: its absence is a usage error, not a
// defaulted/forced apply (spec-skill-packs.md §3.1).
func TestSkillsPackSet_MissingExpectedRevision_UsageError(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	_, stderr := captureOutput(t, func() {
		code = runSkills([]string{"pack-set", "matt-pocock-skills", "--skills", "tdd"})
	})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "--expected-revision") {
		t.Errorf("stderr = %q, want it to name the missing --expected-revision flag", stderr)
	}
	if !strings.Contains(stderr, "usage: zcp skills pack-set") {
		t.Errorf("stderr = %q, want a pack-set usage line", stderr)
	}
}

// TestSkillsPackSet_JSONResult_ConflictShape proves a stale-revision
// pack-set emits the same single bounded --json envelope as every other
// skills mutation, with a stable "conflict" code.
func TestSkillsPackSet_JSONResult_ConflictShape(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	stdout, _ := captureOutput(t, func() {
		code = runSkills([]string{
			"pack-set", "matt-pocock-skills",
			"--skills", "tdd",
			"--expected-revision", "definitely-not-the-real-revision",
			"--json",
		})
	})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	var got mutationJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\nstdout: %s", err, stdout)
	}
	if got.OK {
		t.Error("ok = true, want false")
	}
	if got.Code != "conflict" {
		t.Errorf("code = %q, want %q", got.Code, "conflict")
	}
	if got.Operation != "set" || got.PackID != "matt-pocock-skills" {
		t.Errorf("operation/packId = %q/%q, want set/matt-pocock-skills", got.Operation, got.PackID)
	}
	if got.Warnings == nil {
		t.Error("warnings should be an empty array, not null")
	}
}

// TestSkillsCLI_PackSet_JSONContract proves the full pack-set --json
// success contract end to end via the CLI adapter, network-free: reading
// pack-status for an absent skill-level pack yields a revision; applying an
// EMPTY selection against that revision is a legitimate no-op success (never
// installed, nothing to remove), and the resulting --json envelope carries
// the post-apply "revision" and "selected" fields pack-status's own
// contract promises (spec-skill-packs.md §3.1) — no follow-up pack-status
// read required.
func TestSkillsCLI_PackSet_JSONContract(t *testing.T) {
	t.Chdir(t.TempDir())

	var statusCode int
	statusStdout, _ := captureOutput(t, func() {
		statusCode = runSkills([]string{"pack-status", "matt-pocock-skills", "--json"})
	})
	if statusCode != 0 {
		t.Fatalf("pack-status code = %d, want 0", statusCode)
	}
	var status statusJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(statusStdout)), &status); err != nil {
		t.Fatalf("pack-status stdout is not a single JSON object: %v\nstdout: %s", err, statusStdout)
	}
	if len(status.Packs) != 1 || status.Packs[0].Revision == "" {
		t.Fatalf("pack-status packs = %+v, want exactly one entry with a non-empty revision", status.Packs)
	}
	revision := status.Packs[0].Revision

	var setCode int
	setStdout, _ := captureOutput(t, func() {
		setCode = runSkills([]string{
			"pack-set", "matt-pocock-skills",
			"--skills", "",
			"--expected-revision", revision,
			"--json",
		})
	})
	if setCode != 0 {
		t.Fatalf("pack-set code = %d, want 0", setCode)
	}
	var got mutationJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(setStdout)), &got); err != nil {
		t.Fatalf("pack-set stdout is not a single JSON object: %v\nstdout: %s", err, setStdout)
	}
	if !got.OK {
		t.Fatalf("ok = false, want true: %+v", got)
	}
	if got.Operation != "set" || got.PackID != "matt-pocock-skills" {
		t.Errorf("operation/packId = %q/%q, want set/matt-pocock-skills", got.Operation, got.PackID)
	}
	if got.Revision == "" {
		t.Error("revision must be present in a successful pack-set --json result (a caller never needs a follow-up pack-status read)")
	}
	if len(got.Selected) != 0 {
		t.Errorf("Selected = %v, want none for an empty selection", got.Selected)
	}
	if got.Warnings == nil {
		t.Error("warnings should be an empty array, not null")
	}
}

func TestRunSkills_PackStatus_ExtraArgument_Rejected(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	_, stderr := captureOutput(t, func() { code = runSkills([]string{"pack-status", "one", "two"}) })
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "two") {
		t.Errorf("stderr = %q, want it to name the rejected extra argument", stderr)
	}
}

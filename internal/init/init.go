// Package init implements the zcp init subcommand.
// It generates project configuration files for Zerops + Claude Code integration.
// All operations are idempotent — re-running overwrites with defaults.
package init

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/z3"
)

const (
	// Shell-comment markers used in files parsed as shell config (e.g. SSH config).
	shellMarkerBegin = "# ZCP:BEGIN"
	shellMarkerEnd   = "# ZCP:END"
	// HTML-comment markers used in files rendered as markdown (e.g. CLAUDE.md).
	// Invisible in rendered output but work as text markers for upsert.
	mdMarkerBegin = "<!-- ZCP:BEGIN -->"
	mdMarkerEnd   = "<!-- ZCP:END -->"
	// Reflog section marker — historical bootstrap records appended by the
	// workflow engine. Must be preserved across re-init.
	reflogMarker = "<!-- ZEROPS:REFLOG -->"
)

// step is a named, idempotent init operation. The runtime.Info argument
// lets steps that need env-aware behaviour (currently CLAUDE.md render)
// pick the right shape; steps that don't simply ignore it.
type step struct {
	name string
	fn   func(string, runtime.Info) error
	// degraded names what stops working when this step fails. A non-empty
	// value marks the step BEST-EFFORT: the failure is reported and init
	// carries on. An empty value marks it REQUIRED — the failure aborts
	// init with a non-zero exit.
	//
	// The split exists because `zcp init` is a run.init command at
	// container boot: a non-zero exit fails the whole service start. A step
	// that only sets up convenience (a discovery directory, a shell alias,
	// an editor config) must never take the container down with it — the
	// more so because several of them write into paths other tools own,
	// where ZCP can find an entry it did not create and cannot repair.
	// Stating the cost here is the price of the tolerance: a best-effort
	// step must say what the operator loses.
	degraded string
}

// Run executes the init subcommand, generating configuration files in baseDir.
// All steps are idempotent — re-running resets to defaults.
func Run(baseDir string, rt runtime.Info) error {
	// Ensure HOME is set — many tools (git, ssh) need it but Zerops
	// containers may start with HOME="" or HOME="/".
	if home := os.Getenv("HOME"); home == "" || home == "/" {
		_ = os.Setenv("HOME", runtime.HomeDir())
	}

	// Shared steps (both local and container). Only the agent context is
	// required — it IS what `zcp init` delivers; everything else degrades
	// (see step.degraded).
	steps := []step{
		{"Skill roots", generateSkillRoots, "installed skills under that root are not discovered by agent sessions"},
		{"Agent context (AGENTS.md + CLAUDE.md)", generateAgentContext, ""},
		{"Permissions", generateSettingsLocal, "the agent prompts for permissions ZCP would have pre-approved"},
		{"Cursor project config", generateCursorProjectConfig, "Cursor does not pick up the Zerops MCP server"},
		{"Shell aliases", generateAliases, "the zcp shell aliases are missing"},
		{"Guided skill", generateGuidedSkill, "guided mode has no skill to route into"},
		{"Trusted domains (code-server)", generateTrustedDomains, "code-server asks for confirmation on links to zerops.io"},
	}
	if rt.InContainer {
		// Container: SSH config + per-agent adapter dispatch.
		steps = append(steps, step{"SSH config", generateSSHConfig, "SSH into project services needs a hand-written config entry"})
		steps = append(steps, step{"Zerops Code (z3)", generateZ3, "Zerops Code is unavailable on this container — nothing answers under " + z3.BasePath + "/"})
	} else {
		// Local: project-scoped .mcp.json (carries ZCP_API_KEY per-project).
		// Required: it is the local deliverable, and a local init is not a
		// run.init command, so failing loudly costs no container start.
		steps = append(steps, step{"MCP config", generateMCPConfig, ""})
	}

	for _, s := range steps {
		fmt.Fprintf(os.Stderr, "  → %s\n", s.name)
		err := s.fn(baseDir, rt)
		if err == nil {
			continue
		}
		if s.degraded == "" {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		fmt.Fprintf(os.Stderr, "  ! %s: %v\n    (continuing — %s)\n", s.name, err, s.degraded)
	}

	// Container-only: dispatch each per-agent adapter (Detect → Validate
	// → ContainerInit). Claude is always-on (backward compat); every other
	// adapter (Codex, Gemini, Antigravity, Cursor, Grok) is Detect-gated on
	// its binary's presence. The full set lives in registeredAdapters().
	if rt.InContainer {
		env := adapters.Env{
			BaseDir:       baseDir,
			Home:          runtime.HomeDir(),
			RT:            rt,
			VSCodeWorkDir: vsCodeWorkDir,
			CommandRunner: commandRunner,
			CommandOutput: adapters.DefaultCommandOutput,
			LookPath:      adapters.DefaultLookPath,
		}
		runContainerAdapters(env)

		// Records that this run reached the end of its step list — not that
		// every step succeeded. nginx serves the file's body verbatim at
		// /healthz, which is how a client tells "still initializing" from
		// "broken" before it holds any credential, and how it sees that a
		// restart re-initialized the container (initAt moves).
		//
		// Best-effort: a write failure only means /healthz under-reports, and
		// is never a reason to fail a container start. Derived from baseDir
		// rather than the absolute z3.InitMarkerPath so a test never touches
		// the real /var/www; the two agree because `zcp init` always runs with
		// baseDir "." from a /var/www cwd in production.
		if err := writeInitCompleteMarker(baseDir); err != nil {
			fmt.Fprintf(os.Stderr, "  ! init-complete marker: %v\n    (continuing — /healthz will report this container as uninitialized)\n", err)
		}
	}

	fmt.Fprintln(os.Stderr, "  ✓ Init complete")
	return nil
}

// writeInitCompleteMarker writes the JSON body nginx serves at /healthz.
func writeInitCompleteMarker(baseDir string) error {
	path := filepath.Join(baseDir, filepath.FromSlash(z3.InitMarkerRelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	body, err := json.Marshal(struct {
		InitComplete bool   `json:"initComplete"`
		InitAt       string `json:"initAt"`
	}{true, time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return fmt.Errorf("marshal marker: %w", err)
	}
	// 0644: nginx's worker reads this on every /healthz request. It carries no
	// secret — two booleans and a timestamp.
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil { //nolint:gosec // G306: nginx serves this file
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// writeTemplate reads a named template and writes it to path, creating parent dirs.
func writeTemplate(templateName, path string) error {
	tmpl, err := content.GetTemplate(templateName)
	if err != nil {
		return err
	}
	if err := adapters.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}

// generateAgentContext writes the env-rendered AGENTS.md (canonical
// body, ZCP-managed marker section) and CLAUDE.md (thin @AGENTS.md
// wrapper for Claude Code's @-include syntax).
//
// AGENTS.md is the cross-tool canonical context file (Codex, Cursor,
// Gemini, Antigravity, and ~17 other agents read it natively).
// CLAUDE.md exists separately because Claude Code reads only CLAUDE.md;
// the wrapper's @AGENTS.md include pulls the canonical body into
// Claude's system prompt.
//
// One-time migration: pre-multi-agent users have CLAUDE.md with the
// full body AND REFLOG sections appended by bootstrap. On first run
// after upgrade, REFLOG sections move from CLAUDE.md → AGENTS.md
// (LLM-startup visibility preserved cross-agent), and CLAUDE.md
// shrinks to the wrapper. Idempotent on subsequent runs.
//
// Both files: re-runs replace only the marked section, preserving any
// content outside the markers (user prose, REFLOG entries appended by
// bootstrap into AGENTS.md going forward).
//
// Legacy compat path: if AGENTS.md exists without markers but contains
// a REFLOG section (theoretical — AGENTS.md is new in this version),
// the pre-REFLOG portion is replaced by the marker-wrapped body while
// REFLOG onwards is preserved.
func generateAgentContext(baseDir string, rt runtime.Info) error {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("mkdir baseDir: %w", err)
	}

	// Phase 1: one-time REFLOG migration CLAUDE.md → AGENTS.md (no-op
	// when already migrated or no CLAUDE.md REFLOG exists).
	if err := migrateReflogToAgentsMD(baseDir); err != nil {
		return fmt.Errorf("migrate reflog: %w", err)
	}

	// Phase 2: write AGENTS.md (canonical body, preserves REFLOG inside).
	// Guided is a local per-project preference (.zcp marker), not runtime
	// detection — read it here and pass into the renderer.
	agentsBody, err := content.BuildAgentsMD(rt, content.GuidedEnabled(baseDir))
	if err != nil {
		return err
	}
	agentsPath := filepath.Join(baseDir, "AGENTS.md")
	if err := writeManagedSectionPreservingReflog(agentsPath, agentsBody); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}

	// Phase 3: write CLAUDE.md (thin @AGENTS.md wrapper). REFLOG is now
	// in AGENTS.md (after migration), so CLAUDE.md needs no REFLOG
	// preservation path.
	claudePath := filepath.Join(baseDir, "CLAUDE.md")
	if err := writeManagedSectionPreservingReflog(claudePath, content.BuildClaudeWrapper()); err != nil {
		return fmt.Errorf("write CLAUDE.md: %w", err)
	}

	return nil
}

// writeManagedSectionPreservingReflog writes a ZCP-managed marker block
// at path, preserving any content outside the markers (REFLOG entries,
// user prose, user-authored content from a pre-existing file). Three
// cases:
//
//   - File missing / empty → create with just the block.
//   - File exists with a line-anchored marker pair → upsert managed
//     section in place; all content outside the markers is preserved
//     verbatim.
//   - Any other non-empty file (no anchored block: markerless, OR a
//     block damaged by the pre-fix mid-line-mention bug, OR one whose
//     markers carry stray whitespace) → PREPEND the block, keeping ALL
//     existing content verbatim. This never drops user content: a
//     damaged old block simply survives below the fresh one as inert
//     duplicate text (cosmetic, self-inflicted on already-broken
//     files) rather than being clobbered.
//
// The prepend branch deliberately does NOT special-case a trailing
// REFLOG. An earlier version kept only text from the first REFLOG
// marker onward (dropping everything above it), on the false premise
// that a file without an anchored block has nothing worth keeping above
// its REFLOG. On a file whose managed block was corrupted by the
// pre-fix bug — an anchored REFLOG below, user prose + a mangled block
// above — that dropped real user content on the next `zcp init`. The
// affected population is exactly the users this fix exists to protect,
// so preservation is unconditional.
//
// Markers count only when they occupy an entire line
// (content.IndexMarkerLine) — a mid-line mention in prose is content.
func writeManagedSectionPreservingReflog(path, body string) error {
	block := mdMarkerBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + mdMarkerEnd + "\n"

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	text := string(existing)

	if begin := content.IndexMarkerLine(text, mdMarkerBegin, 0); begin >= 0 &&
		content.IndexMarkerLine(text, mdMarkerEnd, begin+len(mdMarkerBegin)) >= 0 {
		return upsertManagedSection(path, block, mdMarkerBegin, mdMarkerEnd)
	}
	if len(text) > 0 {
		// No anchored block — preserve ALL content by prepending the
		// managed block, separated by a blank line. Covers markerless
		// user files and files with a damaged/unanchored block alike.
		return os.WriteFile(path, []byte(block+"\n"+text), 0o644) //nolint:gosec // G306: config file
	}
	return os.WriteFile(path, []byte(block), 0o644) //nolint:gosec // G306: config file
}

// migrateReflogToAgentsMD relocates REFLOG sections from a pre-existing
// CLAUDE.md to AGENTS.md. Content-resumable (not existence-gated): the
// migration looks at actual REFLOG content in both files and merges
// what's missing, then cleans CLAUDE.md.
//
// Cases handled:
//
//   - Fresh install (no CLAUDE.md): no-op.
//   - CLAUDE.md without REFLOG: no-op.
//   - CLAUDE.md REFLOG, AGENTS.md missing: append REFLOG sections to a
//     new AGENTS.md (with empty marker block; generateAgentContext fills
//     the body next). Clean CLAUDE.md.
//   - CLAUDE.md REFLOG, AGENTS.md exists with same REFLOG already
//     present: only clean CLAUDE.md (no duplication).
//   - CLAUDE.md REFLOG, AGENTS.md exists without those REFLOG sections
//     (crashed mid-migration / user-created AGENTS.md / etc.): append
//     the missing sections and clean CLAUDE.md.
//   - CLAUDE.md REFLOG, AGENTS.md user-content but markerless: prepend
//     marker block + missing REFLOG, preserve user content.
//
// Idempotent and concurrent-safe in the steady state: once REFLOG
// sections live in AGENTS.md and are removed from CLAUDE.md, both
// content-checks come up empty and the function returns no-op.
//
// REFLOG section identity is compared by content (string equality of
// the section bytes including markers). Two bootstraps that produced
// the same bytes would dedupe — a non-issue in practice since each
// AppendReflogEntry call writes a unique session ID + timestamp.
//
// Preserves CLAUDE.md content OUTSIDE the REFLOG markers (user prose,
// ZCP-managed body section) — only the REFLOG blocks themselves are
// removed from CLAUDE.md.
func migrateReflogToAgentsMD(baseDir string) error {
	claudePath := filepath.Join(baseDir, "CLAUDE.md")
	agentsPath := filepath.Join(baseDir, "AGENTS.md")

	claudeContent, err := os.ReadFile(claudePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh install — nothing to migrate
		}
		return fmt.Errorf("read CLAUDE.md for migration: %w", err)
	}

	claudeSections := extractReflogSections(string(claudeContent))
	if len(claudeSections) == 0 {
		return nil // no REFLOG in CLAUDE.md — nothing to migrate
	}

	// Read whatever exists at AGENTS.md (may be missing, may be
	// user-created with no markers, may be a prior partial migration).
	var existingAgents string
	agentsBytes, err := os.ReadFile(agentsPath)
	switch {
	case err == nil:
		existingAgents = string(agentsBytes)
	case os.IsNotExist(err):
		existingAgents = ""
	default:
		return fmt.Errorf("read AGENTS.md for migration: %w", err)
	}
	agentsAlreadyHas := extractReflogSections(existingAgents)
	already := make(map[string]struct{}, len(agentsAlreadyHas))
	for _, s := range agentsAlreadyHas {
		already[s] = struct{}{}
	}

	// Append only the CLAUDE REFLOG sections that aren't already in
	// AGENTS.md (content-equal dedupe).
	var toAppend strings.Builder
	for _, s := range claudeSections {
		if _, dup := already[s]; dup {
			continue
		}
		toAppend.WriteString("\n")
		toAppend.WriteString(s)
	}

	if toAppend.Len() > 0 {
		if existingAgents == "" {
			// New AGENTS.md: empty marker block + REFLOG sections.
			// generateAgentContext fills the body in the next step.
			var initial strings.Builder
			initial.WriteString(mdMarkerBegin + "\n" + mdMarkerEnd + "\n")
			initial.WriteString(toAppend.String())
			if err := os.WriteFile(agentsPath, []byte(initial.String()), 0o644); err != nil { //nolint:gosec // G306: config file
				return fmt.Errorf("write migrated AGENTS.md: %w", err)
			}
		} else {
			// Existing AGENTS.md: append missing REFLOG sections at
			// end. Preserves all prior content (markers + body + any
			// user prose).
			merged := strings.TrimRight(existingAgents, "\n") + "\n" + toAppend.String()
			if err := os.WriteFile(agentsPath, []byte(merged), 0o644); err != nil { //nolint:gosec // G306: config file
				return fmt.Errorf("append migrated REFLOG to AGENTS.md: %w", err)
			}
		}
	}

	// Remove REFLOG sections from CLAUDE.md. Content outside REFLOG
	// markers (ZCP-managed body, user prose) is preserved verbatim.
	// Runs even if toAppend was empty (sections were already in
	// AGENTS.md) — the goal is a CLAUDE.md without stale REFLOG.
	cleaned := removeReflogSections(string(claudeContent))
	if cleaned != string(claudeContent) {
		if err := os.WriteFile(claudePath, []byte(cleaned), 0o644); err != nil { //nolint:gosec // G306: config file
			return fmt.Errorf("rewrite CLAUDE.md without REFLOG: %w", err)
		}
	}

	return nil
}

// reflogMarkerEnd is the closing marker for a REFLOG section appended
// by workflow.AppendReflogEntry. Mirrors reflogMarker (the opener).
const reflogMarkerEnd = "<!-- /ZEROPS:REFLOG -->"

// extractReflogSections returns every <!-- ZEROPS:REFLOG --> .. <!-- /ZEROPS:REFLOG -->
// block in text, in order (preserves exact bytes for migration).
// Markers count only when they occupy an entire line
// (content.IndexMarkerLine) — a mid-line mention in prose is content,
// never a section boundary.
func extractReflogSections(text string) []string {
	var out []string
	from := 0
	for {
		begin := content.IndexMarkerLine(text, reflogMarker, from)
		if begin < 0 {
			break
		}
		end := content.IndexMarkerLine(text, reflogMarkerEnd, begin+len(reflogMarker))
		if end < 0 {
			break
		}
		sectionEnd := end + len(reflogMarkerEnd)
		if sectionEnd < len(text) && text[sectionEnd] == '\n' {
			sectionEnd++
		}
		out = append(out, text[begin:sectionEnd])
		from = sectionEnd
	}
	return out
}

// removeReflogSections returns text with every REFLOG block stripped.
// Preserves all other content — mid-line marker mentions included
// (same line-anchored matching as extractReflogSections). Drops the
// leading newline immediately before a REFLOG block so the file
// doesn't accumulate blank lines on repeated migrations.
func removeReflogSections(text string) string {
	var b strings.Builder
	from := 0
	for {
		begin := content.IndexMarkerLine(text, reflogMarker, from)
		if begin < 0 {
			b.WriteString(text[from:])
			break
		}
		end := content.IndexMarkerLine(text, reflogMarkerEnd, begin+len(reflogMarker))
		if end < 0 {
			b.WriteString(text[from:])
			break
		}
		sectionEnd := end + len(reflogMarkerEnd)
		if sectionEnd < len(text) && text[sectionEnd] == '\n' {
			sectionEnd++
		}
		// Drop the leading newline if present (avoid blank-line drift).
		writeUntil := begin
		if writeUntil > from && text[writeUntil-1] == '\n' {
			writeUntil--
		}
		b.WriteString(text[from:writeUntil])
		from = sectionEnd
	}
	return b.String()
}

// skillRootsRel are the two project-local agent-discovery roots every
// installed skill (community pack or guided) materializes under. Both must
// exist unconditionally — even with no skill installed — because Claude
// Code and Codex each watch their own root via native filesystem discovery
// and only pick up a directory that already existed at session start; a
// root created only as a side effect of the first installed skill would
// require restarting an already-running session. Spec: spec-skill-packs.md
// §2.
var skillRootsRel = []string{
	filepath.Join(".agents", "skills"),
	filepath.Join(".claude", "skills"),
}

// generateSkillRoots ensures both skillRootsRel directories exist.
// MkdirAll is a no-op on an already-existing directory and never touches
// its contents, so this is safe to run on every `zcp init`, idempotently,
// regardless of guided mode or any installed skill pack.
//
// Both roots are attempted and their failures joined: `.agents` and
// `.claude` are owned by different tools, so one being unusable says
// nothing about the other. The step is best-effort (see step.degraded) —
// an unusable root costs skill discovery under it, which is exactly the
// case spec-skill-packs.md §2 already sends to a new agent session.
func generateSkillRoots(baseDir string, _ runtime.Info) error {
	var errs []error
	for _, rel := range skillRootsRel {
		if err := adapters.EnsureDir(filepath.Join(baseDir, rel)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// generateGuidedSkill materializes the guided-skill SUBTREE (the router
// SKILL.md plus the phases/*.md progressive-disclosure files) into EVERY
// agent-discovery root, to match the local guided preference (.zcp marker,
// the source of truth):
//   - guided enabled (and not authoring): write the whole embedded subtree.
//   - guided off: remove it, so a plain `zcp init` leaves a clean tree.
//   - authoring: leave the maintainer's tree untouched (guided is user-only).
//
// Every root gets the same tree because guided is agent-neutral — an agent
// that discovers skills under .agents/skills/ must find what Claude Code
// finds under .claude/skills/ (content.GuidedSkillDirsRel owns the list).
//
// Each directory is reset (RemoveAll) before each write so a re-init after a
// binary upgrade drops phase files that were removed from the embed — the
// materialized tree always matches this build, never accumulates orphans.
//
// Spec: docs/spec-guided-mode.md.
func generateGuidedSkill(baseDir string, rt runtime.Info) error {
	if rt.Authoring {
		return nil
	}
	dirs := make([]string, 0, len(content.GuidedSkillDirsRel))
	for _, rel := range content.GuidedSkillDirsRel {
		dir := filepath.Join(baseDir, filepath.FromSlash(rel))
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("reset guided skill dir %s: %w", rel, err)
		}
		dirs = append(dirs, dir)
	}
	if !content.GuidedEnabled(baseDir) {
		return nil
	}
	files, err := content.ReadGuidedSkillTree()
	if err != nil {
		return fmt.Errorf("read guided skill tree: %w", err)
	}
	for _, dir := range dirs {
		for _, f := range files {
			p := filepath.Join(dir, filepath.FromSlash(f.RelPath))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return fmt.Errorf("mkdir guided skill subdir: %w", err)
			}
			if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil { //nolint:gosec // G306: skill content the agent reads
				return fmt.Errorf("write guided skill file %s: %w", f.RelPath, err)
			}
		}
	}
	return nil
}

// parseMCPConfigTemplate parses the mcp-config.json template and returns
// its mcpServers map as key -> entry (command/args). This is the single
// parse site for the canonical MCP server key(s), pinned by
// content.TestMCPServerNameCanonical — every writer of a "zerops" MCP
// server entry (generateMCPConfig, generateCursorProjectConfig) derives
// its key + entry from here rather than hardcoding a second literal.
func parseMCPConfigTemplate() (map[string]map[string]any, error) {
	tmpl, err := content.GetTemplate("mcp-config.json")
	if err != nil {
		return nil, err
	}
	var base map[string]any
	if err := json.Unmarshal([]byte(tmpl), &base); err != nil {
		return nil, fmt.Errorf("parse mcp-config.json template: %w", err)
	}
	servers, ok := base["mcpServers"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp-config.json template: mcpServers not a map")
	}
	out := make(map[string]map[string]any, len(servers))
	for key, entry := range servers {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("mcp-config.json template: server %q not a map", key)
		}
		out[key] = entryMap
	}
	return out, nil
}

// ensureObjectShape returns an error when data[key] is present, non-null,
// and not a JSON object. ZCP writes through these nodes; the merge
// helpers treat a wrong-type node as "missing" and would silently
// replace it (user data loss). Posture matches malformed JSON: abort,
// never rewrite unexpected shapes. Absent/null is fine — helpers create
// the node.
func ensureObjectShape(file string, data map[string]any, key string) error {
	v, ok := data[key]
	if !ok || v == nil {
		return nil
	}
	if _, isMap := v.(map[string]any); !isMap {
		return fmt.Errorf("%s: %q is %T, expected a JSON object — fix or remove it (zcp never rewrites unexpected shapes)", file, key, v)
	}
	return nil
}

// ensureArraySlotShape returns an error when parent[key] — a slot ZCP
// appends array entries into — is present with a shape the normalizer
// can't preserve faithfully: absent/null/array pass through, a scalar
// string is tolerated (AppendIfMissingString wraps it, intent
// preserved), anything else (object, number, bool) would be wrapped
// into a schema-invalid array — abort instead.
func ensureArraySlotShape(file string, parent map[string]any, key string) error {
	v, ok := parent[key]
	if !ok || v == nil {
		return nil
	}
	switch v.(type) {
	case []any, string:
		return nil
	default:
		return fmt.Errorf("%s: %q is %T, expected a JSON array — fix or remove it (zcp never rewrites unexpected shapes)", file, key, v)
	}
}

// generateMCPConfig upserts the ZCP-owned zerops server entry into the
// project-scope .mcp.json, merge-aware. ZCP owns mcpServers.zerops
// {command,args} — reasserted from the mcp-config.json template every
// init (single owner, pinned by content.TestMCPServerNameCanonical).
// Everything else is user-owned and survives re-init: extra keys inside
// the zerops entry (env.ZCP_API_KEY is the documented per-project key
// location build-integration reads), other mcpServers entries, and
// top-level fields. Malformed JSON aborts — the broken bytes may still
// hold the user's key, so they are never silently overwritten.
func generateMCPConfig(baseDir string, _ runtime.Info) error {
	servers, err := parseMCPConfigTemplate()
	if err != nil {
		return err
	}

	path := filepath.Join(baseDir, ".mcp.json")
	data, err := adapters.LoadJSONFile(path)
	if err != nil {
		return err
	}
	if err := ensureObjectShape(path, data, "mcpServers"); err != nil {
		return err
	}
	for key, entryMap := range servers {
		adapters.ShallowMergeAtPath(data, entryMap, "mcpServers", key)
	}
	return adapters.SaveJSONFileIndented(path, data)
}

// generateCursorProjectConfig writes project-scope Cursor CLI/IDE
// permission + registration files, merge-aware and idempotent. Unlike
// generateMCPConfig (local-only), this step runs in BOTH local and
// container mode — Cursor's headless CLI reads project-scope permission
// files regardless of where `zcp init` ran.
//
//  1. .cursor/cli.json — upserts "Mcp(<key>:*)" into permissions.allow so
//     cursor-agent's per-tool-call gate pre-approves zerops_* tools. The
//     write always carries at least this one allow entry, so the
//     permissions block is never empty — an empty/partial project
//     permissions block is a documented Cursor merge bug that overrides
//     the user's global allowlist.
//  2. .cursor/permissions.json — upserts "<key>:*" into mcpAllowlist
//     (Cursor IDE's separate permission surface; user-scope + project-
//     scope arrays are concatenated by Cursor, no override trap here).
//  3. .cursor/mcp.json — LOCAL MODE ONLY. Container mode's single owner
//     for MCP registration is user-scope ~/.cursor/mcp.json, written by
//     the Cursor adapter's ContainerInit; a project-scope copy would fork
//     registration across two owners (and risk Cursor's project→global
//     same-key-replaces-wholesale merge breaking the env-forwarding entry
//     the container adapter depends on).
//
// <key> and the mcp.json entry's command/args are derived from the
// mcp-config.json template via parseMCPConfigTemplate — never a second
// hardcoded "zerops" literal for the server identity. Malformed existing
// JSON aborts the step (LoadJSONFile's error), never silently overwritten.
func generateCursorProjectConfig(baseDir string, rt runtime.Info) error {
	servers, err := parseMCPConfigTemplate()
	if err != nil {
		return err
	}

	cliPath := filepath.Join(baseDir, ".cursor", "cli.json")
	cliData, err := adapters.LoadJSONFile(cliPath)
	if err != nil {
		return err
	}
	permPath := filepath.Join(baseDir, ".cursor", "permissions.json")
	permData, err := adapters.LoadJSONFile(permPath)
	if err != nil {
		return err
	}

	mcpPath := filepath.Join(baseDir, ".cursor", "mcp.json")
	var mcpData map[string]any
	if !rt.InContainer {
		mcpData, err = adapters.LoadJSONFile(mcpPath)
		if err != nil {
			return err
		}
	}

	// Wrong-TYPE nodes at ZCP-written paths abort before any write —
	// the merge helpers would silently replace them (see
	// ensureObjectShape / ensureArraySlotShape).
	if err := ensureObjectShape(cliPath, cliData, "permissions"); err != nil {
		return err
	}
	if perms, ok := cliData["permissions"].(map[string]any); ok {
		if err := ensureArraySlotShape(cliPath, perms, "allow"); err != nil {
			return err
		}
		if err := ensureArraySlotShape(cliPath, perms, "deny"); err != nil {
			return err
		}
	}
	if err := ensureArraySlotShape(permPath, permData, "mcpAllowlist"); err != nil {
		return err
	}
	if !rt.InContainer {
		if err := ensureObjectShape(mcpPath, mcpData, "mcpServers"); err != nil {
			return err
		}
	}

	// permissions.deny is REQUIRED by Cursor's cli.json schema —
	// live-verified 2026-07-03 (binary 2026.07.01-41b2de7): a cli.json
	// without the key fails schema validation and cursor-agent refuses
	// to start in the workspace entirely (even --version exits 1). ZCP
	// owns no deny entries; the key is materialized empty and existing
	// user entries are preserved.
	adapters.EnsureArrayAt(cliData, "permissions", "deny")

	for key, entry := range servers {
		adapters.EnsureArrayContains(cliData, fmt.Sprintf("Mcp(%s:*)", key), "permissions", "allow")
		adapters.EnsureArrayContains(permData, fmt.Sprintf("%s:*", key), "mcpAllowlist")
		if !rt.InContainer {
			adapters.ShallowMergeAtPath(mcpData, map[string]any{
				"type":    "stdio",
				"command": entry["command"],
				"args":    entry["args"],
			}, "mcpServers", key)
		}
	}

	if err := adapters.SaveJSONFileIndented(cliPath, cliData); err != nil {
		return fmt.Errorf("write %s: %w", cliPath, err)
	}
	if err := adapters.SaveJSONFileIndented(permPath, permData); err != nil {
		return fmt.Errorf("write %s: %w", permPath, err)
	}
	if !rt.InContainer {
		if err := adapters.SaveJSONFileIndented(mcpPath, mcpData); err != nil {
			return fmt.Errorf("write %s: %w", mcpPath, err)
		}
	}
	return nil
}

func generateSettingsLocal(baseDir string, _ runtime.Info) error {
	return writeTemplate("settings-local.json", filepath.Join(baseDir, ".claude", "settings.local.json"))
}

func generateSSHConfig(_ string, _ runtime.Info) error {
	tmpl, err := content.GetTemplate("ssh-config")
	if err != nil {
		return err
	}

	home := runtime.HomeDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := filepath.Join(dir, "config")
	block := shellMarkerBegin + "\n" + strings.TrimRight(tmpl, "\n") + "\n" + shellMarkerEnd + "\n"
	return upsertManagedSection(path, block, shellMarkerBegin, shellMarkerEnd)
}

// generateTrustedDomains merges the ZCP-managed domain allowlist into
// code-server's config.yaml (adapters.EnsureTrustedDomains) so links to
// zerops.io/docs/YouTube/GitHub open without the "Do you want to open the
// external website?" confirmation. A no-op on hosts without a code-server
// config directory (e.g. a plain laptop), so this step runs unconditionally
// for both local and container `zcp init`. Failures are cosmetic-only and
// carry no exit code — the step is registered best-effort (step.degraded),
// which is the one site that owns that tolerance for every step.
func generateTrustedDomains(_ string, _ runtime.Info) error {
	return adapters.EnsureTrustedDomains(runtime.HomeDir())
}

// upsertManagedSection reads the file at path, finds the managed block
// (between beginMarker and endMarker lines), replaces it with block, and
// writes back atomically. If no markers exist, block is appended. If the
// file doesn't exist, it is created with just block.
func upsertManagedSection(path, block, beginMarker, endMarker string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var result string
	text := string(existing)

	// Line-anchored matching (content.IndexMarkerLine): a mid-line
	// marker mention in prose is content, not a section boundary. The
	// end marker is located AFTER the begin marker so a stray end
	// occurrence earlier in the file can't select a reversed span.
	beginIdx := content.IndexMarkerLine(text, beginMarker, 0)
	endIdx := -1
	if beginIdx >= 0 {
		endIdx = content.IndexMarkerLine(text, endMarker, beginIdx+len(beginMarker))
	}

	if beginIdx >= 0 && endIdx >= 0 {
		// Replace existing managed section (include the endMarker line).
		endLineEnd := endIdx + len(endMarker)
		if endLineEnd < len(text) && text[endLineEnd] == '\n' {
			endLineEnd++
		}
		result = text[:beginIdx] + block + text[endLineEnd:]
	} else {
		// Append managed section.
		if len(text) > 0 && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if len(text) > 0 {
			text += "\n"
		}
		result = text + block
	}

	// Atomic write: temp file + rename.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ssh-config-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(result); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

const shellAliasSourceLine = `# Zerops shell aliases
[ -f "$HOME/.config/zerops/aliases" ] && . "$HOME/.config/zerops/aliases"`

// shellRCFile defines a shell RC file to source aliases from.
type shellRCFile struct {
	name   string
	create bool // create if not exists (.bashrc: yes for compat, .zshrc: no)
}

// shellRCFiles lists shell RC files to patch with alias sourcing.
// .bashrc: always create (backwards compat).
// .zshrc: only patch if it exists (don't create on bash-only systems).
var shellRCFiles = []shellRCFile{
	{".bashrc", true},
	{".zshrc", false},
}

func generateAliases(_ string, _ runtime.Info) error {
	tmpl, err := content.GetTemplate("aliases")
	if err != nil {
		return err
	}
	home := runtime.HomeDir()

	// Write managed aliases file (overwritten each run).
	dir := filepath.Join(home, ".config", "zerops")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "aliases"), []byte(tmpl), 0644); err != nil { //nolint:gosec // G306: config files need to be readable
		return err
	}

	// Append source line to each shell RC file.
	for _, rc := range shellRCFiles {
		if err := patchShellRC(home, rc); err != nil {
			return err
		}
	}
	return nil
}

// patchShellRC appends the alias source line to a shell RC file if not already present.
func patchShellRC(home string, rc shellRCFile) error {
	rcPath := filepath.Join(home, rc.name)
	existing, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			if !rc.create {
				return nil // shell not installed, skip
			}
			// Create the file (backwards compat for .bashrc).
		} else {
			return fmt.Errorf("read %s: %w", rc.name, err)
		}
	}
	if strings.Contains(string(existing), ".config/zerops/aliases") {
		return nil // already sourced
	}
	flags := os.O_APPEND | os.O_WRONLY
	if rc.create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(rcPath, flags, 0644)
	if err != nil {
		if !rc.create && os.IsNotExist(err) {
			return nil // race: file deleted between ReadFile and OpenFile
		}
		return fmt.Errorf("open %s: %w", rc.name, err)
	}
	defer f.Close()
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	if _, err := f.WriteString("\n" + shellAliasSourceLine + "\n"); err != nil {
		return err
	}
	return nil
}

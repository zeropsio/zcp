# Instruction Surfaces Refactor — CLAUDE.md + MCP init split

**Created**: 2026-04-25
**Status**: Approved, ready to ship
**Scope**: One atomic commit
**Estimated effort**: 3–4 hours

---

## 1. Background — what's broken today

### 1.1 Triggering incident

Live eval session on `zcp` container, project `eval-zcp` (`i6HLVWoiQeeLv8tV0ZZ0EQ`):

User: "vytvor dashbvoard s pocasim v pythnu"

LLM response (paraphrased): "Než začnu, potřebuju vědět: 1. Kam to má běžet? Lokálně, nebo nasadit na Zerops jako službu? 2. Framework? 3. Zdroj dat?"

User had to say "ok pust se do toho" before LLM started bootstrap. The "kam to má běžet, lokálně nebo na Zerops?" question is **structurally impossible** on a ZCP container — Ubuntu has no Python runtime, no browser, no port-bind for the user. Yet the LLM asked. It pattern-matched to training-data behavior ("user wants to build app → ask where to host") instead of reading the ZCP context.

### 1.2 Root causes (three layered drifts)

**Drift 1: descriptive opening, not imperative.** `baseInstructions` opens with *"ZCP manages Zerops PaaS infrastructure through workflows."* — a description of what ZCP is. LLM reads this as background, not instruction. No imperative directive at the top.

**Drift 2: develop-first concept eroded into "three peer entries".** Original framing (commit `683be156`, 2026-04-21, by krls): develop is THE workflow; bootstrap is a sub-flow when needed. Concept survived in `baseInstructions` ("Primary entry: develop. Other entries (when they fit): bootstrap."). But in commit `0139699f` (2026-04-23, by Aleš), `claude.md` template was restructured to "Three entry points — pick the right one" with recipe at #1, develop #2, bootstrap #3. Reason: defensive fix for recipe-flow being abandoned. **Side effect**: claude.md and baseInstructions now disagree on the hierarchy.

**Drift 3: env-agnostic CLAUDE.md template branches.** Single `claude.md` template ships to both container and local installs. Has `or` branches like *"either the ZCP container [...] or a local machine [...]"*. On local, the LLM reads false claims about `/var/www/{hostname}/` SSHFS mounts that don't exist locally. On container, it reads irrelevant `zcli vpn up` content for local.

**Drift 4: heavy duplication between MCP init and CLAUDE.md.** Both surfaces carry: workflow entries table, decision rule, direct tools list, status pointer, env block. Two sources of truth → guaranteed drift over time.

### 1.3 What the LLM actually needed

For the weather-dashboard case, the LLM needed three things at the strongest surface (CLAUDE.md), pre-tool-call:

1. **Env identity**: "You're inside a Zerops project's ZCP container. App code does NOT run here."
2. **Decision rule with develop primacy**: "Three entry points: develop primary, bootstrap when no services, recipe special track."
3. **Intent-as-proposal rule**: "intent is YOUR one-line proposal — pick a sensible default, start, refine inside the workflow's plan-adjust step."

None of these were sharply enough stated in the strong surface to override training-data patterns.

---

## 2. Architecture — four layers with single ownership

Each piece of guidance has exactly **one home**. No duplication.

| Layer | OWNS | Lifecycle | Strength |
|---|---|---|---|
| **CLAUDE.md** (env-rendered) | Static project rules: env preamble, Three entry points, intent rule, project idioms, status recovery hint, direct tools list, per-service pointer (container only) | Generated at `zcp init`, persistent on disk, re-rendered on re-init | User/project-contract — strongest LLM adherence |
| **MCP init** (`baseInstructions`) | Per-run state delta: adoption note + active session summary | Composed fresh at every `zcp serve` startup | MCP-server-doc — weaker, but appropriate for ephemeral state |
| **Tool descriptions** (per tool's `Description` + field schemas) | Per-tool inputs / effects / specific syntax | Compiled into binary | Tool-protocol level |
| **Atoms** (synthesized at tool calls) | Phase-specific dynamic guidance | Computed per tool call from envelope + corpus | Inline in tool response |

**Principle**: each layer answers a different question.
- CLAUDE.md: *"How do I operate ZCP in general?"* (rules)
- MCP init: *"What just happened / what's going on right now?"* (state delta)
- Atoms: *"What's my next concrete step in this phase?"* (per-call guidance)
- Tool descriptions: *"How does THIS specific tool work?"* (per-tool docs)

No layer reaches into another's domain.

---

## 3. CLAUDE.md — env-rendered (Design D)

### 3.1 Three template files (embedded)

**`internal/content/templates/claude_container.md`** — container preamble (~10 lines):

```markdown
You're running on the **ZCP control-plane container `{{.SelfHostname}}`**
in this Zerops project. The other services in this project are yours
to operate on. Container is Ubuntu with `Read`/`Edit`/`Write`, `zcli`,
`psql`, `mysql`, `redis-cli`, `jq`, and network to every service.
Service code is SSHFS-mounted at `/var/www/{hostname}/` — edit there
with Read/Edit/Write, not over SSH. Edits on the mount survive
restart but not deploy.

Per-service rules (reload behaviour, start commands, asset pipeline)
live at `/var/www/{hostname}/CLAUDE.md` — read before editing.
```

**`internal/content/templates/claude_local.md`** — local preamble (~6 lines):

```markdown
You're on a **developer machine** bound to a Zerops project. Code in
your working directory is the source of truth — deploy via
`zerops_deploy targetService="<hostname>"` (pushes the working
directory to the matching service, blocks until build completes).
Requires `zerops.yaml` at repo root. No SSHFS mount; reach managed
services via `zcli vpn up <projectId>`.
```

**`internal/content/templates/claude_shared.md`** — env-agnostic body (~25 lines):

```markdown
Zerops has its own syntax and conventions. Don't guess — look them up
via `zerops_knowledge`, and inspect live state via `zerops_*` tools.
Runtime app code always runs in Zerops runtime containers.

## Three entry points

1. **Develop** — every task that touches a specific service's code
   (editing, scaffolding, debugging, deploying, planning):

   ```
   zerops_workflow action="start" workflow="develop" \
     intent="<one-line proposal>" scope=["appdev"]
   ```

   `intent` is your one-line proposal — pick a sensible default
   ("Streamlit weather dashboard, python@3.12, public subdomain"),
   start, refine inside the plan-adjust step. Don't ask the user for
   details the workflow itself collects. 1 task = 1 session: a new
   `intent` on an open develop session auto-closes the prior one.

2. **Bootstrap** — when no services exist yet, or you need to add
   infrastructure (new service, mode expansion):

   ```
   zerops_workflow action="start" workflow="bootstrap" intent="<...>"
   ```

   Provisions services. After it closes, continue in develop. If
   infrastructure work comes up mid-develop, start bootstrap — your
   develop work session persists.

3. **Recipe authoring** — only when the user said "create a
   {framework} recipe", "build a recipe", or named a slug like
   `nestjs-showcase`:

   ```
   zerops_recipe action="start" slug="<slug>" outputRoot="<dir>"
   ```

   Self-contained pipeline (research → provision → scaffold → feature
   → finalize). Do NOT start bootstrap or develop during recipe
   authoring — the recipe atoms guide every step.

If state is unclear (after compaction or between tasks):
`zerops_workflow action="status"` (or `zerops_recipe action="status"`
for recipe sessions) returns the current phase and next action.

Direct tools skip the workflow — `zerops_discover`, `zerops_logs`,
`zerops_env`, `zerops_manage`, `zerops_scale`, `zerops_subdomain`,
`zerops_knowledge` auto-apply without a deploy cycle.
```

### 3.2 Composition function

**New file**: `internal/content/build_claude.go`

```go
package content

import (
    "fmt"
    "strings"

    "github.com/zeropsio/zcp/internal/runtime"
)

// BuildClaudeMD composes the env-rendered CLAUDE.md content from the three
// embedded templates: claude_shared.md (env-agnostic body) plus exactly one
// env-specific preamble (claude_container.md or claude_local.md).
//
// Container preamble carries a {{.SelfHostname}} template var, resolved to
// rt.ServiceName. The composed output is wrapped in <!-- ZCP:BEGIN/END -->
// markers by the caller (init.generateCLAUDEMD).
//
// Render is install-time: zcp init detects rt.InContainer and freezes the
// env into the disk file. Subsequent zcp serve runs do not re-render. Env
// is stable per install; if the install moves between envs, zcp init must
// be re-run.
func BuildClaudeMD(rt runtime.Info) (string, error) {
    shared, err := GetTemplate("claude_shared.md")
    if err != nil {
        return "", fmt.Errorf("read claude_shared.md: %w", err)
    }

    var preamble string
    if rt.InContainer {
        tmpl, err := GetTemplate("claude_container.md")
        if err != nil {
            return "", fmt.Errorf("read claude_container.md: %w", err)
        }
        preamble = strings.ReplaceAll(tmpl, "{{.SelfHostname}}", rt.ServiceName)
    } else {
        tmpl, err := GetTemplate("claude_local.md")
        if err != nil {
            return "", fmt.Errorf("read claude_local.md: %w", err)
        }
        preamble = tmpl
    }

    return "# Zerops\n\n" + strings.TrimSpace(preamble) + "\n\n" + strings.TrimSpace(shared) + "\n", nil
}
```

### 3.3 init wiring

**`internal/init/init.go`** — thread `runtime.Info` through `step.fn`:

```go
type step struct {
    name string
    fn   func(string, runtime.Info) error  // CHANGED: was func(string) error
}

func Run(baseDir string, rt runtime.Info) error {
    // ... HOME setup unchanged ...

    steps := []step{
        {"CLAUDE.md", generateCLAUDEMD},
        {"Permissions", generateSettingsLocal},
        {"Shell aliases", generateAliases},
    }
    // env-conditional steps unchanged

    for _, s := range steps {
        fmt.Fprintf(os.Stderr, "  → %s\n", s.name)
        if err := s.fn(baseDir, rt); err != nil {  // CHANGED: pass rt
            return fmt.Errorf("%s: %w", s.name, err)
        }
    }
    // ...
}

func generateCLAUDEMD(baseDir string, rt runtime.Info) error {
    body, err := content.BuildClaudeMD(rt)
    if err != nil {
        return err
    }
    path := filepath.Join(baseDir, "CLAUDE.md")
    if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
        return fmt.Errorf("mkdir: %w", err)
    }
    block := mdMarkerBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + mdMarkerEnd + "\n"

    // existing marker upsert / migration logic unchanged
    existing, err := os.ReadFile(path)
    if err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("read CLAUDE.md: %w", err)
    }
    text := string(existing)

    if strings.Contains(text, mdMarkerBegin) && strings.Contains(text, mdMarkerEnd) {
        return upsertManagedSection(path, block, mdMarkerBegin, mdMarkerEnd)
    }
    if idx := strings.Index(text, reflogMarker); idx >= 0 {
        return os.WriteFile(path, []byte(block+"\n"+text[idx:]), 0644)
    }
    return os.WriteFile(path, []byte(block), 0644)
}

// All other step functions get the rt param too. Most ignore it.
// Update signatures: generateSettingsLocal, generateAliases, generateSSHConfig,
// containerSteps()'s functions, generateMCPConfig.
```

### 3.4 Delete

- `internal/content/templates/claude.md` — replaced by three new templates.
- Any `content_test.go` test that pinned `claude.md` substrings — update or delete.

---

## 4. MCP init — runtime-only thin surface

### 4.1 New `internal/server/instructions.go`

Replace the existing file entirely:

```go
package server

import (
    "fmt"
    "os"
    "strings"

    "github.com/zeropsio/zcp/internal/runtime"
    "github.com/zeropsio/zcp/internal/workflow"
)

// RuntimeContext carries the per-server-start runtime injections that go
// into the MCP `instructions` field. Both fields are optional; an empty
// RuntimeContext yields an empty MCP init payload (which is valid per the
// MCP protocol).
//
// AdoptionNote — local-env LocalAutoAdopt result for THIS server start.
//   Format: workflow.FormatAdoptionNote(result). Empty when no auto-adopt
//   ran or when nothing was adopted this run.
//
// StateHint — summary of active sessions for the current PID. Surfaces
//   live recipe / bootstrap / work sessions so the LLM doesn't make a
//   wasted tool call before discovering them. Empty when no sessions are
//   open for this PID.
//
// Static project rules (Three entry points, intent rule, env preamble)
// live in CLAUDE.md (env-rendered at zcp init). MCP init is for runtime
// context only — the strict separation is the architecture invariant
// pinned by TestMCPInit_NoStaticRulesLeak.
type RuntimeContext struct {
    AdoptionNote string
    StateHint    string
}

// BuildInstructions composes the MCP init payload from runtime context.
// Returns "" when no runtime context applies — empty instructions field
// is valid per MCP protocol and signals "nothing notable this run."
func BuildInstructions(rc RuntimeContext) string {
    var parts []string
    if rc.AdoptionNote != "" {
        parts = append(parts, rc.AdoptionNote)
    }
    if rc.StateHint != "" {
        parts = append(parts, rc.StateHint)
    }
    return strings.Join(parts, "\n\n")
}

// ComposeStateHint builds the active-session summary line(s) for the current
// PID. Returns "" when no sessions are open. Format is human-readable, terse,
// and includes the next-action pointer (status call) so the LLM can act on
// the hint without further inference.
//
// Sources read:
//   - workflow.ListSessions(stateDir): recipe + bootstrap sessions
//   - workflow.LoadWorkSession(stateDir, pid): per-PID develop work session
//
// Performance: two file system reads. Called once at server start.
func ComposeStateHint(stateDir string, pid int) string {
    if stateDir == "" {
        return ""
    }
    var lines []string

    sessions, _ := workflow.ListSessions(stateDir)
    for _, s := range sessions {
        if s.PID != pid {
            continue
        }
        switch s.Workflow {
        case workflow.WorkflowRecipe:
            lines = append(lines, fmt.Sprintf(
                "Active recipe session: %s. Use zerops_recipe action=\"status\" "+
                    "for the next action — do NOT start zerops_workflow during "+
                    "recipe authoring.",
                describeRecipeSession(s)))
        case workflow.WorkflowBootstrap:
            lines = append(lines, fmt.Sprintf(
                "Active bootstrap session (%s). Use zerops_workflow "+
                    "action=\"status\" to continue.",
                describeBootstrapSession(s)))
        }
    }

    if ws, _ := workflow.LoadWorkSession(stateDir, pid); ws != nil && ws.ClosedAt == "" {
        lines = append(lines, fmt.Sprintf(
            "Open develop work session: %q on %v. Use "+
                "zerops_workflow action=\"status\" for current state.",
            ws.Intent, ws.Services))
    }

    return strings.Join(lines, "\n\n")
}

// describeRecipeSession returns "<slug>" or "<slug> (phase=<phase>)" depending
// on what the registry entry exposes.
func describeRecipeSession(s workflow.SessionRegistryEntry) string {
    // Implementation: pull slug + phase from session entry.
    // Concrete fields TBD when implementing — read SessionRegistryEntry shape.
    return s.SessionID // placeholder
}

func describeBootstrapSession(s workflow.SessionRegistryEntry) string {
    // Implementation: pull route + step from session entry.
    return s.SessionID // placeholder
}

// CurrentPID is wrapped for testability.
var CurrentPID = func() int { return os.Getpid() }
```

### 4.2 Update `internal/server/server.go`

```go
// Replace existing BuildInstructionsWithNote call site:
adoptionNote := ""
if !rtInfo.InContainer {
    if authInfo != nil && authInfo.ProjectID != "" {
        adoptionNote = runLocalAutoAdopt(ctx, client, authInfo.ProjectID, stateDir, logger)
    }
}
stateHint := ComposeStateHint(stateDir, os.Getpid())
instructions := BuildInstructions(RuntimeContext{
    AdoptionNote: adoptionNote,
    StateHint:    stateHint,
})
// Pass instructions to MCP server initialization.
```

### 4.3 Delete

- `baseInstructions` constant.
- `containerEnvironment` constant.
- `localEnvironment` constant.
- `BuildInstructionsWithNote` function (replaced by `BuildInstructions(RuntimeContext)`).
- The self-hostname injection logic (moved to CLAUDE.md template var).

---

## 5. Behavioral matrix — what LLM sees per scenario

| # | Project state | CLAUDE.md content | MCP init content |
|---|---|---|---|
| 1 | Empty project | Three entry points (full) | "" |
| 2 | Bootstrap done, no active session | Three entry points (full) | "" |
| 3 | Bootstrap mid-flight, current PID | Three entry points (full) | "Active bootstrap session (route=classic, step=provision). Use zerops_workflow action='status' to continue." |
| 4 | Develop closed-auto | Three entry points (full) | "" (closed sessions not surfaced) |
| 5 | Develop abandoned (dead PID) | Three entry points (full) | "" (cleanup runs at server start) |
| 6 | Develop iteration-cap | Three entry points (full) | "" (closed sessions not surfaced) |
| 7 | Recipe authoring active, current PID | Three entry points (full) | "Active recipe session: strapi (phase=feature). Use zerops_recipe action='status' for the next action — do NOT start zerops_workflow during recipe authoring." |
| 8 | Recipe abandoned (dead PID) | Three entry points (full) | "" (cleanup runs at server start) |
| 9 | Adopted services this run (local) | Three entry points (full) | "Auto-adopted services this run: appdev (nodejs@22), appstage (nodejs@22)." |
| 10 | Mixed (adoption + active bootstrap) | Three entry points (full) | adoption note + bootstrap state hint, separated by blank line |

**Critical scenario** (#7) — recipe-active mid-session, new conversation: without state hint, LLM might call `zerops_workflow action="start" workflow="develop"` and hit `ErrSubagentMisuse`. With state hint, LLM correctly uses `zerops_recipe action="status"` first call.

---

## 6. Test changes

### 6.1 Delete

- `TestBuildInstructions_Container_HasEnvironmentBlock` — substrings (`"ZCP manages Zerops"`, `/var/www/`, `SSHFS`, `ssh`) move to CLAUDE.md tests.
- `TestBuildInstructions_Local_HasEnvironmentBlock` — same.
- `TestBuildInstructions_Container_WithSelfHostname` — self-hostname now in CLAUDE.md, not MCP init.

### 6.2 Update

- `TestBuildInstructions_FitsIn2KB` — keep (still under, by huge margin).
- `TestBuildInstructions_Static_NoDynamicContent` — replace with `TestBuildInstructions_DeterministicForSameRuntimeContext`.
- `TestBuildInstructions_DevelopEntryPrecedesStatus` — delete (no entry points in MCP init now). Move equivalent to claude_shared.md test.

### 6.3 New tests

**`internal/server/instructions_test.go`**:

```go
func TestBuildInstructions_Empty_WhenNothingApplies(t *testing.T) {
    out := BuildInstructions(RuntimeContext{})
    if out != "" {
        t.Errorf("expected empty MCP init, got %q", out)
    }
}

func TestBuildInstructions_AdoptionNoteOnly(t *testing.T) {
    out := BuildInstructions(RuntimeContext{AdoptionNote: "Adopted: appdev"})
    if out != "Adopted: appdev" {
        t.Errorf("got %q", out)
    }
}

func TestBuildInstructions_StateHintOnly(t *testing.T) {
    out := BuildInstructions(RuntimeContext{StateHint: "Active recipe session: foo"})
    if out != "Active recipe session: foo" {
        t.Errorf("got %q", out)
    }
}

func TestBuildInstructions_BothJoinedByBlankLine(t *testing.T) {
    out := BuildInstructions(RuntimeContext{
        AdoptionNote: "Adopted: appdev",
        StateHint:    "Active recipe session: foo",
    })
    expected := "Adopted: appdev\n\nActive recipe session: foo"
    if out != expected {
        t.Errorf("got %q, want %q", out, expected)
    }
}

func TestBuildInstructions_NoStaticRulesLeak(t *testing.T) {
    // Architecture invariant: MCP init must not contain static project rules.
    // Static rules live in CLAUDE.md (the strong surface).
    out := BuildInstructions(RuntimeContext{
        AdoptionNote: "Adopted: appdev",
        StateHint:    "Active recipe session: foo",
    })
    forbidden := []string{
        "Three entry points",
        "workflow=\"develop\"",
        "workflow=\"bootstrap\"",
        "intent",
        "/var/www/",
        "SSHFS",
        "Don't guess",
    }
    for _, f := range forbidden {
        if strings.Contains(out, f) {
            t.Errorf("MCP init must not contain %q (belongs in CLAUDE.md): %s", f, out)
        }
    }
}

func TestBuildInstructions_FitsIn2KB(t *testing.T) {
    // Even with rich state hint + adoption, MCP init stays under MCP protocol
    // 2KB guidance budget.
    rc := RuntimeContext{
        AdoptionNote: strings.Repeat("Adopted: ", 50) + "appdev",
        StateHint:    strings.Repeat("Active session: ", 50) + "details",
    }
    out := BuildInstructions(rc)
    if len(out) > 2048 {
        t.Errorf("instructions = %d bytes, must be under 2048", len(out))
    }
}
```

**State-hint composition tests** (`ComposeStateHint`):

```go
func TestComposeStateHint_NoSessions_ReturnsEmpty(t *testing.T) { ... }
func TestComposeStateHint_RecipeSession_HasNoActionPointer(t *testing.T) { ... }
func TestComposeStateHint_BootstrapSession_HasStatusPointer(t *testing.T) { ... }
func TestComposeStateHint_OpenWorkSession_SurfacesIntentAndScope(t *testing.T) { ... }
func TestComposeStateHint_ClosedWorkSession_NotSurfaced(t *testing.T) { ... }
func TestComposeStateHint_DeadPIDSession_NotSurfaced(t *testing.T) { ... }
func TestComposeStateHint_MultipleActiveSessions_JoinedByBlankLine(t *testing.T) { ... }
```

**`internal/content/build_claude_test.go`** (new file):

```go
func TestBuildClaudeMD_Container_InjectsHostname(t *testing.T) {
    out, err := BuildClaudeMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
    if err != nil { t.Fatal(err) }
    if !strings.Contains(out, "ZCP control-plane container `zcp`") {
        t.Errorf("hostname not injected: %s", out)
    }
}

func TestBuildClaudeMD_Container_HasContainerFacts(t *testing.T) {
    out, _ := BuildClaudeMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
    for _, want := range []string{
        "/var/www/{hostname}/",
        "SSHFS",
        "Read", "Edit", "Write",
        "Three entry points",
        "Don't guess",
        "intent is your one-line proposal",
    } {
        if !strings.Contains(out, want) {
            t.Errorf("container CLAUDE.md missing %q", want)
        }
    }
}

func TestBuildClaudeMD_Local_HasLocalFacts(t *testing.T) {
    out, _ := BuildClaudeMD(runtime.Info{InContainer: false})
    for _, want := range []string{
        "developer machine",
        "zerops_deploy",
        "zerops.yaml at repo root",
        "zcli vpn up",
        "Three entry points",
        "Don't guess",
    } {
        if !strings.Contains(out, want) {
            t.Errorf("local CLAUDE.md missing %q", want)
        }
    }
}

func TestBuildClaudeMD_Local_NoContainerLeak(t *testing.T) {
    out, _ := BuildClaudeMD(runtime.Info{InContainer: false})
    for _, forbidden := range []string{
        "/var/www/",
        "SSHFS",
        "ZCP control-plane container",
        "{{.SelfHostname}}",  // template var must be resolved or absent
    } {
        if strings.Contains(out, forbidden) {
            t.Errorf("local CLAUDE.md leaked container content %q", forbidden)
        }
    }
}

func TestBuildClaudeMD_Container_NoLocalLeak(t *testing.T) {
    out, _ := BuildClaudeMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
    for _, forbidden := range []string{
        "developer machine",
        "zcli vpn up",
        "working directory",
    } {
        if strings.Contains(out, forbidden) {
            t.Errorf("container CLAUDE.md leaked local content %q", forbidden)
        }
    }
}

func TestBuildClaudeMD_Deterministic(t *testing.T) {
    rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
    a, _ := BuildClaudeMD(rt)
    b, _ := BuildClaudeMD(rt)
    if a != b {
        t.Error("BuildClaudeMD not deterministic for same Info")
    }
}

func TestBuildClaudeMD_DevelopFirst(t *testing.T) {
    // Pin develop primacy in Three entry points order.
    out, _ := BuildClaudeMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
    devIdx := strings.Index(out, "1. **Develop**")
    bootIdx := strings.Index(out, "2. **Bootstrap**")
    recipeIdx := strings.Index(out, "3. **Recipe")
    if devIdx < 0 || bootIdx < 0 || recipeIdx < 0 {
        t.Fatal("missing one of the three entry-point headers")
    }
    if !(devIdx < bootIdx && bootIdx < recipeIdx) {
        t.Errorf("entry points out of order: develop=%d, bootstrap=%d, recipe=%d",
            devIdx, bootIdx, recipeIdx)
    }
}
```

**`internal/content/templates/claude_shared_test.go`** (new file, content-pin):

```go
func TestClaudeShared_NoEnvLeak(t *testing.T) {
    body, err := GetTemplate("claude_shared.md")
    if err != nil { t.Fatal(err) }
    forbidden := []string{
        "/var/www/",
        "SSHFS",
        "developer machine",
        "working directory",
        "zcli vpn up",
        "{{.SelfHostname}}",
    }
    for _, f := range forbidden {
        if strings.Contains(body, f) {
            t.Errorf("claude_shared.md must not contain env-specific %q", f)
        }
    }
}

func TestClaudeContainer_HasHostnameTemplate(t *testing.T) {
    body, err := GetTemplate("claude_container.md")
    if err != nil { t.Fatal(err) }
    if !strings.Contains(body, "{{.SelfHostname}}") {
        t.Error("claude_container.md must reference {{.SelfHostname}} template var")
    }
}

func TestClaudeLocal_NoContainerPaths(t *testing.T) {
    body, err := GetTemplate("claude_local.md")
    if err != nil { t.Fatal(err) }
    forbidden := []string{"/var/www/", "SSHFS", "{{.SelfHostname}}"}
    for _, f := range forbidden {
        if strings.Contains(body, f) {
            t.Errorf("claude_local.md must not contain container-specific %q", f)
        }
    }
}
```

**Update `internal/content/content_test.go:97`** — replace `claude.md` substring assertion with one for each new template.

---

## 7. Implementation steps (single atomic commit)

Execute in order. Verify build at each step locally before committing.

1. **Create three new templates**:
   - `internal/content/templates/claude_shared.md`
   - `internal/content/templates/claude_container.md`
   - `internal/content/templates/claude_local.md`

2. **Add `BuildClaudeMD`**:
   - New file `internal/content/build_claude.go`.
   - Imports `runtime` package, reads templates, composes, substitutes hostname.

3. **Delete old template**:
   - `rm internal/content/templates/claude.md`.

4. **Update init wiring**:
   - `internal/init/init.go`:
     - Change `step.fn` signature to `func(string, runtime.Info) error`.
     - Update `Run` to pass `rt` to each step.
     - Update `generateCLAUDEMD` to call `BuildClaudeMD(rt)`.
     - Update other step functions to accept (and ignore) `rt`: `generateSettingsLocal`, `generateAliases`, `generateSSHConfig`, `generateMCPConfig`, `containerSteps()` returns.

5. **Rewrite `internal/server/instructions.go`**:
   - Delete `baseInstructions`, `containerEnvironment`, `localEnvironment` constants.
   - Delete `BuildInstructions(rt runtime.Info)` and `BuildInstructionsWithNote(...)`.
   - Add `RuntimeContext` struct.
   - Add `BuildInstructions(rc RuntimeContext) string`.
   - Add `ComposeStateHint(stateDir string, pid int) string`.

6. **Update `internal/server/server.go`**:
   - Compose `RuntimeContext{AdoptionNote, StateHint}` in `New`.
   - Pass `BuildInstructions(rc)` to MCP server.

7. **Delete and update tests**:
   - Delete: container/local environment block tests, develop-precedes-status, self-hostname.
   - Update: 2KB fits, content_test.go template name.
   - Add: tests listed in §6.

8. **Build + lint locally**:
   - `go build ./...`
   - `go test ./... -short -count=1`
   - `make lint-local` (atom-tree gates etc.)

9. **Cross-build + container deploy**:
   - `./eval/scripts/build-deploy.sh`
   - SSH to zcp container, verify `/var/www/CLAUDE.md` content matches container render (no `or local` branches, hostname injected).

10. **Smoke test on container**:
    - Start fresh Claude Code session in `/var/www/`.
    - Issue: "vytvor dashboard s pocasim v pythnu".
    - LLM should: read CLAUDE.md → no clarifying questions → directly start `zerops_workflow action="start" workflow="bootstrap" intent="..."` with sensible default.

11. **Atomic commit**:
    - Single commit with conventional message: `instructions(refactor): split static rules into env-rendered CLAUDE.md, slim MCP init to runtime context only`.
    - Body explains the four-layer architecture, reference this plan doc.

---

## 8. Verification gates

Before declaring done:

| Gate | Check |
|---|---|
| Build | `go build ./...` clean |
| Unit tests | `go test ./... -short -count=1` all pass |
| Race tests | `go test ./... -race -count=1` all pass |
| Full lint | `make lint-local` clean |
| Container render | `/var/www/CLAUDE.md` on zcp has hostname `zcp`, no local branches |
| Local render (synthetic) | `BuildClaudeMD(rt{InContainer:false})` output has no `/var/www/` |
| MCP init shape | Container session: empty MCP init when no auto-adopt + no active session |
| MCP init shape | Recipe-mid-session test: state hint contains "Active recipe session" |
| Smoke test | Live container session: weather-dashboard prompt → no pre-litigation |

---

## 9. Out of scope (explicit deferrals)

- **Non-Claude-Code MCP clients** (Cursor, Cline, custom). Per user direction, treated as monolithic with Claude Code (assume CLAUDE.md is read). If non-CLAUDE.md-aware clients become a target, add a thin "see CLAUDE.md for the operational guide" pointer to MCP init or move some content back. Tracked for later via agents-based dynamic delivery.

- **Tool descriptions polish.** `zerops_workflow.intent` field schema may benefit from "your one-line proposal" hint, but not strictly needed if CLAUDE.md is loaded. Defer to a separate refinement pass.

- **Atom corpus changes.** This refactor does not touch atoms. Atoms continue to deliver phase-specific guidance per tool call. Existing idle-bootstrap-entry / idle-develop-entry / idle-adopt-entry remain as the dynamic supplement to CLAUDE.md.

- **Recipe-related changes.** Aleš's domain (`internal/recipe/`, `internal/workflow/recipe_*`). The recipe state hint in `ComposeStateHint` reads existing `workflow.SessionRegistryEntry`; no changes to recipe internals.

- **Self-hostname renaming resilience.** If a container is renamed mid-life, CLAUDE.md hostname goes stale until next `zcp init` re-run. Acceptable: container renames are rare and `zcp init` is cheap.

- **Cross-machine state directory copies.** Edge case (NFS / dotfile sync). Stale registry entries from another machine's PIDs would be cleaned on next session by PID-liveness check. Not specifically tested by this refactor.

---

## 10. Why this is the right architecture

**Single source of truth per rule.** Each piece of guidance lives in exactly one of: CLAUDE.md (static rules), MCP init (runtime context), tool descriptions (per-tool inputs), atoms (phase guidance). No drift possible.

**Strong-surface placement for high-stakes rules.** Decision rule, intent rule, env grounding all live in CLAUDE.md — the user/project-contract surface with strongest LLM adherence. The "kam to má běžet, lokálně nebo na Zerops?" question becomes structurally impossible because env grounding is in the strongest surface.

**Env-precise rendering.** Design D (three templates, composed at install time) eliminates the "or container or local" branches. Each LLM sees only the truth for its environment.

**Develop-first concept restored.** Three entry points list with develop at #1 (primary, day-to-day), bootstrap at #2 (when needed), recipe at #3 (special track). Recipe defense (Aleš's commit `0139699f` reason) preserved via the explicit "do NOT start workflow during recipe" rule, but not at the cost of inverting the day-to-day mental model.

**Runtime state proactively surfaced.** `ComposeStateHint` ensures the LLM never makes a wasted tool call on first message because it walked into an active session blind. Recipe-mid-session (scenario #7) gets a clear hint upfront.

**Future-proof for non-Claude-Code clients.** When non-CLAUDE.md clients become a target, MCP init has clean room to add a static-rules pointer or full mirror without disrupting Claude Code's experience.

---

## 11. Reference — existing code touched

| File | Action |
|---|---|
| `internal/server/instructions.go` | Rewrite (~80 → ~80 lines, different content) |
| `internal/server/instructions_test.go` | Rewrite (delete old subtests, add new per §6) |
| `internal/server/server.go` | Update `New` to compose RuntimeContext (~10 line diff) |
| `internal/init/init.go` | Update step signature, thread `rt`, update `generateCLAUDEMD` |
| `internal/content/templates/claude.md` | Delete |
| `internal/content/templates/claude_shared.md` | Create |
| `internal/content/templates/claude_container.md` | Create |
| `internal/content/templates/claude_local.md` | Create |
| `internal/content/build_claude.go` | Create |
| `internal/content/build_claude_test.go` | Create |
| `internal/content/content_test.go` | Update line 97 (template name) |

Net LOC: roughly net-neutral (CLAUDE.md template loses ~67, three new templates add ~40, instructions.go reshapes ~80, build_claude.go adds ~30, tests add ~150). Drift surface dramatically reduced.

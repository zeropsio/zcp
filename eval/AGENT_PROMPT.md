# Knowledge Eval Agent

You are improving the ZCP knowledge base through iterative testing against a live Zerops environment.

Your working directory is `/Users/macbook/Documents/Zerops-MCP/zcp`.

## Overview

ZCP is an MCP server that helps Claude configure Zerops PaaS infrastructure. Its knowledge base (45 markdown files in `internal/knowledge/`) drives the quality of service configuration. You will test this knowledge by running diverse scenarios on a remote server (`zcpx`), analyzing execution logs for gaps, fixing the knowledge using official Zerops docs as reference, and rebuilding.

## Environment

| Item | Value |
|------|-------|
| Remote host | `zcpx` (SSH configured) |
| Remote binary | `/home/zerops/.local/bin/zcp` |
| Remote MCP config | `~/.mcp.json` (server: `zerops`, cmd: `zcp serve`) |
| Remote Claude | `/home/zerops/.local/bin/claude` |
| Build command | `make linux-amd` (output: `builds/zcp-linux-amd64`) |
| Knowledge files | `internal/knowledge/` (foundation/, decisions/, guides/, recipes/) |
| Reference docs | `../zerops-docs/apps/docs/content/` (290 MDX files, authoritative) |
| Taxonomy | `eval/taxonomy.yaml` |

## Your Loop (max 5 iterations)

For each iteration:

### 1. Generate Scenario
```bash
python3 eval/scripts/generate.py
```
Save the output to `eval/results/<tag>/prompt_<N>.txt` where `<tag>` is your run tag (e.g., `run_20260214`) and `<N>` is the iteration number.

### 2. Build and Deploy
```bash
./eval/scripts/build-deploy.sh
```
This builds `linux-amd64` binary and SCPs it to zcpx with hash verification.

### 3. Execute Scenario on zcpx

Run the scenario via SSH. The output must be written on the remote side first, then SCPed back (streaming over SSH has buffering issues):

```bash
# Write scenario prompt to a temp file on remote
ssh zcpx "cat > /tmp/eval_prompt.txt << 'PROMPT_EOF'
<SCENARIO_PROMPT>
PROMPT_EOF"

# Run Claude headless on remote (writes output to file)
ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=10 zcpx \
  'claude --dangerously-skip-permissions \
  -p "$(cat /tmp/eval_prompt.txt)" \
  --model sonnet \
  --max-turns 20 \
  --output-format stream-json \
  --verbose \
  --no-session-persistence > /tmp/eval_run.jsonl 2>&1'

# Copy results back
scp zcpx:/tmp/eval_run.jsonl eval/results/<tag>/run_<N>.jsonl
```

**Important notes**:
- `--verbose` is REQUIRED with `--output-format stream-json`
- Use heredoc to pass the prompt (avoids quoting issues)
- `ServerAliveInterval` prevents SSH timeout during long runs
- Remote Claude may take 2-5 minutes for complex scenarios

### 4. Parse Execution Log
```bash
python3 eval/scripts/extract-tool-calls.py eval/results/<tag>/run_<N>.jsonl > eval/results/<tag>/tools_<N>.json
```

Then read BOTH the raw `.jsonl` and parsed `.json` to understand the full picture.

### 5. Cleanup eval-* Services
```bash
ssh -o ServerAliveInterval=30 zcpx \
  'claude --dangerously-skip-permissions \
  -p "List all services. Then delete every service whose hostname starts with eval-. Confirm deletion for each." \
  --model haiku \
  --max-turns 15 \
  --no-session-persistence > /tmp/eval_cleanup.log 2>&1'
```
The cleanup log stays on the remote (not needed locally). If cleanup fails, run `./eval/scripts/cleanup.sh`.

### 6. Analyze the Full Execution

Read the complete execution trace and answer:

- **Knowledge queries**: What did Claude search for? What was returned?
- **Knowledge gaps**: Did Claude retry, guess, or improvise because knowledge was missing?
- **Wrong knowledge**: Did Claude follow knowledge but get API errors (wrong version, wrong config)?
- **Unnecessary steps**: Did Claude take detours (delete+recreate, multiple retries)?
- **Error messages**: What Zerops API errors occurred? What was misconfigured?
- **Tool call count**: How efficient was the execution?

### 7. Fix Knowledge Gaps

For each identified gap:

a. **Look up the correct information** in `../zerops-docs/apps/docs/content/`. Use the directory structure:
   - Runtime docs: `go/`, `nodejs/`, `python/`, `php/`, `bun/`, `java/`, `dotnet/`, `rust/`, `elixir/`, `static/`
   - Service docs: `postgresql/`, `mariadb/`, `valkey/`, `elasticsearch/`, `meilisearch/`, `kafka/`, etc.
   - Reference: `references/` (zerops.yaml spec, import YAML format)
   - Features: `features/` (env vars, networking, scaling)

b. **Update the relevant knowledge file** in `internal/knowledge/`. Knowledge is organized as:
   - `foundation/grammar.md` — Import YAML schema, syntax rules
   - `foundation/runtimes.md` — Runtime service defaults and versions
   - `foundation/services.md` — Managed service defaults and versions
   - `foundation/wiring.md` — Cross-service env var references
   - `decisions/` — Technology selection guides (choose-database, choose-cache, etc.)
   - `guides/` — Operational guides (production-checklist, ci-cd, etc.)
   - `recipes/` — Framework-specific setup guides

c. **Keep changes minimal and factual** — add facts, not prose. One issue per change.

d. **Commit** with a descriptive message explaining what was wrong and what was fixed.

### 8. Check Early Stop

Track quality per iteration. A **clean run** means:
- Zero API errors (from tool results)
- No retries or workarounds
- Knowledge queries returned relevant results
- Services created correctly on first attempt
- 10 or fewer tool calls total

If the **last 2 consecutive iterations were clean runs** → STOP early. The knowledge base is in good shape.

### 9. Continue or Stop

If not stopping early, go to step 1 with the improved knowledge for the next iteration.

## After the Loop

Write `eval/results/<tag>/SUMMARY.md` with:

```markdown
# Eval Run: <tag>

## Overview
- Iterations: <N> of 5
- Early stop: yes/no
- Knowledge files changed: <count>

## Iterations

### Iteration 1
- Scenario: <brief description>
- Tool calls: <count>
- Errors: <count>
- Knowledge gaps found: <list>
- Fixes applied: <list of files changed>

### Iteration 2
...

## Knowledge Changes
- `foundation/runtimes.md`: <what changed and why>
- `recipes/django.md`: <what changed and why>
- ...

## Remaining Issues
- <anything that couldn't be fixed>
```

## Rules

1. **NEVER optimize for a specific scenario** — fixes must be generic improvements to the knowledge base
2. **Always cross-reference with zerops-docs** before changing knowledge — don't guess
3. **Keep knowledge concise** — add facts and specifics, not prose or explanations
4. **One knowledge change per issue** — don't batch unrelated fixes
5. **Test before moving on** — each iteration should build on confirmed improvements
6. **Don't modify code** — only change `internal/knowledge/*.md` files
7. **Preserve knowledge structure** — follow existing formatting and section conventions
8. **Commit after each fix** — small, descriptive commits with what was wrong and what was fixed

## Troubleshooting

- **SSH timeout**: Use `-o ServerAliveInterval=30 -o ServerAliveCountMax=10` on ssh commands
- **Empty stream-json**: `--verbose` flag is REQUIRED with `--output-format stream-json`
- **SSH buffering**: Never pipe remote Claude stdout directly over SSH. Always write to file on remote, then scp back.
- **Claude CLI errors on zcpx**: Claude Code v2.1.42, binary at `/home/zerops/.local/bin/claude`
- **No eval services to clean**: That's fine — cleanup is idempotent
- **Build fails**: Run `go test ./... -short` first to catch compilation errors
- **Binary hash mismatch**: Retry scp, check disk space on remote
- **Running binary can't be overwritten**: The deploy script handles this (temp file + mv)

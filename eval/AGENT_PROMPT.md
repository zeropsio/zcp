# Knowledge Eval Agent

You are improving the ZCP knowledge base through iterative testing against a live Zerops environment.

Your working directory is `/Users/macbook/Documents/Zerops-MCP/zcp`.

## Overview

ZCP is an MCP server that helps Claude configure Zerops PaaS infrastructure. Its knowledge base (7 theme files + 31 recipes in `internal/knowledge/`) drives the quality of service configuration. You will test this knowledge by running diverse scenarios on a remote server (`zcpx`), analyzing execution logs for gaps, fixing the knowledge using official Zerops docs as reference, and rebuilding.

## Environment

| Item | Value |
|------|-------|
| Remote host | `zcpx` (SSH configured) |
| Remote binary | `/home/zerops/.local/bin/zcp` |
| Remote MCP config | `~/.mcp.json` (server: `zerops`, cmd: `zcp serve`) |
| Remote Claude | `/home/zerops/.local/bin/claude` |
| Build command | `make linux-amd` (output: `builds/zcp-linux-amd64`) |
| Knowledge files | `internal/knowledge/` (themes/, recipes/) — embedded in binary |
| Legacy files | `internal/knowledge/` (foundation/, decisions/, guides/) — NOT embedded, reference only |
| Reference docs | `../zerops-docs/apps/docs/content/` (290 MDX files, authoritative) |
| Taxonomy | `eval/taxonomy.yaml` |

## Your Loop (max 5 iterations)

For each iteration:

### 1. Generate Scenario
```bash
python3 eval/scripts/generate.py
```
Save the output to `eval/results/<tag>/prompt_<N>.txt` where `<tag>` is your run tag (e.g., `run_20260214`) and `<N>` is the iteration number.

**Task types**: The generator picks from both import-only scenarios (single-service, runtime-db, full-stack, etc.) and **functional scenarios** (functional-dev, functional-fullstack). Prompts are intentionally minimal — they describe WHAT to create, not HOW. This tests whether the knowledge base and system prompt guide Claude to the right flow on its own. You can force a task type with `--task functional-dev` or let it pick randomly.

To focus on functional scenarios, use:
```bash
python3 eval/scripts/generate.py --task functional-dev
python3 eval/scripts/generate.py --task functional-fullstack
```

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
ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=20 zcpx \
  'claude --dangerously-skip-permissions \
  -p "$(cat /tmp/eval_prompt.txt)" \
  --model opus \
  --max-turns 60 \
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
- Import-only scenarios: 2-5 minutes. Functional scenarios: 3-10 minutes (build + deploy + verify)

### 4. Parse Execution Log
```bash
python3 eval/scripts/extract-tool-calls.py eval/results/<tag>/run_<N>.jsonl > eval/results/<tag>/tools_<N>.json
```

Then read BOTH the raw `.jsonl` and parsed `.json` to understand the full picture.

### 5. Cleanup eval Services
```bash
ssh -o ServerAliveInterval=30 zcpx \
  'claude --dangerously-skip-permissions \
  -p "List all services. Then delete every service whose hostname starts with eval. Confirm deletion for each." \
  --model haiku \
  --max-turns 15 \
  --no-session-persistence > /tmp/eval_cleanup.log 2>&1'
```
This cleans up both random-hostname services (`evalappbun242`) and fixed-hostname services (`evalappnodejs`, `evalsvcpostgresql`). The cleanup log stays on the remote. If cleanup fails, run `./eval/scripts/cleanup.sh`.

### 6. Analyze the Full Execution

Read the complete execution trace and answer:

- **Tool discovery**: Which tools did Claude call first? Did it find the right approach on its own?
- **Knowledge loading**: How did Claude load platform knowledge? What mode/params did it use?
- **Flow**: What sequence of steps did Claude follow? Was it efficient or did it wander?
- **Knowledge gaps**: Did Claude retry, guess, or improvise because knowledge was missing or wrong?
- **Error recovery**: How did Claude handle API errors? Did it recover gracefully?
- **Tool call count**: How many total calls? How many were wasted (retries, detours)?

**For functional scenarios**, also check:
- **Build success**: Did the build succeed on first attempt?
- **Deploy success**: Did the app come up and respond correctly?
- **Service connectivity**: Did the app connect to all managed services?
- **EVAL RESULT block**: Look for the `=== EVAL RESULT ===` block in the output.

### 7. Fix Knowledge Gaps

For each identified gap:

a. **Look up the correct information** in `../zerops-docs/apps/docs/content/`. Use the directory structure:
   - Runtime docs: `go/`, `nodejs/`, `python/`, `php/`, `bun/`, `java/`, `dotnet/`, `rust/`, `elixir/`, `static/`
   - Service docs: `postgresql/`, `mariadb/`, `valkey/`, `elasticsearch/`, `meilisearch/`, `kafka/`, etc.
   - Reference: `references/` (zerops.yaml spec, import YAML format)
   - Features: `features/` (env vars, networking, scaling)

b. **Update the relevant knowledge file** in `internal/knowledge/`. Only `themes/` and `recipes/` are embedded in the binary:
   - `themes/platform.md` — Zerops platform conceptual model
   - `themes/rules.md` — Actionable DO/DON'T rules and pitfalls
   - `themes/grammar.md` — Import YAML and zerops.yml schema reference
   - `themes/runtimes.md` — Runtime-specific exceptions (PHP, Node.js, Go, Bun, Java, .NET, etc.)
   - `themes/services.md` — Managed service cards (PostgreSQL, Valkey, Elasticsearch, etc.)
   - `themes/wiring.md` — Cross-service env var wiring patterns and syntax
   - `themes/operations.md` — Operational decisions (choose-database, choose-cache, choose-runtime)
   - `recipes/` — Framework-specific setup guides (31 recipes)

c. **Keep changes minimal and factual** — add facts, not prose. One issue per change.

d. **Commit** with a descriptive message explaining what was wrong and what was fixed.

### 8. Check Early Stop

Track quality per iteration. A **clean run** means:

**Import-only scenarios:**
- Zero API errors (from tool results)
- No retries or workarounds
- Services created correctly on first attempt
- 10 or fewer tool calls total

**Functional scenarios:**
- Import succeeded on first attempt
- Build succeeded on first attempt
- All verification checks passed
- EVAL RESULT verdict is PASS
- 30 or fewer tool calls total

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
- `themes/runtimes.md`: <what changed and why>
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

- **SSH timeout**: Use `-o ServerAliveInterval=30 -o ServerAliveCountMax=20` on ssh commands
- **Empty stream-json**: `--verbose` flag is REQUIRED with `--output-format stream-json`
- **SSH buffering**: Never pipe remote Claude stdout directly over SSH. Always write to file on remote, then scp back.
- **Claude CLI errors on zcpx**: Claude Code v2.1.42, binary at `/home/zerops/.local/bin/claude`
- **No eval services to clean**: That's fine — cleanup is idempotent
- **Build fails**: Run `go test ./... -short` first to catch compilation errors
- **Binary hash mismatch**: Retry scp, check disk space on remote
- **Running binary can't be overwritten**: The deploy script handles this (temp file + mv)
- **Functional: 502 Bad Gateway**: App not binding to 0.0.0.0. Check knowledge for the runtime's binding rule.
- **Functional: build FAILED**: Check zerops_logs + `zcli service log <hostname> --showBuildLogs --limit 50`
- **Functional: no EVAL RESULT block**: Claude ran out of turns. Increase --max-turns or check raw .jsonl for partial results.
- **Workflow engine**: ZCP now has action-based workflows (`action=show/start/transition/evidence/reset/iterate`). The legacy `workflow="bootstrap"` still works but the new engine tracks state across calls.

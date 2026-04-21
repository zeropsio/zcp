# Dispatch 6 — writer-fix-up-pass

**Dispatched at**: `2026-04-21T15:49:41`
**Description**: Writer fix-up pass
**Subagent type**: `general-purpose`
**Prompt length**: 8257 chars
**Tool-use id**: `toolu_01D3kqWipaKkLj7crDpkWJ2c`

---

## Transmitted prompt (verbatim)

```
You are fixing 4 validator failures from the first writer-sub-agent pass. Do NOT rewrite content; only apply the targeted fixes below. Every Edit must be preceded by a Read of the same file in this session.

## Fix 1 — Fragment marker format (ALL per-codebase READMEs)

The validator requires the EXACT literal form with a `#` before `-->`:
```
<!-- #ZEROPS_EXTRACT_START:intro# -->
...content...
<!-- #ZEROPS_EXTRACT_END:intro# -->
```

Your first pass used `<!-- #ZEROPS_EXTRACT_START:intro -->` (missing the trailing `#`). Fix all three fragment names (`intro`, `integration-guide`, `knowledge-base`) in all three READMEs: `/var/www/appdev/README.md`, `/var/www/apidev/README.md`, `/var/www/workerdev/README.md`.

Use Read then Edit with `replace_all: true` on each file. Replacements:
- `<!-- #ZEROPS_EXTRACT_START:intro -->` → `<!-- #ZEROPS_EXTRACT_START:intro# -->`
- `<!-- #ZEROPS_EXTRACT_END:intro -->` → `<!-- #ZEROPS_EXTRACT_END:intro# -->`
- `<!-- #ZEROPS_EXTRACT_START:integration-guide -->` → `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->`
- `<!-- #ZEROPS_EXTRACT_END:integration-guide -->` → `<!-- #ZEROPS_EXTRACT_END:integration-guide# -->`
- `<!-- #ZEROPS_EXTRACT_START:knowledge-base -->` → `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->`
- `<!-- #ZEROPS_EXTRACT_END:knowledge-base -->` → `<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->`

(If your first pass used different syntax, Read each file first and use the actual text you find.)

## Fix 2 — Worker drain code block (workerdev)

Validator: "worker README has a drain-topic gotcha but no fenced code block showing SIGTERM → drain → exit call sequence."

Add a NEW H3 item to `/var/www/workerdev/README.md` inside the `integration-guide` fragment (after any existing integration-guide items). The item needs: a heading, a one-sentence reason, and a fenced TypeScript code block showing the exact call sequence. Also mirror to `/var/www/workerdev/INTEGRATION-GUIDE.md`.

Suggested item:

```markdown
### Drain on SIGTERM

Rolling deploys send SIGTERM and wait up to 30 seconds before force-killing. The consumer must stop pulling new messages, let in-flight handlers complete, close the broker connection, and exit cleanly. See the platform's `rolling-deploys` guide.

```ts
// src/main.ts
import { NestFactory } from '@nestjs/core';
import { AppModule } from './app.module';
import { WorkerService } from './worker.service';

async function bootstrap() {
  const app = await NestFactory.createApplicationContext(AppModule);
  app.enableShutdownHooks();            // wires SIGTERM to onModuleDestroy
  await app.get(WorkerService).start();
}
bootstrap();

// src/worker.service.ts
async onModuleDestroy(): Promise<void> {
  // drain() stops pulling new messages, flushes in-flight ones,
  // then closes the NATS connection. Without it, mid-flight
  // messages redeliver to another replica on next deploy.
  try { await this.sub?.drain(); } catch { /* ignore */ }
  try { await this.nc?.drain(); } catch { /* ignore */ }
}
```
```

## Fix 3 — Intro fragment leak (appdev)

Validator: `app/README.md` intro fragment contains "Vite dev server on port 5173 with a same-origin proxy forwarding /api to apidev:3000" — that detail matches the fact "Vite dev needs server.proxy to forward /api to apidev:3000" which is manifest-marked `content_ig` (not `content_intro`).

Fix options:
- (A) Rewrite the appdev intro to NOT mention the proxy detail. Keep the intro high-level (what the app IS), not configuration detail.
- (B) Reclassify the fact as `content_intro` in the manifest. Less preferred.

Prefer (A). Read `/var/www/appdev/README.md`, find the intro fragment, replace the sentence mentioning the Vite proxy with a high-level statement about the dashboard (e.g., "A Svelte 5 + Vite SPA that renders a dashboard of managed-service feature cards calling the NestJS API."). The proxy detail stays in the integration-guide item where it already is.

## Fix 4 — Manifest completeness

Six facts from the log are missing from `/var/www/ZCP_CONTENT_MANIFEST.json`:

1. "Zerops-native env var names (db_hostname, queue_password, ...) read verbatim — no rename layer needed"
2. "zsc execOnce ${appVersionId} — bare key shared across multiple initCommands makes the later commands silently no-op"
3. "Feature healthCheck must be reachable without query args — made GET /api/cache?key optional"
4. "Cross-service env vars auto-inject project-wide — run.envVariables self-shadow creates literal ${key} strings"
5. "NATS subject `jobs.process` + queue group `workers` — apidev publishes, workerdev subscribes"
6. "Stage round-trip: dispatch → NATS → workerstage → Postgres UPDATE processedAt populated in <500ms"

Add each as a manifest entry with classification + routed_to. Copy the `fact_title` byte-for-byte from the titles above (note: HTML entity `&lt;` in #6 — use the literal `<` since the facts log likely has plain `<`; but CHECK by running `jq -r '.title' /tmp/zcp-facts-7743c6d8c8a912fd.jsonl` to get the exact titles).

Classification guidance for these 6:
1. Zerops-native names — `framework-invariant` (platform behavior) → route: `content_intro` (paraphrased in app/api README intros) OR `zerops_yaml_comment`. Pick one; check whether the concept appears in an intro or a YAML comment. If already in an intro, route `content_intro`; else `zerops_yaml_comment`.
2. bare ${appVersionId} no-op — `framework-invariant` (init-commands mechanism) → `content_gotcha` with citation to `init-commands`. Add a gotcha to `apidev/README.md` knowledge-base fragment if not already present.
3. /api/cache optional key — `self-inflicted` (the recipe's own controller had it wrong; a porter's own controller would be correct from day one) → `discarded` with reason "self-inflicted scaffold bug; porter's own controllers are independent."
4. env auto-inject self-shadow — `framework-invariant` → `content_ig` (platform-forced code change: do NOT re-declare cross-service vars in run.envVariables). Likely already teached in the IG; set route accordingly.
5. NATS jobs.process + workers — `intersection` (NATS queue group IS framework-side; Zerops HA semantics makes queue-group required) → `content_gotcha` (likely in workerdev/README.md) or `scaffold_preamble`. If the concept appears as a workerdev gotcha, route `content_gotcha`; else `scaffold_preamble`.
6. Stage round-trip verified — `verified_behavior` plain, no fix/no surprise → `discarded` with reason "verified behavior (positive evidence), not a porter-facing trap."

Adjust classifications per your read of where the content actually lives. Maintain the JSON validity.

## Process

1. Read each of `/var/www/{appdev,apidev,workerdev}/README.md` and `/var/www/{appdev,apidev,workerdev}/INTEGRATION-GUIDE.md`.
2. Apply Fix 1 (marker replacements) using Edit + replace_all on each README.
3. Apply Fix 2 (workerdev drain item) using Edit on both workerdev/README.md (inside integration-guide fragment) and workerdev/INTEGRATION-GUIDE.md.
4. Apply Fix 3 (appdev intro rewrite) using Edit on appdev/README.md.
5. Read `/var/www/ZCP_CONTENT_MANIFEST.json`, Read `/tmp/zcp-facts-7743c6d8c8a912fd.jsonl` to get exact titles, apply Fix 4 (add 6 manifest entries with classification + routed_to).
6. Run these checks:

```bash
# Marker format
for h in appdev apidev workerdev; do
  for f in intro integration-guide knowledge-base; do
    grep -q "#ZEROPS_EXTRACT_START:$f#" /var/www/$h/README.md || { echo "MISS $h $f start"; exit 1; }
    grep -q "#ZEROPS_EXTRACT_END:$f#" /var/www/$h/README.md || { echo "MISS $h $f end"; exit 1; }
  done
done

# Worker drain code block
grep -q "drain" /var/www/workerdev/README.md
grep -qE 'SIGTERM|onModuleDestroy' /var/www/workerdev/README.md

# Manifest valid + contains all facts
jq empty /var/www/ZCP_CONTENT_MANIFEST.json
jq -r '.title' /tmp/zcp-facts-7743c6d8c8a912fd.jsonl | while read t; do
  jq -e --arg t "$t" '.facts | map(select(.fact_title == $t)) | length > 0' /var/www/ZCP_CONTENT_MANIFEST.json > /dev/null || { echo "MANIFEST missing: $t"; exit 1; }
done

echo "all-green"
```

## Return format (under 200 words)

1. Files edited (one line each with final byte count)
2. Pre-attest check: exit code + last line of output
3. Anything that needed judgement beyond the instructions above
```

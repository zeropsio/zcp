# Dispatch 7 — final-readme-fix-round

**Dispatched at**: `2026-04-21T20:56:51`
**Description**: Final README fix round
**Subagent type**: `general-purpose`
**Prompt length**: 5741 chars
**Tool-use id**: `toolu_01JjXsZCfYX4hdBnU6BTXeKc`

---

## Transmitted prompt (verbatim)

```
Fix remaining Zerops recipe README check failures. Three mechanical fixes across three files.

Files:
- `/var/www/apidev/README.md`
- `/var/www/workerdev/README.md`
- `/var/www/appdev/README.md`

Do NOT touch any other files. CLAUDE.md, INTEGRATION-GUIDE.md, GOTCHAS.md, zerops.yaml, environments/**, manifest — ALL OFF-LIMITS.

## Fix A — Single-line gotcha stems

Each `### Gotchas` bullet in every README's `knowledge-base` fragment must have its `**bold stem**` on a SINGLE line. Current bullets wrap the stem across two lines like:

```
- **502 Bad Gateway from the L7 balancer even though the container
  is healthy** -- ...
```

The stem text (between the `**`) must not contain a newline. Rewrite every bullet in every knowledge-base fragment so the opening `- **<stem>**` sits on ONE source line, then the ` -- <body>` can wrap freely onto subsequent indented lines.

Example target:
```
- **L7 balancer returns 502 even though the container is healthy** -- `app.listen(port)` on NestJS defaults to binding `127.0.0.1`. Zerops routes to the container over its VXLAN IP, so the request reaches the container but nothing is listening on the non-loopback interface. Pass `'0.0.0.0'` as the second argument. See `http-support`.
```

Stem on one line. Body follows `-- ` and wraps as needed.

Apply to ALL bullets in ALL three READMEs. The existing content is good — just collapse the stems.

## Fix B — appdev: rewrite 2 synthetic gotchas

Two appdev gotchas are flagged synthetic. Each must (a) name a Zerops platform constraint (`base: static`, `L7 balancer`, `${env_var}`, `httpSupport`, `execOnce`, `VXLAN`, etc.) AND/OR (b) describe a concrete failure mode (quoted error string, HTTP status, measurable wrong state).

Target bullets:

1. Current: `- **Multipart upload returns 400 \`Unexpected end of form\`** -- ...`
   Rewrite to explicitly name the L7 balancer and/or VXLAN routing AND keep the `400 Unexpected end of form` failure mode. Example: `- **L7 balancer forwards multipart POST but the API rejects it with \`400 Unexpected end of form\`** -- the fetch call set \`Content-Type: multipart/form-data\` manually and lost the boundary parameter the balancer forwards verbatim to Nest's body parser. Pass a \`FormData\` body without a Content-Type header; the browser emits the boundary and Nest's multer reads it correctly.`

2. Current: `- **Static container 404s on a deep route like \`/items\`** -- ...`
   Rewrite to explicitly name `base: static` and/or the Nginx fallback absence. Example: `- **\`base: static\` returns 404 on any SPA route deeper than \`/\`** -- Zerops's \`base: static\` serves files verbatim through Nginx with no SPA fallback rewrite. A deep-link request like \`/items\` has no \`items/index.html\` on disk and the balancer surfaces the 404. For SPA client-side routes, configure a rewrite-to-\`index.html\` on the static base or deploy behind a runtime service that can rewrite. See \`deploy-files\`.`

Keep both bullets inside the appdev knowledge-base fragment. Apply Fix A (single-line stem) to them as well.

## Fix C — workerdev intro: remove subject/queue names

The workerdev intro fragment still mentions `jobs.run` and `jobs-worker` literally. A fact in the manifest is routed non-intro and names those same identifiers — having them in the intro trips the cross-surface honesty check. Rewrite the intro to describe capability generically.

Current: `A NestJS 11 NATS microservice on \`nodejs@22\` that consumes \`jobs.run\` in queue group \`jobs-worker\` and writes results back to the shared Postgres \`jobs\` table. Part of the nestjs-showcase recipe.`

Rewrite to: `A NestJS 11 NATS microservice on \`nodejs@22\` that consumes job-dispatch messages from the shared broker, simulates a short unit of work, and writes the result back to the shared Postgres. Part of the nestjs-showcase recipe.`

Keep the intro as a single line (1-3 lines total).

Also scan the header section of workerdev README (the H1 + subtitle above the intro marker, roughly lines 1-5) for `jobs.run` / `jobs-worker` literal mentions and soften them to generic terms. Example header rewrite:

From:
```
# workerdev -- NestJS NATS worker

NestJS 11 microservice on `nodejs@22`. Subscribes to NATS subject
`jobs.run` in queue group `jobs-worker` and updates `jobs` rows in
the shared Postgres. Part of the nestjs-showcase recipe.
```

To:
```
# workerdev -- NestJS NATS worker

NestJS 11 microservice on `nodejs@22`. Subscribes to a NATS job-dispatch subject and updates rows in the shared Postgres. Part of the nestjs-showcase recipe.
```

The specific `jobs.run` + `jobs-worker` names remain in the integration-guide code blocks and in CLAUDE.md — those are the declared surfaces for that fact, per the manifest.

## Execution

1. Read all three READMEs.
2. Apply Fix A by rewriting every knowledge-base bullet with a single-line stem. Apply via Edit targeted replacements or Write full-file rewrites — either works.
3. Apply Fix B by rewriting the two flagged appdev bullets.
4. Apply Fix C by editing the workerdev intro fragment and header.
5. Verify with:
   ```bash
   # Stems on single line (no stem spans two lines)
   for h in apidev workerdev appdev; do
     awk '/ZEROPS_EXTRACT_START:knowledge-base#/{f=1;next} /ZEROPS_EXTRACT_END:knowledge-base#/{f=0} f' /var/www/$h/README.md | grep -c '^- \*\*[^*]*$'
   done
   # Expected output: 0 0 0 (no bullets with open ** without close ** on same line)
   
   # workerdev intro has no jobs.run / jobs-worker
   awk '/ZEROPS_EXTRACT_START:intro#/{f=1;next} /ZEROPS_EXTRACT_END:intro#/{f=0} f' /var/www/workerdev/README.md | grep -cE 'jobs\.run|jobs-worker'
   # Expected: 0
   ```

Report the three files' new byte counts and the verification command output.
```

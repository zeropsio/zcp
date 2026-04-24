# Content authoring

## Voice — the reader is a porter, never another recipe author

Everything you write — fragment bodies, `zerops.yaml` inline comments,
committed source-code comments, README prose — is read by someone
deploying this recipe into their own project.

**Never write:** "the scaffold", "feature phase", "pre-ship contract
item N", "showcase default", "showcase tier", "showcase tradeoff",
"the recipe", "we chose", "we added", "grew from", "scaffold smoke
test".

**Always write:** descriptions of the finished product. The product
IS wired. The product HAS the health probe. The product HANDLES the
upload. There is no authoring "before" for a porter.

Good vs bad:

- yaml inline: `# Bucket policy is private — signed URLs give
  time-bounded access without exposing the bucket.` ← GOOD
- yaml inline: `# Private (showcase default) — 15 min tradeoff.` ← BAD
- ts: `// /health returns 200 once the runtime is ready.` ← GOOD
- ts: `// /health added per pre-ship contract item 1.` ← BAD

Produce your codebase's `zerops.yaml` (with inline comments) + record
5 fragments via `zerops_recipe action=record-fragment`:

- `codebase/<h>/intro` — one paragraph
- `codebase/<h>/integration-guide` — porter items starting at `### 2.`
  (item #1 is engine-generated — see below)
- `codebase/<h>/knowledge-base` — `**Topic**` bullets with guide ids
- `codebase/<h>/claude-md/service-facts` — port/hostname facts
- `codebase/<h>/claude-md/notes` — operator notes (dev loop, SSH)

### Integration Guide — item #1 is engine-owned

The engine auto-generates IG item #1 during stitch: a `### 1. Adding
\`zerops.yaml\`` heading, an intro derived from your yaml (setups
declared, whether initCommands run migrations / seed / search-index,
whether readiness + health checks ship), and a fenced yaml block
carrying `<cb.SourceRoot>/zerops.yaml` verbatim. Reference:
`laravel-showcase-app/README.md`.

Your `codebase/<h>/integration-guide` fragment contains items #2+ —
porter-facing app-side changes. Start headings at `### 2.`, `### 3.`,
etc. Do NOT author item #1. Do NOT describe the yaml in English as
a numbered item — the yaml block IS the description; clarifications
go in yaml inline comments.

### Knowledge Base — `**Topic** — prose` only

Every `codebase/<h>/knowledge-base` bullet: `**<topic>**` + em-dash +
2–5 sentences of prose. A porter scans topic names to find the entry.

Good:

```
- **Expose X-Cache via CORS** — a cross-origin fetch only sees headers
  listed under Access-Control-Expose-Headers. app.enableCors() must
  pass exposedHeaders: ['X-Cache']. Cited guide: `http-support`.
```

Bad (debugging-runbook triple — belongs in CLAUDE.md/notes, NOT KB):

```
- **symptom**: 502 from balancer. **mechanism**: bind default.
  **fix**: listen on '0.0.0.0'.
```

Do NOT use `**symptom**:` / `**mechanism**:` / `**fix**:` triples in
`codebase/<h>/knowledge-base`. Debugging runbooks live in
`codebase/<h>/claude-md/notes`.

## Placement

- Stanza IS in yaml → yaml inline comment
- Absence / alternative / consequence → KB (`**Topic** — prose`)
- Topology walkthrough → IG (items #2+)
- Debugging runbook (symptom/mechanism/fix) → claude-md/notes
- Dev loop / SSH / curl → claude-md/notes

Why-not-what. Use `because`, `so that`, `otherwise`, `trade-off`.

## Classify before routing

Self-inflicted bugs and pure framework quirks DISCARD. Platform ×
framework intersections → KB with a `zerops_knowledge` citation.

Dev vs prod process model + `zerops_dev_server` live in
`principles/dev-loop.md` (injected above). Implicit-webserver runtimes
(php-nginx, static) skip the `zsc noop` rule for their backend but may
still need the dev-server for a compiled frontend — see the atom's
carve-out.

Mount vs container execution-split (editor tools on the mount,
framework CLIs via ssh) lives in `principles/mount-vs-container.md`
(injected above). Never run `npm install` / `tsc` / `nest build`
against the SSHFS mount locally.

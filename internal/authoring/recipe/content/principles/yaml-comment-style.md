# YAML comment style

ASCII `#` only, one hash per line, one space after, then prose.

## Comment density — aim for ~35%, not "every directive"

yaml comments justify DECISIONS THIS RECIPE MADE. They are not field
documentation (the field name says what; the docs say how). Aim for
~35% comment-line density (1 line of comment per 2-3 lines of yaml on
average). The goldens consistently hold around 36%; run-32 shipped
56-63% and the porter waded through prose to find yaml.

**Comment the non-obvious only.** If the directive's purpose is
obvious from its name + value, the comment is noise. `os: ubuntu`,
`cache: [node_modules]`, `enableSubdomainAccess: true` need no
rationale at the field site (subdomain access is named in the env
intro; cache survives between builds is the field's literal name).

**Don't paragraph-defend every field.** A 4-7 line essay defending
`npm ci` over `npm install` is below-bar — the porter doesn't need a
treatise on lockfile semantics to read the build commands. One
sentence naming the decision suffices: *"`npm ci` for reproducible
builds — fails fast on lockfile drift."*

**Don't restate what the field does.** *"`readinessCheck` blocks
traffic until the new container responds"* — the field name says
that. Comment instead with the recipe-specific decision: *"`/api/health`
is the readiness path because it doesn't touch the DB — readiness
shouldn't gate on a downstream service."*

**The signal-vs-noise test.** Read the yaml without the comment. If
the porter would still know what's happening (field name + value
self-explain), the comment is redundant — drop it. If the porter
would wonder *why this value*, the comment earns its line.

## The shape

Each comment is a **multi-line block**. Each line carries up to ~65
characters of prose. A run of adjacent `#` lines reads as one
paragraph. To start a new paragraph inside the same block, use a
bare `#` line (not an empty line — yaml block continuity needs the
hash).

**Wrap, do NOT stuff.** A 500-char line of prose with no breaks is
not a "block" — it's a single line that happens to start with `#`.
Goldens never do this. Look at any block in
`/Users/fxck/www/laravel-showcase-app/zerops.yaml` — every line
ends well before 70 chars.

**One causal word per block is enough.** The first paragraph
carries rationale (`because` / `so that` / `otherwise` /
`trade-off` / em-dash). Following paragraphs carry detail or
porter-adapt invitations. Do NOT stuff every sentence with a
because.

Short labels (≤40 chars) pass unconditionally — `# Base image`,
`# Bucket policy` need no rationale.

## GOOD vs BAD — the same content authored two ways

### GOOD (~38% density: 3 comment lines / 5 yaml lines)

```yaml
# Aliased to stable own-keys so application code reads its own
# names — swap a managed service later with a yaml-only edit.
# S3_REGION is required by the SDK; value is irrelevant on Zerops.
envVariables:
  DB_HOST: ${db_hostname}
  CACHE_HOST: ${cache_hostname}
  NATS_HOST: ${broker_hostname}
  S3_REGION: us-east-1
```

### BAD (8-line essay + empty `#` separator: 62% density, paragraph-defends every field)

```yaml
# Cross-service refs (db_*, cache_*, broker_*) re-aliased under
# stable own-keys (DB_HOST, CACHE_HOST, NATS_HOST, etc.) so the
# application code reads its own names — swap a managed service
# later with a yaml-only edit, no code rewrite.
#
# Replace S3_REGION with whatever your library expects; the value
# is irrelevant for the platform's S3-compatible storage but must
# be set or the SDK refuses to construct.
envVariables:
  DB_HOST: ${db_hostname}
  ...
```

Same teaching, twice the prose. The decision is "alias under own
keys" + "S3_REGION must be set"; readers don't need a treatise on
swap-without-code-rewrite to extract that. Drop the
parenthetical enumerations and the "in case you're wondering"
qualifiers; lead with the decision in one sentence.

**Also forbidden — single-line wall of prose.** A 400-char line
that happens to start with `#` is not a comment block; the
viewer soft-wraps it but it reads as a paragraph, not as
field-adjacent rationale. Wrap to ~65 chars per line.

## Anti-patterns to NOT produce

- One unwrapped sentence per block. Wrap to ~65 chars.
- `# ` lines with no body text. Either drop them or use them as
  paragraph separators between actual `#`-prefixed body lines.
- Decorative dividers — ANY shape, not just ASCII. Forbidden:
  `# =====`, `# ---`, `# ----`, AND Unicode box-drawing glyphs
  `# ──`, `# ━━`, `# ══` (codepoints U+2500..U+257F + block
  elements U+2580..U+259F). Block boundaries are the directive
  lines themselves, not ASCII art and not pretty-print Unicode.
  Cross-terminal rendering breaks: some renderers show real
  box-drawing, some show mojibake. Plain ASCII is the only
  portable choice.
- Restating the field name. `# initCommands: runs init commands` is
  filler — the field name is right there. Lead with the WHY.

## When in doubt — read the goldens

`/Users/fxck/www/laravel-showcase-app/zerops.yaml` and
`/Users/fxck/www/laravel-jetstream-app/zerops.yaml` are the two
reference shapes. Every block in those yamls is a multi-line
paragraph wrapping at ~65 chars. Match that.

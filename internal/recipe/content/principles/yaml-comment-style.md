# YAML comment style

ASCII `#` only, one hash per line, one space after, then prose.

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

### GOOD (multi-line wrap, ~60 chars per line, paragraph break, voice)

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
  CACHE_HOST: ${cache_hostname}
  NATS_HOST: ${broker_hostname}
  S3_REGION: us-east-1
```

### BAD (one long line + empty `#` separator + another long line)

```yaml
# Cross-service refs (db_*, cache_*, broker_*) re-aliased under stable own-keys (DB_HOST, CACHE_HOST, NATS_HOST, etc.) so the application code reads its own names — swap a managed service later with a yaml-only edit, no code rewrite.
# 
# Replace S3_REGION with whatever your library expects; the value is irrelevant for the platform's S3-compatible storage but must be set or the SDK refuses to construct.
envVariables:
  ...
```

Same words, but the BAD shape is one ~400-char line followed by an
empty `#` followed by another long line. The browser / file viewer
soft-wraps it, but it reads as a wall of prose, not a paragraph.
The empty `#` separator with no content is also wrong — the GOOD
shape uses `#` as a paragraph separator BETWEEN body lines, not as
a decorative gap.

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

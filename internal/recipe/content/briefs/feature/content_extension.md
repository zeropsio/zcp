# Content extension

## Voice — the reader is a porter, never another recipe author

Everything you write — fragment bodies, `zerops.yaml` inline comments,
committed source-code comments, README prose — is read by someone
deploying this recipe into their own project, not another recipe
author. **Never write:** "the scaffold", "feature phase", "pre-ship
contract item N", "showcase default", "showcase tradeoff", "the
recipe", "we chose", "we added", "grew from", "scaffold smoke test".
**Always write:** descriptions of the finished product. The product
IS wired; the product HANDLES the upload; there is no authoring-phase
"before" for a porter.

Your additions EXTEND the scaffold's fragments — they do not replace.
`record-fragment` on IG / knowledge-base / claude-md/* appends to the
existing body; root/env ids overwrite. Same placement rubric as
scaffold — yaml-comment, IG, KB, CLAUDE.md notes.

KB bullets use `**Topic** — prose` only. Do NOT use `**symptom**:` /
`**mechanism**:` / `**fix**:` triples; debugging runbooks live in
`claude-md/notes`.

- Adding a dep → extend KB if the choice is non-obvious
- Adding an env var → extend `zerops.yaml` with an inline comment
- Adding an `initCommand` (seed, scout:import) → consult the execOnce
  key-shape atom below before picking the key

Keep `codebase/<h>/claude-md/*` extensions terse (30–50 lines, cap
60). Never add `Quick curls`, `Smoke test`, `Local curl`, `Redeploy
vs edit`, or `Boot-time connectivity` subsections — cross-codebase
runbooks live in the recipe root README.

Typical scale: 1–2 KB bullets + 0–1 IG item per feature. Most features
change code, not topology.

Mount vs container execution-split (editor tools on the mount,
framework CLIs via ssh) lives in `principles/mount-vs-container.md`
(injected above). Local `npm install` / `npx build` against the SSHFS
mount tunnels through FUSE and misses the container's env vars — run
framework CLIs via `ssh <hostname>dev "..."`.

# Recording feature-phase facts

Record every platform-trap, porter-change, scaffold-decision, and
browser-verification fact via **`zerops_recipe action=record-fact`**
(the v3 tool) — **NOT** the legacy `zerops_record_fact` tool. v3
records land in `facts.jsonl` where the classifier and surface
validators see them; the legacy tool writes to `legacy-facts.jsonl`
(invisible to v3's classification pipeline).

Shape (all fields required — camelCase):

- `topic` — short kebab-case
- `symptom` — observable failure or signal (status + quoted line)
- `mechanism` — why (platform-side; both sides if intersection)
- `surfaceHint` — one of: `root-overview`, `tier-promotion`,
  `tier-decision`, `porter-change`, `platform-trap`, `operational`,
  `scaffold-decision`, `browser-verification`
- `citation` — `zerops_knowledge` guide id, published-recipe URL, or
  `none` for browser-verification / operational observations
- `scope` — optional `<service>/<area>` string when the fact is tied
  to a specific codebase or tab

Classification before routing (same contract as scaffold):

- Self-inflicted findings (code bugs you authored then fixed) → DISCARD
- Platform × framework intersections → KB bullet + cite the guide
- Genuine platform traps → KB bullet + cite the guide
- Operational observations (logs, dev-loop ergonomics) → CLAUDE.md notes
- Browser-walk verifications (see phase-entry step 7) → `surfaceHint:
  browser-verification` with the screenshot + console digest in
  `extra.screenshot` / `extra.console`

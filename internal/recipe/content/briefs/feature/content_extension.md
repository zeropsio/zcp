# Content extension

Your additions EXTEND the scaffold's fragments — they do not replace.
`record-fragment` on IG / knowledge-base / claude-md/* appends to the
existing body; root/env ids overwrite. Same placement rubric as
scaffold — yaml-comment, IG, KB, CLAUDE.md notes.

- Adding a dep → extend KB if the choice is non-obvious
- Adding an env var → extend `zerops.yaml` with an inline comment
- Adding an `initCommand` (seed, scout:import) → consult the execOnce
  key-shape atom below before picking the key

Typical scale: 1–2 KB bullets + 0–1 IG item per feature. Most features
change code, not topology.

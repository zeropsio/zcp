# (rejected) Dedicated atom teaching the P3 `source="zerops.yaml"` tag

**Why rejected** (2026-05-28, Stage-2 review): the tag is self-describing and
already documented in the `zerops_discover` `includeEnvs` schema description; a
dedicated atom would spend scarce 32 KB-cap budget for near-zero marginal value
(the `develop-env-var-channels` precedence atom already teaches "yaml owns the
key"). Agents see + understand the tag without proactive prep.

**Context**: Phase 3 made `zerops_discover` / `zerops_env get` surface a live
runtime's yaml-baked `run.envVariables` tagged `source: "zerops.yaml"` (the GUI
"from master" layer). Both the atom-composition review agent and Codex noted no
atom prepares the agent for the tag. Considered adding a small teaching line;
rejected per the reasoning above. If a future eval shows agents misreading the
tag, revisit — but the schema text + the existing precedence atom are expected
to suffice.

**Refs**: `internal/tools/discover.go:44` (schema description carries the tag
explanation), `internal/ops/discover.go` `attachEnvs` (emits the tag),
`internal/content/atoms/develop-env-var-channels.md` (precedence teaching).

# Single-question tests

Each of the seven surfaces has exactly one question. You apply it to every item on that surface. An item that fails its surface's question is a finding — it is not repaired by rewording; the content does not belong on the surface at all.

## The seven questions

### Root README

**"Would a developer, reading only this file for 30 seconds, know what this recipe deploys and which tier to pick for their situation?"**

Pass: every managed service is named; every environment tier is listed with a one-line description of its audience; a deploy pointer exists. Fail: architecture deep-dives, debugging narratives, gotchas, code details, scaffold-decision rationale.

### Environment README

**"Does this teach me when I would outgrow this tier and what changes when I promote to the next one?"**

Pass: the tier's audience is named (agent-driven iteration / remote dev / local dev / stage reviewer / small prod / HA prod); scale is stated (single replica / multi-replica / HA); the delta against the adjacent tier is stated; any tier-specific operational concern is called out. Fail: service-by-service rationale (belongs on the `import.yaml` comments next door); framework quirks; restatements of platform mechanisms.

### Environment `import.yaml` comment

**"Does each service block explain a decision — why this service is present at this tier, why this scale, why this mode — or does it merely narrate what the field does?"**

Pass: every service block's comment explains a tier-specific decision. Fail: templated per-service opening repeated word-for-word across services; field-narration (`# enables zero-downtime rolling deploys` on every service); general platform facts that belong in a platform guide.

### Per-codebase README (intro + body)

**"After reading this file, can I tell in one paragraph what this codebase is and where to go next — Integration Guide for porting, Gotchas for platform traps, `CLAUDE.md` for operating the repo?"**

Pass: one-paragraph intro identifies the codebase's role, names the framework, points to the companion files. Fail: the intro rehashes Integration Guide material; the intro rehashes gotchas; the file is template boilerplate with no recipe-specific content.

### Per-codebase `CLAUDE.md`

**"Is this useful for operating THIS repo — running the dev loop, exercising features by hand — as distinct from deploying it to Zerops or porting it to other code?"**

Pass: the dev loop is stated (how to start, where it binds, the health path); migration and seed commands are stated for hand-iteration; any repo-local trap (SSHFS uid, dev-deps pruning on redeploy) is called out. Fail: deploy instructions; platform gotchas; framework basics the operator already knows.

### Per-codebase `INTEGRATION-GUIDE.md` item

**"Would a porter bringing their own existing code — NOT using this recipe as a template — need to copy THIS exact content into their own app?"**

Pass: each item is a concrete action (bind `0.0.0.0`, add `forcePathStyle: true`, register a SIGTERM handler) with a one-sentence reason tied to a platform mechanism and a code block the porter can copy. Fail: an item that describes the recipe's own scaffold helper files (the porter does not have those files); framework-setup steps the porter has already done for their own app; debugging narratives phrased as imperatives.

### Per-codebase `GOTCHAS.md` item

**"Would a developer who has read the Zerops docs AND the relevant framework docs STILL be surprised by this?"**

Pass: the gotcha describes a platform behaviour that surprises even a docs-literate reader; it cites the matching platform topic per the citation-audit atom; it names a concrete observable symptom (status code, quoted error, measurable wrong state). Fail: the gotcha restates something already in the Zerops docs (no added value); the gotcha is a framework-only quirk (belongs in framework docs); the gotcha is self-inflicted (a bug in the recipe's own code that got fixed).

## Disposition on fail

An item that fails its surface's question is **removed or rerouted**, not rewritten to pass. The test fails because the content doesn't belong on this surface, not because the phrasing is wrong. Rerouting means either moving the item to the surface where it DOES belong (if the item is genuinely useful elsewhere) or discarding it (if no surface claims it). The reporting-taxonomy atom covers how to act on the finding.

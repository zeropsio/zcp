# symbol-contract-consumption

The SymbolContract JSON interpolated into this dispatch is the authoritative cross-codebase symbol table. It was computed once before the first scaffold dispatch and transmitted byte-identically to every scaffold sub-agent that worked in this session. You consume the same contract now, so every name the scaffold phase committed to is already correct — your job is to extend the codebases without drifting from it.

## What the contract declares

- `EnvVarsByKind` — for each managed-service kind (db, cache, queue, storage, search, mail), the platform-provided env var names the containers see at runtime. Read these names as-is from `process.env` (or the framework equivalent) in every codebase. Do not invent variants — no `_PASSWORD` where the contract says `_PASS`, no `URL` where the contract says `HOST` + `PORT`.
- `HTTPRoutes` — the cross-codebase route table. The feature list declares which entries each feature adds; implement each route at the exact path the contract declares.
- `NATSSubjects` + `NATSQueues` — producer publishes to the subject; every consumer of that subject subscribes with the contract's queue group. Subject names and queue names are identical across every codebase that speaks to them.
- `DTOs` — the list of TypeScript interface (or language equivalent) names that must appear identically across api + worker + ui codebases. Each DTO is declared once at the top of the owning api controller (or sibling `dto.ts`), then copy-pasted byte-identically into the consuming code in the other codebases. There is no shared module by convention.
- `FixRecurrenceRules` — rules whose `appliesTo` contains a role you are editing continue to bind. Any code you add that would re-trigger a rule (for example, URL-embedded credentials in a new publisher) is a regression of a closed class.

## How to use it while implementing a feature

1. For every new HTTP route: read the route path from the contract's `HTTPRoutes` entry for the feature; do not paraphrase.
2. For every new NATS publish: read subject from `NATSSubjects`; for every new subscription: read subject + queue group from `NATSSubjects` + `NATSQueues.workers` (or the role-matching entry).
3. For every new DTO: declare the interface with the exact name from `DTOs` at the top of the api controller, then copy-paste into the frontend component and the worker handler in the same session.
4. For every new env var read: use the exact name from `EnvVarsByKind[kind]` for the service kind you are reading.
5. Before finishing a feature, grep your own diff across every codebase you touched for the contract's names — any divergence is a contract break, not a style preference.

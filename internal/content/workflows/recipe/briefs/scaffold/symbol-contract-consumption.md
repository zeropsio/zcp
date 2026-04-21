# symbol-contract-consumption

The JSON object that follows this section is the canonical symbol table shared across every scaffold dispatch in this run. It is the same bytes on every parallel codebase's dispatch — consume it directly; do not re-derive symbol names from framework documentation or memory.

## How to read the contract

- `EnvVarsByKind[kind]` — platform-provided env var names per managed-service kind (`db`, `cache`, `queue`, `storage`, `search`, `mail`). Read env var names from this map; never invent (`DB_PASS` vs `DB_PASSWORD` is decided here). A scaffold that reads `process.env.DB_PASSWORD` while a sibling scaffold reads `process.env.DB_PASS` causes a runtime class that the contract exists to eliminate.
- `HTTPRoutes` — cross-codebase route paths declared once. API scaffolds expose these paths; frontend scaffolds call these paths. Feature routes that are not present here are out-of-scope for this substep (the feature sub-agent adds them later).
- `NATSSubjects` — producer-side subject names, keyed by logical event. Producers publish to the subject; consumers subscribe with the paired queue.
- `NATSQueues` — consumer-side queue names for competing-consumer semantics. Every NATS subscriber must declare `queue: '<contract.NATSQueues[role]>'` so `minContainers > 1` processes each message once.
- `Hostnames` — `{role, dev, stage}` entries per runtime target. Your mount is `/var/www/{{.Hostname}}/`; other roles' hostnames tell you who you talk to at runtime, not where you write files.
- `DTOs` — DTO / interface names that must match byte-for-byte across every codebase consuming them. API scaffolds produce them; frontend and worker scaffolds import or mirror them.
- `FixRecurrenceRules` — a list of positive-form invariants. Each rule carries an `id`, a `positiveForm` sentence stating what must be true, a `preAttestCmd` shell command that proves it, and an `appliesTo` list of hostname roles. Before returning, run every rule's `preAttestCmd` whose `appliesTo` contains your role or `any`. Non-zero exit = fix the code and re-run that rule. This is your author-runnable pre-attest layer; the server gate runs the same commands.

## Applying the contract while you write code

- When writing a service client, read the env var names from `EnvVarsByKind[kind]` first, then write the client-init code against those exact names.
- When writing an HTTP route, look up the path in `HTTPRoutes` and use that string literal.
- When writing a NATS subscription, read the queue name from `NATSQueues` and pass it as the queue-group option. Pass user/pass as separate client-options fields; never embed them in a `nats://user:pass@host` URL.
- When a rule's `appliesTo` does not contain your role, skip its `preAttestCmd` but still honor the contract's shared sections (routes, DTOs, subjects) because other codebases in this run consume them.

## Contract

The contract JSON for this run follows.

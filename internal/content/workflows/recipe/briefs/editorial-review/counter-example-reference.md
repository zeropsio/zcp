# Counter-example reference

Concrete anti-patterns from prior recipe runs. Use this atom as a pattern-matching library: for each item on each surface you walk, ask whether the item's shape resembles any of the patterns below. A resemblance is not proof — but it is a signal to apply the surface's single-question test more carefully and to check the classification-reclassify atom's taxonomy more strictly.

Each entry below describes the published anti-pattern by behaviour — what the content looked like, why it was wrong, what the correct form would have been. The behavioural description is what you pattern-match on; the severity to report is at the reporting-taxonomy atom.

## Self-inflicted content shipped as a gotcha

### Pattern: "`zsc execOnce` recorded a successful seed that produced zero output"

**What the published content looked like**: a gotcha on an api codebase's `GOTCHAS.md` describing how an init-command that silently exited with status 0 inserted zero rows, and `execOnce` then refused to re-run on subsequent deploys because it remembered success.

**Why it was wrong**: the seed script silently exited 0 with no output. That is a seed-script bug — the script should fail loudly on empty inserts. `execOnce` honoured the exit code exactly as the `init-commands` guide says it will. The platform mechanism is doing what the docs describe. The observation is a self-inflicted bug fixed in the recipe's own seed script; a porter with a seed script that fails loudly on empty inserts will not hit this.

**Correct form**: discard entirely. The fix belongs in the seed script; there is no teaching for a porter. If the recipe wants to warn future maintainers, the warning belongs in a code comment on the seed script, not in the deliverable.

## Framework-quirk shipped as a gotcha

### Pattern: "`setGlobalPrefix` collides with `@Controller` decorators"

**What the published content looked like**: a gotcha on a NestJS api codebase's `GOTCHAS.md` describing how calling the global-prefix API on the Nest application instance double-prefixes routes declared with decorator-level path arguments.

**Why it was wrong**: pure NestJS framework behaviour. The platform is not involved. A porter using NestJS already knows this or will learn it from the NestJS docs. Shipping it as a Zerops gotcha teaches the wrong thing — it suggests the platform is the variable.

**Correct form**: discard. Belongs in framework docs or, at most, a code comment next to the line where the global-prefix call lives.

### Pattern: "vite-plugin peer-requires a specific Vite major version"

**What the published content looked like**: a gotcha on a Svelte frontend codebase's `GOTCHAS.md` describing how a plugin's `peerDependencies` field requires a newer Vite than the project had, producing an `EPEERINVALID` from npm.

**Why it was wrong**: npm registry metadata. Zero platform involvement. Belongs in the project's `package.json` or dependency-upgrade notes.

**Correct form**: discard. If the recipe needs a note, put it in the dependency manifest itself.

## Scaffold decision shipped as a gotcha

### Pattern: "our `api.ts` helper's content-type check catches the SPA-fallback class of bug"

**What the published content looked like**: a gotcha on a frontend codebase's `GOTCHAS.md` describing how a helper file the recipe itself authored checks the `application/json` content-type header to detect Nginx SPA fallbacks returning `200 text/html` on `/api/*` misses.

**Why it was wrong**: `api.ts` is the recipe's own scaffold helper. A porter bringing their own frontend does not have `api.ts`. The underlying platform invariant — SPA fallback can shadow API routes returning HTML instead of JSON — is real and worth teaching, but the teaching belongs in the Integration Guide (as a principle a porter applies to their own HTTP client) and the specific implementation belongs in a code comment on the helper file.

**Correct form**: split the content. The principle (check response content-type before parsing JSON) goes to `INTEGRATION-GUIDE.md`. The implementation detail (our `api.ts` does it this way) goes to a code comment. Nothing belongs in `GOTCHAS.md`.

## Folk-doctrine (real trap, fabricated explanation)

### Pattern: "the API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed"

**What the published content looked like**: a gotcha on a worker codebase's `GOTCHAS.md` describing an environment-variable self-shadow trap, with an explanation that one codebase escaped the symptom because of a timing-dependent resolver ordering.

**Why it was wrong**: the explanation is invented. Both codebases shipped identical self-shadow patterns; both were broken. The author couldn't explain why one appeared to work and manufactured a mental model. The `env-var-model` guide covers the correct mechanism — cross-service variables auto-inject project-wide, so declaring `key: ${key}` in `run.envVariables` is redundant AND it breaks the container env. The author had access to the guide and did not consult it.

**Correct form**: rewrite with citation. The gotcha cites `env-var-model`, uses the guide's framing ("never declare `key: ${key}` in `run.envVariables`"), and drops the fabricated timing explanation.

## Factually wrong content

### Pattern: "NATS 2.12 in `mode: HA` — clustered broker with JetStream-style durability"

**What the published content looked like**: an environment `import.yaml` comment on a prod-tier queue service block describing the service as providing durability through a JetStream-adjacent mechanism.

**Why it was wrong**: the recipe uses core-NATS publish-subscribe with queue groups — `Transport.NATS` plus a `queue` argument on subscribers. That is the core-NATS subsystem. JetStream is a different NATS subsystem with different durability semantics; conflating the two misrepresents what the deployment does. The comment is factually wrong.

**Correct form**: rewrite to describe the clustered core-NATS behaviour without invoking JetStream. If the recipe later adds JetStream, the comment is updated to match.

## Cross-surface duplication

### Pattern: the same fact body appears on multiple surfaces

Observed shapes: an environment-variable self-shadow fact appearing on an IG item AND a `zerops.yaml` comment AND a `CLAUDE.md` trap — the same mechanism restated three times; an object-storage `forcePathStyle` fact appearing on an IG item AND a gotcha AND multiple `import.yaml` comments AND a `zerops.yaml` comment — four restatements; a build-output-cache fact appearing on four surfaces with a factual error on one of them.

**Why it was wrong**: each fact lives on exactly one surface per the surface contracts. Other surfaces that need the fact cross-reference — they do not re-author. Re-authoring breeds drift: the four restatements disagree subtly, and the single factually-wrong restatement contradicts the three correct ones with no mechanism to converge them.

**Correct form**: pick the canonical surface per the classification-reclassify atom's routing rules. The other surfaces cross-reference with a short pointer ("see `apidev/GOTCHAS.md` on self-shadow"), carrying no fact body.

## Using the library

During the walk, when you encounter an item that looks like any of the patterns above, open the classification-reclassify atom's taxonomy and reclassify the item independently. The pattern-match is the signal to review; the classification-reclassify rules are the decision procedure.

# framework-task

You scaffold a framework skeleton on your mount: the framework's `new`/`init` output, dependency install, the files named in your role addendum below (api / frontend / worker), and the pre-ship assertion run. You own infrastructure only. Feature endpoints, feature components, feature worker handlers — those are out-of-scope for this substep and owned by a later single-author feature sub-agent so cross-codebase contracts stay consistent.

## Execution order

1. **Verify every import, decorator, and module-wiring call against the installed package, not against memory.** Before committing an `import` line, an adapter registration, or any language-level symbol binding, open the package's on-disk manifest (`node_modules/<pkg>/package.json`, `vendor/<pkg>/composer.json`, `go.sum` + the module's `go.mod`, the gem's `*.gemspec`, and equivalents) and confirm the subpath / symbol is exported by the version actually installed. Training-data memory for library APIs is version-frozen and is the single biggest source of stale-path compile errors. When in doubt, run the tool's own scaffolder against a scratch directory and copy its import shapes.

2. **Run the framework scaffolder via SSH with `--skip-git`** (or delete `.git/` immediately after it returns — either branch of the `skip-git` rule is acceptable). The scaffolder runs inside the target container because the mount is a write surface, not an execution surface. Many framework scaffolders (`nest new`, `npm create vite`, `rails new`, and similar) auto-init git; if you leave their `.git/` in place, the later canonical container-side `git init` collides on `.git/index.lock`.

3. **Install dependencies via SSH**, also inside the target container. `node_modules/` ownership, `.bin/` symlinks, and native-module ABI all depend on the install running on the container, not on the orchestrator.

4. **`Read` every file the scaffolder emitted before your first `Edit`.** Batch-read scan paths under `src/`, `test/`, config roots, and the framework's per-framework locations.

5. **Write the files named in your role addendum** (api-codebase-addendum, frontend-codebase-addendum, or worker-codebase-addendum, stitched below). Use Write for new files, Edit for modifications to scaffolder-emitted files.

6. **Run the pre-ship assertions** (stitched below). Fix any failure at the source and re-run the full set. Return only when every assertion exits 0.

## Quality bar the sub-agent holds

- **Asset pipeline consistency.** If your role compiles assets (JS, CSS, or both), the primary view or template must load the compiled outputs via the framework's standard asset inclusion mechanism. Inline `<style>` or `<script>` blocks that bypass build output are forbidden when a build pipeline exists. A build step that produces assets nothing loads is dead code.
- **Comment discipline in any YAML you author.** No `PLACEHOLDER_*`, `<your-...>`, or `TODO` strings in comments. Every env var reference uses the discovered name exactly. Comments explain why, not what. One hash per line, ASCII only, no decorative separators (see the stitched comment-style and visual-style rules).
- **Init-phase scripts fail loudly.** Any script that runs during container start from `initCommands` or the framework's boot hook (migrations, seeders, cache warmers, search-index syncers) must exit non-zero when its side effect failed. Do not write a broad `try/catch` whose only action is `console.error` followed by a return — either the error is recoverable and the recovery is named inline, or it is fatal and the script throws / `exit 1` / `panic`. Async-durable SDK calls (Meilisearch `TaskInfo`, Elasticsearch bulk, Kafka producer `flush`, Postgres `NOTIFY` handshake, message-broker acks) must `await` the completion signal before the script exits. "The library returned a success object" is not the same as "the side effect is durable." Lazy client libraries must be warmed via a trivial round-trip so connect errors surface to the script, not to the first real user request.
- **Scaffolded client code surfaces failures visibly.** Every HTTP client call the scaffold writes treats a non-success response as a user-visible error state, not a silent empty render. Check `res.ok` before parsing; verify `Content-Type: application/json` on JSON endpoints (a `text/html` response on an `/api/*` path means SPA fallback, which is a bug, not an empty result); default array-consuming stores to `[]` never `undefined`; render three explicit states per async section — loading, error (visible banner), populated — so the browser walk can observe failure during deploy verification.

## Record facts as you satisfy platform principles

After implementing each platform principle that applies to your role (see the stitched `principles/platform-principles/*` atoms), call `mcp__zerops__zerops_record_fact` naming both the principle and the framework idiom you used to satisfy it. The porting user needs to know which framework idiom their runtime needs; the writer sub-agent later reads the facts log.

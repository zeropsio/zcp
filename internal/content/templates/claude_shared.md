Zerops has its own syntax and conventions. Don't guess — look them up
via `zerops_knowledge`, and inspect live state via `zerops_*` tools.
Runtime app code always runs in Zerops runtime containers.

## Three entry points

1. **Develop** — every task that touches a specific service's code
   (editing, scaffolding, debugging, deploying, planning):

   ```
   zerops_workflow action="start" workflow="develop" \
     intent="<one-line proposal>" scope=["appdev"]
   ```

   `intent` is your one-line proposal — pick a sensible default
   ("Streamlit weather dashboard, python@3.12, public subdomain"),
   start, refine inside the plan-adjust step. Don't ask the user for
   details the workflow itself collects. 1 task = 1 session: a new
   `intent` on an open develop session auto-closes the prior one.

2. **Bootstrap** — when no services exist yet, or you need to add
   infrastructure (new service, mode expansion):

   ```
   zerops_workflow action="start" workflow="bootstrap" intent="<...>"
   ```

   Provisions services. After it closes, continue in develop. If
   infrastructure work comes up mid-develop, start bootstrap — your
   develop work session persists.

3. **Recipe authoring** — only when the user said "create a
   {framework} recipe", "build a recipe", or named a slug like
   `nestjs-showcase`:

   ```
   zerops_recipe action="start" slug="<slug>" outputRoot="<dir>"
   ```

   Self-contained pipeline (research → provision → scaffold → feature
   → finalize). Do NOT start bootstrap or develop during recipe
   authoring — the recipe atoms guide every step.

If state is unclear (after compaction or between tasks):
`zerops_workflow action="status"` (or `zerops_recipe action="status"`
for recipe sessions) returns the current phase and next action.

Direct tools skip the workflow — `zerops_discover`, `zerops_logs`,
`zerops_env`, `zerops_manage`, `zerops_scale`, `zerops_subdomain`,
`zerops_knowledge` auto-apply without a deploy cycle.

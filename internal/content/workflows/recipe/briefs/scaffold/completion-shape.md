# completion-shape

Return a single structured message containing the following fields. The caller reads this payload as proof that your scaffold substep is attestable.

1. **Files written** — a bulleted list. Each item names the path relative to the mount root and the file's byte count. Include every file you created or modified; omit files the framework scaffolder emitted that you did not touch.

2. **Pre-ship aggregate exit code** — the literal exit code of the pre-ship assertion run. Must be 0.

3. **Per-rule pre-attest summary** — one line per `FixRecurrenceRules` entry whose `appliesTo` list matched your role or `any`. Each line reports the rule `id` and `pass` or the specific failure text the rule's `preAttestCmd` emitted before you repaired it. Rules that did not apply to your role are omitted (not listed as `skip`).

4. **Build tail** — the last 40 lines of your framework's build or compile command. Must show a successful build.

5. **Facts recorded** — every `mcp__zerops__zerops_record_fact` call you made, with its title and scope. Include facts you recorded for each platform principle you satisfied and for each pre-ship assertion you repaired.

6. **Env var names consumed** — the env var names your code reads, ordered by managed-service kind. These must match `EnvVarsByKind` byte-for-byte.

Do not claim feature implementation. The feature sub-agent runs later and owns every feature endpoint, component, and worker handler as a single unit.

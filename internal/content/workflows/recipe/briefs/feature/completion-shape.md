# completion-shape

Return a structured message with the following sections. Do not claim implementation of a feature you could not verify; an honest blocker is worth more than a faked green.

## Return payload

1. **Files written per codebase** — bulleted list per mount, with byte counts. One sub-list per mount named in this dispatch.
2. **Per-feature smoke-test verdict** — one line per feature, including the exact `curl` output line (status code + content-type) for the api-surface route, and the `MustObserve` selector count for the ui-surface section.
3. **Per-feature recorded facts** — one line per fact you recorded, with title + scope (content / downstream / both) + `routed_to` surface when you knew it at record time.
4. **Build + dev-server status per codebase** — one line per codebase: build exit code, dev-server running status, healthCheck curl result.
5. **Environment variables newly required** — any env var you referenced that is not already on the container's environment. List with a one-line reason; route this back to the caller for a platform-scope decision.
6. **Blockers** — any feature whose verification you could not complete. Describe the symptom, the probe batches you ran (per the cadence rule), the hypothesis status of each, and why you stopped. Do not attempt a fourth batch; escalating after three is the rule.

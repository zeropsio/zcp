# Finalize — post-generate README review

After `generate-finalize` has regenerated the six env import.yaml files with comments baked in, walk the README deliverables and confirm they describe the recipe that actually shipped.

## What to review

- **Root README** — verify the intro text describes what this recipe actually demonstrates. Feature list, framework name, managed services named, any capability claims — match against `plan.Features` and the target list. If the generate step added or removed a feature and the root README intro hasn't caught up, update it here.
- **Env READMEs** — every env folder ships a `README.md` auto-generated from plan data (env name, tier framing, service list, deploy instructions). Confirm the description matches the final shape of that env's import.yaml: service list aligned, hostnames aligned, deploy button metadata aligned.

## How to review

Read each file on the SSHFS mount. Spot-check the facts against the corresponding env's import.yaml and against `plan.Features`. Any factual drift (a service in the README that isn't in the import.yaml, a feature in the README intro that isn't in `plan.Features`) is the target of a fix here — either correct the README or revisit the plan before attesting.

Comment voice and depth rules apply to README prose the same way they apply to env comment prose (WHY not WHAT, dev-to-dev tone, no decoration). The review is factual accuracy first, voice second.

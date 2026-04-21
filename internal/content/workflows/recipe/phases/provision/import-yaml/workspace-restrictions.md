# Workspace import.yaml — allowed sections

The workspace import declares service shells inside an existing ZCP project. Its allowed top-level shape is a single `services:` list.

- `services:` — the only top-level section. Every runtime dev/stage pair and every managed service appears here.
- Project-level env variables belong on the project, set via `zerops_env project=true action=set` once the needed keys are known. The finalize step writes the six recipe deliverable imports that carry their own `project:` sections with `envVariables` and preprocessor expressions; those are different files for a different use case.
- Service-level `envSecrets` and `dotEnvSecrets` belong in the deliverable imports finalize writes. During iteration set values via `zerops_env set`.
- `zeropsSetup` and `buildFromGit` belong in the deliverable imports. The workspace deploys via `zerops_deploy` with the `setup` parameter.
- Preprocessor expressions like `<@generateRandomString(<32>)>` belong in the deliverable imports at finalize where they run once per recipe consumer.

The ZCP project already exists — the API accepts only the `services:` shape on a workspace import.

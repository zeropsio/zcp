# zerops_workflow's description per runtime (`workflowDescription`, `hqRepositoryActionsRefused`)

- WD-1 — zerops_workflow's description offers git-push-setup and build-integration for CI/CD exactly where the handler accepts them, and where a Mate refuses them it says CI/CD is HQ's and that neither runs there. Breaks: an agent in a Mate tells its person to run two actions that refuse, or an agent outside a Mate hand-writes the CI file build-integration generates.
- WD-8 — The workflow's input and description name the edit's scope and describe how an open change is handed over. Breaks: an agent cannot discover the accepted pool, lease, or description inputs.
- WD-NOSESSION — A Mate's untracked deploy names the stage rule; another container's does not.
- WD-PAIRPUSH — A Mate's refused pair push names the stage rule; a standalone refusal keeps its direct-deploy advice.
- WD-LEASEPUSH — A Mate's refused lease push names the stage rule; a standalone refusal keeps its stage-check advice.

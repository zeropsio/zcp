## Recipe authoring (maintainer mode)

`ZCP_AUTHORING` is on — the recipe-authoring surface (`zerops_recipe`) is available. This is for AUTHORING new recipes for the Zerops corpus, NOT for deploying from an existing recipe (that's bootstrap `route="recipe"` above — available to everyone).

- `zerops_recipe action="start" slug="..." outputRoot="..."` — opens an authoring session and returns the research-phase guidance plus the parent recipe inline. The engine drives the whole port → author → stitch pipeline through its own responses; follow each response's next action. The tool's errors tell you the exact `outputRoot` shape if you get it wrong.
- `zerops_recipe action="status"` — phase recovery after compaction (the authoring analogue of `zerops_workflow action="status"`).
- Publish a finished recipe via the CLI: `zcp sync recipe {create-repo,push-app,publish}` (needs `gh auth` + `.sync.yaml`).

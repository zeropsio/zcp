# canonical-output-paths

Every file the system writes as part of a recipe lives under a single canonical output tree. The tree is computed from the recipe slug: there is exactly one root, exactly one set of per-codebase directories, exactly one environments directory, and one manifest. Paths outside the tree are out of scope.

## The canonical tree

```
<recipe-output-dir>/zerops-recipe-<slug>/
    {hostname}/                              # one directory per runtime codebase
        README.md
        CLAUDE.md
        (codebase source, scaffold artifacts)
        zerops.yaml
        .gitignore
        .env.example                         # if the scaffolder produced one
    environments/
        {env-folder-name}/
            README.md
            import.yaml
    README.md                                # root README for the recipe
    ZCP_CONTENT_MANIFEST.json                # content manifest (classification + routing)
```

Every path in every file operation the system performs resolves inside this tree.

## How the tree is populated

- `<recipe-output-dir>` is the orchestrator-side working directory for the recipe run.
- `<slug>` is the recipe's stable slug (for example `nestjs-showcase`, `bun-hello-world`).
- `{hostname}` is each runtime target's hostname, as declared in the plan (for example `apidev`, `workerdev`, `appdev`).
- `{env-folder-name}` is computed by the platform-side env-folder helper; your job is to read the helper's output exactly as it produced it, not to invent or paraphrase names.

## No parallel trees

There is exactly one output root per recipe. Do not create alternative roots (for example a second directory at the same level with an `-alt`, `-new`, or `-copy` suffix). Do not mirror portions of the tree into other locations. If you find yourself about to write to a path that does not resolve inside `<recipe-output-dir>/zerops-recipe-<slug>/`, stop and re-read this atom.

## The manifest is authoritative

`ZCP_CONTENT_MANIFEST.json` at the root records every classified fact and the surface it was routed to. Downstream review reads the manifest as the authoritative map of what was published where. If a file was written but the manifest does not list its contribution, either the file should not have been written or the manifest is missing an entry — fix one or the other.

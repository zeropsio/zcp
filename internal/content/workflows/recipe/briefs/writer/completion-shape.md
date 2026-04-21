# Completion shape

Your return payload to the step above you. The payload is advisory prose; the load-bearing outputs are the files on disk (the canonical-output-tree atom) plus the manifest (the manifest-contract atom). This atom defines the prose shape.

## Required sections in the return

1. Files written. One line per authored file with its byte count:

   ```
   {{.ProjectRoot}}/README.md                                 <bytes>
   {{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json                 <bytes>
   {{.ProjectRoot}}/{hostname}/README.md                       <bytes>
   {{.ProjectRoot}}/{hostname}/CLAUDE.md                       <bytes>
   {{.ProjectRoot}}/environments/{env-folder}/README.md        <bytes>
   ```

   One row per hostname in `{{.Hostnames}}`; one row per env tier in `{{.EnvFolders}}`. If a file is absent, the row is absent; rows do not carry zero-byte placeholders.

2. Manifest summary. Three totals the step above you parses:

   - Total entry count: `<N>`.
   - Per-classification totals: framework-invariant=`<n1>`, intersection=`<n2>`, framework-quirk=`<n3>`, scaffold-decision=`<n4>`, operational=`<n5>`, self-inflicted=`<n6>`.
   - Per-routed_to totals: content_gotcha=`<n>`, content_intro=`<n>`, content_ig=`<n>`, content_env_comment=`<n>`, claude_md=`<n>`, zerops_yaml_comment=`<n>`, scaffold_preamble=`<n>`, feature_preamble=`<n>`, discarded=`<n>`.

3. `env-comment-set` JSON payload. Per env tier, per service block, the comment text. The step above you applies this at finalize. Shape:

   ```json
   {
     "environments": {
       "{env-folder}": {
         "project": "<project-level comment text>",
         "services": { "<hostname>": "<service comment text>" }
       }
     }
   }
   ```

4. Discarded facts with reasoning. A list of every FactRecord.Title you classified or routed to `discarded` with a one-line reason per entry. The reviewer audits this list against the manifest.

5. Pre-attest aggregate exit code. The exit code of the aggregate shell block in the self-review atom. `0` means all checks passed; non-zero means at least one surface has a remaining item that needs removal or relocation, and the completion return documents which surface and which item.

No other sections. The step above you reads the five sections above; prose outside them is ignored.

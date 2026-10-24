---
id: bootstrap-user-code-box
priority: 2
phases: [bootstrap-active]
routes: [classic, recipe]
steps: [discover]
title: "Code the app's users send runs in a Zerops Box"
---

### Code the app's users send runs in a Zerops Box

When the app runs code it does not write itself — a playground's snippets, a grader's submissions, notebook cells, an agent's tool calls, plugins — plan a **Zerops Box** for that code. It runs in a Box sandbox beside the app, never inside the app's runtime container: namespaces, chroot or a subprocess there still share its kernel, its files and its project network. Each sandbox is its own microVM; it reads none of the project's values unless they are named for it (never a `ZBOX_` one), and the `AIRGAPPED` or `INTERNET` network mode keeps it away from the project's services.

- The Box (`ubuntu/zbox@1`) is infrastructure the app uses, never a runtime target: in a classic plan, a dependency of the runtime with `"resolution": "CREATE"`; on either route, its own `zerops_import` beside the runtime.
- A Box can be created only in a project in region prg1; in any other region the plan and the import are refused.
- What the app calls — the key wired by reference, then create, exec and terminate a sandbox: `zerops_knowledge uri="zerops://themes/services"`, section "Running code the app's users send".

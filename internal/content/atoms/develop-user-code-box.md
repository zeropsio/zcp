---
id: develop-user-code-box
priority: 3
phases: [develop-active]
deployStates: [deployed]
title: "A feature that runs code the app's users send"
---

### A feature that runs code the app's users send

A feature that runs code the app does not write itself — users' snippets or submissions, notebook cells, an agent's tool calls, plugins — runs that code in a Zerops Box sandbox beside the app, never in the app's runtime container, where namespaces or a subprocess still share its kernel, files and project network. A project without a Box gets one through `zerops_import` (`type: ubuntu/zbox@1`; a Box exists only in a project in region prg1), and the app calls its workspace API with the key wired by reference: `zerops_knowledge uri="zerops://themes/services"`, section "Running code the app's users send".

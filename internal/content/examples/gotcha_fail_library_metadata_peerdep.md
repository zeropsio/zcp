---
surface: gotcha
verdict: fail
reason: library-metadata
title: "@sveltejs/vite-plugin-svelte@^5 peer-requires Vite 6 (v28 appdev gotcha #5)"
---

> ### `@sveltejs/vite-plugin-svelte@^5` peer-requires Vite 6, not Vite 5
>
> Upgrading `vite-plugin-svelte` to 5.x produced EPEERINVALID because the
> repo pinned Vite 5. Either downgrade the plugin or upgrade Vite.

**Why this fails the gotcha test.**
The fact is npm registry metadata — a peer-dependency range — with zero
Zerops involvement. It would be equally true on the porter's laptop with no
Zerops account. Classification: library-metadata → DISCARD from recipe
gotcha surface.

**Correct routing**: belongs in `package.json` notes or the relevant
library's CHANGELOG, not on a Zerops-recipe content surface.

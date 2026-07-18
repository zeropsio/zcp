# Codex round: Phase 4 POST-WORK validation (2026-04-27)

Round type: POST-WORK per §10.1 Phase 4 row 2
Reviewer: Codex (post-work fresh agent)

> **Artifact write protocol note (carries over).** Codex sandbox blocks artifact writes; reconstructed verbatim from text response.

## Per-atom diff review

### develop-implicit-webserver
- Dropped: `index.php` / `index.html` examples from the wrong-`documentRoot` bullet, plus the separate composer-app `public/index.php` missing-entrypoint bullet.
- ZCP-specific? No — framework/webserver triage detail; the atom still preserves the ZCP-specific `documentRoot` rule.
- Verdict: **SAFE**

### develop-first-deploy-write-app
- Dropped: explicit negative examples that `run.start` is not `npm install` or `build`, and the requirement that `/status` or `/health` return **200** on success.
- ZCP-specific? Yes — the "return 200" detail encodes what `zerops_verify` needs from the deterministic endpoint.
- Verdict: **NUANCE-LOST**
- Reasoning: `zerops_verify` behavior detail dropped.

### develop-first-deploy-scaffold-yaml
- Dropped: `ContentRootPath = CWD`, the explicit ASP.NET lookup path `/var/www/wwwroot/`, and the `run.start` subpath example `./out/app/App.dll`.
- ZCP-specific? Yes — encodes Zerops working-directory/container path behavior.
- Verdict: **NUANCE-LOST**

### develop-deploy-modes
- Dropped: the explicit preserve-dir start example `./out/app/App.dll`, the ASP.NET `wwwroot/` example, and `ContentRootPath = /var/www/`.
- ZCP-specific? Yes — encodes how Zerops places cross-deploy artifacts.
- Verdict: **NUANCE-LOST**

### develop-platform-rules-local
- Dropped: framework dev-command examples.
- ZCP-specific? No.
- Verdict: **SAFE**

### develop-dynamic-runtime-start-local
- Dropped: framework dev-command examples + HTTP probe interpretation.
- ZCP-specific? No.
- Verdict: **SAFE**

### develop-first-deploy-asset-pipeline-container
- Dropped: helper examples `@vite(...)`, `<%= vite_* %>`, `{% entry_link_tags %}`; PHP-FPM picking up the manifest on the next request; routing prose.
- ZCP-specific? Yes — "PHP-FPM picks it up on the next request" encodes container-runtime behavior after SSH build + SSHFS propagation.
- Verdict: **NUANCE-LOST**

## Verdict

**Phase 4 EXIT clean: NO** (at the time of round)

Blocking findings (all rated Phase 6+ severity, none ship-blockers):

| Atom | Issue | Proposed fix | Severity |
|------|-------|-------------|----------|
| `develop-first-deploy-write-app` | `zerops_verify` HTTP 200 success requirement dropped | Restore "Return 200 on success" | Phase 6+ follow-up |
| `develop-first-deploy-scaffold-yaml` | ASP.NET `/var/www/wwwroot/` content-root failure shape dropped | Reintroduce `wwwroot/` lookup at `/var/www/wwwroot/` | Phase 6+ follow-up |
| `develop-deploy-modes` | Preserve-vs-tilde concrete examples dropped | Restore one compact example per row | Phase 6+ follow-up |
| `develop-first-deploy-asset-pipeline-container` | PHP-FPM next-request behavior dropped | Add "PHP-FPM reads it on next request; no redeploy/restart needed" | Phase 6+ follow-up |

## Post-round resolution (executor edit, commit <pending>)

All 4 NUANCE-LOST findings RESOLVED:

- `develop-first-deploy-write-app`: restored "returning HTTP 200" in observability hook bullet.
- `develop-first-deploy-scaffold-yaml`: restored ASP.NET `wwwroot/` lookup at `/var/www/wwwroot/` + `./out/app/App.dll` example.
- `develop-deploy-modes`: restored ASP.NET `wwwroot/` example in extract-contents row + `./out/app/App.dll` in preserve-dir row.
- `develop-first-deploy-asset-pipeline-container`: added "PHP-FPM reads it on the next request — no restart needed" after the SSHFS propagation note.

Net Phase 4 recovery (post-restoration): 1006 B in-probe aggregate
(was 1627 B pre-restoration; 621 B traded for nuance preservation).
Within Phase 4 target (1-2 KB).

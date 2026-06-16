# In-flight experiments (Karel's personal dev-loop threads)

Moved out of `CLAUDE.local.md` 2026-06-16 — these are personal "až se k tomu
vrátíme" notes, not instructions, so they shouldn't pay always-loaded context
cost. Pick them back up when the trigger fires.

---

## Worktree-per-session aliases (started 2026-05-08)

Karel přidal `zclw` (`claude --worktree`) a `zclwt` (`claude --worktree --tmux`) do `~/.zshrc:31-33` jako reakci na edit-collisions ve 3-5 paralelních Claude sessions. Teď ve fázi testování — flow se ladí v reálném použití.

**Až se k tomu vrátíme, vyhodnotit a navrhnout vylepšení:**
- Ergonomika `zclw <name>` v praxi — funguje pojmenovávání, nebo je friction větší než užitek a chce to jiný default?
- **Merge-back flow** — kandidát na `/merge-worktree` slash command nebo shell funkci s checklistem: commit pendingu → testy → `git -C <main> status` clean check → `git merge --ff-only worktree-<name>` (STOP na divergenci) → `git worktree remove` + `git branch -d`, push **ne**dělat. Recept jsme probrali, ale není zhmotněný.
- Per-worktree gitignored state (`.claude/tasks/`, `.claude/agent-memory/`, `.claude/settings.local.json`) — co reálně bolelo? Kandidát na promotion permissions do user-level `~/.claude/settings.json`.
- Sémantické konflikty mezi paralelními sessions na stejném subsystému (`internal/ops/`, `internal/workflow/`) — vyskytly se? Worktree řeší jen file-level izolaci na disku, ne logickou koordinaci.
- Případně: explicit `--worktree` spuštění v Agent tool (`isolation: "worktree"`) — má smysl ho použít víc agresivně pro některé subagenty?

Plný kontext rozhodnutí (proč `zclw` + `zclwt` a ne přepsání `zcl`, jak `--worktree` funguje, edge cases) → conversation z 2026-05-08, kde se to zaváděl.

## Spec corpus restructure draft (started 2026-05-19)

Na lokální branche `spec-corpus-draft` leží draft přepisu specifikací: `docs/specs/{README,behavior,internals,content-authoring}.md` (4 soubory, ~3500 řádků) místo dnešních `docs/spec-*.md` (11 souborů, ~6900 řádků). Behavior je domain-organized (17 domén v 6 partech) s anchor systémem `G.<domain>.<promise>` + `R.<domain>.<case>` (118 G + 83 R), každý anchor mapován na pinning testy/scénáře/atomy. Internals + content-authoring napsány Codexem.

Branch commit: `5eb5b459 docs/specs: new corpus draft — behavior + internals + content-authoring` (na `spec-corpus-draft`, nepushováno).

**Až se k tomu vrátíme:**
- Karel čte `docs/specs/README.md` → `behavior.md` ToC → spot-check 2-3 domain (zejména git-push-setup jako exemplar v Part 4, status-recovery v Part 0, local-mode v Part 2).
- Resolve open questions z `plans/spec-corpus-restructure-2026-05-19.md §9`: 3-doc shape OK? naming bez `spec-` prefixu OK? content-authoring scope (Aleš vs společný cleanup)? authoring further flows by Claude per session vs Codex review pass?
- Per-domain hygiene review: 17 domain drafts byly autorovány 7 paralelními Explore agenty + Codexem (style consistency cca 80 %, post-hoc sweep mnou). Některé domain bodies mají drobné style drifty — projít se Karlem nebo Codexem.
- Verifikační loop wiring (anchor lint CI) — naplánováno v `plans/spec-corpus-restructure-2026-05-19.md` Phase 6 + 8, neimplementováno.
- Cleanup legacy `docs/spec-*.md` — Phase 7 plánu; až Karel reviewne novou strukturu, smazat staré per migration table.
- Možná rebrand: `behavior.md` → něco user-facing-více? Karel říkal "spec" je ok pokud sémanticky čitelné. Open.
- Vrátit se k `plans/spec-anchor-iteration-decouple-2026-05-19.md` (předchůdce, subsumed) — buď fold relevant parts do behavior.md (U1 invariant) nebo discard po new corpus stabilization.

Plný kontext (proč 3 docs, proč anchor systém, proč Codex pro internals/content-authoring) → conversation z 2026-05-19.

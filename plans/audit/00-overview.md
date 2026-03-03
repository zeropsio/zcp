# MCP Instruování LLM — Audit Overview

Audit provedený 2026-03-02 týmem 4 agentů, syntéza team-leadem.

## Identifikované problémy (samostatné soubory)

| # | Soubor | Problém | Severita |
|---|--------|---------|----------|
| 1 | `01-phantom-parameters.md` | MCP host descriptions obsahují parametry neexistující v kódu | P0 |
| 2 | `02-gate-evidence-circular.md` | Bootstrap gates fabrikují vlastní evidence — cirkulární validace | P1 |
| 3 | `03-recipes-wrong-pattern.md` | Recipes učí single-service pattern, core.md vyžaduje dev/stage | P1 |
| 4 | `04-error-translation-lossy.md` | API error codes se ztrácí v translation řetězci | P1 |
| 5 | `05-hostname-validation.md` | Špatná, nekonzistentní, jen na jednom místě | P1 |
| 6 | `06-token-exposure-deploy.md` | Token plaintext v SSH command, může leaknout do LLM contextu | P0 |
| 7 | `07-instruction-vs-structure.md` | Meta-pattern: pravidla vynucována textem místo kódem | P2 |

## Jak s tím pracovat

Každý soubor je self-contained. Obsahuje:
- Popis problému a proč je fundamentální
- Všechny relevantní soubory a řádky z auditu
- Co nebylo prozkoumáno / kde se podívat dál
- Žádné navádění k řešení — to je na iteraci v izolované instanci

## Co audit NEPOKRYL

- E2E flow z pohledu reálného LLM klienta (Claude, GPT) — jak se skutečně chová při phantom parametrech
- Zerops API response formáty — auditovali jsme jen klientskou stranu, ne co API vrací
- MCP resource protocol (List/Get) — knowledge auditor zmínil ale neprošel do hloubky
- Výkonnostní dopad redundantních ListServices volání — jen identifikováno, neměřeno
- Interakce mezi více MCP klienty sdílejícími stejný state directory

# Knowledge System — Context Flow to LLM

How the ZCP knowledge system assembles and delivers context to the LLM via MCP tools.

## High-Level Architecture

```
┌──────────┐    MCP STDIO     ┌──────────────────────────────────────────┐
│  Claude  │◄────────────────►│  ZCP Binary (cmd/zcp/main.go)           │
│  Code    │   JSON-RPC 2.0   │  └─ server.Server (MCP server)          │
│  (LLM)   │                  │     └─ Instructions: "ZCP manages..."   │
└────┬─────┘                  └──────────────────────────────────────────┘
     │                                         │
     │  System prompt gets:                    │ Registers 15 MCP tools
     │  • Instructions (1 line)                │ at startup via
     │  • Tool schemas (auto)                  │ server.registerTools()
     │                                         │
     │                                         ▼
     │                        ┌────────────────────────────────────────┐
     │                        │  internal/tools/                       │
     │                        │                                        │
     │   3 Knowledge Tools:   │  context.go   → zerops_context         │
     │                        │  knowledge.go → zerops_knowledge       │
     │                        │  workflow.go  → zerops_workflow         │
     │                        └──────┬────────────┬───────────┬────────┘
     │                               │            │           │
     ▼                               ▼            ▼           ▼
```

---

## Tool Call Paths

### PATH 1: zerops_context (orientation)

```
LLM calls: zerops_context {}

┌──────────────┐    ┌──────────────────┐    ┌──────────────────────┐
│ context.go   │───►│ ops.GetContext() │───►│ Static: zeropsContext│
│ (handler)    │    │                  │    │ (hardcoded string)   │
└──────────────┘    │                  │    └──────────────────────┘
                    │  + if client:    │    ┌──────────────────────┐
                    │                  │───►│ StackTypeCache.Get() │
                    │                  │    │ → Zerops API call    │
                    └──────────────────┘    │ → Live service types │
                                           │   (cached w/ TTL)   │
                                           └──────────────────────┘
Returns: platform overview + live versions
```

### PATH 2: zerops_knowledge (3 exclusive modes)

#### Mode A: BM25 Search

```
LLM calls: zerops_knowledge { query: "postgresql connection" }

knowledge.go ──► Store.Search(query, limit)
                     │
                     ▼
             ┌─────────────┐     ┌───────────────────────┐
             │ expandQuery │────►│ Bleve BM25 Index      │
             │ redis→valkey│     │ title:2x kw:1.5x      │
             │ postgres→pg │     │ content:1x             │
             └─────────────┘     └───────────┬───────────┘
                                             │
Returns: JSON [{uri, title, score, snippet}] │
```

#### Mode B: Contextual Briefing (main flow)

```
LLM calls: zerops_knowledge {
  runtime: "nodejs@22",
  services: ["postgresql@16", "valkey@7.2"]
}

knowledge.go ──► Store.GetBriefing(runtime, services, liveTypes)
                     │
                     ▼  LAYERED COMPOSITION
┌────────────────────────────────────────────────────────────┐
│                                                            │
│  L0  grammar.md          ← ALWAYS (universal schema)      │
│  ────────────────────                                      │
│  import.yml schema + zerops.yml schema +                   │
│  12 platform rules (binding, ports, deploy, cache...)      │
│                                                            │
│  L1  runtimes.md § Node.js  ← IF runtime specified        │
│  ──────────────────────────                                │
│  normalizeRuntimeName("nodejs@22") → "Node.js"             │
│  parseH2Sections() → sections["Node.js"]                   │
│  → BIND: 0.0.0.0, DEPLOY: node_modules, etc.              │
│                                                            │
│  L1b Matching Recipes     ← IF runtime has known recipes   │
│  ────────────────────                                      │
│  runtimeRecipeHints["nodejs"] → prefixes                   │
│  → "nestjs", "nextjs", "ghost", "medusa"...               │
│                                                            │
│  L2  services.md § PostgreSQL, § Valkey  ← PER SERVICE    │
│  ────────────────────────────────────────                  │
│  normalizeServiceName("postgresql@16") → "PostgreSQL"      │
│  → ports, env vars, HA config, gotchas                     │
│                                                            │
│  L3  wiring.md § Syntax + § PostgreSQL + § Valkey          │
│  ──────────────────────────────────────────── ← IF services│
│  → ${hostname_var} syntax                                  │
│  → DATABASE_URL, REDIS_URL templates                       │
│                                                            │
│  L4  decisions/ TL;DRs    ← AUTO-SELECTED                 │
│  ────────────────────                                      │
│  postgresql → choose-database.md TLDR                      │
│  valkey → choose-cache.md TLDR                             │
│  (deduplicated per decision doc)                           │
│                                                            │
│  L5  FormatVersionCheck   ← IF liveTypes available         │
│  ──────────────────────                                    │
│  ✓ nodejs@22   ⚠ postgresql@17 → suggest @16              │
│                                                            │
└────────────────────────────────────────────────────────────┘

Returns: concatenated markdown (~800 tokens for 2-service stack)
```

#### Mode C: Recipe Retrieval

```
LLM calls: zerops_knowledge { recipe: "laravel-jetstream" }

knowledge.go ──► Store.GetRecipe("laravel-jetstream")
                     │
                     ▼
             docs["zerops://recipes/laravel-jetstream"]

Returns: full recipe markdown (complete framework setup)
```

### PATH 3: zerops_workflow (step-by-step guides)

```
LLM calls: zerops_workflow { workflow: "deploy" }

workflow.go ──► content.GetWorkflow("deploy")
                    │
                    ▼
            ┌──────────────────────────┐
            │ content/workflows/*.md   │  (embedded at compile time)
            │ 8 workflow guides        │
            └──────────────────────────┘

Returns: step-by-step workflow markdown
```

---

## Embedded Knowledge

All knowledge is compiled into the binary at build time via `//go:embed`:

```
internal/knowledge/
├── foundation/          ← Universal rules (4 files, ~506 lines)
│   ├── grammar.md       Schema-derived import.yml + zerops.yml grammar
│   ├── runtimes.md      Runtime deltas (only what differs from universal)
│   ├── services.md      13 managed service reference cards
│   └── wiring.md        Cross-service wiring templates
├── recipes/             ← Framework-specific guides (39 files)
├── guides/              ← Operational guides (10 files)
└── decisions/           ← Architecture decision docs (5 files)
```

Document loading pipeline:
```
loadFromEmbedded() → parseDocument() per .md file:
  path → URI:     "foundation/grammar.md" → "zerops://foundation/grammar"
  # H1 → Title:   "Zerops Grammar"
  ## Keywords → []string for BM25 index
  ## TL;DR → Description
  Full content → BM25 index + direct access via Store.Get()
```

---

## Typical Session Flow

```
User: "Deploy a Node.js app with PostgreSQL and Valkey"

1. LLM reads system prompt (has Instructions + tool schemas)

2. LLM calls: zerops_workflow { workflow: "bootstrap" }
   ← Gets step-by-step bootstrap guide

3. LLM calls: zerops_knowledge {
     runtime: "nodejs@22",
     services: ["postgresql@16", "valkey@7.2"]
   }
   ← Gets layered briefing:
      L0: grammar (schema rules)
      L1: Node.js delta (0.0.0.0, node_modules)
      L1b: Matching recipes (nestjs, nextjs, ghost...)
      L2: PostgreSQL card + Valkey card
      L3: Wiring syntax + DB/cache connection templates
      L4: "Use PostgreSQL for everything" + "Use Valkey, KeyDB deprecated"
      L5: ✓ nodejs@22, ✓ postgresql@16, ✓ valkey@7.2

4. LLM generates import.yml + zerops.yml from the rules

5. LLM calls: zerops_import { content: "...", dryRun: true }
   ← Validates against real API

6. LLM calls: zerops_import { content: "..." }
   ← Creates infrastructure
```

---

## Key Files

| File | Responsibility |
|------|----------------|
| `internal/server/server.go` | MCP server setup, tool registration |
| `internal/server/instructions.go` | System prompt instructions |
| `internal/tools/knowledge.go` | zerops_knowledge handler, mode routing |
| `internal/tools/context.go` | zerops_context handler |
| `internal/tools/workflow.go` | zerops_workflow handler |
| `internal/knowledge/engine.go` | Store, BM25 Search, GetBriefing, GetRecipe |
| `internal/knowledge/documents.go` | Embed loading, document parsing, URI mapping |
| `internal/knowledge/sections.go` | H2 parsing, normalizers, wiring/decision helpers |
| `internal/knowledge/versions.go` | Version validation and formatting |
| `internal/content/content.go` | Workflow + template embedding |
| `internal/ops/context.go` | Platform context assembly |

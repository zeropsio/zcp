# Env-content phase — single sub-agent for root + per-tier surfaces

After codebase-content completes, one `env-content` sub-agent dispatches
and authors:

- `root/intro` (Surface 1)
- `env/<N>/intro` for N in 0..5 (Surface 2)
- `env/<N>/import-comments/project` and `env/<N>/import-comments/<host>`
  for every service block (Surface 3, ~54 fragments across 6 tiers)

The brief is pointer-based — read spec + parent on demand rather than
receiving them embedded.

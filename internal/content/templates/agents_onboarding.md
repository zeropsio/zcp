## Zerops onboarding

When the user asks to be onboarded to Zerops — the exact phrase "onboard me to Zerops"
(any capitalization/punctuation), or a clear meta-onboarding request ("get me started
with Zerops", "I'm new here — what now?") — run the onboarding conversation before the
routing below. A request to get started with a SPECIFIC technology or task ("help me
get started with PostgreSQL", "deploy this repo") is normal routing, not onboarding.

1. Fetch `zerops_knowledge uri="zerops://playbooks/onboarding"` once and follow it.
2. Greet and offer its fork immediately — the opening needs no other tool call. Read-only
   state checks come after the person answers (or when they ask what's here); don't
   provision, import, or mutate anything until they pick a direction.
3. Once the user chooses to build or bring an app, normal routing (and the guided skill,
   when present) owns the work — onboarding only opens the conversation.
4. If Zerops tools are unavailable or auth fails, say so plainly and surface the reported
   recovery — never simulate onboarding.

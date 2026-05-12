# Golden voice principles (derived from `laravel-jetstream` + `laravel-showcase`)

The two reference recipes are the empirical floor for recipe content
voice. Read these principles before authoring KB / yaml comments / IG
prose; the goldens are not embedded but are the calibration target.

## Operational voice over defensive trap-cataloging

KB teaches what a porter can DO with Zerops, not what mistakes the
author made during scaffold. The reference recipes' KB sections
contain operational hints (`zsc health-check disable` before
maintenance mode; `zsc scale ram +0.5GB 10m` for ad-hoc resource
bumps) — actions the porter takes when they want a Zerops capability.
They do NOT enumerate every scaffold-time stumble.

Failure mode (the journal): KB ships as defensive trap-walking —
every framework quirk, library version pin, or transient bug becomes
a bullet. Reader gets a wall of "watch out for X" instead of "here's
what you can DO with Zerops". The spec calls this out at §"Why this
exists — the content-quality failure mode": the agent writes a
journal, not a reader-facing document.

Operational signals:

- The bullet teaches a `zsc <command>` the porter uses
  (`zsc health-check disable`, `zsc scale ram`, `zsc cron`).
- The bullet teaches a Zerops setting the porter changes
  (`enableSubdomainAccess: false` once a custom domain is set up).
- The bullet teaches a managed-service capability the porter
  leverages (Object Storage path-style endpoints).

Defensive signals (anti-pattern):

- The bullet starts with the symptom of a scaffold-time mistake.
- The fix in the bullet is "do X instead of Y" where the shipped
  yaml ALREADY does X — the porter never sees Y.
- The bullet body narrates the recipe author's debugging path.

## Friendly-authority adaptation pattern

Yaml + tier-import comments speak TO the porter with **declarative
statement + invitation to adapt**. The reference shape:

> *"<statement of fact>. Feel free to <adapt>, after <named porter
> trigger>."*

Concrete shape examples (drawn from the references without quoting
verbatim): a comment about subdomain access notes the porter can
swap to a custom domain after registering it; a comment about a
mailer service notes the porter can switch from the local mock to a
real SMTP provider when going to production.

The triggers are porter-side decisions the porter actually makes
(registering a domain, switching to production SMTP, raising replica
count). The voice is "this is the choice; here is the porter-side
trigger that flips it" — not "you might consider", not "perhaps
you'd want", not unqualified "you can". Hedging is the wrong shape.

## Yaml comments stand alone

Mechanism + reason in one breath. Comments do NOT defer to other
surfaces ("see IG #N for the mechanism", "the pattern is taught in
section X"). If the topic needs more depth than fits in a
self-contained comment, KB carries it with a `zerops_knowledge`
citation; the yaml comment is still self-contained. (See also
`synthesis_workflow.md` §"Yaml comments stand alone" + refinement-1
derived rule F-XSURF-REF.)

## What the goldens look like in shape (high-level)

- Root README: short, factual, tier links, no narrative padding.
- Tier README extract markers: 1-2 sentences naming audience + the
  one defining property of the tier.
- Tier `import.yaml` comments: short per-service blocks explaining
  WHY this scale, mode, or presence at this tier — never "promote to
  tier N when…" sentences (cross-tier shifts surface implicitly
  through the contrast).
- App-repo `zerops.yaml` comments: WHY-this-choice per directive,
  mechanism + reason in one breath, friendly-authority adaptation
  hints where the choice is porter-tunable.
- App-repo IG: porter-transferable concrete steps + one copyable
  artifact (3-5 line code diff or `npm install` / `composer require`
  line).
- App-repo KB: 2-5 bullets of operational + intersection content,
  NOT a wall of defensive trap-cataloging.

## Citation pattern

When a KB topic is covered by a `zerops_knowledge` guide, cite the
guide once per bullet using one of: canonical guide ID in citation
framing, friendly display name as link text, or bare docs URL. Never
restate the guide's mechanism in the bullet body; the citation
transfers the mechanism teaching, the bullet adds the
application-specific corollary.

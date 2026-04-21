# comment-style

YAML comments use ASCII `#`. One hash per line; the hash is followed by one space; every comment line is prose that carries a decision or a platform-behavior note the reader benefits from having beside the config.

## Shape

- Each comment line begins with `#` followed by one space, then prose.
- Section transitions use a single bare `#` as a blank-comment line between one section and the next.
- Comments sit above the key they describe. Inline annotations are reserved for short value-level clarifications beside the value.
- One thought per comment. Comment blocks of 1 to 3 lines describe a logical group; longer blocks break across several 1-to-3-line groups with blank comment lines between.
- Line width stays under 70 characters; existing recipes average around 53.

## Voice

Write like a senior engineer explaining a config choice to a colleague. Three dimensions earn their space:

- **Why this choice**, plus the consequence. "CGO_ENABLED=0 produces a fully static binary — no C libraries linked at runtime" rather than "Set CGO_ENABLED to 0."
- **How the platform behaves here** — contextual behavior that makes the file self-contained, so the reader never has to leave to understand what is happening. "project-level — propagates to all containers automatically", "priority 10 — starts before app containers so migrations do not hit an absent database."
- **Reasoning markers at decision points** — use "because ...", "so that ...", "to avoid ..." where the choice has a non-obvious consequence.

Do not restate the field name or its value. The reader can see `base: php@8.4`; they cannot see that project envVariables propagate to child services.

## Example

```yaml
    # CGO_ENABLED=0 produces a fully static binary — no C compiler
    # or system libraries linked at runtime. lib/pq is pure Go
    # so this is safe and results in a portable artifact.
    envVariables:
      CGO_ENABLED: "0"
    buildCommands:
      # Download all module dependencies, then build both the
      # app server and the database migration binary.
      - go mod download
```

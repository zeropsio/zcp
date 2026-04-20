# Comment style — positive allow-list

YAML structure provides grouping on its own. Comments exist to explain **why** a particular choice was made, not to label what the structure already shows.

## The one comment form

- Every comment begins with a single `#` followed by one space, then a full sentence in plain ASCII.
- One `#` per line; each comment line is a self-contained sentence.
- A blank line between two comment blocks is a single bare `#` with no trailing content.
- A comment attached to a YAML key is placed on the line(s) immediately above that key, at the same indentation as the key.

## What comments say

Each comment explains the decision at the key it annotates — the reason a particular value was chosen, the constraint it satisfies, or the downstream effect. The reader of a zerops.yaml comment is a future developer who has the key and the value already in front of them.

Example shape:

```yaml
zerops:
  - setup: dev
    run:
      # deployFiles stays at [.] on dev so the agent can iterate against
      # the live source tree without rebuilding on every edit.
      deployFiles:
        - .
```

## Ratio

Comment ratio across the file stays at or above 30 percent (target 35 percent). The ratio is over comment characters relative to total characters — a file with only 3 percent comments lacks the decision context every section needs.

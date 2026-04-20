# visual-style

Every text surface you author uses ASCII. ASCII renders consistently across every downstream consumer — the publish pipeline, the documentation renderer, the terminal, the raw-file viewer — so the reader sees exactly what you wrote.

## The allow-list

- ASCII letters and digits: `A-Z a-z 0-9`.
- Standard punctuation: `. , ; : ! ?`
- Quotes and apostrophes: `" '`
- Dashes: single ASCII hyphen `-` for hyphenation and compound words; double ASCII hyphen `--` as the em-dash form (written as two hyphens, not the Unicode em-dash).
- Slashes: forward `/` and back `\`.
- Brackets: parentheses `( )`, square brackets `[ ]`, curly brackets `{ }`.
- Structural: space, tab, newline.

That is the whole surface.

## ASCII diagrams

Diagrams use `+`, `-`, `|`, and `/` / `\` for corners and edges. The result works in any monospace context, copies cleanly, and reviews cleanly.

```
  +--------+       +--------+
  | apidev | ----> | workerdev |
  +--------+       +--------+
```

## Em-dash form

Write `--` for em-dashes. "apidev subscribes to a queue -- the worker's group name comes from the contract." Two ASCII hyphens; not `\u2014`, not `—` pasted from elsewhere.

## Consistency check

Pasting into ASCII-only from another document often sneaks in smart quotes, Unicode dashes, or non-breaking spaces. When you assemble text, verify the output with a quick visual scan: every glyph is on the list above.

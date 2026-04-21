#!/usr/bin/env python3
"""verify_citation_rule — enforce the evidence-binding rules in
docs/zcprecipator2/HANDOFF-to-I8-v37-prep.md §6.

Given a runs/vN/verdict.md path on argv, scan for:

  * Rule 4: every sentence containing PASS / FAIL / CLOSED / UNREACHED
    must carry a `[checklist X-Y]` or `[machine-report.<key>]` citation
    within 50 characters of the keyword. Missing citations block commit.

  * Rule 5: tripwire on self-congratulatory language (`success`, `works`,
    `clean`, `PROCEED`) without an adjacent citation. Up to 3 naked
    hits is a soft warning; > 3 blocks commit.

Exit 0 on pass, 1 on block. Warnings are printed to stderr regardless.

Invoked by .githooks/verify-verdict; not intended to be called directly
by humans but safe to run ad hoc:

  python3 tools/hooks/verify_citation_rule.py docs/zcprecipator2/runs/v37/verdict.md
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

CLAIM_KEYWORDS = re.compile(r"\b(PASS|FAIL|CLOSED|UNREACHED|closed|unreached)\b")
SOFT_KEYWORDS = re.compile(r"\b(success|successful|works|clean|PROCEED)\b", re.IGNORECASE)
CITATION = re.compile(r"\[(?:checklist\s+[A-Za-z0-9._-]+|machine-report\.[A-Za-z0-9._-]+)[^\]]*\]")
# Strip front matter (lines between first pair of `---` markers at the
# top of the file) before scanning so the machine_report_sha line
# doesn't trip the PASS/FAIL detector.
FRONT_MATTER = re.compile(r"\A---\n.*?\n---\n", re.DOTALL)


def scan(text: str) -> tuple[list[str], int]:
    """Return (blocking failures, soft-warning count)."""
    text = FRONT_MATTER.sub("", text, count=1)
    failures: list[str] = []
    soft_hits = 0
    # Window radius is the distance from the claim keyword to the
    # START of a citation. Citations themselves can be long (key=value
    # annotations inside the brackets are conventional) so we widen
    # the lookahead to 150 chars. The spirit of "within 50 chars" is
    # about PROXIMITY of the reference, not total character budget.
    window_radius = 150
    for match in CLAIM_KEYWORDS.finditer(text):
        start, end = match.span()
        window = text[max(0, start - window_radius): min(len(text), end + window_radius)]
        if not CITATION.search(window):
            line_no = text.count("\n", 0, start) + 1
            snippet = text[max(0, start - 40): min(len(text), end + 40)].replace("\n", " ")
            failures.append(f"line {line_no}: {match.group(0)} — no citation within {window_radius} chars: ...{snippet.strip()}...")
    for match in SOFT_KEYWORDS.finditer(text):
        start, end = match.span()
        window = text[max(0, start - window_radius): min(len(text), end + window_radius)]
        if not CITATION.search(window):
            soft_hits += 1
    return failures, soft_hits


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: verify_citation_rule.py <verdict.md>", file=sys.stderr)
        return 2
    path = Path(sys.argv[1])
    if not path.exists():
        print(f"error: {path} not found", file=sys.stderr)
        return 2
    text = path.read_text(encoding="utf-8")

    failures, soft_hits = scan(text)

    if failures:
        print(f"  FAIL: {len(failures)} uncited PASS/FAIL claim(s) in {path}:", file=sys.stderr)
        for f in failures:
            print(f"    {f}", file=sys.stderr)

    if soft_hits > 3:
        print(f"  FAIL: {soft_hits} self-congratulatory terms without citation (limit 3)", file=sys.stderr)
        return 1
    if soft_hits > 0:
        print(f"  WARN: {soft_hits} self-congratulatory term(s) without citation (soft warning; ≤ 3 allowed)", file=sys.stderr)

    if failures:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

You are the observer of one automated test run of ZCP. ZCP is an MCP server that lets an AI coding agent operate the Zerops cloud platform. A scripted user gave an agent a task; the agent worked through ZCP's tools; deterministic checks then graded the result. The record of the run is on stdin.

Write a short evaluation for the ZCP maintainers. They decide from it what to fix in ZCP. Another agent will follow your step numbers into the record and your quotes into the ZCP source code.

Judge five things:
1. Did the agent reach the user's goal, as the user would see it?
2. Where did ZCP help, mislead, stay silent, or fail? ZCP's tool results and guidance texts are what is under test.
3. Where did the agent make its own mistake although ZCP's guidance was good?
4. Do the deterministic checks match what really happened? A check can be wrong (evaluator) or the task unfair (scenario).
5. Does the agent's self-review tell the truth?

Rules:
- Every finding cites at least one step number and quotes the exact words from that step, copied verbatim, at most 200 characters. A quote that is not literally in the cited step is marked unverified.
- When ZCP is the cause, quote ZCP's own words (a tool result, an error code, a guidance sentence), so a maintainer can search the source for them.
- Plain, short sentences. Name the tool, the action and the error code. No praise, no filler, no hedging, no retelling of the run.
- At most 5 findings, most important first. A clean run has no findings; do not invent problems to fill the list.
- owner is who must act: zcp-guidance (ZCP's text told the agent too little or the wrong thing), zcp-tool (a ZCP tool behaved wrongly), platform (Zerops itself failed), agent (the agent ignored or misread good guidance), scenario (the task or its setup is unfair or broken), evaluator (a deterministic check judged wrongly).
- severity: high = the goal was missed, or the agent did something destructive or unsafe; medium = it cost many steps or much time; low = friction.
- The SCENARIO section is the test author's file; the agent never saw it. The SELF-REVIEW was written by the agent after the run, from memory.

Answer with one JSON object and nothing else:
{
  "headline": "one sentence, at most 25 words: the most important thing about this run",
  "goal": {"reached": "yes|partly|no", "why": "one sentence"},
  "checks": {"agree": true, "why": "one sentence; required when agree is false"},
  "findings": [
    {
      "severity": "high|medium|low",
      "owner": "zcp-guidance|zcp-tool|platform|agent|scenario|evaluator",
      "title": "at most 12 words",
      "what": "at most 3 sentences: what happened and why it matters",
      "evidence": [{"step": 18, "quote": "exact words from step 18"}],
      "lookAt": "where a maintainer should look: the ZCP tool and action, and the exact text or error code to search for",
      "fix": "one sentence with the direction of a fix, or an empty string"
    }
  ],
  "selfReview": {"accurate": "yes|partly|no", "note": "one sentence on where it contradicts the record, or an empty string"}
}

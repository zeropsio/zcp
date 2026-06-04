export const meta = {
  name: 'wave-analysis',
  description: 'Analyze a wave of flow-eval transcripts for root causes (tool-response quality + agent process)',
  phases: [
    { title: 'Analyze', detail: 'one analyst per scenario transcript → structured findings' },
    { title: 'Synthesize', detail: 'dedup + rank findings across the wave, flag regressions' },
  ],
}

// args = {
//   wave: <int>,
//   changedThisSession: <string>,   // the "what was just changed" context for regression detection
//   scenarios: [{ id, distilled, raw, def, meta }]   // absolute paths
// }
const A = (typeof args === 'string') ? JSON.parse(args) : (args && typeof args === 'object' ? args : {})
const SCENARIOS = A.scenarios || []
const CHANGED = A.changedThisSession || '(none provided)'
log(`args type=${typeof args}; scenarios=${SCENARIOS.length}; changed.len=${CHANGED.length}`)

const FINDINGS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['scenario', 'outcome_summary', 'task_completed', 'findings', 'positive_signals'],
  properties: {
    scenario: { type: 'string' },
    outcome_summary: { type: 'string', description: 'Neutral factual account of what actually happened in the run — what the agent did, where it ended. NOT a grade.' },
    task_completed: { type: 'string', enum: ['yes', 'partial', 'no', 'unclear'], description: 'Did the agent achieve the USER goal stated in the scenario prompt? Factual, from the transcript — not the agent self-assessment.' },
    findings: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['surface', 'symptom', 'evidence', 'suspected_root_cause', 'layer', 'fix_candidate', 'confidence', 'regression_risk'],
        properties: {
          surface: { type: 'string', enum: ['tool-response', 'agent-process', 'both'], description: 'tool-response = a ZCP tool returned wrong/misleading/missing/contradictory data. agent-process = wasted turns, confusion, wrong guess, retry, ignored guidance, dead-end.' },
          symptom: { type: 'string', description: 'The observable problem in one sentence.' },
          evidence: { type: 'string', description: 'VERBATIM quote(s) from the transcript (tool input/output or thinking text) that prove the symptom. Include enough to locate it.' },
          suspected_root_cause: { type: 'string', description: 'Look ONE LAYER UP: is the defect at the layer where it surfaced, or at a layer above that should have prevented the state? Name the most-likely owner.' },
          layer: { type: 'string', enum: ['handler', 'ops', 'atom', 'theme', 'guide', 'schema', 'recipe', 'boot-shim', 'workflow-engine', 'unknown'] },
          fix_candidate: { type: 'string', description: 'Concrete fix proposal at the root-cause layer.' },
          confidence: { type: 'string', enum: ['high', 'medium', 'low'] },
          regression_risk: { type: 'string', enum: ['likely', 'possible', 'no', 'unknown'], description: 'Does this look like a regression from THIS SESSION\'s changes (P4 guide cuts / P5 atom trims / P6 schema lint / P7 classifier / Friction-1 discover-adoption / Friction-2 verify-lifecycle)?' },
        },
      },
    },
    positive_signals: { type: 'array', items: { type: 'string' }, description: 'Things that worked well, ESPECIALLY around the changed surfaces (evidence the change helped, not hurt).' },
  },
}

const SYNTHESIS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['wave_summary', 'ranked_findings', 'cross_scenario_patterns', 'regression_flags', 'recommended_fixes_this_iteration'],
  properties: {
    wave_summary: { type: 'string', description: '2-4 sentence executive view: how the wave went, dominant friction themes.' },
    ranked_findings: {
      type: 'array',
      description: 'Deduped findings across all scenarios, highest-impact first.',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['title', 'scenarios', 'surface', 'layer', 'root_cause', 'fix_candidate', 'impact', 'confidence', 'regression_risk'],
        properties: {
          title: { type: 'string' },
          scenarios: { type: 'array', items: { type: 'string' }, description: 'Which scenarios exhibited this.' },
          surface: { type: 'string', enum: ['tool-response', 'agent-process', 'both'] },
          layer: { type: 'string' },
          root_cause: { type: 'string' },
          fix_candidate: { type: 'string' },
          impact: { type: 'string', enum: ['high', 'medium', 'low'], description: 'How much it hurt the agent / how often it recurs.' },
          confidence: { type: 'string', enum: ['high', 'medium', 'low'] },
          regression_risk: { type: 'string', enum: ['likely', 'possible', 'no', 'unknown'] },
        },
      },
    },
    cross_scenario_patterns: { type: 'array', items: { type: 'string' }, description: 'Friction that appears across multiple scenarios — usually a deeper root cause.' },
    regression_flags: { type: 'array', items: { type: 'string' }, description: 'Anything that looks like a regression from this session\'s changes — flagged loud.' },
    recommended_fixes_this_iteration: {
      type: 'array',
      description: 'The 1-3 root-cause fixes worth doing THIS iteration (per CLAUDE.local.md: max 3 actionable; rest to appendix).',
      items: { type: 'string' },
    },
  },
}

phase('Analyze')

const analystResults = await parallel(
  SCENARIOS.map((sc) => () =>
    agent(
      `You are analyzing ONE flow-eval run of the ZCP (Zerops Control Plane) MCP server, to find ROOT CAUSES of problems that ZCP should fix. ZCP exists to HELP an LLM coding agent provision/operate apps on the Zerops PaaS — its job is to give the agent correct, curated, current information and ease its work.

SCENARIO: ${sc.id}

CRITICAL METHOD — read carefully:
- You are analyzing the RAW run like you'd review a colleague's working session. Do NOT read or trust any self-review / retrospective file — those are the agent's self-grade and are explicitly OFF-LIMITS as a verdict. Form your OWN judgment from the transcript.
- Analyze TWO surfaces, both are findings:
  (a) TOOL-RESPONSE QUALITY: for each ZCP tool call (mcp__zerops__*), was the response correct, helpful, complete, non-contradictory? Did it return WRONG or MISLEADING data? Did it OMIT info the agent needed? Did it DUMP info that misled the agent into a trap? Did two responses contradict each other?
  (b) AGENT PROCESS: wasted turns, visible confusion (read the 🧠 THINKING blocks — that's the agent's reasoning), wrong guesses, repeated retries, guidance that was present but IGNORED (and why — was it buried/unclear?), dead-ends.
- ROOT CAUSE, not trigger: for every finding, look ONE LAYER UP. Is the wrong behavior at the layer where it surfaced, or at a layer ABOVE that should have prevented the state? (e.g. an atom emits a bad command = atom layer; but a handler that ACCEPTED bad meta state and let the atom fire = handler layer — the deeper root.) Name the most likely OWNER layer.
- Evidence is mandatory: quote VERBATIM from the transcript (tool input/output or thinking) for every finding.
- Be skeptical and specific. "The agent seemed confused" is not a finding; "the agent called zerops_deploy then in THINKING said 'wait, that returned X which contradicts the discover result Y' and retried" IS.

CONTEXT — what changed in ZCP THIS session (so you can flag REGRESSIONS — set regression_risk accordingly):
${CHANGED}

INPUTS:
- Scenario definition (the EXPECTED behavior + notableFriction the scenario probes): ${sc.def}
- Distilled turn-by-turn transcript (READ THIS FIRST, it's the readable view): ${sc.distilled}
- Raw transcript JSONL (use jq/Read to inspect any specific tool response in full when the distilled view truncated something you need): ${sc.raw}
- Run meta (wall time, compaction flag): ${sc.meta}

STEPS:
1. Read the scenario definition — note the user goal + the notableFriction items it probes (those are hypotheses to confirm/deny, not the only findings).
2. Read the distilled transcript fully, turn by turn.
3. For any tool response where quality is suspect, Read/jq the RAW transcript to see the full untrimmed response.
4. Produce findings (tool-response + agent-process), each root-caused with verbatim evidence, plus positive_signals (especially evidence that this session's changes HELPED).

Return the structured findings object. Do not summarize for a human — return data.`,
      { label: `analyze:${sc.id}`, phase: 'Analyze', schema: FINDINGS_SCHEMA }
    ).then((r) => ({ ...r, scenario: r.scenario || sc.id }))
  )
)

const valid = analystResults.filter(Boolean)
log(`Analyzed ${valid.length}/${SCENARIOS.length} scenarios; ${valid.reduce((n, r) => n + (r.findings ? r.findings.length : 0), 0)} raw findings`)

phase('Synthesize')

const synthesis = await agent(
  `You are the synthesis pass for a wave of ZCP flow-eval analyses. Below are the per-scenario findings (JSON). Dedup across scenarios, rank by impact, surface cross-scenario patterns (those usually point at a DEEPER shared root cause), and flag anything that looks like a regression from this session's changes.

This session's changes (for regression context):
${CHANGED}

Per-scenario findings:
${JSON.stringify(valid, null, 2)}

Rules:
- Merge findings that are the same root cause across scenarios into ONE ranked finding (list all scenarios).
- Rank by IMPACT (how much it hurt the agent + how often it recurs), highest first.
- recommended_fixes_this_iteration: pick the 1-3 root-cause fixes most worth doing THIS iteration (max 3 actionable; deeper/riskier ones note as patterns). Prefer a root-cause fix that dissolves multiple symptoms over many symptom patches.
- Be honest about confidence. A low-confidence finding that needs live/Codex verification is fine — say so.

Return the structured synthesis object.`,
  { label: 'synthesize', phase: 'Synthesize', schema: SYNTHESIS_SCHEMA }
)

return { wave: A.wave, perScenario: valid, synthesis }

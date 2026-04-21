package workflow

import (
	"fmt"
	"strings"
)

// Cx-BRIEF-OVERFLOW (v35 F-1 close) — composition-pure envelope pattern
// for dispatch briefs whose inlined form would exceed the MCP tool-
// response token cap. See defect-class-registry §16.1 and DISPATCH.md
// for the composition contract the envelope preserves: atoms transit
// verbatim; the envelope is a delivery-layer shim that names which atoms
// the main agent must stitch, not a substitute for the atom content.
//
// v35 smoking gun: the composed writer-substep brief was 71,720 chars at
// the `complete substep=feature-sweep-stage` response, exceeding the
// runtime's ~32KB cap. The harness spilled the payload to a scratch file
// whose path the main agent tried to excavate — and it only read the
// first ~3KB, losing the wire contract. The envelope splits the
// delivery across multiple tool round-trips (one `dispatch-brief-atom`
// call per atom) at the cost of a handful of extra round-trips; each
// atom fits comfortably under the cap and the main agent assembles the
// full brief locally before dispatching.

// dispatchBriefInlineThresholdBytes is the soft size cap above which an
// inlined dispatch brief is replaced with an envelope pointing at
// individually-retrievable atoms. Chosen conservatively below the
// observed ~32KB MCP tool-response cap to leave room for the surrounding
// substep-guide body + prior-discoveries block.
const dispatchBriefInlineThresholdBytes = 28_000

// formatDispatchBriefAttachment returns the markdown block appended to
// the substep guide carrying the sub-agent dispatch brief. When the
// composed brief fits under the inline-threshold, the block embeds the
// brief verbatim (historical behavior). When it would exceed the
// threshold, the block emits a stitch-instruction envelope that names
// the atoms the main agent must retrieve via
// `zerops_workflow action=dispatch-brief-atom atomID=<id>`.
//
// Only dispatch-owning substeps currently known to produce large briefs
// get the envelope treatment. Other dispatch substeps (scaffold,
// code-review) keep the inline path because their composed briefs
// comfortably fit the cap; the envelope is a targeted counter to the
// specific v35 overflow class, not a blanket restructure.
func formatDispatchBriefAttachment(step, subStep string, plan *RecipePlan, sessionID, brief string) string {
	if envelope := envelopeForLargeBrief(step, subStep, plan, sessionID, brief); envelope != "" {
		return envelope
	}
	return "## Dispatch brief (transmit verbatim)\n\n" + brief
}

// envelopeForLargeBrief returns a non-empty envelope string when the
// (step, substep) pair produces a brief whose inlined size exceeds the
// threshold AND the substep has an envelope shape defined. Returns ""
// when the brief fits inline OR no envelope shape is declared for the
// substep (in which case the caller falls back to inline embedding).
func envelopeForLargeBrief(step, subStep string, plan *RecipePlan, sessionID, brief string) string {
	if len(brief) <= dispatchBriefInlineThresholdBytes {
		return ""
	}
	// v35's overflow class is specifically the writer substep (readmes)
	// in showcase. Other large-brief substeps are handled as they
	// surface; scoping the envelope narrowly avoids disturbing the
	// scaffold + code-review paths that already fit the cap.
	if step == RecipeStepDeploy && subStep == SubStepReadmes && isShowcase(plan) {
		return buildWriterDispatchBriefEnvelope(plan, sessionID, len(brief))
	}
	return ""
}

// buildWriterDispatchBriefEnvelope emits the writer substep's stitch-
// instruction envelope. The main agent retrieves each listed atom via
// `zerops_workflow action=dispatch-brief-atom atomID=<id>`, concatenates
// body atoms then principles atoms with `\n\n---\n\n` separators, and
// appends the input-files interpolation block before dispatching to the
// writer sub-agent.
func buildWriterDispatchBriefEnvelope(_ *RecipePlan, sessionID string, briefSize int) string {
	bodyIDs := writerBriefBodyAtomIDs()
	principleIDs := writerPrinciples()

	factsPath := ""
	if sessionID != "" {
		factsPath = factLogPathLocal(sessionID)
	}

	var b strings.Builder
	fmt.Fprintf(&b,
		"## Dispatch brief (retrieve + stitch before transmitting)\n\n"+
			"The composed brief for the `writer` sub-agent is %d bytes — larger than the "+
			"MCP tool-response token cap (~32 KB). Inlining it here would cause a spillover "+
			"that v35 showed the main agent cannot reliably excavate (see "+
			"`docs/zcprecipator2/runs/v35/analysis.md` §F-1). Retrieve each atom individually, "+
			"stitch locally, then dispatch.\n\n"+
			"### Stitch procedure\n\n"+
			"1. For each atom ID in **Body atoms** (in order), call "+
			"`zerops_workflow action=dispatch-brief-atom atomID=<id>` and keep the returned "+
			"`body` string.\n"+
			"2. Concatenate the body-atom strings with `\\n\\n---\\n\\n` as separator — produces the `<body>` section.\n"+
			"3. Repeat for **Principles atoms** to produce a `<principles>` section.\n"+
			"4. Assemble the final brief as: `<body> + \"\\n\\n---\\n\\n\" + <principles> + \"\\n\\n---\\n\\n## Input files\\n\\n- Facts log: `<factsPath>`\\n\"`.\n"+
			"5. Transmit the assembled brief to the writer sub-agent verbatim "+
			"(per `docs/zcprecipator2/DISPATCH.md` §2, atoms transit verbatim — "+
			"do not paraphrase or trim).\n\n",
		briefSize,
	)

	b.WriteString("### Body atoms (in order)\n\n")
	for _, id := range bodyIDs {
		fmt.Fprintf(&b, "- `%s`\n", id)
	}

	b.WriteString("\n### Principles atoms (in order)\n\n")
	for _, id := range principleIDs {
		fmt.Fprintf(&b, "- `%s`\n", id)
	}

	b.WriteString("\n### Interpolation inputs\n\n")
	if factsPath != "" {
		fmt.Fprintf(&b, "- Facts log path: `%s`\n", factsPath)
	} else {
		b.WriteString("- Facts log path: (no active session; skip the Input files tail)\n")
	}

	return b.String()
}

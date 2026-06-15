package recipe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Run-46 Item 1 — surface-manifest-as-JSON-file + walked-ledger receipt.
//
// Refinement-2 sub-agent walked partial state across run-41 → run-45 in
// five distinct ways. Run-45 forensic: the brief rendered absolute paths
// to codebase READMEs and the sub-agent STILL emitted 0 findings on S4/
// S5/S7 across all 3 codebases. Path-correction was monkey-patch. The
// structural fix removes the filesystem walk from the sub-agent's
// responsibility — engine enumerates every in-scope item across every
// surface across every codebase as one comparable corpus and writes it
// to `<outputRoot>/.refinement-2-manifest.json`. The brief instructs the
// sub-agent to Read the manifest first; the close-gate verifies a
// `walked` ledger covers every manifest entry.
//
// S6 (CLAUDE.md) is intentionally OUT of the manifest per run-46 design
// (CLAUDE.md is repo-local, not published; refinement effort wasted).
// audit_checklist.md is updated alongside.

// Refinement2ManifestEntry is one item the sub-agent must evaluate. The
// shape is intentionally flat — Body is the text the sub-agent reads;
// IDKey is the stable identifier the walked ledger names; the optional
// fields carry per-surface metadata the audit's single-question test
// references.
type Refinement2ManifestEntry struct {
	IDKey string `json:"idKey"`
	// Body is the prose the sub-agent applies the single-question test
	// against. For S3 it's the comment block; for S4 it's the IG slot's
	// body; for S5 it's the KB bullet body; for S7 it's the whole yaml
	// fragment.
	Body string `json:"body"`
	// Slot — S4 only. The IG slot number (`2` / `3` / …). Engine-emitted
	// IG slot 1 is intentionally NOT in the manifest — audit_checklist.md
	// routes IG #1 issues to the underlying yaml fragment (S7).
	Slot string `json:"slot,omitempty"`
	// Stem — S5 only. The KB bullet's bold-symptom stem (the text inside
	// `**...**` at bullet open).
	Stem string `json:"stem,omitempty"`
	// Service — S3 only. The hostname the comment block is bound to
	// (`project` for the tier-level block).
	Service string `json:"service,omitempty"`

	// TopicCandidates — S3/S4/S5 only. The citation-map topic ids the
	// body's keywords match (best-effort via DetectBulletTopicAll). Empty
	// when no topic family matches. Populated by the engine so the
	// refinement-2 audit can route findings against the right citation
	// guide without a re-detect per dispatch. Run-46 Item 1 G2-followup.
	TopicCandidates []string `json:"topicCandidates,omitempty"`
	// PorterTunableDirectives — S3 and S7 only. The list of porter-tunable
	// field names found in the adjacent yaml fragment (S7 = whole yaml;
	// S3 = comment block's owning service yaml). Run-46 Item 1
	// G2-followup. Field-name allowlist matches
	// EnumeratePorterTunableDirectives.
	PorterTunableDirectives []string `json:"porterTunableDirectives,omitempty"`
	// PerFieldComments — S7 only. Per porter-tunable field, the
	// contiguous comment block immediately preceding the field
	// declaration. Run-46 Item 1 G2-followup. The audit reads per-field
	// rationale to detect bare-mechanism comments vs friendly-authority
	// adapt-paths.
	PerFieldComments []PerFieldComment `json:"perFieldComments,omitempty"`
}

// Refinement2Manifest is the structured corpus the refinement-2 audit
// walks. One JSON file per dispatch; structure mirrors the four in-scope
// surfaces (S3 tier yaml comments, S4 codebase IG, S5 codebase KB, S7
// codebase zerops.yaml).
type Refinement2Manifest struct {
	// TierYAMLComments keyed by tier index ("0".."5"). One entry per
	// comment block (project + per-service).
	TierYAMLComments map[string][]Refinement2ManifestEntry `json:"tierYamlComments,omitempty"`
	// CodebaseIG keyed by codebase short-form host. One entry per
	// agent-authored IG slot.
	CodebaseIG map[string][]Refinement2ManifestEntry `json:"codebaseIg,omitempty"`
	// CodebaseKB keyed by codebase short-form host. One entry per KB
	// bullet (parsed by bold-symptom opener).
	CodebaseKB map[string][]Refinement2ManifestEntry `json:"codebaseKb,omitempty"`
	// CodebaseZeropsYAML keyed by codebase short-form host. One entry
	// per codebase (the whole yaml fragment is one item).
	CodebaseZeropsYAML map[string][]Refinement2ManifestEntry `json:"codebaseZeropsYaml,omitempty"`
}

// AllKeys returns every IDKey in stable sort order. The walked ledger
// MUST cover this set for the close-gate to accept the refinement-2
// audit as complete.
func (m *Refinement2Manifest) AllKeys() []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0)
	for _, group := range []map[string][]Refinement2ManifestEntry{
		m.TierYAMLComments, m.CodebaseIG, m.CodebaseKB, m.CodebaseZeropsYAML,
	} {
		for _, entries := range group {
			for _, e := range entries {
				keys = append(keys, e.IDKey)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// AllKeysSet returns AllKeys() materialized as a set so callers can
// do O(1) membership tests. Used by enrichFindingsAction to validate
// the walked-ledger receipt against the manifest grammar BEFORE
// mutating sess.Refinement2Ledger — refusal must not corrupt prior
// ledger state. Run-47 Item A.
func (m *Refinement2Manifest) AllKeysSet() map[string]bool {
	keys := m.AllKeys()
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// Refinement2Ledger is the receipt the sub-agent emits. `Walked` lists
// the IDKey of every manifest item the sub-agent applied the single-
// question test against — zero-finding allowed only when the item is in
// `Walked` (proves the sub-agent looked). The close-gate distinguishes
// "didn't dispatch" from "walked-but-found-nothing".
type Refinement2Ledger struct {
	Walked []string `json:"walked"`
	// CrossSurfaceUniquenessScanned + Duplicates are Item 6's fields.
	// Defined here so Item 1's manifest infrastructure can host them
	// without an Item 6 reshape; Item 6's close-gate read is the future
	// PR consumer.
	CrossSurfaceUniquenessScanned int      `json:"crossSurfaceUniquenessScanned,omitempty"`
	Duplicates                    []string `json:"duplicates,omitempty"`
}

// BatchedLedger is the in-flight accumulator for Run-47 Item H's typed
// multi-batch enrich-findings API. Walked entries from each batch are
// appended (NOT latest-wins — codex requirement) until every batch in
// {1..TotalBatches} has arrived; the engine then promotes the
// aggregated state to sess.Refinement2Ledger and clears
// sess.BatchedRefinement2Ledger. Lives on the session, so a session
// snapshot/restore survives compaction.
//
// CrossSurfaceUniquenessScanned + Duplicates carry the cross-surface
// counter the sub-agent typically reports on a single batch (the final
// emission); on promotion these land on the Refinement2Ledger so Item
// C's close-gate consistency check operates on the aggregated walked
// count.
type BatchedLedger struct {
	Walked                        []string     `json:"walked"`
	ReceivedBatches               map[int]bool `json:"receivedBatches"`
	TotalBatches                  int          `json:"totalBatches"`
	CrossSurfaceUniquenessScanned int          `json:"crossSurfaceUniquenessScanned,omitempty"`
	Duplicates                    []string     `json:"duplicates,omitempty"`
}

// MissingBatchIDs returns the {1..TotalBatches} ids that have NOT yet
// been marked received. Returned sorted ascending. Empty result means
// the batched set is complete and ready for promotion.
func (b *BatchedLedger) MissingBatchIDs() []int {
	if b == nil || b.TotalBatches <= 0 {
		return nil
	}
	var missing []int
	for i := 1; i <= b.TotalBatches; i++ {
		if !b.ReceivedBatches[i] {
			missing = append(missing, i)
		}
	}
	return missing
}

// missingHead returns the first n elements of missing — bounded so the
// close-gate error message stays under a sane envelope size when the
// sub-agent walked nothing.
func missingHead(missing []string, n int) []string {
	if len(missing) <= n {
		return missing
	}
	return missing[:n]
}

// Missing returns the IDKey set the ledger does NOT cover. Empty result
// means full coverage — close-gate passes.
func (l *Refinement2Ledger) Missing(m *Refinement2Manifest) []string {
	if l == nil || m == nil {
		return m.AllKeys()
	}
	walked := make(map[string]bool, len(l.Walked))
	for _, k := range l.Walked {
		walked[k] = true
	}
	var missing []string
	for _, k := range m.AllKeys() {
		if !walked[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

// BuildRefinement2Manifest walks the plan's fragments + env-comments and
// returns the manifest. Engine-side; no sub-agent input required.
func BuildRefinement2Manifest(plan *Plan) (*Refinement2Manifest, error) {
	if plan == nil {
		return nil, fmt.Errorf("BuildRefinement2Manifest: nil plan")
	}
	m := &Refinement2Manifest{
		TierYAMLComments:   map[string][]Refinement2ManifestEntry{},
		CodebaseIG:         map[string][]Refinement2ManifestEntry{},
		CodebaseKB:         map[string][]Refinement2ManifestEntry{},
		CodebaseZeropsYAML: map[string][]Refinement2ManifestEntry{},
	}

	// S3 — tier yaml comments. EnvComments keyed by tier index string.
	// Sort keys so the manifest enumerates deterministically.
	tierIdxs := make([]string, 0, len(plan.EnvComments))
	for k := range plan.EnvComments {
		tierIdxs = append(tierIdxs, k)
	}
	sort.Strings(tierIdxs)
	for _, tierIdx := range tierIdxs {
		ec := plan.EnvComments[tierIdx]
		if strings.TrimSpace(ec.Project) != "" {
			m.TierYAMLComments[tierIdx] = append(m.TierYAMLComments[tierIdx], Refinement2ManifestEntry{
				IDKey:           "tier_yaml_comments:" + tierIdx + ":project",
				Body:            ec.Project,
				Service:         "project",
				TopicCandidates: DetectBulletTopicCandidates(ec.Project),
			})
		}
		// Sort service keys so the manifest is stable run-to-run.
		svcKeys := make([]string, 0, len(ec.Service))
		for k := range ec.Service {
			svcKeys = append(svcKeys, k)
		}
		sort.Strings(svcKeys)
		for _, svc := range svcKeys {
			body := ec.Service[svc]
			if strings.TrimSpace(body) == "" {
				continue
			}
			// S3 PorterTunableDirectives — comment block targets the
			// service's yaml shape (tier yaml = import.yaml); recipe
			// authors annotate `verticalAutoscaling.*` / `minContainers`
			// rationale here. Enumerate against the comment body so the
			// audit can connect block→directive without an extra walk.
			m.TierYAMLComments[tierIdx] = append(m.TierYAMLComments[tierIdx], Refinement2ManifestEntry{
				IDKey:                   "tier_yaml_comments:" + tierIdx + ":" + svc,
				Body:                    body,
				Service:                 svc,
				TopicCandidates:         DetectBulletTopicCandidates(body),
				PorterTunableDirectives: EnumeratePorterTunableDirectives(body),
			})
		}
	}

	// S4/S5/S7 — per codebase. Iterate codebases in declared order so the
	// manifest mirrors the plan's authoring order; the keys map sort
	// above only stabilizes within-surface ordering.
	for _, cb := range plan.Codebases {
		host := cb.Hostname

		// S4 — codebase IG. Two storage shapes:
		//   1. slotted fragments — `codebase/<host>/integration-guide/<n>`
		//   2. unslotted fragment — `codebase/<host>/integration-guide`
		// Slotted form is the modern (run-16 §6.5) shape; we enumerate
		// both so a legacy/unslotted plan still gets a row.
		slotted := codebaseIGSlotEntries(plan.Fragments, host)
		m.CodebaseIG[host] = append(m.CodebaseIG[host], slotted...)
		if unslotted := plan.Fragments["codebase/"+host+"/integration-guide"]; strings.TrimSpace(unslotted) != "" {
			m.CodebaseIG[host] = append(m.CodebaseIG[host], parseUnslottedIGEntries(host, unslotted)...)
		}
		// Even when the codebase has no IG entries yet, hold a placeholder
		// key so the walked-ledger close-gate forces the sub-agent to
		// acknowledge it (zero-finding allowed only when ledger names
		// the key — empty-IG is still a walked decision).
		if len(m.CodebaseIG[host]) == 0 {
			m.CodebaseIG[host] = []Refinement2ManifestEntry{{
				IDKey: "codebase_ig:" + host + ":<empty>",
				Body:  "",
				Slot:  "<empty>",
			}}
		}

		// S5 — codebase KB. Parsed by bold-symptom bullet. Empty-KB still
		// gets a placeholder key for the walked-ledger.
		kbBody := plan.Fragments["codebase/"+host+"/knowledge-base"]
		kbEntries := parseKBBullets(host, kbBody)
		if len(kbEntries) == 0 {
			kbEntries = []Refinement2ManifestEntry{{
				IDKey: "codebase_kb:" + host + ":<empty>",
				Body:  "",
				Stem:  "<empty>",
			}}
		}
		m.CodebaseKB[host] = kbEntries

		// S7 — codebase zerops.yaml. One fragment per codebase; the WHOLE
		// yaml is one item per audit_checklist.md §S7. Even an empty
		// fragment gets a placeholder so the walked-ledger forces a yes/
		// no acknowledgement.
		yamlBody := plan.Fragments["codebase/"+host+"/zerops-yaml"]
		m.CodebaseZeropsYAML[host] = []Refinement2ManifestEntry{{
			IDKey:                   "codebase_zerops_yaml:" + host,
			Body:                    yamlBody,
			PorterTunableDirectives: EnumeratePorterTunableDirectives(yamlBody),
			PerFieldComments:        ExtractPerFieldComments(yamlBody),
		}}
	}
	return m, nil
}

// Refinement2ManifestFileName — the path under outputRoot the brief
// teaches the sub-agent to read. Fixed (no per-dispatch suffix);
// regenerated on every dispatch.
const Refinement2ManifestFileName = ".refinement-2-manifest.json"

// WriteRefinement2Manifest serializes the manifest JSON-Indented and
// writes it to `<outputRoot>/.refinement-2-manifest.json`. Returns the
// absolute path on success. Atomic via temp+rename.
func WriteRefinement2Manifest(outputRoot string, plan *Plan) (string, error) {
	if outputRoot == "" {
		return "", fmt.Errorf("WriteRefinement2Manifest: empty outputRoot")
	}
	m, err := BuildRefinement2Manifest(plan)
	if err != nil {
		return "", err
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	body = append(body, '\n')
	dst := filepath.Join(outputRoot, Refinement2ManifestFileName)
	tmp, err := os.CreateTemp(outputRoot, ".refinement-2-manifest.*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp manifest: %w", err)
	}
	tmpPath := tmp.Name()
	if _, wErr := tmp.Write(body); wErr != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write temp manifest: %w", wErr)
	}
	if cErr := tmp.Close(); cErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close temp manifest: %w", cErr)
	}
	if rErr := os.Rename(tmpPath, dst); rErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("rename temp manifest: %w", rErr)
	}
	return dst, nil
}

// codebaseIGSlotEntries collects every slotted IG fragment for a host.
// Slotted ids look like `codebase/<host>/integration-guide/<n>`. Engine-
// emitted slot 1 (the yaml mirror) is intentionally NOT enumerated —
// audit_checklist.md routes IG #1 issues to S7 instead.
func codebaseIGSlotEntries(fragments map[string]string, host string) []Refinement2ManifestEntry {
	prefix := "codebase/" + host + "/integration-guide/"
	ids := make([]string, 0)
	for id := range fragments {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		slotStr := strings.TrimPrefix(id, prefix)
		if slotStr == "" {
			continue
		}
		// Skip engine-emitted slot 1 — IG #1 mirrors the yaml fragment;
		// the audit routes IG #1 issues to S7 (codebase/<h>/zerops-yaml).
		if slotStr == "1" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Refinement2ManifestEntry, 0, len(ids))
	for _, id := range ids {
		slot := strings.TrimPrefix(id, prefix)
		body := fragments[id]
		if strings.TrimSpace(body) == "" {
			continue
		}
		out = append(out, Refinement2ManifestEntry{
			IDKey:           "codebase_ig:" + host + ":" + slot,
			Body:            body,
			Slot:            slot,
			TopicCandidates: DetectBulletTopicCandidates(body),
		})
	}
	return out
}

// igUnslottedHeadingRE matches the canonical `### N. <title>` IG item
// heading inside an unslotted IG body. Mirrors validators_codebase.go's
// igHeadingItemRE so the manifest enumerates the same items the
// validator gates against.
var igUnslottedHeadingRE = regexp.MustCompile(`(?m)^### (\d+)\.\s+(\S.*)$`)

// parseUnslottedIGEntries parses an unslotted IG fragment body into one
// entry per `### N. <title>` heading + body span. Used for the legacy
// pre-run-16 fragment shape; the slotted shape is the canonical modern
// form.
func parseUnslottedIGEntries(host, body string) []Refinement2ManifestEntry {
	body = extractBetweenMarkers(body, "integration-guide")
	if body == "" {
		return nil
	}
	matches := igUnslottedHeadingRE.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]Refinement2ManifestEntry, 0, len(matches))
	for i, m := range matches {
		slot := body[m[2]:m[3]]
		// Skip engine-emitted IG #1.
		if slot == "1" {
			continue
		}
		var end int
		if i+1 < len(matches) {
			end = matches[i+1][0]
		} else {
			end = len(body)
		}
		segment := strings.TrimSpace(body[m[0]:end])
		out = append(out, Refinement2ManifestEntry{
			IDKey:           "codebase_ig:" + host + ":" + slot,
			Body:            segment,
			Slot:            slot,
			TopicCandidates: DetectBulletTopicCandidates(segment),
		})
	}
	return out
}

// kbBulletRE matches a KB bullet header — opens with `- **<stem>**`.
// Captures the stem text inside the bold pair so the manifest can route
// findings by stem (matches the writer's KB authoring shape).
var kbBulletRE = regexp.MustCompile(`(?m)^\s*-\s+\*\*([^*]+)\*\*`)

// parseKBBullets parses a KB fragment body into one entry per bullet.
// Each bullet's body spans from its `- **stem**` opener to the next
// bullet (or end-of-body).
func parseKBBullets(host, body string) []Refinement2ManifestEntry {
	body = extractBetweenMarkers(body, "knowledge-base")
	if body == "" {
		return nil
	}
	matches := kbBulletRE.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]Refinement2ManifestEntry, 0, len(matches))
	for i, m := range matches {
		stem := strings.TrimSpace(body[m[2]:m[3]])
		var end int
		if i+1 < len(matches) {
			end = matches[i+1][0]
		} else {
			end = len(body)
		}
		segment := strings.TrimSpace(body[m[0]:end])
		out = append(out, Refinement2ManifestEntry{
			IDKey:           "codebase_kb:" + host + ":" + strconv.Itoa(i),
			Body:            segment,
			Stem:            stem,
			TopicCandidates: DetectBulletTopicCandidates(segment),
		})
	}
	return out
}

package recipe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Run-13 §V — structural-relation validator for tier import.yaml
// prose vs emitted yaml fields.
//
// Per system.md §4 the load-bearing fix is §T (push the tier
// capability matrix into the brief composer so the agent authors
// against truth). §V is the backstop that catches divergence the
// brief teaching missed: a comment paragraph above a service block
// claims a numeric or categorical fact that the field 6-10 lines
// below contradicts. Structural relation between two yaml elements,
// not a phrase-ban catalog — defensible per §4.
//
// Wired as Notice severity. §T eliminates these at the source; §V
// surfaces the residual without blocking publication. Promote to
// Blocking only after a dogfood run validates the §T teaching
// landed.
//
// Detected divergences:
//   - prose claims `\b(\d+) replicas?\b` adjacent to a runtime block;
//     yaml emits a different `minContainers: <N>` (default 1)
//     → tier-prose-replica-count-mismatch
//   - prose claims `HA` / `high availability` / `backed-up` /
//     `replicated` adjacent to a managed-service block whose `mode:`
//     field is `NON_HA` → tier-prose-ha-claim-vs-non-ha
//   - prose claims `\b(\d+)\s*(GB|MB)\b` adjacent to a storage block
//     whose `objectStorageSize: <N>` differs
//     → tier-prose-storage-size-mismatch
//   - prose claims `static[ -]runtime` adjacent to an `app` runtime
//     block whose `type:` is NOT `static`
//     → tier-prose-runtime-type-mismatch

// modeNonHA is the literal yaml field value the emitter writes when a
// managed service is downgraded out of HA at tier 5.
const modeNonHA = "NON_HA"

// kindStorage tags a parsed yaml service block as object-storage so
// the §V storage-quota check applies. Distinct from the ServiceKind
// constant — that is a Plan-level taxonomy, this is the parser's
// classification of a yaml type string the validator just read.
const kindStorage = "storage"

var (
	// replicaClaimRE picks up a numeric or written-out quantity
	// adjacent to "replica(s)". Run-12 tier-5 README shipped "every
	// runtime carries at least three replicas" — agent prose
	// idiomatically writes single-digit counts as words. Limit the
	// written-out forms to one..ten so phrases like "many replicas" /
	// "production replicas" don't trip the detector.
	replicaClaimRE     = regexp.MustCompile(`(?i)\b(\d+|one|two|three|four|five|six|seven|eight|nine|ten)\s+replicas?\b`)
	haClaimRE          = regexp.MustCompile(`(?i)\b(HA(\s+mode)?|high[- ]availability|backed[- ]up|replicated)\b`)
	storageSizeClaimRE = regexp.MustCompile(`\b(\d+)\s*(GB|MB)\b`)
	staticRuntimeRE    = regexp.MustCompile(`(?i)\bstatic[- ]runtime\b`)
)

// wordToInt resolves the written-out forms one..ten to their integer
// equivalents. Returns 0 when the input is a digit string parsed by
// strconv (caller already has the int) or unrecognized.
func wordToInt(s string) int {
	switch strings.ToLower(s) {
	case "one":
		return 1
	case "two":
		return 2
	case "three":
		return 3
	case "four":
		return 4
	case "five":
		return 5
	case "six":
		return 6
	case "seven":
		return 7
	case "eight":
		return 8
	case "nine":
		return 9
	case "ten":
		return 10
	}
	return 0
}

// validateTierProseVsEmit walks the tier import.yaml body, parses the
// service blocks with their preceding comment paragraphs, and emits a
// Notice for each prose claim that disagrees with the adjacent
// emitted field.
//
// Returns nil violations when the path doesn't resolve to a known tier
// (the validator is only meaningful at the env import.yaml surface)
// or when the body has no service blocks.
//
// Why a non-context-context-aware signature: this is a pure helper on
// (path, body, inputs). The wired surface validator
// (`validateEnvImportComments`) holds the `context.Context` and just
// delegates here. Keeping it small lets tests call it directly.
func validateTierProseVsEmit(path string, body []byte, inputs SurfaceInputs) []Violation {
	if inputs.Plan == nil {
		return nil
	}
	tierKey := tierKeyFromPath(path)
	if tierKey == "" {
		return nil
	}
	tierIdx, _ := strconv.Atoi(tierKey)
	tier, ok := TierAt(tierIdx)
	if !ok {
		return nil
	}
	blocks := parseYAMLServiceBlocks(string(body))
	if len(blocks) == 0 {
		return nil
	}
	var vs []Violation
	for _, blk := range blocks {
		if blk.precedingComment == "" {
			continue
		}
		comment := blk.precedingComment
		// Replica-count claim vs emitted minContainers.
		if m := replicaClaimRE.FindStringSubmatch(comment); m != nil {
			claimed, _ := strconv.Atoi(m[1])
			if claimed == 0 {
				claimed = wordToInt(m[1])
			}
			actual := blk.minContainers
			if actual == 0 {
				actual = 1
			}
			if claimed > 0 && claimed != actual {
				vs = append(vs, notice("tier-prose-replica-count-mismatch", path,
					fmt.Sprintf("tier %d / %s: prose claims %d replicas; field emits minContainers: %d (excerpt: %s)",
						tierIdx, blk.hostname, claimed, actual, excerpt(comment))))
			}
		}
		// HA claim vs `mode: NON_HA` field.
		if haClaimRE.MatchString(comment) && blk.mode == modeNonHA {
			vs = append(vs, notice("tier-prose-ha-claim-vs-non-ha", path,
				fmt.Sprintf("tier %d / %s: prose claims HA / replicated / backed-up; field emits mode: NON_HA (%s) (excerpt: %s)",
					tierIdx, blk.hostname, capabilityHint(blk.serviceType), excerpt(comment))))
		}
		// Storage-quota claim vs emitted objectStorageSize.
		if blk.serviceKind == kindStorage {
			if m := storageSizeClaimRE.FindStringSubmatch(comment); m != nil {
				claimed, _ := strconv.Atoi(m[1])
				actual := blk.objectStorageSize
				if actual == 0 {
					actual = 1
				}
				if claimed != actual {
					vs = append(vs, notice("tier-prose-storage-size-mismatch", path,
						fmt.Sprintf("tier %d / %s: prose claims %d %s; field emits objectStorageSize: %d (excerpt: %s)",
							tierIdx, blk.hostname, claimed, m[2], actual, excerpt(comment))))
				}
			}
		}
		// Static-runtime claim vs emitted runtime type.
		if staticRuntimeRE.MatchString(comment) && blk.serviceType != "" && !strings.HasPrefix(blk.serviceType, "static") {
			vs = append(vs, notice("tier-prose-runtime-type-mismatch", path,
				fmt.Sprintf("tier %d / %s: prose claims static-runtime; field emits type: %s (excerpt: %s)",
					tierIdx, blk.hostname, blk.serviceType, excerpt(comment))))
		}
	}
	// Tier 5 is the only tier whose ServiceMode promotes to HA — make
	// sure the variable lands in a Notice when relevant. Tier index is
	// referenced for prose-on-other-tiers contexts (no special branch
	// here today).
	_ = tier
	return vs
}

// capabilityHint names the family-level HA-capability for a managed
// service, so the violation message tells the author *why* the field
// emit downgraded.
func capabilityHint(serviceType string) string {
	if serviceType == "" {
		return "see plan.go::managedServiceSupportsHA"
	}
	if managedServiceSupportsHA(serviceType) {
		return "family supports HA — explicit Plan.Service.SupportsHA=false?"
	}
	return "family is single-node only on Zerops; engine downgrades HA→NON_HA at emit"
}

// excerpt returns up to ~80 characters of a comment paragraph for
// inclusion in violation messages.
func excerpt(s string) string {
	s = strings.TrimSpace(s)
	const maxLen = 80
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// yamlServiceBlock is one parsed service-block snapshot — the
// preceding-comment paragraph, the hostname, the type, the kind
// (managed/storage/runtime), and the relevant field values needed for
// cross-checks.
type yamlServiceBlock struct {
	hostname          string
	serviceType       string
	serviceKind       string
	mode              string
	minContainers     int
	objectStorageSize int
	precedingComment  string
}

// parseYAMLServiceBlocks scans an import.yaml body and returns one
// yamlServiceBlock per `- hostname: <name>` entry. The preceding
// comment paragraph is the contiguous run of `# ...` lines immediately
// above the block (separated from any earlier block by at least one
// non-comment line). Field reads are line-prefix matches — keeps the
// scan independent of the yaml package and tolerant of trailing
// whitespace.
//
// Comment lines are joined into a single string with the leading `# `
// stripped, so regex matchers can pattern-match across the paragraph.
func parseYAMLServiceBlocks(body string) []yamlServiceBlock {
	lines := strings.Split(body, "\n")
	var out []yamlServiceBlock
	var commentBuf []string
	var current *yamlServiceBlock
	flush := func() {
		if current != nil {
			out = append(out, *current)
			current = nil
		}
	}
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			commentBuf = append(commentBuf, strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
		case strings.HasPrefix(trimmed, "- hostname:"):
			flush()
			current = &yamlServiceBlock{
				hostname:         strings.TrimSpace(strings.TrimPrefix(trimmed, "- hostname:")),
				precedingComment: strings.Join(commentBuf, " "),
			}
			commentBuf = nil
		case current != nil && strings.HasPrefix(trimmed, "type:"):
			current.serviceType = strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
			current.serviceKind = serviceKindForType(current.serviceType)
		case current != nil && strings.HasPrefix(trimmed, "mode:"):
			current.mode = strings.TrimSpace(strings.TrimPrefix(trimmed, "mode:"))
		case current != nil && strings.HasPrefix(trimmed, "minContainers:"):
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "minContainers:")))
			current.minContainers = n
		case current != nil && strings.HasPrefix(trimmed, "objectStorageSize:"):
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "objectStorageSize:")))
			current.objectStorageSize = n
			current.serviceKind = kindStorage
		case trimmed == "":
			// blank line — comments accumulated above don't belong to
			// the next block unless that block follows immediately.
			if current == nil {
				commentBuf = nil
			}
		}
	}
	flush()
	return out
}

// serviceKindForType classifies a yaml-emitted service type string
// against the recipe's ServiceKind taxonomy. Used by §V to decide
// whether the storage-size claim check applies.
func serviceKindForType(serviceType string) string {
	switch {
	case strings.HasPrefix(serviceType, "object-storage"):
		return kindStorage
	case strings.HasPrefix(serviceType, "nodejs"),
		strings.HasPrefix(serviceType, "static"),
		strings.HasPrefix(serviceType, "go"),
		strings.HasPrefix(serviceType, "php"),
		strings.HasPrefix(serviceType, "python"),
		strings.HasPrefix(serviceType, "ruby"):
		return "runtime"
	default:
		return "managed"
	}
}

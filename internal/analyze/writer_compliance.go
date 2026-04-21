package analyze

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Minimum sizes & counts per content-surface-contracts.md §Surface 4..6.
const (
	claudeMdSizeFloor   = 1200 // bytes
	claudeMdCustomFloor = 2
	introLineMin        = 1
	introLineMax        = 3
	igH3MinItems        = 3
	igH3MaxItems        = 6
	kbBulletMinCount    = 3
	kbBulletMaxCount    = 6
)

// fragmentRe captures a named ZEROPS_EXTRACT fragment body, in exact
// (post-Cx-MARKER-FORM-FIX) form only. Bodies authored without the
// trailing `#` are invisible to this regex — the matching per-file
// compliance result then records `markers_exact_form=false` so the
// analyst sees the defect attribution.
func fragmentRe(key string) *regexp.Regexp {
	esc := regexp.QuoteMeta(key)
	return regexp.MustCompile(`<!--\s*#ZEROPS_EXTRACT_START:` + esc + `#\s*-->(?s)(.*?)<!--\s*#ZEROPS_EXTRACT_END:` + esc + `#\s*-->`)
}

// Readme implements the Surface-4/5 per-file bars for one per-codebase
// README.md. Missing file → FileExists=false, status=fail. The caller
// decides whether to skip or include the result based on its own
// presence check.
func Readme(path string) ReadmeCompliance {
	data, err := os.ReadFile(path)
	if err != nil {
		return ReadmeCompliance{FileExists: false, Status: StatusFail}
	}
	size := len(data)
	body := string(data)
	r := ReadmeCompliance{
		FileExists:               true,
		SizeBytes:                size,
		IntroFragment:            analyzeIntroFragment(body),
		IntegrationGuideFragment: analyzeIGFragment(body),
		KnowledgeBaseFragment:    analyzeKBFragment(body),
	}
	r.Status = rollupStatus(r.IntroFragment.Status, r.IntegrationGuideFragment.Status, r.KnowledgeBaseFragment.Status)
	return r
}

func analyzeIntroFragment(body string) FragmentCompliance {
	fc := FragmentCompliance{}
	if matches := fragmentRe("intro").FindStringSubmatch(body); len(matches) == 2 {
		fc.MarkersPresent = true
		fc.MarkersExactForm = true
		inner := strings.TrimSpace(matches[1])
		lines := 0
		for ln := range strings.SplitSeq(inner, "\n") {
			if strings.TrimSpace(ln) != "" {
				lines++
			}
		}
		fc.LineCount = lines
		fc.InRange = lines >= introLineMin && lines <= introLineMax
		fc.Status = PassOrFail(fc.InRange)
		return fc
	}
	// Markers absent OR wrong form (broken form won't match the strict
	// regex). Disambiguate using the loose regex.
	if strings.Contains(body, "ZEROPS_EXTRACT_START:intro") {
		fc.MarkersPresent = true
		fc.MarkersExactForm = false
	}
	fc.Status = StatusFail
	return fc
}

func analyzeIGFragment(body string) FragmentCompliance {
	fc := FragmentCompliance{}
	matches := fragmentRe("integration-guide").FindStringSubmatch(body)
	if len(matches) != 2 {
		if strings.Contains(body, "ZEROPS_EXTRACT_START:integration-guide") {
			fc.MarkersPresent = true
			fc.MarkersExactForm = false
		}
		fc.Status = StatusFail
		return fc
	}
	fc.MarkersPresent = true
	fc.MarkersExactForm = true
	inner := matches[1]
	h3Count := 0
	h3s := strings.Split(inner, "\n### ")
	// first element is pre-first-###; skip it.
	if len(h3s) > 1 {
		h3Count = len(h3s) - 1
	}
	fc.H3Count = h3Count
	codeOK := true
	for i := 1; i < len(h3s); i++ {
		if !strings.Contains(h3s[i], "```") {
			codeOK = false
			break
		}
	}
	fc.EveryH3HasFencedCode = codeOK
	fc.InRange = h3Count >= igH3MinItems && h3Count <= igH3MaxItems
	if fc.InRange && codeOK {
		fc.Status = StatusPass
	} else {
		fc.Status = StatusFail
	}
	return fc
}

func analyzeKBFragment(body string) FragmentCompliance {
	fc := FragmentCompliance{}
	matches := fragmentRe("knowledge-base").FindStringSubmatch(body)
	if len(matches) != 2 {
		if strings.Contains(body, "ZEROPS_EXTRACT_START:knowledge-base") {
			fc.MarkersPresent = true
			fc.MarkersExactForm = false
		}
		fc.Status = StatusFail
		return fc
	}
	fc.MarkersPresent = true
	fc.MarkersExactForm = true
	inner := matches[1]
	fc.GotchasH3Present = strings.Contains(inner, "### Gotchas")
	// Count bullets at the fragment top level (stemmed "- **…**").
	bullets := 0
	for ln := range strings.SplitSeq(inner, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "- **") {
			bullets++
		}
	}
	fc.GotchaBulletCount = bullets
	fc.BulletsInRange = bullets >= kbBulletMinCount && bullets <= kbBulletMaxCount
	if fc.GotchasH3Present && fc.BulletsInRange {
		fc.Status = StatusPass
	} else {
		fc.Status = StatusFail
	}
	return fc
}

// ClaudeMd implements the Surface-6 bar.
func ClaudeMd(path string) CLAUDECompliance {
	data, err := os.ReadFile(path)
	if err != nil {
		return CLAUDECompliance{FileExists: false, Status: StatusFail}
	}
	size := len(data)
	body := string(data)
	base := []string{"Dev Loop", "Migrations", "Container Traps", "Testing"}
	basePresent := []string{}
	for _, s := range base {
		if strings.Contains(body, "## "+s) || strings.Contains(body, "# "+s) {
			basePresent = append(basePresent, s)
		}
	}
	customCount := max(h2Count(body)-len(basePresent), 0)
	cc := CLAUDECompliance{
		FileExists:          true,
		SizeBytes:           size,
		SizeGE1200:          size >= claudeMdSizeFloor,
		BaseSectionsPresent: basePresent,
		CustomSectionCount:  customCount,
		CustomSectionsGE2:   customCount >= claudeMdCustomFloor,
	}
	if cc.SizeGE1200 && cc.CustomSectionsGE2 && len(basePresent) == len(base) {
		cc.Status = StatusPass
	} else {
		cc.Status = StatusFail
	}
	return cc
}

// h2Count counts unique `## ` section headings (one per line).
func h2Count(body string) int {
	n := 0
	for ln := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(ln, "## ") {
			n++
		}
	}
	return n
}

func rollupStatus(parts ...string) string {
	if slices.Contains(parts, StatusFail) {
		return StatusFail
	}
	if slices.Contains(parts, StatusSkip) {
		return StatusSkip
	}
	return StatusPass
}

// CollectWriterCompliance builds the writer_readmes + writer_claude_md
// maps from the deliverable tree. Missing files are reported with
// FileExists=false so the caller can cite "stranded by F-10".
func CollectWriterCompliance(deliverableDir string, codebases []string) (map[string]ReadmeCompliance, map[string]CLAUDECompliance) {
	readmes := make(map[string]ReadmeCompliance, len(codebases))
	claudeMds := make(map[string]CLAUDECompliance, len(codebases))
	for _, cb := range codebases {
		rp := filepath.Join(deliverableDir, cb, "README.md")
		cp := filepath.Join(deliverableDir, cb, "CLAUDE.md")
		readmes[cb+"/README.md"] = Readme(rp)
		claudeMds[cb+"/CLAUDE.md"] = ClaudeMd(cp)
	}
	return readmes, claudeMds
}

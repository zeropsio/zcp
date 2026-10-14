package workflow

import (
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// Rebuild (spec-mate §2.8a.4): a pair meta read without `stages` — written by
// an older zcp, which keeps only the stageHostname/stageSetupName mirror —
// whose repository's root zerops.yml declares more than one part gets its
// Stages rebuilt from the repository and the app's Mate tier. The parts in
// zerops.yml are the truth; each further part's hostname is the tier entry
// that builds that part from the pair's repository. zcp never derives a
// hostname: a part no tier entry names, or two name, stays out until a plan
// adds its stage.

// StagesRebuildDue reports whether meta is a pair read without `stages`
// whose zerops.yml (the repository root's, as text) declares more than one
// part. others are the other pairs' metas, whose dev setups are no part. It
// reads nothing else, so a caller asks it before reading the tier.
func StagesRebuildDue(meta *ServiceMeta, others []*ServiceMeta, zeropsYml string) bool {
	if meta == nil || !meta.stagesUnrecorded || meta.Mode != topology.ModeStandard || len(meta.Stages) != 1 {
		return false
	}
	parts, ok := zeropsYmlParts(zeropsYml, devSetups(meta, others), meta.Stages[0].SetupName)
	return ok && len(parts) > 1
}

// RebuildStages is meta's stages rebuilt from zeropsYml and the Mate tier
// (its import YAML): the mirror's stage first, then each further part, in
// zerops.yml order, whose hostname the one tier entry building that part from
// the pair's repository names. A tier hostname a pair already records is
// never taken. When the tier does not name the dev half, or two dev services
// of other pairs build from the same repository — which of them owns a part
// is not known — the mirror stays the only stage. Call it when
// StagesRebuildDue holds.
func RebuildStages(meta *ServiceMeta, others []*ServiceMeta, zeropsYml, tierYml string) []topology.StageRef {
	stages := slices.Clone(meta.Stages)
	parts, ok := zeropsYmlParts(zeropsYml, devSetups(meta, others), meta.Stages[0].SetupName)
	if !ok {
		return stages
	}
	var tier recipeImportDoc
	if err := yaml.Unmarshal([]byte(tierYml), &tier); err != nil {
		return stages
	}
	repo := ""
	for _, svc := range tier.Services {
		if strings.TrimSpace(svc.Hostname) == meta.Hostname {
			repo = topology.CanonicalRepoURL(svc.BuildFromGit.URL)
		}
	}
	if repo == "" {
		return stages
	}
	claimed := map[string]bool{}
	otherDevs := map[string]bool{}
	for _, m := range append([]*ServiceMeta{meta}, others...) {
		claimed[m.Hostname] = true
		if m != meta {
			otherDevs[m.Hostname] = true
		}
		for _, h := range m.StageHostnames() {
			claimed[h] = true
		}
	}
	bySetup := map[string][]string{}
	for _, svc := range tier.Services {
		host := strings.TrimSpace(svc.Hostname)
		if topology.CanonicalRepoURL(svc.BuildFromGit.URL) != repo {
			continue
		}
		if otherDevs[host] {
			return stages
		}
		if setup := strings.TrimSpace(svc.ZeropsSetup); setup != "" && !claimed[host] {
			bySetup[setup] = append(bySetup[setup], host)
		}
	}
	for _, part := range parts {
		if part == meta.Stages[0].SetupName {
			continue
		}
		if hosts := bySetup[part]; len(hosts) == 1 {
			stages = append(stages, topology.StageRef{Hostname: hosts[0], SetupName: part})
		}
	}
	return stages
}

// devSetups are the setups the pairs' dev halves deploy with: no part.
func devSetups(meta *ServiceMeta, others []*ServiceMeta) map[string]bool {
	dev := map[string]bool{}
	for _, m := range append([]*ServiceMeta{meta}, others...) {
		if m.PrimarySetupName != "" {
			dev[m.PrimarySetupName] = true
		}
	}
	return dev
}

// zeropsYmlParts are the parts zeropsYml declares, in its order: every setup
// but a dev setup and a base — a setup another names in `extends` — unless it
// is the mirror's setup. ok is false when zeropsYml does not read.
func zeropsYmlParts(zeropsYml string, dev map[string]bool, mirrorSetup string) ([]string, bool) {
	var doc struct {
		Zerops []struct {
			Setup   string `yaml:"setup"`
			Extends any    `yaml:"extends"`
		} `yaml:"zerops"`
	}
	if err := yaml.Unmarshal([]byte(zeropsYml), &doc); err != nil {
		return nil, false
	}
	base := map[string]bool{}
	for _, s := range doc.Zerops {
		switch v := s.Extends.(type) {
		case string:
			base[v] = true
		case []any:
			for _, item := range v {
				if name, isName := item.(string); isName {
					base[name] = true
				}
			}
		}
	}
	var parts []string
	for _, s := range doc.Zerops {
		setup := strings.TrimSpace(s.Setup)
		if setup == "" || dev[setup] || (base[setup] && setup != mirrorSetup) {
			continue
		}
		parts = append(parts, setup)
	}
	return parts, true
}

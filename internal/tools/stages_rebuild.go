package tools

import (
	"context"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// rebuildMateStages gives every pair meta an older zcp left without `stages`
// its stages back (spec-mate §2.8a.4 Rebuild): each pair's zerops.yml is read
// on its mount under mountRoot, and only when one declares more than one part
// is the app's Mate tier read from HQ — once per pass. A rebuilt meta is
// written, so it records stages from then on. It runs on a Mate only and is
// silent: a pass that cannot read the tier leaves the metas as they are, and
// the next pass tries again.
func rebuildMateStages(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info, stateDir, mountRoot string) {
	if !rt.MateEnabled || !rt.InContainer {
		return
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return
	}
	tier, tierRead := "", false
	for _, m := range metas {
		// A pool keying its pair has no checkout on the mount — its hostname
		// names no mount (docs/spec-workflows.md §13 PM-10) — and its stages
		// came over with the pair.
		if m.DevIsPool() {
			continue
		}
		yml, _ := readLocalZeropsYAML(filepath.Join(mountRoot, m.Hostname))
		if !workflow.StagesRebuildDue(m, othersThan(metas, m.Hostname), yml) {
			continue
		}
		if !tierRead {
			tierRead = true
			tier = readMateTier(ctx, httpClient, rt)
		}
		if tier == "" {
			return
		}
		// Under the meta lock every pair is read again: a stage the pass
		// just gave an earlier pair is that pair's.
		_ = workflow.UpdateServiceMeta(stateDir, m.Hostname, func(fresh *workflow.ServiceMeta) error {
			all, err := workflow.ListServiceMetas(stateDir)
			if err != nil {
				return workflow.ErrSkipWrite
			}
			others := othersThan(all, fresh.Hostname)
			if !workflow.StagesRebuildDue(fresh, others, yml) {
				return workflow.ErrSkipWrite
			}
			fresh.Stages = workflow.RebuildStages(fresh, others, yml, tier)
			return nil
		})
	}
}

// readMateTier is the app's Mate tier on main of its recipe repository, ""
// when this Mate is not linked, HQ does not answer or main lacks it.
func readMateTier(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info) string {
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
		return ""
	}
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	defer cancel()
	read, err := hqc.RecipeTier(callCtx, hq.RecipeTierMate)
	if err != nil || read.State != hq.RecipePresent {
		return ""
	}
	return read.ImportYAML
}

// othersThan is every meta but the pair of hostname.
func othersThan(metas []*workflow.ServiceMeta, hostname string) []*workflow.ServiceMeta {
	others := make([]*workflow.ServiceMeta, 0, len(metas))
	for _, o := range metas {
		if o.Hostname != hostname {
			others = append(others, o)
		}
	}
	return others
}

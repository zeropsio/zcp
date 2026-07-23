package eval

import "github.com/zeropsio/zcp/internal/content"

func resetGuidedForScenario(workDir string) error {
	return content.SetGuided(workDir, false)
}

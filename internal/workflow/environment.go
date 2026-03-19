package workflow

import "github.com/zeropsio/zcp/internal/runtime"

// Environment represents the execution environment.
type Environment string

const (
	EnvContainer Environment = "container"
	EnvLocal     Environment = "local"
)

// DetectEnvironment returns the environment based on runtime detection.
func DetectEnvironment(rt runtime.Info) Environment {
	if rt.InContainer {
		return EnvContainer
	}
	return EnvLocal
}

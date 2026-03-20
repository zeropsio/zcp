package tools

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkStrategy validates that every runtime target has a valid strategy assigned.
func checkStrategy() workflow.StepChecker {
	return func(_ context.Context, plan *workflow.ServicePlan, state *workflow.BootstrapState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}

		var checks []workflow.StepCheck
		allPassed := true

		if state == nil || len(state.Strategies) == 0 {
			for _, target := range plan.Targets {
				checks = append(checks, workflow.StepCheck{
					Name:   target.Runtime.DevHostname + "_strategy",
					Status: statusFail,
					Detail: "no strategy assigned",
				})
			}
			return &workflow.StepCheckResult{
				Passed:  false,
				Checks:  checks,
				Summary: "no strategies assigned",
			}, nil
		}

		for _, target := range plan.Targets {
			hostname := target.Runtime.DevHostname
			value, exists := state.Strategies[hostname]
			switch {
			case !exists:
				checks = append(checks, workflow.StepCheck{
					Name:   hostname + "_strategy",
					Status: statusFail,
					Detail: "no strategy assigned",
				})
				allPassed = false
			case !validStrategies[value]:
				checks = append(checks, workflow.StepCheck{
					Name:   hostname + "_strategy",
					Status: statusFail,
					Detail: fmt.Sprintf("invalid strategy %q", value),
				})
				allPassed = false
			default:
				checks = append(checks, workflow.StepCheck{
					Name:   hostname + "_strategy",
					Status: statusPass,
					Detail: value,
				})
			}
		}

		summary := "all targets have valid strategies"
		if !allPassed {
			summary = "strategy validation failed"
		}
		return &workflow.StepCheckResult{
			Passed:  allPassed,
			Checks:  checks,
			Summary: summary,
		}, nil
	}
}

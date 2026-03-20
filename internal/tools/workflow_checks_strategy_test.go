package tools

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func TestCheckStrategy_AllTargetsCovered_Pass(t *testing.T) {
	t.Parallel()
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{
			{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
			{Runtime: workflow.RuntimeTarget{DevHostname: "apidev", Type: "go@1"}},
		},
	}
	state := &workflow.BootstrapState{
		Strategies: map[string]string{
			"appdev": "push-dev",
			"apidev": "ci-cd",
		},
	}

	checker := checkStrategy()
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckStrategy_MissingTarget_Fail(t *testing.T) {
	t.Parallel()
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{
			{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
		},
	}
	state := &workflow.BootstrapState{
		Strategies: map[string]string{},
	}

	checker := checkStrategy()
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for missing target strategy")
	}
	hasFail := false
	for _, c := range result.Checks {
		if c.Status == "fail" && c.Name == "appdev_strategy" {
			hasFail = true
		}
	}
	if !hasFail {
		t.Error("expected appdev_strategy fail check")
	}
}

func TestCheckStrategy_InvalidValue_Fail(t *testing.T) {
	t.Parallel()
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{
			{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
		},
	}
	state := &workflow.BootstrapState{
		Strategies: map[string]string{
			"appdev": "invalid",
		},
	}

	checker := checkStrategy()
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for invalid strategy value")
	}
	hasFail := false
	for _, c := range result.Checks {
		if c.Status == "fail" && c.Name == "appdev_strategy" {
			hasFail = true
		}
	}
	if !hasFail {
		t.Error("expected appdev_strategy fail check for invalid value")
	}
}

func TestCheckStrategy_NilStrategies_Fail(t *testing.T) {
	t.Parallel()
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{
			{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
		},
	}
	state := &workflow.BootstrapState{
		Strategies: nil,
	}

	checker := checkStrategy()
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for nil strategies")
	}
}

func TestCheckStrategy_NilPlan_ReturnsNil(t *testing.T) {
	t.Parallel()
	checker := checkStrategy()
	result, err := checker(context.Background(), nil, &workflow.BootstrapState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil plan")
	}
}

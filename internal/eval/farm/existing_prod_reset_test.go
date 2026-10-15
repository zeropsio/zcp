package farm

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/platform"
)

// stackClient is a platform.Client whose direct service list shrinks as
// services are deleted.
type stackClient struct {
	platform.Client
	services []platform.ServiceStack
	deleted  []string
}

func (c *stackClient) ListServicesDirect(context.Context, string) ([]platform.ServiceStack, error) {
	return slices.Clone(c.services), nil
}

func (c *stackClient) DeleteService(_ context.Context, id string) (*platform.Process, error) {
	c.deleted = append(c.deleted, id)
	c.services = slices.DeleteFunc(c.services, func(s platform.ServiceStack) bool { return s.ID == id })
	return &platform.Process{}, nil
}

func userService(id, name string) platform.ServiceStack {
	return platform.ServiceStack{ID: id, Name: name, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"}}
}

func coreService() platform.ServiceStack {
	return platform.ServiceStack{ID: "core-id", Name: "core", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "CORE"}}
}

// TestDeleteUserServices_LeavesOnlyCore pins the existing-prod reset: every
// non-system service is deleted and the project keeps `core`.
func TestDeleteUserServices_LeavesOnlyCore(t *testing.T) {
	t.Parallel()
	c := &stackClient{services: []platform.ServiceStack{coreService(), userService("a", "app"), userService("d", "db")}}
	if err := deleteUserServices(context.Background(), c, "p", time.Millisecond, time.Second); err != nil {
		t.Fatalf("deleteUserServices: %v", err)
	}
	if len(c.services) != 1 || c.services[0].Name != "core" {
		t.Fatalf("services left = %+v, want core only", c.services)
	}
	if slices.Contains(c.deleted, "core-id") {
		t.Errorf("core was deleted: %v", c.deleted)
	}
}

// TestDeleteUserServices_EmptyProject_DeletesNothing pins that a project
// with only core is left untouched.
func TestDeleteUserServices_EmptyProject_DeletesNothing(t *testing.T) {
	t.Parallel()
	c := &stackClient{services: []platform.ServiceStack{coreService()}}
	if err := deleteUserServices(context.Background(), c, "p", time.Millisecond, time.Second); err != nil {
		t.Fatalf("deleteUserServices: %v", err)
	}
	if len(c.deleted) != 0 {
		t.Errorf("deleted %v, want nothing", c.deleted)
	}
}

// TestFinalizeActiveRun_ExistingProdScenario_ResetsTheStandingProject pins
// that a settled run which declared the existing-prod project id has that
// project reset with the farm's id+token pair, and a run that did not never
// touches it.
func TestFinalizeActiveRun_ExistingProdScenario_ResetsTheStandingProject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		required []string
		want     bool
	}{
		{"declared", []string{eval.ExistingProjectIDEnvVar, eval.ExistingProdTokenEnvVar}, true},
		{"not declared", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const clientID = "client-existing-prod-reset"
			f := newControllerFixture(t, clientID)
			batch, scenario := "batch-existing-prod-reset", "sc"
			runID := testRunID(t, batch, scenario)
			primaryID := f.account.seedProject(ProjectPrefix + runID)
			seedSettledRun(t, f.s3, runID, scenario, ResultPassed)
			now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
			a := activeRun{
				scheduledRun: scheduledRun{ScenarioRun: ScenarioRun{ID: scenario, RequiredEnvVars: tc.required}, RunID: runID},
				ProjectID:    primaryID, Deadline: now.Add(time.Minute),
			}
			var gotID, gotToken string
			calls := 0
			rr, _, _, err := finalizeActiveRun(context.Background(), f.client, f.sink, a, RunOptions{
				ClientID: clientID, CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha",
				ExistingProjectID: "prod-id", ExistingProdToken: "prod-token",
				ResetExistingProd: func(_ context.Context, id, token string) error {
					calls++
					gotID, gotToken = id, token
					return nil
				},
			}, func() time.Time { return now }, time.Millisecond)
			if err != nil {
				t.Fatalf("finalizeActiveRun: %v (result %+v)", err, rr)
			}
			if tc.want && (calls != 1 || gotID != "prod-id" || gotToken != "prod-token") {
				t.Fatalf("reset calls=%d id=%q token=%q, want one call with the farm's pair", calls, gotID, gotToken)
			}
			if !tc.want && calls != 0 {
				t.Fatalf("reset called %d times for a scenario that never declared the project", calls)
			}
		})
	}
}

// TestFinalizeActiveRun_ExistingProdResetFailure_IsCleanupError pins that a
// failed reset surfaces as a cleanup error instead of vanishing.
func TestFinalizeActiveRun_ExistingProdResetFailure_IsCleanupError(t *testing.T) {
	t.Parallel()
	const clientID = "client-existing-prod-reset-fail"
	f := newControllerFixture(t, clientID)
	batch, scenario := "batch-existing-prod-reset-fail", "sc"
	runID := testRunID(t, batch, scenario)
	primaryID := f.account.seedProject(ProjectPrefix + runID)
	seedSettledRun(t, f.s3, runID, scenario, ResultPassed)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	a := activeRun{
		scheduledRun: scheduledRun{ScenarioRun: ScenarioRun{ID: scenario, RequiredEnvVars: []string{eval.ExistingProjectIDEnvVar}}, RunID: runID},
		ProjectID:    primaryID, Deadline: now.Add(time.Minute),
	}
	rr, _, _, err := finalizeActiveRun(context.Background(), f.client, f.sink, a, RunOptions{
		ClientID: clientID, CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha",
		ResetExistingProd: func(context.Context, string, string) error { return errors.New("boom") },
	}, func() time.Time { return now }, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "reset existing prod project") {
		t.Fatalf("err = %v (result %+v), want reset cleanup failure", err, rr)
	}
}

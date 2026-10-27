package workflow

import (
	"strings"
	"testing"
)

// TestRenderStatus_RequiredStepsHeadTheStatus: a step the Mate's setup is
// not done without — its group recipe, which a second Mate and the group's
// stage and production are built from, that cannot be proposed — is said in
// the head of status, where the agent reads what it has to do, each naming
// what is missing and the call that fixes it; none says nothing.
func TestRenderStatus_RequiredStepsHeadTheStatus(t *testing.T) {
	t.Parallel()
	step := `the group recipe cannot be proposed: runtime "webdev": its stage "webstage" builds a setup nothing records — zerops_workflow action="set-default-setup" targetService="webstage" setup="dev" stageSetup="<the setup webstage builds>"`
	out := RenderStatus(Response{Envelope: StateEnvelope{Phase: PhaseIdle, Required: []string{step}}, Guidance: []string{"some guidance"}})
	line := "→ REQUIRED: " + step
	if !strings.Contains(out, line) {
		t.Fatalf("status does not head with the required step:\n%s", out)
	}
	if strings.Index(out, line) > strings.Index(out, "some guidance") {
		t.Errorf("the required step comes after the guidance:\n%s", out)
	}
	if none := RenderStatus(Response{Envelope: StateEnvelope{Phase: PhaseIdle}}); strings.Contains(none, "REQUIRED") {
		t.Errorf("a status with nothing required says REQUIRED:\n%s", none)
	}
}

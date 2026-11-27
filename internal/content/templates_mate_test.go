package content

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// witness: internal/content/templates/skills/guided/phases/review-deploy.md | The Review build puts the described head on the stage → The stage half is their preview
func TestTemplates_TP1_MatePreviewAndReviewBuild(t *testing.T) {
	t.Parallel()

	agents, err := BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp", MateEnabled: true}, true)
	if err != nil {
		t.Fatalf("render Mate guidance: %v", err)
	}
	standalone, err := BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp"}, true)
	if err != nil {
		t.Fatalf("render standalone guidance: %v", err)
	}
	skill, err := ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("read guided skill: %v", err)
	}
	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("read atoms: %v", err)
	}

	texts := make([]struct{ name, body string }, 0, 1+len(skill)+len(atoms))
	texts = append(texts, struct{ name, body string }{"rendered Mate AGENTS.md", agents})
	var review string
	for _, file := range skill {
		texts = append(texts, struct{ name, body string }{file.RelPath, file.Content})
		if file.RelPath == "phases/review-deploy.md" {
			review = file.Content
		}
	}
	for _, atom := range atoms {
		texts = append(texts, struct{ name, body string }{atom.Name, atom.Content})
	}

	// The standalone AGENTS.md is rendered separately. Shared skill and atom
	// instructions name that environment explicitly when its stage is a preview.
	forbidden := []string{
		"stage service is this mate's preview",
		"stage service is their preview",
		"stage is this mate's preview",
		"stage is their preview",
		"stage is your preview",
		"stage is a preview",
		"this mate's preview of it",
		"stage half is their preview",
		"stage half is your preview",
		"stage half is this mate's preview",
		"stage half is a preview",
		"their preview (the stage half)",
		"preview is the stage",
		"stage half's url instead — still their preview",
	}
	for _, text := range texts {
		body := strings.ToLower(strings.Join(strings.Fields(text.body), " "))
		for _, phrase := range forbidden {
			if strings.Contains(body, phrase) {
				t.Errorf("%s calls the stage a preview: %q", text.name, phrase)
			}
		}
	}

	for _, want := range []string{"development copy of each app with a Preview", "a stage for the Review build"} {
		if !strings.Contains(agents, want) {
			t.Errorf("Mate standup guidance lacks %q", want)
		}
	}
	if !strings.Contains(standalone, "The stage half is **your preview**") {
		t.Error("standalone guidance does not call the stage the person's preview")
	}
	_, afterMate, found := strings.Cut(review, "### In a Mate\n")
	if !found {
		t.Fatal("guided review lacks the Mate path")
	}
	mate, outside, found := strings.Cut(afterMate, "### Outside a Mate\n")
	if !found {
		t.Fatal("guided review lacks the standalone path")
	}
	for _, want := range []string{"Commit and push", "describe", "The Review build puts the described head on the stage"} {
		if !strings.Contains(mate, want) {
			t.Errorf("guided Mate handover lacks %q", want)
		}
	}
	if strings.Contains(mate, "zerops_deploy") {
		t.Error("guided Mate handover names zerops_deploy")
	}
	for _, want := range []string{"checkpoint", "zerops_deploy", "zerops_verify"} {
		if !strings.Contains(outside, want) {
			t.Errorf("standalone guided review lacks %q", want)
		}
	}
}

package content

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/runtime"
)

// witness: internal/content/templates/skills/guided/phases/review-deploy.md | Describing it does not build the stage. → Describing it builds the stage.
func TestTemplates_TP1_MateDraftAndApp(t *testing.T) {
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

	// Scan knowledge authored here. Synced recipes, guides and decisions are
	// owned upstream; mailpit is the one recipe authored in this checkout.
	err = filepath.WalkDir("../knowledge", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (filepath.Ext(path) != ".md" && filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		switch filepath.Dir(path) {
		case filepath.Join("..", "knowledge", "guides"), filepath.Join("..", "knowledge", "decisions"):
			if filepath.Ext(path) == ".md" {
				return nil
			}
		case filepath.Join("..", "knowledge", "recipes"):
			if filepath.Base(path) != "mailpit.md" && filepath.Base(path) != "mailpit.import.yml" {
				return nil
			}
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		texts = append(texts, struct{ name, body string }{path, string(body)})
		return nil
	})
	if err != nil {
		t.Fatalf("read knowledge corpus: %v", err)
	}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("load embedded knowledge: %v", err)
	}
	for _, docCase := range []struct {
		uri   string
		wants []string
	}{
		{"zerops://playbooks/standup", []string{"draft the person opens in Mate", "adding it starts its build"}},
		{"zerops://playbooks/preview-pane", []string{"stage setup for its part", "Keep an existing setup's name"}},
	} {
		doc, getErr := store.Get(docCase.uri)
		if getErr != nil {
			t.Fatalf("read embedded %s: %v", docCase.uri, getErr)
		}
		body := doc.Description + "\n" + doc.Content
		for _, want := range docCase.wants {
			if !strings.Contains(body, want) {
				t.Errorf("embedded %s lacks %q", docCase.uri, want)
			}
		}
		texts = append(texts, struct{ name, body string }{docCase.uri, body})
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
		"until a part's stage stands never tell the person that part's preview is up",
		"give the person this mate's preview, with its address",
		"stage's preview variant",
		"give the stage mate's preview variant",
	}
	for _, text := range texts {
		body := strings.ToLower(strings.Join(strings.Fields(text.body), " "))
		for _, phrase := range forbidden {
			if strings.Contains(body, phrase) {
				t.Errorf("%s calls the stage a preview: %q", text.name, phrase)
			}
		}
	}

	for _, want := range []string{"Mate shows preparation", "do not narrate it in chat before a draft is ready"} {
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
	for _, want := range []string{"Commit and push", "describe", "Describing it does not build the stage."} {
		if !strings.Contains(mate, want) {
			t.Errorf("guided Mate handover lacks %q", want)
		}
	}
	if strings.Contains(mate, "zerops_deploy") {
		t.Error("guided Mate handover names zerops_deploy")
	}
	if !strings.Contains(review, "In a Mate, describe the draft and the app") {
		t.Error("guided narration does not name the draft and app")
	}
	for _, want := range []string{"checkpoint", "zerops_deploy", "zerops_verify"} {
		if !strings.Contains(outside, want) {
			t.Errorf("standalone guided review lacks %q", want)
		}
	}
}

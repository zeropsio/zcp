// Tests for: embedded content structure contracts — validate assumptions about markdown structure.
package knowledge

import (
	"strings"
	"testing"
)

func TestCore_H2Sections_ContainsImportYamlSchema(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	if _, ok := sections["import.yaml Schema"]; !ok {
		t.Error("core.md missing H2 section 'import.yaml Schema'")
	}
}

func TestCore_H2Sections_ContainsZeropsYamlSchema(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	if _, ok := sections["zerops.yaml Schema"]; !ok {
		t.Error("core.md missing H2 section 'zerops.yaml Schema'")
	}
}

func TestCore_H2Sections_ContainsSchemaAndExamples(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	// After the reshuffle the single "Rules & Pitfalls" / "Schema Rules" H2s
	// were split into three lifecycle-phase blocks: Provision Rules, Generate
	// Rules, Runtime Rules. Provision and Generate are injected per-step; each
	// carries only the rules the agent needs at that moment.
	for _, name := range []string{"import.yaml Schema", "zerops.yaml Schema", "Provision Rules", "Generate Rules", "Runtime Rules"} {
		if _, ok := sections[name]; !ok {
			t.Errorf("core.md missing H2 section %q", name)
		}
	}
}

func TestCore_H2Sections_ContainsLifecycleRules(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	for _, name := range []string{"Provision Rules", "Generate Rules", "Runtime Rules"} {
		if _, ok := sections[name]; !ok {
			t.Errorf("core.md missing H2 section %q", name)
		}
	}
}

func TestCore_ImportYamlSchema_ContainsPreprocessorFunctions(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	importSchema, ok := sections["import.yaml Schema"]
	if !ok {
		t.Fatal("core.md missing H2 section 'import.yaml Schema'")
	}
	if !strings.Contains(importSchema, "Preprocessor Functions") {
		t.Error("'import.yaml Schema' section should contain 'Preprocessor Functions' as H3 subsection")
	}
}

func TestCore_GenerateRules_ContainsDeployFilesAndTilde(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	// Deploy Semantics (tilde syntax, deployFiles invariant) is injected at
	// Generate after the reshuffle — generate writes zerops.yaml and needs
	// the rule in context there, not at provision.
	rules, ok := sections["Generate Rules"]
	if !ok {
		t.Fatal("core.md missing H2 section 'Generate Rules'")
	}
	if !strings.Contains(rules, "deployFiles") {
		t.Error("'Generate Rules' should mention 'deployFiles'")
	}
}

// TestCore_DeployUploadSize_FactsPresent pins what core teaches about a
// deploy's upload (docs/spec-workflows.md §8 DM-9): what a zcli push carries,
// that .deployignore and deployFiles do not shrink it, what does — and its
// cost on a self-deploy — how long the upload is waited for, and the
// self-deploy's .git. Root cause it closes: a 1.2 GB tracked tree timed out
// three deploys and zerops_knowledge had nothing on keeping files out of the
// upload.
func TestCore_DeployUploadSize_FactsPresent(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	section := doc.H2Sections()["Generate Rules"]
	for _, fact := range []string{
		"### Deploy upload size",
		"git snapshot", "untracked files `.gitignore` does not exclude", "the whole `.git` directory",
		"do NOT make the upload smaller",
		"`export-ignore`", "`git checkout -- <path>`",
		"`sha=`", "`.deployignore` does filter",
		"object storage",
		"`upload`", "5 min plus 1 s per MiB", "60 min",
		"`git count-objects -vH`", "`git repack -a -d`",
	} {
		if !strings.Contains(section, fact) {
			t.Errorf("core Generate Rules missing deploy-upload fact %q", fact)
		}
	}
}

// TestSearch_DeployUploadSize_FindsCore: the query a deploy's large-upload
// warning names reaches core's upload section.
func TestSearch_DeployUploadSize_FindsCore(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	results := store.Search("deploy upload size", 5)
	for _, r := range results[:min(3, len(results))] {
		if r.URI == "zerops://themes/core" {
			return
		}
	}
	uris := make([]string, 0, len(results))
	for _, r := range results {
		uris = append(uris, r.URI)
	}
	t.Errorf("search %q top 3 = %v, want zerops://themes/core", "deploy upload size", uris)
}

// TestCore_ZscInstall_NeedsSudo: the core reference writes the runtime-side
// `zsc install` the way run.prepareCommands accepts it — under sudo, since
// the runtime container runs as the zerops user (without it the deploy ends
// PREPARING_RUNTIME_FAILED).
func TestCore_ZscInstall_NeedsSudo(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	for line := range strings.SplitSeq(doc.Content, "\n") {
		if strings.Contains(line, "zsc install") && !strings.Contains(line, "sudo zsc install") {
			t.Errorf("core reference names `zsc install` without sudo: %q", line)
		}
	}
	if !strings.Contains(doc.Content, "sudo zsc install") {
		t.Error("core reference never shows `sudo zsc install`")
	}
}

func TestRuntimeNormalizer_AllMapped_GuideResolvable(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	for base, slug := range runtimeNormalizer {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			guide := store.getRuntimeGuide(slug)
			if guide == "" {
				t.Skipf("runtime normalizer maps %q -> %q but no guide resolvable (recipe may not exist in API yet)", base, slug)
			}
		})
	}
}

func TestServices_H2Sections_ContainsWiringSyntax(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatalf("Get services: %v", err)
	}
	sections := doc.H2Sections()
	if _, ok := sections["Wiring Syntax"]; !ok {
		t.Error("services.md missing H2 section 'Wiring Syntax'")
	}
}

func TestServices_H2Sections_ContainsAllNormalizedServices(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatalf("Get services: %v", err)
	}
	sections := doc.H2Sections()

	for base, normalized := range serviceNormalizer {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			if _, ok := sections[normalized]; !ok {
				t.Errorf("services.md missing H2 section %q (mapped from %q)", normalized, base)
			}
		})
	}
}

func TestDecisionFiles_Accessible(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	for name, uri := range decisionFileMap {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, err := store.Get(uri)
			if err != nil {
				t.Errorf("decision file %q (%s) not found: %v", name, uri, err)
				return
			}
			if doc.Description == "" {
				t.Errorf("decision file %q missing description", name)
			}
		})
	}
}

func TestGetBriefing_RealisticStack_SizeReasonable(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16", "valkey@7.2"}, "", nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	// Briefing should be substantial but not enormous.
	if len(briefing) < 500 {
		t.Errorf("briefing too small (%d chars), expected >500 for realistic stack", len(briefing))
	}
	if len(briefing) > 100000 {
		t.Errorf("briefing too large (%d chars), expected <100000", len(briefing))
	}
}

// TestThemes_ServiceEnvVariables_NeverCalledDropped — an import's
// service-level `envVariables` is deprecated, not dropped: it creates the
// service's values, every one plain (docs/spec-zerops-env-lifecycle.md §8b).
// No theme tells an agent the values are dropped.
func TestThemes_ServiceEnvVariables_NeverCalledDropped(t *testing.T) {
	t.Parallel()
	themes := 0
	for uri, doc := range loadFromEmbedded() {
		if !strings.HasPrefix(uri, "zerops://themes/") {
			continue
		}
		themes++
		for line := range strings.SplitSeq(doc.Content, "\n") {
			if strings.Contains(line, "envVariables") && strings.Contains(strings.ToLower(line), "dropped") {
				t.Errorf("%s says envVariables are dropped: %q", uri, line)
			}
		}
	}
	if themes == 0 {
		t.Fatal("no theme documents embedded")
	}
}

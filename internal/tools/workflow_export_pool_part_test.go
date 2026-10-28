// Tests for: tools/workflow_export.go, tools/pool_part_origin.go — a part
// whose pair is keyed by its dev pool is exported by its stage, from its
// origin (docs/spec-workflows.md §9 E10): the bundle's runtime is the stage,
// its zerops.yaml and buildFromGit the recorded origin's, and nothing
// reaches the pool, a lease or the stage over ssh.
package tools

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// poolPartExportClient is the export's project after the move: the pool
// holding the dev's GIT_TOKEN, the stage, a database and one project env.
func poolPartExportClient() *platform.Mock {
	return newExportMock(append(poolPairServices(), managedService()),
		[]platform.ProjectEnvVar{{Key: "LOG_LEVEL", Content: "info"}}).
		WithServiceEnv(testPoolID, []platform.ServiceEnvVar{{Key: "GIT_TOKEN", Content: poolPartGitToken, Sensitive: true}})
}

func exportPoolPart(t *testing.T, stateDir, target string, ssh *hostLogSSH) map[string]any {
	t.Helper()
	result, _, err := handleExport(context.Background(), "proj1", nil, poolPartExportClient(), WorkflowInput{
		Workflow: workflowExport, TargetService: target,
		EnvClassifications: map[string]string{"LOG_LEVEL": "plain-config"},
	}, ssh, stateDir, poolPartRuntime())
	if err != nil {
		t.Fatalf("export(%s): %v", target, err)
	}
	text := extractText(result)
	if strings.Contains(text, poolPartGitToken) {
		t.Errorf("the export's answer carries the pool's GIT_TOKEN: %s", text)
	}
	body := map[string]any{"isError": result.IsError, "text": text}
	if !result.IsError {
		maps.Copy(body, decodeJSONResult(t, text))
	}
	return body
}

// TestHandleExport_PoolKeyedPart_StageFromOrigin: the stage of a pool-keyed
// pair exports as the bundle's one buildFromGit runtime — the stage's
// hostname and type, its recorded setup, the recorded origin — with the
// origin's zerops.yaml at its tracked ref; the answer is compose-ready, its
// next steps through a lease of the pool.
func TestHandleExport_PoolKeyedPart_StageFromOrigin(t *testing.T) {
	t.Parallel()
	url, heads := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	stateDir := poolPartPair(t, url, "main")
	ssh := &hostLogSSH{}

	out := exportPoolPart(t, stateDir, "appstage", ssh)

	if out["isError"] == true || out["status"] != "compose-ready" {
		t.Fatalf("export = %v, want compose-ready", out["text"])
	}
	b, _ := out["bundle"].(map[string]any)
	importYAML, _ := b["importYaml"].(string)
	for _, want := range []string{"hostname: appstage", "type: nodejs@22", "zeropsSetup: prod", "buildFromGit: " + topology.CanonicalRepoURL(url)} {
		if !strings.Contains(importYAML, want) {
			t.Errorf("importYaml lacks %q:\n%s", want, importYAML)
		}
	}
	if strings.Count(importYAML, "buildFromGit") != 1 || strings.Contains(importYAML, testPool) {
		t.Errorf("importYaml is not one buildFromGit runtime without the pool:\n%s", importYAML)
	}
	if b["zeropsYaml"] != strings.TrimRight(poolPartYAML, "\n") {
		t.Errorf("zeropsYaml = %v, want the origin's", b["zeropsYaml"])
	}
	source, _ := out["source"].(map[string]any)
	if source["commit"] != heads["main"] || source["ref"] != "main" {
		t.Errorf("source = %v, want the origin's main at %s", source, heads["main"])
	}
	steps := strings.Join(retireStrings(out["nextSteps"]), "\n")
	if !strings.Contains(steps, "zerops_lease") || !strings.Contains(steps, testPool) {
		t.Errorf("nextSteps do not go through a lease of the pool: %s", steps)
	}
	if strings.Contains(steps, "zerops_deploy") || strings.Contains(steps, "git-push-setup") {
		t.Errorf("nextSteps name a delivery the pair refuses: %s", steps)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("export reached %v over ssh", ssh.hosts)
	}
}

// TestHandleExport_PoolKeyedPart_PoolNamed_RefusedNamingTheStage: the pool
// keying the pair is no app runtime (PS-3); its refusal names the stage to
// export instead, before any read.
func TestHandleExport_PoolKeyedPart_PoolNamed_RefusedNamingTheStage(t *testing.T) {
	t.Parallel()
	stateDir := poolPartPair(t, "https://github.com/acme/app.git", "main")
	ssh := &hostLogSSH{}

	out := exportPoolPart(t, stateDir, testPool, ssh)

	text, _ := out["text"].(string)
	if out["isError"] != true || !strings.Contains(text, "Swarm pool") || !strings.Contains(text, "appstage") {
		t.Fatalf("export of the pool = %s, want refused naming the stage", text)
	}
	if len(ssh.hosts) != 0 {
		t.Errorf("export reached %v over ssh before refusing", ssh.hosts)
	}
}

// TestHandleExport_PoolKeyedPart_UnreadableOrigin_NamesWhatToFix: an origin
// export cannot read, or whose ref holds no zerops.yaml, is refused naming
// the origin, the ref and what to fix — never a scaffold or a git-push setup
// on the stage.
func TestHandleExport_PoolKeyedPart_UnreadableOrigin_NamesWhatToFix(t *testing.T) {
	t.Parallel()
	missing := poolPartMissingOrigin(t)
	present, _ := poolPartOrigin(t, map[string]string{"zerops.yaml": poolPartYAML}, nil)
	bare, _ := poolPartOrigin(t, map[string]string{"README.md": "hello\n"}, nil)
	tests := []struct {
		name       string
		remoteURL  string
		trackedRef string
		want       []string
	}{
		{"origin unreachable", missing, "main", []string{missing, "main", "GIT_TOKEN"}},
		{"tracked ref absent", present, "release", []string{"release", "zerops_lease"}},
		{"no zerops.yaml at the ref", bare, "main", []string{"zerops.yaml", "main", "zerops_lease"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := poolPartPair(t, tt.remoteURL, tt.trackedRef)
			ssh := &hostLogSSH{}

			out := exportPoolPart(t, stateDir, "appstage", ssh)

			text, _ := out["text"].(string)
			if out["isError"] != true {
				t.Fatalf("export = %s, want a refusal", text)
			}
			for _, w := range tt.want {
				if !strings.Contains(text, w) {
					t.Errorf("the refusal does not name %q: %s", w, text)
				}
			}
			if len(ssh.hosts) != 0 {
				t.Errorf("export reached %v over ssh", ssh.hosts)
			}
		})
	}
}

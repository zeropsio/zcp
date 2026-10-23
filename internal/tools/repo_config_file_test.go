package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestRepoConfigFile_AnAgentIsOnlyEverToldTheReposOwnConfigFileName: once
// zcp has read a repo that keeps its config as zerops.yml, neither the
// develop guidance nor any knowledge it points to calls that file
// zerops.yaml — the agent has no name to rename it to — and a repo with
// zerops.yaml never hears zerops.yml. A text that names both files names
// the repo's file once, never one name twice. A GitHub Actions workflow file
// (.github/workflows/zerops.yml) and a file whose name only ends in
// zerops.yml (notzerops.yml) are other files and keep their names.
func TestRepoConfigFile_AnAgentIsOnlyEverToldTheReposOwnConfigFileName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		file, never string
	}{
		{file: "zerops.yml", never: "zerops.yaml"},
		{file: "zerops.yaml", never: "zerops.yml"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeDev,
				BootstrapSession: "sess1", BootstrappedAt: "2026-10-15",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj1", Name: "weather"}).
				WithServices([]platform.ServiceStack{{
					ID: "svc-appdev", Name: "appdev", Status: "ACTIVE",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
				}})
			repos := &partsRepos{out: map[string]string{
				"appdev": "@@file\n" + tc.file + "\n@@yaml\nzerops:\n  - setup: dev\n    run:\n      start: zsc noop\n@@declaration\n@@markers\n@@packages\n",
			}}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
			RegisterWorkflow(srv, mock, nil, "proj1", nil, engine, nil, dir, "zcp", nil, repos, rt, "")
			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatalf("GetEmbeddedStore: %v", err)
			}
			RegisterKnowledge(srv, store, nil, nil, nil, engine)

			develop := callTool(t, srv, "zerops_workflow", map[string]any{
				"action": "start", "workflow": "develop", "intent": "get the weather app running", "scope": []string{"appdev"},
			})
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })
			if develop.IsError {
				t.Fatalf("develop start failed: %s", getTextContent(t, develop))
			}
			told := map[string]string{"develop start": getTextContent(t, develop)}

			// An agent that goes straight to the adopt route reads its guidance
			// before any other call has read the repo.
			adoptDir := t.TempDir()
			adoptSrv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterWorkflow(adoptSrv, mock, nil, "proj1", nil, workflow.NewEngine(adoptDir, workflow.EnvContainer, nil), nil, adoptDir, "zcp", nil, repos, rt, "")
			adopt := callTool(t, adoptSrv, "zerops_workflow", map[string]any{
				"action": "start", "workflow": "bootstrap", "route": "adopt", "intent": "adopt the weather app",
			})
			if adopt.IsError {
				t.Fatalf("bootstrap adopt start failed: %s", getTextContent(t, adopt))
			}
			told["bootstrap adopt start"] = getTextContent(t, adopt)
			for name, args := range map[string]map[string]any{
				"core reference":    {"uri": "zerops://themes/core"},
				"monorepo playbook": {"uri": "zerops://playbooks/monorepo"},
				"scaffold atom":     {"uri": "zerops://atoms/develop-first-deploy-scaffold-yaml"},
				"search":            {"query": "zerops.yaml filename yml vs yaml"},
			} {
				told[name] = getTextContent(t, callTool(t, srv, "zerops_knowledge", args))
			}
			if !strings.Contains(told["develop start"], tc.file) {
				t.Errorf("develop start never names %s:\n%s", tc.file, told["develop start"])
			}
			// The export scaffold atom is delivered inline in the export
			// workflow's response, which the same wrapper renames.
			atoms, err := workflow.LoadAtomCorpus()
			if err != nil {
				t.Fatalf("LoadAtomCorpus: %v", err)
			}
			for _, a := range atoms {
				if a.ID == "scaffold-zerops-yaml" {
					told["export scaffold"] = ops.NameRepoConfigFile(a.Body, tc.file)
				}
			}
			if got, want := ops.NameRepoConfigFile("notzerops.yml beside /var/www/zerops.yml", tc.file), "notzerops.yml beside /var/www/"+tc.file; got != want {
				t.Errorf("renamed = %q, want %q", got, want)
			}
			for name, text := range told {
				if twice := "-name " + tc.file + " -o -name " + tc.file; strings.Contains(text, twice) {
					t.Errorf("%s names the repo's file twice: %q", name, twice)
				}
				for at := 0; ; {
					i := strings.Index(text[at:], tc.never)
					if i < 0 {
						break
					}
					i += at
					if !strings.HasSuffix(text[:i], "workflows/") {
						t.Errorf("%s tells a %s repo %q: …%s…", name, tc.file, tc.never, text[max(0, i-80):min(len(text), i+80)])
						break
					}
					at = i + len(tc.never)
				}
			}
		})
	}
}

// TestRepoConfigFile_ABuildFailureAndTheDiscoverEnvSource_NameTheReposOwnConfigFile:
// in a repo that keeps zerops.yml, the build-failure suggestion of
// zerops_deploy tells the agent to fix zerops.yml, and zerops_discover tags a
// yaml-baked env var as coming from zerops.yml. While zcp has not read which
// name the repo keeps, both say zerops.yaml as before.
func TestRepoConfigFile_ABuildFailureAndTheDiscoverEnvSource_NameTheReposOwnConfigFile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, recorded, want, never string
	}{
		{name: "zerops.yml repo", recorded: "zerops.yml", want: "zerops.yml", never: "zerops.yaml"},
		{name: "name not read yet", recorded: "", want: "zerops.yaml", never: "zerops.yml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
			// The deploy reads the name from the repo it deploys; discover
			// from the latest parts read.
			stateDir := ""
			if tc.recorded != "" {
				stateDir = repoWithConfigFile(t, "app", tc.recorded)
				engine.RecordRepoConfigFile("app", tc.recorded)
			}

			buildSvcID := "build-svc-1"
			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "p", Status: statusActive}).
				WithServices([]platform.ServiceStack{{
					ID: "svc-1", Name: "app", Status: statusActive,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
					ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-0"},
				}}).
				WithAppVersionUserData("av-0", []platform.ServiceEnvVar{{Key: "FOO", Content: "fromyaml", Type: platform.ServiceEnvUser}}).
				WithAppVersionEvents([]platform.AppVersionEvent{{
					ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusBuildFailed, Sequence: 1,
					Build: &platform.BuildInfo{ServiceStackID: &buildSvcID},
				}}).
				WithLogAccess(&platform.LogAccess{URL: "https://log.example.com/logs"})
			logFetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
				{Facility: "local0", Tag: "zbuilder@av-1", Message: "something unusual went wrong"},
			})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, okHTTP, "proj-1", &stubSSH{output: []byte("ok")},
				&auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}, logFetcher, runtime.Info{InContainer: true}, stateDir, engine, nil, DevsupSource{})
			RegisterDiscover(srv, mock, "proj-1", "", engine)

			var deployed ops.DeployResult
			if err := json.Unmarshal([]byte(getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "app"}))), &deployed); err != nil {
				t.Fatalf("parse deploy result: %v", err)
			}
			if deployed.FailureClassification == nil {
				t.Fatalf("a failed build carries a classification: %+v", deployed)
			}
			for field, text := range map[string]string{
				"failureClassification.suggestedAction": deployed.FailureClassification.SuggestedAction,
				"suggestion":                            deployed.Suggestion,
			} {
				if !strings.Contains(text, "buildCommands or dependencies in "+tc.want+".") || strings.Contains(text, tc.never) {
					t.Errorf("%s names the config file other than %s: %q", field, tc.want, text)
				}
			}

			var discovered ops.DiscoverResult
			if err := json.Unmarshal([]byte(getTextContent(t, callTool(t, srv, "zerops_discover", map[string]any{"service": "app", "includeEnvs": true}))), &discovered); err != nil {
				t.Fatalf("parse discover result: %v", err)
			}
			var source any
			for _, e := range discovered.Services[0].Envs {
				if e["key"] == "FOO" {
					source = e["source"]
				}
			}
			if source != tc.want {
				t.Errorf("yaml-baked FOO source = %v, want %s", source, tc.want)
			}
		})
	}
}

// TestRepoConfigFile_ADeployReplyKeepsBuildLogsAndErrorsAsWritten: in a repo
// that keeps zerops.yml, a zerops_deploy reply still quotes a build-log line
// that says zerops.yaml as the build wrote it, and the in-place redeploy's
// error names the raw zerops.yaml URL zcp really fetched. Only the guidance
// zcp writes itself names the repo's file.
func TestRepoConfigFile_ADeployReplyKeepsBuildLogsAndErrorsAsWritten(t *testing.T) {
	t.Parallel()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	t.Run("build log line", func(t *testing.T) {
		t.Parallel()
		engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
		engine.RecordRepoConfigFile("app", "zerops.yml")
		buildSvcID := "build-svc-1"
		mock := platform.NewMock().
			WithProject(&platform.Project{ID: "proj-1", Name: "p", Status: statusActive}).
			WithServices([]platform.ServiceStack{{
				ID: "svc-1", Name: "app", Status: statusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			}}).
			WithAppVersionEvents([]platform.AppVersionEvent{{
				ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusBuildFailed, Sequence: 1,
				Build: &platform.BuildInfo{ServiceStackID: &buildSvcID},
			}}).
			WithLogAccess(&platform.LogAccess{URL: "https://log.example.com/logs"})
		const logLine = "reading zerops.yaml from /build/source failed"
		logFetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
			{Facility: "local0", Tag: "zbuilder@av-1", Message: logLine},
		})
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
		RegisterDeploySSH(srv, mock, okHTTP, "proj-1", &stubSSH{output: []byte("ok")},
			authInfo, logFetcher, runtime.Info{}, "", engine, nil, DevsupSource{})

		var deployed ops.DeployResult
		if err := json.Unmarshal([]byte(getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "app"}))), &deployed); err != nil {
			t.Fatalf("parse deploy result: %v", err)
		}
		if !slices.ContainsFunc(deployed.BuildLogs, func(l string) bool { return strings.Contains(l, logLine) }) {
			t.Errorf("buildLogs = %q, want the build's own line %q", deployed.BuildLogs, logLine)
		}
	})

	t.Run("in-place redeploy raw URL error", func(t *testing.T) {
		t.Parallel()
		engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
		engine.RecordRepoConfigFile("api", "zerops.yml")
		// The branch carries a control byte so the raw fetch fails on its URL
		// before any network call; the error still names that URL.
		mock := platform.NewMock().
			WithServices([]platform.ServiceStack{{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy}}).
			WithServiceAppVersions("s1", []platform.AppVersionEvent{{
				ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2,
				PublicGitSource: &platform.AppVersionGitSource{GitURL: "https://github.com/o/r", BranchName: "main\x7f"},
			}})
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
		RegisterDeploySSH(srv, mock, okHTTP, "proj-1", &countingSSH{}, authInfo, nil, runtime.Info{}, t.TempDir(), engine, nil, DevsupSource{})

		text := getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "api", "appVersion": "latest", "setup": "prod"}))
		if !strings.Contains(text, "https://raw.githubusercontent.com/o/r/main") || !strings.Contains(text, "/zerops.yaml") || strings.Contains(text, "zerops.yml") {
			t.Errorf("redeploy error does not name the raw zerops.yaml URL zcp fetched: %s", text)
		}
	})
}

// TestRepoConfigFile_ADeployNamesTheConfigFileItDeploysFrom: the build-failure
// suggestion of zerops_deploy and zerops_deploy_batch names the config file
// the deploy itself read from the source's repo — zerops.yml here — even when
// another repo of the project keeps zerops.yaml, and even before zcp has read
// any repo's parts.
func TestRepoConfigFile_ADeployNamesTheConfigFileItDeploysFrom(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		other string // config file another repo of the project was read keeping
	}{
		{name: "another repo keeps zerops.yaml", other: "zerops.yaml"},
		{name: "no repo read yet"},
	} {
		for _, tool := range []string{"zerops_deploy", "zerops_deploy_batch"} {
			t.Run(tc.name+"/"+tool, func(t *testing.T) {
				t.Parallel()
				stateDir := repoWithConfigFile(t, "app", "zerops.yml")
				engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
				engine.RecordRepoConfigFile("app", "zerops.yml")
				if tc.other != "" {
					engine.RecordRepoConfigFile("api", tc.other)
				} else {
					engine.RecordRepoConfigFile("app", "")
				}

				buildSvcID := "build-svc-1"
				mock := platform.NewMock().
					WithProject(&platform.Project{ID: "proj-1", Name: "p", Status: statusActive}).
					WithServices([]platform.ServiceStack{{
						ID: "svc-1", Name: "app", Status: statusActive,
						ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
					}}).
					WithAppVersionEvents([]platform.AppVersionEvent{{
						ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusBuildFailed, Sequence: 1,
						Build: &platform.BuildInfo{ServiceStackID: &buildSvcID},
					}}).
					WithLogAccess(&platform.LogAccess{URL: "https://log.example.com/logs"})
				logFetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
					{Facility: "local0", Tag: "zbuilder@av-1", Message: "something unusual went wrong"},
				})
				authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
				rt := runtime.Info{InContainer: true}
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
				var deployed *ops.DeployResult
				if tool == "zerops_deploy" {
					RegisterDeploySSH(srv, mock, okHTTP, "proj-1", &stubSSH{output: []byte("ok")}, authInfo, logFetcher, rt, stateDir, engine, nil, DevsupSource{})
					deployed = &ops.DeployResult{}
					text := getTextContent(t, callTool(t, srv, tool, map[string]any{"targetService": "app"}))
					if err := json.Unmarshal([]byte(text), deployed); err != nil {
						t.Fatalf("parse deploy result: %v: %s", err, text)
					}
				} else {
					RegisterDeployBatch(srv, mock, okHTTP, "proj-1", &stubSSH{output: []byte("ok")}, authInfo, logFetcher, rt, stateDir, engine, nil, DevsupSource{})
					var batch ops.DeployBatchResult
					text := getTextContent(t, callTool(t, srv, tool, map[string]any{"targets": []any{map[string]any{"targetService": "app"}}}))
					if err := json.Unmarshal([]byte(text), &batch); err != nil || len(batch.Entries) != 1 || batch.Entries[0].Result == nil {
						t.Fatalf("parse batch result: %v: %s", err, text)
					}
					deployed = batch.Entries[0].Result
				}
				if deployed.FailureClassification == nil {
					t.Fatalf("a failed build carries a classification: %+v", deployed)
				}
				for field, text := range map[string]string{
					"failureClassification.suggestedAction": deployed.FailureClassification.SuggestedAction,
					"suggestion":                            deployed.Suggestion,
				} {
					if !strings.Contains(text, "buildCommands or dependencies in zerops.yml.") || strings.Contains(text, "zerops.yaml") {
						t.Errorf("%s names the config file other than zerops.yml: %q", field, text)
					}
				}
			})
		}
	}
}

// repoWithConfigFile lays out a container project whose adopted dev service
// host keeps its repo's config, a one-setup node app, under file on its
// mount, and returns the project's state dir.
func repoWithConfigFile(t *testing.T, host, file string) string {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	mount := filepath.Join(dir, host)
	for _, d := range []string{stateDir, mount} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yml := "zerops:\n  - setup: " + host + "\n    build:\n      base: nodejs@22\n      deployFiles: [.]\n    run:\n      base: nodejs@22\n      start: node index.js\n"
	if err := os.WriteFile(filepath.Join(mount, file), []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	setupAdoptedService(t, stateDir, host, "")
	return stateDir
}

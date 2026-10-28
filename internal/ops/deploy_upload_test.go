// Tests for: ops/deploy_upload.go — what a working-tree push uploads, how long
// its upload is waited for, and how an upload that did not finish is told
// (docs/spec-workflows.md §8 DM-9).
package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// writeUploadFile writes size bytes (or content when size is 0) at dir/rel.
func writeUploadFile(t *testing.T, dir, rel string, size int, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(content)
	if size > 0 {
		data = make([]byte, size)
		for i := range data {
			data[i] = byte(i*31 + i/251) // barely compressible
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBuildSSHCommand_MeasuresWhatTheUploadCarries: before zcli starts, the
// push prints what its upload carries — the working directory's tracked files
// less those `.gitattributes` marks export-ignore (git archive leaves them
// out), its untracked files .gitignore does not exclude, and for a self-deploy
// (-g) the .git directory too — with the largest top-level paths.
func TestBuildSSHCommand_MeasuresWhatTheUploadCarries(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	for _, includeGit := range []bool{false, true} {
		t.Run(fmt.Sprintf("includeGit=%v", includeGit), func(t *testing.T) {
			t.Parallel()
			r := newPushRig(t, "echo 'Deploying service'\n")
			writeUploadFile(t, r.dir, "app/frames/a.bin", 2<<20, "")
			writeUploadFile(t, r.dir, "app/main.js", 0, "console.log(1)\n")
			writeUploadFile(t, r.dir, "fixtures/huge.bin", 3<<20, "")
			writeUploadFile(t, r.dir, ".gitattributes", 0, "fixtures export-ignore\n")
			writeUploadFile(t, r.dir, ".gitignore", 0, "dist/\n")
			mustRunGit(t, r.dir, r.env, "add", "-A")
			mustRunGit(t, r.dir, r.env, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "-m", "files")
			writeUploadFile(t, r.dir, "notes/draft.bin", 512<<10, "") // untracked, shipped
			writeUploadFile(t, r.dir, "dist/out.bin", 4<<20, "")      // ignored, never shipped

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			out, code := r.run(ctx, buildSSHCommand("svc-1", r.dir, "", includeGit, "", r.pushDir))
			if code != 0 {
				t.Fatalf("push = exit %d: %s", code, out)
			}
			report, _ := splitUploadReport(out)
			size := report.size
			if size == nil {
				t.Fatalf("push printed no upload report:\n%s", out)
			}
			tracked := int64(2<<20) + int64(len("console.log(1)\n")) + int64(len("fixtures export-ignore\n")) + int64(len("dist/\n"))
			if size.Bytes-size.UntrackedBytes != tracked {
				t.Errorf("tracked bytes = %d, want %d (export-ignored fixtures/ left out)", size.Bytes-size.UntrackedBytes, tracked)
			}
			if size.UntrackedBytes < 512<<10 || size.UntrackedBytes >= 1<<20 {
				t.Errorf("untracked bytes = %d, want about 512 KiB (notes/, never the ignored dist/)", size.UntrackedBytes)
			}
			if size.Files != 4 {
				t.Errorf("files = %d, want the 4 tracked files the archive carries", size.Files)
			}
			if len(size.Largest) == 0 || size.Largest[0].Path != "app/" {
				t.Errorf("largest = %+v, want app/ first", size.Largest)
			}
			for _, p := range size.Largest {
				if p.Path == "fixtures/" || p.Path == "dist/" {
					t.Errorf("largest names %q, which the upload does not carry", p.Path)
				}
			}
			if includeGit != (size.GitBytes > 0) {
				t.Errorf("git bytes = %d with includeGit=%v; a self-deploy carries .git, a cross-deploy does not", size.GitBytes, includeGit)
			}
			if !strings.Contains(size.Summary, "app/") {
				t.Errorf("summary %q should name the largest path", size.Summary)
			}
		})
	}
}

// TestBuildSSHCommand_BudgetGrowsWithTheUploadUpToItsCap: the upload is waited
// for its base plus its time per MiB of what it carries, never past the cap.
func TestBuildSSHCommand_BudgetGrowsWithTheUploadUpToItsCap(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	tests := []struct {
		name   string
		budget uploadBudget
		want   time.Duration
	}{
		{name: "base plus a second per MiB", budget: uploadBudget{base: 10 * time.Second, perMiB: time.Second, cap: time.Hour}, want: 13 * time.Second},
		{name: "never past the cap", budget: uploadBudget{base: 10 * time.Second, perMiB: time.Second, cap: 11 * time.Second}, want: 11 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newPushRig(t, "echo 'Deploying service'\n")
			writeUploadFile(t, r.dir, "assets/a.bin", 3<<20+100, "")
			mustRunGit(t, r.dir, r.env, "add", "-A")
			mustRunGit(t, r.dir, r.env, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "-m", "files")
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			out, code := r.run(ctx, buildSSHCommandWithin("svc-1", r.dir, "", false, "", r.pushDir, tt.budget))
			if code != 0 {
				t.Fatalf("push = exit %d: %s", code, out)
			}
			report, _ := splitUploadReport(out)
			if report.budget != tt.want {
				t.Errorf("budget = %s, want %s:\n%s", report.budget, tt.want, out)
			}
		})
	}
}

// TestBuildSSHCommand_UploadPastItsBudget_StoppedNoBuild: a push still
// uploading when its budget runs out returns with the unfinished-upload mark,
// and the stop that follows ends zcli before it asks for the build.
func TestBuildSSHCommand_UploadPastItsBudget_StoppedNoBuild(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	r := newPushRig(t, "echo 'Creating package'\nwhile [ ! -e upload-go ]; do sleep 0.1; done\necho 'Deploying service'\necho built > built\n")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := time.Now()
	out, code := r.run(ctx, buildSSHCommandWithin("svc-1", r.dir, "", false, "", r.pushDir, uploadBudget{base: time.Second, cap: time.Second}))
	if code != uploadUnfinishedExit {
		t.Fatalf("push = exit %d, want %d (upload unfinished): %s", code, uploadUnfinishedExit, out)
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Errorf("push past a 1 s budget returned after %s", took)
	}
	report, _ := splitUploadReport(out)
	if !report.unfinished || report.budget != time.Second {
		t.Errorf("report = %+v, want unfinished after its 1 s budget:\n%s", report, out)
	}
	if isSSHBuildTriggered(out) {
		t.Errorf("an unfinished upload reads as a triggered build:\n%s", out)
	}
	stopOut, _ := r.run(ctx, pushStopCommand(r.pushDir))
	if isSSHBuildTriggered(stopOut) {
		t.Errorf("the stop says the build started:\n%s", stopOut)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "upload-go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(r.dir, "built")); err == nil {
		t.Error("zcli asked for the build after its push was stopped")
	}
	if _, err := os.Stat(r.pushDir); err == nil {
		t.Error("the stopped push's run directory is still there")
	}
}

// TestSplitUploadReport: the push's report lines become the upload's size,
// budget and the unfinished mark, and leave zcli's own output as it was.
func TestSplitUploadReport(t *testing.T) {
	t.Parallel()
	out := "ZCP_UPLOAD files=14604 bytes=1288490188 untracked=1048576 git=1932735283 budget=3000\n" +
		"ZCP_UPLOAD_TOP 1181116006 app/\n" +
		"ZCP_UPLOAD_TOP 93323264 sessions/\n" +
		"ZCP_UPLOAD_TOP 38797312 my assets/\n" +
		"Using config file: /home/zerops/.config/zerops/.zcli.yml\n" +
		"➤ INFO Creating package\n" +
		"ZCP_UPLOAD_UNFINISHED budget=3000\n"
	report, rest := splitUploadReport(out)
	size := report.size
	if size == nil {
		t.Fatal("no size parsed")
	}
	if size.Files != 14604 || size.Bytes != 1288490188 || size.UntrackedBytes != 1048576 || size.GitBytes != 1932735283 {
		t.Errorf("size = %+v", size)
	}
	if len(size.Largest) != 3 || size.Largest[2].Path != "my assets/" || size.Largest[0].Bytes != 1181116006 {
		t.Errorf("largest = %+v", size.Largest)
	}
	if report.budget != 3000*time.Second || !report.unfinished {
		t.Errorf("budget %s unfinished %v", report.budget, report.unfinished)
	}
	for _, want := range []string{"1.3 GB", "14604 files", "app/ 1.2 GB", ".git 1.9 GB"} {
		if !strings.Contains(size.Summary, want) {
			t.Errorf("summary %q missing %q", size.Summary, want)
		}
	}
	if strings.Contains(rest, "ZCP_UPLOAD") || !strings.Contains(rest, "Creating package") {
		t.Errorf("rest = %q, want zcli's output without the report lines", rest)
	}

	none, rest := splitUploadReport("plain zcli output\n")
	if none.size != nil || none.unfinished || rest != "plain zcli output\n" {
		t.Errorf("output without a report = %+v, rest %q", none, rest)
	}
}

// TestClassifySSHError_UnfinishedUpload_NamesItsSizeAndIsNoNetworkFailure: an
// upload stopped at its budget — or at the push call's own ceiling — is told
// as an upload that did not finish, naming what it carried, and never
// classified as a network failure; the suggestion points at making it smaller
// and never at pushing around zerops_deploy.
func TestClassifySSHError_UnfinishedUpload_NamesItsSizeAndIsNoNetworkFailure(t *testing.T) {
	t.Parallel()
	report := "ZCP_UPLOAD files=14604 bytes=1288490188 untracked=0 git=0 budget=1528\nZCP_UPLOAD_TOP 1181116006 app/\n"
	zcliLog := "Using config file: /home/zerops/.config/zerops/.zcli.yml\n➤ INFO Creating package\n"
	tests := []struct {
		name     string
		err      error
		output   string
		wantWord string
	}{
		{
			name:     "past its budget",
			err:      errors.New("exit status 124"),
			output:   report + zcliLog + "ZCP_UPLOAD_UNFINISHED budget=1528\n",
			wantWord: "25m28s",
		},
		{
			name:     "past the call's ceiling",
			err:      fmt.Errorf("ssh stopped: %w (signal: killed)", context.DeadlineExceeded),
			output:   report + zcliLog,
			wantWord: "timed out",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pe := classifySSHError(&platform.SSHExecError{Hostname: "flipdev", Output: tt.output, Err: tt.err}, "flipdev", "flipstage")
			for _, want := range []string{uploadUnfinishedPhrase, "1.3 GB", "app/ 1.2 GB", "started no build", tt.wantWord} {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q missing %q", pe.Message, want)
				}
			}
			if strings.Contains(pe.Diagnostic, "ZCP_UPLOAD") {
				t.Errorf("diagnostic carries the report lines: %q", pe.Diagnostic)
			}
			cls := ClassifyDeployFailure(FailureInput{Phase: PhaseTransport, Strategy: "zcli", APIErr: pe, TransportErr: pe})
			if cls == nil || cls.Category == topology.FailureClassNetwork {
				t.Fatalf("classification = %+v, want no network failure", cls)
			}
			if cls.Category != topology.FailureClassOther || !slices.Contains(cls.Signals, "transport:upload-unfinished") {
				t.Errorf("classification = %+v, want other via transport:upload-unfinished", cls)
			}
			for _, want := range []string{"smaller", `zerops_knowledge uri="zerops://themes/deploy-upload"`, "zcli login"} {
				if !strings.Contains(cls.SuggestedAction, want) {
					t.Errorf("suggested action %q missing %q", cls.SuggestedAction, want)
				}
			}
		})
	}
}

// TestClassifyDeployFailure_ClosedUploadPipe_IsNoNetworkFailure: zcli's
// "io: read/write on closed pipe" — the upload's connection closed while zcli
// still sent the package — says no build started and what to try, and is not
// sent to check connectivity.
func TestClassifyDeployFailure_ClosedUploadPipe_IsNoNetworkFailure(t *testing.T) {
	t.Parallel()
	out := "ZCP_UPLOAD files=14604 bytes=1288490188 untracked=0 git=0 budget=1528\n" +
		"time=\"2026-10-28T00:50:03Z\" level=info msg=\"➤ INFO Creating package\"\n" +
		"time=\"2026-10-28T00:51:08Z\" level=info msg=\"➤ INFO Uploading package\"\n" +
		"time=\"2026-10-28T00:51:08Z\" level=error msg=\"✗ ERR io: read/write on closed pipe\"\n"
	pe := classifySSHError(&platform.SSHExecError{Hostname: "flipdev", Output: out, Err: errors.New("exit status 1")}, "flipdev", "flipstage")
	if !strings.Contains(pe.Message, "1.3 GB") {
		t.Errorf("message %q should name the upload's size", pe.Message)
	}
	cls := ClassifyDeployFailure(FailureInput{Phase: PhaseTransport, Strategy: "zcli", APIErr: pe, TransportErr: pe})
	if cls == nil || cls.Category == topology.FailureClassNetwork || !slices.Contains(cls.Signals, "transport:zcli-upload-closed") {
		t.Fatalf("classification = %+v, want transport:zcli-upload-closed, not network", cls)
	}
	for _, want := range []string{"no build started", "Deploy again", "smaller"} {
		if !strings.Contains(cls.LikelyCause+" "+cls.SuggestedAction, want) {
			t.Errorf("classification %+v missing %q", cls, want)
		}
	}
}

// uploadSSH answers every call as mockSSHDeployer does, and a push's call with
// push; it records the ceiling a call asked for through ExecSSHStdinWithin.
type uploadSSH struct {
	mockSSHDeployer
	push     string
	ceilings map[string]time.Duration
}

func (m *uploadSSH) ExecSSHStdin(ctx context.Context, hostname, command string, stdin []byte) ([]byte, error) {
	if !strings.Contains(command, "zcli push") {
		return m.mockSSHDeployer.ExecSSHStdin(ctx, hostname, command, stdin)
	}
	m.calls = append(m.calls, sshCall{hostname: hostname, command: command, stdin: string(stdin)})
	return []byte(m.push), nil
}

func (m *uploadSSH) ExecSSHStdinWithin(ctx context.Context, hostname, command string, stdin []byte, ceiling time.Duration) ([]byte, error) {
	if m.ceilings == nil {
		m.ceilings = map[string]time.Duration{}
	}
	if strings.Contains(command, "zcli push") {
		m.ceilings["push"] = ceiling
	}
	return m.ExecSSHStdin(ctx, hostname, command, stdin)
}

// TestDeploySSH_ReportsWhatTheUploadCarried: a triggered deploy's result says
// what its upload carried, and a large upload adds a warning naming what does
// and does not make it smaller.
func TestDeploySSH_ReportsWhatTheUploadCarried(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		report   string
		wantWarn bool
	}{
		{name: "large", report: "ZCP_UPLOAD files=14604 bytes=1288490188 untracked=0 git=0 budget=1528\nZCP_UPLOAD_TOP 1181116006 app/\n", wantWarn: true},
		{name: "small", report: "ZCP_UPLOAD files=40 bytes=200000 untracked=0 git=0 budget=300\nZCP_UPLOAD_TOP 150000 src/\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices([]platform.ServiceStack{
				{ID: "svc-1", Name: "builder"},
				{ID: "svc-2", Name: "app"},
			})
			ssh := &uploadSSH{mockSSHDeployer: mockSSHDeployer{output: []byte("ok")}, push: tt.report + "Deploying service\n"}
			result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, testAuthInfo(), "builder", "app", "", "", "")
			if err != nil {
				t.Fatalf("deploy: %v", err)
			}
			if result.Upload == nil || result.Upload.Summary == "" {
				t.Fatalf("result.Upload = %+v, want what the upload carried", result.Upload)
			}
			var warned string
			for _, w := range result.Warnings {
				if strings.Contains(w, result.Upload.Summary) {
					warned = w
				}
			}
			if (warned != "") != tt.wantWarn {
				t.Fatalf("warnings = %q, want a large-upload warning: %v", result.Warnings, tt.wantWarn)
			}
			if tt.wantWarn {
				for _, want := range []string{".deployignore", "deployFiles", `zerops_knowledge uri="zerops://themes/deploy-upload"`} {
					if !strings.Contains(warned, want) {
						t.Errorf("warning %q missing %q", warned, want)
					}
				}
			}
		})
	}
}

// TestDeploySSH_PushCallTakesTheUploadCeiling: the push's SSH call is bounded
// by the upload's ceiling, not the 5 min every other SSH call has — a working
// tree's push and a commit's alike.
func TestDeploySSH_PushCallTakesTheUploadCeiling(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "builder"},
		{ID: "svc-2", Name: "app"},
	})
	t.Run("working tree", func(t *testing.T) {
		t.Parallel()
		ssh := &uploadSSH{mockSSHDeployer: mockSSHDeployer{output: []byte("ok")}, push: "Deploying service\n"}
		if _, err := DeploySSH(context.Background(), mock, "proj-1", ssh, testAuthInfo(), "builder", "app", "", "", ""); err != nil {
			t.Fatalf("deploy: %v", err)
		}
		if got := ssh.ceilings["push"]; got < uploadBudgetCap {
			t.Errorf("push ceiling = %s, want at least the upload cap %s", got, uploadBudgetCap)
		}
	})
	t.Run("commit", func(t *testing.T) {
		t.Parallel()
		ssh := &uploadSSH{mockSSHDeployer: mockSSHDeployer{results: []sshResult{
			{output: []byte("f0115ba1234567\n")},
			{output: []byte(validCommitZeropsYaml)},
			{output: []byte("/tmp/zcp-extract-1\n")},
			{output: []byte("")},
			{output: []byte("0123456789abcdef\nZCP:BRANCH:main\n")},
			{output: []byte("")},
		}}, push: "Deploying service\n"}
		if _, err := DeploySSH(context.Background(), mock, "proj-1", ssh, testAuthInfo(), "builder", "app", "", "", "abc123"); err != nil {
			t.Fatalf("deploy: %v", err)
		}
		if got := ssh.ceilings["push"]; got < uploadBudgetCap {
			t.Errorf("push ceiling = %s, want at least the upload cap %s", got, uploadBudgetCap)
		}
	})
}

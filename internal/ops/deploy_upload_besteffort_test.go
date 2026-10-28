// Tests for: ops/deploy_upload.go — the upload measurement is strictly best
// effort: it runs in every dev container before every push, so whatever goes
// wrong with it — it fails, hangs or prints garbage — the push goes on within
// the measurement's own bound, waited for by the unmeasured default, and the
// result says so (docs/spec-workflows.md §8 DM-9).
package ops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// rigShells are the shells a measurement test runs the push in: bash, and dash
// — the /bin/sh of Ubuntu and Debian — where this machine has it.
func rigShells() []string {
	shells := []string{"bash"}
	if _, err := exec.LookPath("dash"); err == nil {
		shells = append(shells, "dash")
	}
	return shells
}

// rigTool puts a fake command first on the rig's PATH.
func rigTool(t *testing.T, r *pushRig, name, body string) {
	t.Helper()
	bin, _, _ := strings.Cut(strings.TrimPrefix(r.env[len(r.env)-1], "PATH="), string(os.PathListSeparator))
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// testBudget is a budget whose unmeasured default (7 s) no measurement ever
// computes for the rig's tiny repository, so a test tells the two apart.
func testBudget(bound time.Duration) uploadBudget {
	return uploadBudget{base: 300 * time.Second, perMiB: time.Second, cap: time.Hour, measureBound: bound, unmeasured: 7 * time.Second}
}

// TestBuildSSHCommand_MeasurementFails_PushGoesOnUnmeasured: a measurement
// that fails (its awk exits 1) never fails the push: zcli runs, the upload is
// waited for by the unmeasured default, and the report says why.
func TestBuildSSHCommand_MeasurementFails_PushGoesOnUnmeasured(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	for _, shell := range rigShells() {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			r := newPushRig(t, "echo 'Deploying service'\n")
			r.shell = shell
			rigTool(t, r, "awk", "echo 'awk: cannot open program' >&2\nexit 1\n")
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			out, code := r.run(ctx, buildSSHCommandWithin("svc-1", r.dir, "", false, "", r.pushDir, testBudget(20*time.Second)))
			if code != 0 || !isSSHBuildTriggered(out) {
				t.Fatalf("push = exit %d, want zcli run to its upload: %s", code, out)
			}
			report, _ := splitUploadReport(out)
			if report.unmeasured != "failed" || report.size != nil || report.budget != 7*time.Second {
				t.Errorf("report = %+v, want unmeasured (failed) with the 7 s default:\n%s", report, out)
			}
		})
	}
}

// TestBuildSSHCommand_MeasurementHangs_PushGoesOnWithinItsBound: a
// measurement that hangs (its du never returns) is given up at its bound, and
// the push goes on right then, waited for by the unmeasured default.
func TestBuildSSHCommand_MeasurementHangs_PushGoesOnWithinItsBound(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	for _, shell := range rigShells() {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			r := newPushRig(t, "echo 'Deploying service'\n")
			r.shell = shell
			rigTool(t, r, "du", "exec sleep 8\n")
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			start := time.Now()
			out, code := r.run(ctx, buildSSHCommandWithin("svc-1", r.dir, "", true, "", r.pushDir, testBudget(2*time.Second)))
			took := time.Since(start)
			if code != 0 || !isSSHBuildTriggered(out) {
				t.Fatalf("push = exit %d, want zcli run to its upload: %s", code, out)
			}
			if took > 6*time.Second {
				t.Errorf("push with a hung measurement took %s, want it on its way soon after the 2 s bound", took)
			}
			report, _ := splitUploadReport(out)
			if report.unmeasured != "timeout" || report.budget != 7*time.Second {
				t.Errorf("report = %+v, want unmeasured (timeout) with the 7 s default:\n%s", report, out)
			}
		})
	}
}

// TestBuildSSHCommand_MeasurementPrintsGarbage_PushGoesOnUnmeasured: what a
// broken measurement prints never reaches the push's output as a report — no
// overflowing budget, no stray unfinished mark — and the upload is waited for
// by the unmeasured default.
func TestBuildSSHCommand_MeasurementPrintsGarbage_PushGoesOnUnmeasured(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	tests := []struct {
		name string
		awk  string
	}{
		{name: "an overflowing budget", awk: "echo 'ZCP_UPLOAD files=1 bytes=2 untracked=0 git=0 budget=99999999999999999999'\n"},
		{name: "a malformed report", awk: "echo 'ZCP_UPLOAD files=x bytes=1.5e9 budget=12'\n"},
		{name: "marks and junk", awk: "echo 'garbage line'\necho 'ZCP_UPLOAD_UNFINISHED budget=1'\necho 'ZCP_UPLOAD_TOP 5 a/'\n"},
		{name: "nothing at all", awk: "exit 0\n"},
	}
	for _, tt := range tests {
		for _, shell := range rigShells() {
			t.Run(tt.name+"/"+shell, func(t *testing.T) {
				t.Parallel()
				r := newPushRig(t, "echo 'Deploying service'\n")
				r.shell = shell
				rigTool(t, r, "awk", "cat > /dev/null\n"+tt.awk)
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				out, code := r.run(ctx, buildSSHCommandWithin("svc-1", r.dir, "", false, "", r.pushDir, testBudget(20*time.Second)))
				if code != 0 || !isSSHBuildTriggered(out) {
					t.Fatalf("push = exit %d, want zcli run to its upload: %s", code, out)
				}
				report, rest := splitUploadReport(out)
				if report.unmeasured != "unparsed" || report.size != nil || report.unfinished || report.budget != 7*time.Second {
					t.Errorf("report = %+v, want unmeasured (unparsed) with the 7 s default:\n%s", report, out)
				}
				if strings.Contains(out, "garbage line") || strings.Contains(rest, "ZCP_UPLOAD") {
					t.Errorf("the measurement's garbage reached the push's output:\n%s", out)
				}
			})
		}
	}
}

// TestSplitUploadReport_UnmeasuredAndMalformed: the unmeasured mark carries its
// reason and the budget the push waited; a malformed report line is no report.
func TestSplitUploadReport_UnmeasuredAndMalformed(t *testing.T) {
	t.Parallel()
	report, rest := splitUploadReport("ZCP_UPLOAD_UNMEASURED reason=timeout budget=1800\nDeploying service\n")
	if report.unmeasured != "timeout" || report.budget != 30*time.Minute || report.size != nil || rest != "Deploying service\n" {
		t.Errorf("unmeasured = %+v, rest %q", report, rest)
	}
	for _, line := range []string{
		"ZCP_UPLOAD files=x bytes=1 untracked=0 git=0 budget=300",
		"ZCP_UPLOAD files=1 bytes=1",
		"ZCP_UPLOAD bytes=1e9",
	} {
		if report, _ := splitUploadReport(line + "\n"); report.size != nil {
			t.Errorf("malformed %q read as a report: %+v", line, report.size)
		}
	}
}

// TestDeploySSH_UnmeasuredUpload_SaysSoInTheResult: a deploy whose upload
// could not be measured says so in one line of its result, with the time the
// upload was given.
func TestDeploySSH_UnmeasuredUpload_SaysSoInTheResult(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "builder"},
		{ID: "svc-2", Name: "app"},
	})
	ssh := &uploadSSH{mockSSHDeployer: mockSSHDeployer{output: []byte("ok")}, push: "ZCP_UPLOAD_UNMEASURED reason=timeout budget=1800\nDeploying service\n"}
	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, testAuthInfo(), "builder", "app", "", "", "")
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if result.Upload != nil {
		t.Errorf("upload = %+v, want none measured", result.Upload)
	}
	notes := 0
	for _, w := range result.Warnings {
		if strings.Contains(w, "could not measure") {
			notes++
			for _, want := range []string{"30m0s", "time limit"} {
				if !strings.Contains(w, want) {
					t.Errorf("note %q missing %q", w, want)
				}
			}
		}
	}
	if notes != 1 {
		t.Errorf("warnings = %q, want one note that the upload was not measured", result.Warnings)
	}
}

// TestClassifySSHError_UnfinishedUnmeasuredUpload_SaysItWasNotMeasured: an
// unmeasured upload past its default says it did not finish, that its size
// could not be measured, and the time it was given.
func TestClassifySSHError_UnfinishedUnmeasuredUpload_SaysItWasNotMeasured(t *testing.T) {
	t.Parallel()
	out := "ZCP_UPLOAD_UNMEASURED reason=failed budget=1800\n➤ INFO Creating package\nZCP_UPLOAD_UNFINISHED budget=1800\n"
	pe := classifySSHError(&platform.SSHExecError{Hostname: "flipdev", Output: out, Err: errors.New("exit status 124")}, "flipdev", "flipstage")
	for _, want := range []string{uploadUnfinishedPhrase, "could not be measured", "30m0s", "started no build"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("message %q missing %q", pe.Message, want)
		}
	}
}
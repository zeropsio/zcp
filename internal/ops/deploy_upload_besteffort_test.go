// Tests for: ops/deploy_upload.go — the upload measurement is strictly best
// effort: it runs in every dev container before every push, so whatever goes
// wrong with it — it fails, hangs or prints garbage — the push goes on within
// the measurement's own bound, waited for by the unmeasured default, and the
// result says so (docs/spec-workflows.md §8 DM-9).
package ops

import (
	"context"
	"errors"
	"io"
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

// runUnmeasuredPush holds the measurement watchdog until awk has answered,
// and wakes the upload poll only after zcli has written its deploying line.
func runUnmeasuredPush(t *testing.T, shell, awk string) (string, int) {
	t.Helper()
	r := newPushRig(t, "echo 'Deploying service'\nprintf 'ready\\n' > \"$UPLOAD_READY\"\n")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	ready, gate := filepath.Join(dir, "watchdog-ready"), filepath.Join(dir, "watchdog-gate")
	upload := filepath.Join(dir, "upload-ready")
	if out, err := exec.CommandContext(ctx, "mkfifo", ready, gate, upload).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	heldReady, err := os.OpenFile(ready, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer heldReady.Close()
	heldGate, err := os.OpenFile(gate, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer heldGate.Close()
	heldUpload, err := os.OpenFile(upload, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer heldUpload.Close()
	r.shell = shell
	rigTool(t, r, "awk", "IFS= read -r _ < \"$WATCHDOG_READY\"\n"+awk)
	rigTool(t, r, "date", "echo 1000\n")
	rigTool(t, r, "sleep", "if [ \"$1\" = 1 ]; then IFS= read -r _ < \"$UPLOAD_READY\"; exit 0; fi\n"+
		"exec 4<\"$WATCHDOG_GATE\"\nprintf 'ready\\n' > \"$WATCHDOG_READY\"\ncat <&4 >/dev/null\n")
	r.env = append(r.env, "WATCHDOG_READY="+ready, "WATCHDOG_GATE="+gate, "UPLOAD_READY="+upload)
	readDone, writeDone, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readDone.Close()
	defer writeDone.Close()
	cmd := exec.CommandContext(ctx, shell, "-c", buildSSHCommandWithin("svc-1", r.dir, "", false, "", r.pushDir, testBudget(20*time.Second))) //nolint:gosec // G204: the command under test uses temporary fixture paths
	cmd.Env, cmd.Stdin, cmd.ExtraFiles = r.env, strings.NewReader(r.stdin), []*os.File{writeDone}
	out, runErr := cmd.CombinedOutput()
	if err := writeDone.Close(); err != nil {
		t.Fatal(err)
	}
	if err := heldGate.Close(); err != nil {
		t.Fatal(err)
	}
	// Release either held reader even when the command under test stopped early.
	_, _ = heldReady.WriteString("stop\n")
	_, _ = heldUpload.WriteString("stop\n")
	joined := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, readDone); joined <- err }()
	select {
	case err := <-joined:
		if err != nil {
			t.Fatalf("push descendants: %v", err)
		}
	case <-ctx.Done():
		_ = readDone.Close()
		<-joined
		t.Fatalf("push descendants did not exit: %v", ctx.Err())
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return string(out), 0
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
			out, code := runUnmeasuredPush(t, shell, "echo 'awk: cannot open program' >&2\nexit 1\n")
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
// measurement that hangs (its ls never returns) is given up at its bound, and
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
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			dir := t.TempDir()
			measurement, watchdog := filepath.Join(dir, "measurement"), filepath.Join(dir, "watchdog")
			measureGate, watchGate, measureDone := filepath.Join(dir, "measure-gate"), filepath.Join(dir, "watch-gate"), filepath.Join(dir, "measure-done")
			if out, err := exec.CommandContext(ctx, "mkfifo", measurement, watchdog, measureGate, watchGate, measureDone).CombinedOutput(); err != nil {
				t.Fatalf("mkfifo: %v: %s", err, out)
			}
			heldMeasure, err := os.OpenFile(measureGate, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer heldMeasure.Close()
			heldWatch, err := os.OpenFile(watchGate, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer heldWatch.Close()
			realSleep, err := exec.LookPath("sleep")
			if err != nil {
				t.Fatal(err)
			}
			rigTool(t, r, "ls", "exec 3>\"$MEASURE_DONE\"\nexec 4<\"$MEASURE_GATE\"\necho reached > \"$MEASUREMENT\"\ncat <&4 >/dev/null\n")
			rigTool(t, r, "sleep", "if [ \"$1\" = 1 ]; then exec "+ShellQuote(realSleep)+" \"$@\"; fi\nexec 4<\"$WATCH_GATE\"\necho \"$1\" > \"$WATCHDOG\"\ncat <&4 >/dev/null\n")
			rigTool(t, r, "date", "echo 1000\n")
			r.env = append(r.env, "MEASUREMENT="+measurement, "WATCHDOG="+watchdog, "MEASURE_GATE="+measureGate, "WATCH_GATE="+watchGate, "MEASURE_DONE="+measureDone)
			done := make(chan error, 1)
			go func() { _, err := os.ReadFile(measureDone); done <- err }()
			type result struct {
				out  string
				code int
			}
			run := make(chan result, 1)
			go func() {
				out, code := r.run(ctx, buildSSHCommandWithin("svc-1", r.dir, "", true, "", r.pushDir, testBudget(2*time.Second)))
				run <- result{out, code}
			}()
			if _, err := os.ReadFile(measurement); err != nil {
				t.Fatal(err)
			}
			bound, err := os.ReadFile(watchdog)
			if err != nil {
				t.Fatal(err)
			}
			if err := heldWatch.Close(); err != nil {
				t.Fatal(err)
			}
			got := <-run
			if err := heldMeasure.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatalf("measurement completion: %v", err)
			}
			out, code := got.out, got.code
			if code != 0 || !isSSHBuildTriggered(out) {
				t.Fatalf("push = exit %d, want zcli run to its upload: %s", code, out)
			}
			if string(bound) != "2\n" {
				t.Errorf("measurement watchdog = %q, want 2 seconds", bound)
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
				out, code := runUnmeasuredPush(t, shell, "cat > /dev/null\n"+tt.awk)
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

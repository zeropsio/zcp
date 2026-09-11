package farm

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This file drives the real eval/farm/console/deploy.sh (POSIX sh) against
// stubbed curl, zcli and go binaries placed first on PATH — never the real
// platform, never a real build (docs/spec-eval-farm.md §8.1 FM-49, brief
// plans/zcp-farm-observer-2026-09-11-briefs/S6-console-deploy.md). It never
// asserts on the script's internals — only on stdout/stderr, the exit
// code, the operator env file it writes, and the stub tools' own call
// logs (argv/env they were invoked with).

// requireShAndPython3 skips the whole offline rig when either real binary
// deploy.sh shells out to (unstubbed) is unavailable.
func requireShAndPython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
}

// consoleDeployTestDir locates this test file's own directory, independent
// of the test binary's working directory.
func consoleDeployTestDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0): not ok")
	}
	return filepath.Dir(thisFile)
}

// consoleDeployRepoRoot resolves the repo root three levels up from
// internal/eval/farm, the same computation wrapperScriptPath uses.
func consoleDeployRepoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(consoleDeployTestDir(t), "..", "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	return abs
}

// consoleDeployScriptPath locates eval/farm/console/deploy.sh.
func consoleDeployScriptPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(consoleDeployRepoRoot(t), "eval", "farm", "console", "deploy.sh")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("eval/farm/console/deploy.sh: %v", err)
	}
	return path
}

// consoleDeployTestdataDir locates internal/eval/farm/testdata/consoledeploy.
func consoleDeployTestdataDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(consoleDeployTestDir(t), "testdata", "consoledeploy")
}

// consoleDeployHarness wires a fresh, isolated environment for one deploy.sh
// invocation: an offline stub bin dir (curl/zcli/go) first on PATH, a
// private HOME (so the default env-file path never touches the real
// ~/.zerops-dev), and a STUB_DIR the stub tools read fixtures from and
// write their call logs to.
type consoleDeployHarness struct {
	t            *testing.T
	home         string
	stubBin      string
	stubDir      string
	oauthToken   string
	accountToken string
	envFile      string // ZCP_FARM_CONSOLE_ENV_FILE override; empty = let deploy.sh default from HOME
}

// newConsoleDeployHarness builds the harness and copies the three stub
// tools (curl.sh/zcli.sh/go.sh) into a bin dir as curl/zcli/go.
func newConsoleDeployHarness(t *testing.T) *consoleDeployHarness {
	t.Helper()
	requireShAndPython3(t)

	stubBin := t.TempDir()
	testdata := consoleDeployTestdataDir(t)
	for _, name := range []string{"curl", "zcli", "go"} {
		src := filepath.Join(testdata, name+".sh")
		body, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read stub %s: %v", src, err)
		}
		dst := filepath.Join(stubBin, name)
		if err := os.WriteFile(dst, body, 0o700); err != nil {
			t.Fatalf("write stub %s: %v", dst, err)
		}
	}

	return &consoleDeployHarness{
		t:            t,
		home:         t.TempDir(),
		stubBin:      stubBin,
		stubDir:      t.TempDir(),
		oauthToken:   "OAUTH-TOKEN-SECRET-4f9c8e21",
		accountToken: "ACCOUNT-TOKEN-SECRET-7b21ad63",
	}
}

func (h *consoleDeployHarness) copyFixture(name, destName string) {
	h.t.Helper()
	src := filepath.Join(consoleDeployTestdataDir(h.t), name)
	body, err := os.ReadFile(src)
	if err != nil {
		h.t.Fatalf("read fixture %s: %v", src, err)
	}
	dst := filepath.Join(h.stubDir, destName)
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		h.t.Fatalf("write fixture %s: %v", dst, err)
	}
}

// consoleDeployResult carries one deploy.sh run's outcome.
type consoleDeployResult struct {
	stdout   string
	stderr   string
	err      error
	exitCode int
}

// run executes deploy.sh with a complete, valid base environment, letting
// overrides replace individual vars and omit remove one entirely (so a
// "missing required input" case can be expressed without a magic value).
func (h *consoleDeployHarness) run(overrides map[string]string, omit ...string) consoleDeployResult {
	h.t.Helper()

	env := map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": h.oauthToken,
		"ZCP_FARM_ACCOUNT_TOKEN":  h.accountToken,
		"ZCP_FARM_PROJECT_ID":     "swY2yczpQlqVLlcz0fCyFA",
		"ZCP_API_HOST":            "fake.zerops.test",
		"REPO_ROOT":               consoleDeployRepoRoot(h.t),
		"HOME":                    h.home,
		"STUB_DIR":                h.stubDir,
		"PATH":                    h.stubBin + ":" + os.Getenv("PATH"),
	}
	if h.envFile != "" {
		env["ZCP_FARM_CONSOLE_ENV_FILE"] = h.envFile
	}
	maps.Copy(env, overrides)
	omitSet := map[string]bool{}
	for _, k := range omit {
		omitSet[k] = true
	}

	var envList []string
	for k, v := range env {
		if omitSet[k] {
			continue
		}
		envList = append(envList, k+"="+v)
	}

	// A 30s safety-net timeout, not a test-logic dependency: every named
	// test in this file completes in well under that; this only guards
	// against a genuinely wedged deploy.sh holding the test binary open
	// (same discipline as wrapperHarness.start in wrapper_test.go).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	//nolint:gosec // G204: deliberate — this IS the test, launching the real deploy.sh under test at a path this test file itself resolves (consoleDeployScriptPath), never external input.
	cmd := exec.CommandContext(ctx, consoleDeployScriptPath(h.t))
	cmd.Env = envList

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := consoleDeployResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.exitCode = exitErr.ExitCode()
	}
	return res
}

// defaultEnvFilePath is where deploy.sh writes the operator copy when
// ZCP_FARM_CONSOLE_ENV_FILE is left at its default (§2.4).
func (h *consoleDeployHarness) defaultEnvFilePath() string {
	return filepath.Join(h.home, ".zerops-dev", "agent-creds", "farm-console.env")
}

func (h *consoleDeployHarness) callsLog() []string {
	h.t.Helper()
	body, err := os.ReadFile(filepath.Join(h.stubDir, "calls.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		h.t.Fatalf("read calls.log: %v", err)
	}
	var lines []string
	for l := range strings.SplitSeq(strings.TrimRight(string(body), "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func oneLine(t *testing.T, s string) string {
	t.Helper()
	trimmed := strings.TrimRight(s, "\n")
	if strings.Contains(trimmed, "\n") {
		t.Fatalf("expected exactly one line, got %q", s)
	}
	return trimmed
}

// TestConsoleDeploy_RefusesMissingInputsOrAPIKey — FM-49: a required input
// missing, or ANTHROPIC_API_KEY present, refuses to start: exit 1, one
// line on stderr naming the offending variable.
func TestConsoleDeploy_RefusesMissingInputsOrAPIKey(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]string
		omit      []string
		wantMatch string
	}{
		{
			name:      "missing CLAUDE_CODE_OAUTH_TOKEN",
			omit:      []string{"CLAUDE_CODE_OAUTH_TOKEN"},
			wantMatch: "CLAUDE_CODE_OAUTH_TOKEN",
		},
		{
			name:      "missing ZCP_FARM_ACCOUNT_TOKEN",
			omit:      []string{"ZCP_FARM_ACCOUNT_TOKEN"},
			wantMatch: "ZCP_FARM_ACCOUNT_TOKEN",
		},
		{
			name:      "ANTHROPIC_API_KEY set",
			overrides: map[string]string{"ANTHROPIC_API_KEY": "sk-ant-should-not-be-here"},
			wantMatch: "ANTHROPIC_API_KEY",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newConsoleDeployHarness(t)
			h.copyFixture("list-empty.json", "list-response.json")
			h.copyFixture("import-response.json", "import-response.json")
			h.copyFixture("single-ready.json", "single-response.json")

			res := h.run(tc.overrides, tc.omit...)

			if res.err == nil {
				t.Fatalf("expected a non-nil error (nonzero exit), got nil; stdout=%q stderr=%q", res.stdout, res.stderr)
			}
			if res.exitCode != 1 {
				t.Fatalf("exit code: got %d want 1 (stderr=%q)", res.exitCode, res.stderr)
			}
			line := oneLine(t, res.stderr)
			if !strings.Contains(line, tc.wantMatch) {
				t.Fatalf("stderr line %q does not name %q", line, tc.wantMatch)
			}
			if res.stdout != "" {
				t.Fatalf("expected no stdout on refusal, got %q", res.stdout)
			}
		})
	}
}

// TestConsoleDeploy_FirstRunImportsWritesEnvFile0600AndPushes — FM-49: the
// service is missing, so deploy.sh mints a console token, writes it to the
// (default-path) operator env file at mode 0600, imports the service, and
// still reaches the push step.
func TestConsoleDeploy_FirstRunImportsWritesEnvFile0600AndPushes(t *testing.T) {
	h := newConsoleDeployHarness(t)
	h.copyFixture("list-empty.json", "list-response.json")
	h.copyFixture("import-response.json", "import-response.json")
	h.copyFixture("single-ready.json", "single-response.json")

	res := h.run(nil)
	if res.err != nil {
		t.Fatalf("deploy.sh failed: %v\nstdout=%s\nstderr=%s", res.err, res.stdout, res.stderr)
	}

	envPath := h.defaultEnvFilePath()
	fi, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("stat env file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("env file mode: got %o want 0600", perm)
	}

	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	token := envValue(t, string(body), "ZCP_FARM_CONSOLE_TOKEN")
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(token) {
		t.Fatalf("ZCP_FARM_CONSOLE_TOKEN=%q is not 64 lowercase hex chars (32 bytes)", token)
	}

	if _, err := os.Stat(filepath.Join(h.stubDir, "zcli-ran.marker")); err != nil {
		t.Fatalf("zcli never ran: %v", err)
	}
}

// envValue extracts KEY=value from env-file text, last match wins (mirrors
// deploy.sh's own read_env_var/write_env_var, but reimplemented
// independently here rather than shelling out to the same helper).
func envValue(t *testing.T, envFileBody, key string) string {
	t.Helper()
	var value string
	found := false
	for line := range strings.SplitSeq(envFileBody, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			value = v
			found = true
		}
	}
	if !found {
		t.Fatalf("env file has no %s= line; body=%q", key, envFileBody)
	}
	return value
}

// TestConsoleDeploy_ImportYAMLCarriesRefsAndSecrets — FM-49: the import
// body's yaml carries both secrets (the OAuth token and the freshly
// generated console token) plus the four ${os_*} references as literal
// text, and never the account token.
func TestConsoleDeploy_ImportYAMLCarriesRefsAndSecrets(t *testing.T) {
	h := newConsoleDeployHarness(t)
	h.copyFixture("list-empty.json", "list-response.json")
	h.copyFixture("import-response.json", "import-response.json")
	h.copyFixture("single-ready.json", "single-response.json")

	res := h.run(nil)
	if res.err != nil {
		t.Fatalf("deploy.sh failed: %v\nstdout=%s\nstderr=%s", res.err, res.stdout, res.stderr)
	}

	captured, err := os.ReadFile(filepath.Join(h.stubDir, "import-body-capture.json"))
	if err != nil {
		t.Fatalf("read import-body-capture.json: %v", err)
	}
	var body struct {
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("unmarshal captured import body: %v\nraw=%s", err, captured)
	}

	for _, ref := range []string{
		"${os_apiUrl}",
		"${os_bucketName}",
		"${os_accessKeyId}",
		"${os_secretAccessKey}",
	} {
		if !strings.Contains(body.YAML, ref) {
			t.Errorf("import yaml missing literal reference %q:\n%s", ref, body.YAML)
		}
	}

	if !strings.Contains(body.YAML, h.oauthToken) {
		t.Errorf("import yaml missing the CLAUDE_CODE_OAUTH_TOKEN secret value")
	}

	envBody, err := os.ReadFile(h.defaultEnvFilePath())
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	consoleToken := envValue(t, string(envBody), "ZCP_FARM_CONSOLE_TOKEN")
	if !strings.Contains(body.YAML, consoleToken) {
		t.Errorf("import yaml missing the generated ZCP_FARM_CONSOLE_TOKEN secret value")
	}

	if strings.Contains(body.YAML, h.accountToken) {
		t.Errorf("import yaml must never carry the account token")
	}
	if strings.Contains(string(captured), h.accountToken) {
		t.Errorf("import request body must never carry the account token")
	}
}

// TestConsoleDeploy_SecondRunSkipsImportKeepsToken — FM-49: the service
// already exists, so deploy.sh must not import again and must not
// regenerate/overwrite the recorded console token.
func TestConsoleDeploy_SecondRunSkipsImportKeepsToken(t *testing.T) {
	h := newConsoleDeployHarness(t)
	h.copyFixture("list-with-console.json", "list-response.json")
	h.copyFixture("import-response.json", "import-response.json")
	h.copyFixture("single-ready.json", "single-response.json")

	envDir := filepath.Join(h.home, ".zerops-dev", "agent-creds")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatalf("mkdir env dir: %v", err)
	}
	const existingToken = "existing-console-token-deadbeef00112233445566778899aabbccddeeff0011223344"
	envPath := filepath.Join(envDir, "farm-console.env")
	if err := os.WriteFile(envPath, []byte("ZCP_FARM_CONSOLE_TOKEN="+existingToken+"\n"), 0o600); err != nil {
		t.Fatalf("write existing env file: %v", err)
	}

	res := h.run(nil)
	if res.err != nil {
		t.Fatalf("deploy.sh failed: %v\nstdout=%s\nstderr=%s", res.err, res.stdout, res.stderr)
	}

	if _, err := os.Stat(filepath.Join(h.stubDir, "import-body-capture.json")); !os.IsNotExist(err) {
		t.Fatalf("import must not have been called; import-body-capture.json exists (err=%v)", err)
	}
	for _, line := range h.callsLog() {
		if strings.Contains(line, "/service-stack/import") {
			t.Fatalf("calls log shows an import call on a second run: %v", h.callsLog())
		}
	}

	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if got := envValue(t, string(body), "ZCP_FARM_CONSOLE_TOKEN"); got != existingToken {
		t.Fatalf("ZCP_FARM_CONSOLE_TOKEN changed: got %q want %q", got, existingToken)
	}

	if _, err := os.Stat(filepath.Join(h.stubDir, "zcli-ran.marker")); err != nil {
		t.Fatalf("zcli never ran on the existing-service path: %v", err)
	}
}

// TestConsoleDeploy_EnablesSubdomainAfterPushAndRecordsURL — FM-49: the
// subdomain is enabled only after the push, and the URL read back
// afterward is recorded in the env file and printed.
func TestConsoleDeploy_EnablesSubdomainAfterPushAndRecordsURL(t *testing.T) {
	h := newConsoleDeployHarness(t)
	h.copyFixture("list-empty.json", "list-response.json")
	h.copyFixture("import-response.json", "import-response.json")
	h.copyFixture("single-ready.json", "single-response.json")

	res := h.run(nil)
	if res.err != nil {
		t.Fatalf("deploy.sh failed: %v\nstdout=%s\nstderr=%s", res.err, res.stdout, res.stderr)
	}

	const wantURL = "https://console-4d2f-8080.prg1.zerops.app"

	if strings.TrimSpace(res.stdout) != "console: "+wantURL {
		t.Fatalf("stdout: got %q want %q", res.stdout, "console: "+wantURL+"\n")
	}

	envBody, err := os.ReadFile(h.defaultEnvFilePath())
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if got := envValue(t, string(envBody), "ZCP_FARM_CONSOLE_URL"); got != wantURL {
		t.Fatalf("ZCP_FARM_CONSOLE_URL: got %q want %q", got, wantURL)
	}
	// The token line must survive the later URL write.
	if got := envValue(t, string(envBody), "ZCP_FARM_CONSOLE_TOKEN"); got == "" {
		t.Fatalf("ZCP_FARM_CONSOLE_TOKEN line was lost when the URL was written")
	}

	calls := h.callsLog()
	idx := func(pred func(string) bool) int {
		for i, l := range calls {
			if pred(l) {
				return i
			}
		}
		t.Fatalf("no matching call in log: %v", calls)
		return -1
	}
	importIdx := idx(func(l string) bool { return strings.Contains(l, "/service-stack/import") })
	pushIdx := idx(func(l string) bool { return strings.HasPrefix(l, "ZCLI ") })
	enableIdx := idx(func(l string) bool { return strings.Contains(l, "/enable-subdomain-access") })

	// The final GET is the LAST call in the log (the poll's GET, if any,
	// happens before the push).
	lastIdx := len(calls) - 1

	if importIdx >= pushIdx || pushIdx >= enableIdx || enableIdx >= lastIdx {
		t.Fatalf("wrong call order (import=%d push=%d enable=%d last=%d): %v", importIdx, pushIdx, enableIdx, lastIdx, calls)
	}
	if !strings.Contains(calls[lastIdx], "GET") || !strings.Contains(calls[lastIdx], "/service-stack/") {
		t.Fatalf("last call is not the final GET service-stack/<sid>: %v", calls)
	}
}

// TestConsoleDeploy_ZcliGetsPrivateDataFile — FM-49: zcli's login-data file
// lives in its own temp dir, outside both $HOME and the pushed --workingDir
// (the account token must never be uploaded).
func TestConsoleDeploy_ZcliGetsPrivateDataFile(t *testing.T) {
	h := newConsoleDeployHarness(t)
	h.copyFixture("list-empty.json", "list-response.json")
	h.copyFixture("import-response.json", "import-response.json")
	h.copyFixture("single-ready.json", "single-response.json")

	res := h.run(nil)
	if res.err != nil {
		t.Fatalf("deploy.sh failed: %v\nstdout=%s\nstderr=%s", res.err, res.stdout, res.stderr)
	}

	envLog, err := os.ReadFile(filepath.Join(h.stubDir, "zcli-env.log"))
	if err != nil {
		t.Fatalf("read zcli-env.log: %v", err)
	}
	dataFilePath := envValue(t, string(envLog), "ZEROPS_CLI_DATA_FILE_PATH")
	if dataFilePath == "" {
		t.Fatal("ZEROPS_CLI_DATA_FILE_PATH was not set for zcli")
	}
	if strings.HasPrefix(dataFilePath, h.home) {
		t.Fatalf("ZEROPS_CLI_DATA_FILE_PATH=%q is under HOME=%q", dataFilePath, h.home)
	}

	argvLog, err := os.ReadFile(filepath.Join(h.stubDir, "zcli-argv.log"))
	if err != nil {
		t.Fatalf("read zcli-argv.log: %v", err)
	}
	workingDir := argAfterFlag(t, string(argvLog), "--workingDir")
	if workingDir == "" {
		t.Fatal("zcli was never given --workingDir")
	}
	if strings.HasPrefix(dataFilePath, workingDir) {
		t.Fatalf("ZEROPS_CLI_DATA_FILE_PATH=%q is under --workingDir=%q", dataFilePath, workingDir)
	}

	if got := envValue(t, string(envLog), "ZEROPS_TOKEN"); got != h.accountToken {
		t.Fatalf("ZEROPS_TOKEN: got %q want the account token", got)
	}
}

// argAfterFlag returns the token immediately following flag in a
// whitespace-joined argv log line (zcli-argv.log is one "$*"-joined line
// per invocation; the harness makes exactly one call).
func argAfterFlag(t *testing.T, argvLine, flag string) string {
	t.Helper()
	fields := strings.Fields(argvLine)
	for i, f := range fields {
		if f == flag && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// TestConsoleDeploy_ExistingServiceWithoutLocalTokenExits1 — FM-49: the
// service exists on the platform but the operator's local env file has no
// token line — deploy.sh must refuse rather than write a tokenless copy or
// silently mint a second, unrecorded token.
func TestConsoleDeploy_ExistingServiceWithoutLocalTokenExits1(t *testing.T) {
	h := newConsoleDeployHarness(t)
	h.copyFixture("list-with-console.json", "list-response.json")
	h.copyFixture("import-response.json", "import-response.json")
	h.copyFixture("single-ready.json", "single-response.json")
	// Deliberately no env file at all (also covers "file exists but has no
	// ZCP_FARM_CONSOLE_TOKEN line": read_env_var returns empty either way).

	res := h.run(nil)

	if res.err == nil {
		t.Fatalf("expected a non-nil error (nonzero exit); stdout=%q stderr=%q", res.stdout, res.stderr)
	}
	if res.exitCode != 1 {
		t.Fatalf("exit code: got %d want 1 (stderr=%q)", res.exitCode, res.stderr)
	}
	line := oneLine(t, res.stderr)
	if !strings.Contains(line, h.defaultEnvFilePath()) {
		t.Fatalf("stderr line %q does not name the env file %q", line, h.defaultEnvFilePath())
	}

	if _, err := os.Stat(filepath.Join(h.stubDir, "import-body-capture.json")); !os.IsNotExist(err) {
		t.Fatalf("import must not have been called")
	}
	if _, err := os.Stat(filepath.Join(h.stubDir, "zcli-ran.marker")); !os.IsNotExist(err) {
		t.Fatalf("zcli must not have run")
	}
	if _, err := os.Stat(h.defaultEnvFilePath()); !os.IsNotExist(err) {
		t.Fatalf("must never write a tokenless env file")
	}
}

// TestConsoleDeploy_NoSecretInOutputOrLeftovers — FM-49: none of the three
// secrets (account token, OAuth token, freshly generated console token)
// ever appear on stdout/stderr, and the STAGE directory deploy.sh builds
// into is gone by the time the script exits (so nothing — secret or not —
// is left behind in it).
func TestConsoleDeploy_NoSecretInOutputOrLeftovers(t *testing.T) {
	h := newConsoleDeployHarness(t)
	h.copyFixture("list-empty.json", "list-response.json")
	h.copyFixture("import-response.json", "import-response.json")
	h.copyFixture("single-ready.json", "single-response.json")

	res := h.run(nil)
	if res.err != nil {
		t.Fatalf("deploy.sh failed: %v\nstdout=%s\nstderr=%s", res.err, res.stdout, res.stderr)
	}

	envBody, err := os.ReadFile(h.defaultEnvFilePath())
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	consoleToken := envValue(t, string(envBody), "ZCP_FARM_CONSOLE_TOKEN")

	for _, secret := range []string{h.accountToken, h.oauthToken, consoleToken} {
		if strings.Contains(res.stdout, secret) {
			t.Errorf("stdout contains a secret value")
		}
		if strings.Contains(res.stderr, secret) {
			t.Errorf("stderr contains a secret value")
		}
	}

	goArgvLog, err := os.ReadFile(filepath.Join(h.stubDir, "go-argv.log"))
	if err != nil {
		t.Fatalf("read go-argv.log: %v", err)
	}
	stagePath := argAfterFlag(t, string(goArgvLog), "-o")
	if stagePath == "" {
		t.Fatal("go stub was never given -o")
	}
	stageDir := filepath.Dir(stagePath)
	if _, err := os.Stat(stageDir); !os.IsNotExist(err) {
		t.Fatalf("stage dir %q still exists after deploy.sh exited (err=%v)", stageDir, err)
	}
}

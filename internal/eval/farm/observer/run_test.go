package observer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFakeClaude writes an executable "claude" script at dir/claude and
// returns its path. The script dumps its own env (one KEY=VALUE per line)
// to envFile, its cwd to cwdFile, and stdin to stdinFile, then prints a
// canned `--output-format json` result line.
func writeFakeClaude(t *testing.T, dir, envFile, cwdFile, stdinFile, resultJSON string) string {
	t.Helper()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"env > " + envFile + "\n" +
		"pwd > " + cwdFile + "\n" +
		"cat > " + stdinFile + "\n" +
		"printf '%s' " + shellQuote(resultJSON) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// shellQuote wraps s in single quotes for embedding literally into a
// generated /bin/sh script (test fixtures only; s here is always a JSON
// literal with no single quotes).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const canonicalResultJSON = `{"result":"observer answer text","total_cost_usd":0.0123,"is_error":false}`

func staticEnviron(kvs ...string) func() []string {
	return func() []string { return kvs }
}

// testPATH prepends dir (the fake-claude bin) to the real PATH, so a
// fixture script's own internal commands (env, cat, sleep, …) still
// resolve — mirroring §7.4's "PATH (from the parent)".
func testPATH(dir string) string {
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// TestRunObserver_EnvIsExactlyPathHomeToken pins §7.4: the child's
// environment is exactly PATH, HOME (a fresh private temp dir, removed
// afterwards), and CLAUDE_CODE_OAUTH_TOKEN — no ANTHROPIC_API_KEY, no
// ZCP_* value, and its cwd is a fresh empty temp dir.
func TestRunObserver_EnvIsExactlyPathHomeToken(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.txt")
	cwdFile := filepath.Join(dir, "cwd.txt")
	stdinFile := filepath.Join(dir, "stdin.txt")
	claudePath := writeFakeClaude(t, dir, envFile, cwdFile, stdinFile, canonicalResultJSON)

	var observedHome string
	cfg := RunConfig{
		ClaudePath: claudePath,
		Model:      "claude-sonnet-5",
		OAuthToken: "test-oauth-token",
		Timeout:    10 * time.Second,
		Environ:    staticEnviron("PATH="+testPATH(dir), "HOME=/should/not/be/used", "ZCP_FARM_S3_KEY=leak-me-not"),
	}
	_, err := RunObserver(context.Background(), cfg, "prompt text", "digest text")
	if err != nil {
		t.Fatalf("RunObserver: %v", err)
	}

	envBytes, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	var keys []string
	for line := range strings.SplitSeq(strings.TrimRight(string(envBytes), "\n"), "\n") {
		if line == "" {
			continue
		}
		key := line
		if k, v, ok := strings.Cut(line, "="); ok {
			key = k
			if key == "HOME" {
				observedHome = v
			}
		}
		keys = append(keys, key)
	}

	// The 3 keys RunObserver itself passes, plus a handful the /bin/sh
	// fixture process auto-exports on its own (PWD, SHLVL, OLDPWD, "_") —
	// present in the fake-claude fixture's own execution, not in what our
	// code hands the child. The assertion that matters is the negative
	// one below: no ANTHROPIC_API_KEY, no ZCP_* value.
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "CLAUDE_CODE_OAUTH_TOKEN": true,
		"PWD": true, "OLDPWD": true, "SHLVL": true, "_": true,
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
		if !allowed[k] {
			t.Errorf("child env carries unexpected key %q (full env: %v)", k, keys)
		}
	}
	for _, want := range []string{"PATH", "HOME", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if !seen[want] {
			t.Errorf("child env missing %q (full env: %v)", want, keys)
		}
	}

	cwdBytes, err := os.ReadFile(cwdFile)
	if err != nil {
		t.Fatalf("read cwd file: %v", err)
	}
	cwd := strings.TrimSpace(string(cwdBytes))
	if cwd == dir || cwd == "" {
		t.Errorf("child cwd = %q, want a fresh temp dir distinct from the test dir", cwd)
	}

	if observedHome == "" || observedHome == "/should/not/be/used" {
		t.Fatalf("child HOME = %q, want a fresh private temp dir", observedHome)
	}
	if _, err := os.Stat(observedHome); !os.IsNotExist(err) {
		t.Errorf("observer HOME %q still exists after RunObserver returned, want removed", observedHome)
	}
}

// TestRunObserver_RefusesWithoutTokenOrWithAPIKey pins §7.4's refusal
// rules: an empty OAuth token, or ANTHROPIC_API_KEY present in the
// observer's own environment, both refuse before any child starts.
func TestRunObserver_RefusesWithoutTokenOrWithAPIKey(t *testing.T) {
	dir := t.TempDir()
	claudePath := writeFakeClaude(t, dir, filepath.Join(dir, "env.txt"), filepath.Join(dir, "cwd.txt"), filepath.Join(dir, "stdin.txt"), canonicalResultJSON)

	cases := []struct {
		name string
		cfg  RunConfig
	}{
		{
			name: "empty token",
			cfg: RunConfig{
				ClaudePath: claudePath, Model: "claude-sonnet-5",
				OAuthToken: "", Timeout: time.Second, Environ: staticEnviron("PATH=" + testPATH(dir)),
			},
		},
		{
			name: "ANTHROPIC_API_KEY set",
			cfg: RunConfig{
				ClaudePath: claudePath, Model: "claude-sonnet-5",
				OAuthToken: "tok", Timeout: time.Second, Environ: staticEnviron("PATH="+testPATH(dir), "ANTHROPIC_API_KEY=shadow-key"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RunObserver(context.Background(), tc.cfg, "prompt", "digest")
			if err == nil {
				t.Fatalf("RunObserver: got no error, want refusal")
			}
		})
	}
}

// TestRunObserver_TimeoutYieldsErrorStatus pins §7.4's 5-minute (here,
// test-shortened) timeout: a claude call that outlives cfg.Timeout returns
// an error, never hangs.
func TestRunObserver_TimeoutYieldsErrorStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	// Never exits on its own within the test's timeout.
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := RunConfig{
		ClaudePath: path, Model: "claude-sonnet-5",
		OAuthToken: "tok", Timeout: 200 * time.Millisecond, Environ: staticEnviron("PATH=" + testPATH(dir)),
	}
	start := time.Now()
	_, err := RunObserver(context.Background(), cfg, "prompt", "digest")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("RunObserver: got no error, want a timeout error")
	}
	if elapsed > 10*time.Second {
		t.Errorf("RunObserver took %s, want it to return promptly after the 200ms timeout", elapsed)
	}
}

// TestRunObserver_RelativeClaudePathResolved pins §7.4: a relative --claude
// path is resolved to an absolute path (exec.LookPath, then filepath.Abs)
// before the child starts, because the child's cwd is a fresh empty temp
// dir where a relative path wouldn't resolve to the same binary.
func TestRunObserver_RelativeClaudePathResolved(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.txt")
	cwdFile := filepath.Join(dir, "cwd.txt")
	stdinFile := filepath.Join(dir, "stdin.txt")
	writeFakeClaude(t, dir, envFile, cwdFile, stdinFile, canonicalResultJSON)

	fullPath := testPATH(dir) // captured before narrowing PATH below
	t.Setenv("PATH", dir)     // LookPath only needs to find "claude" in dir
	cfg := RunConfig{
		ClaudePath: "claude", // relative — must be resolved via PATH
		Model:      "claude-sonnet-5",
		OAuthToken: "tok",
		Timeout:    10 * time.Second,
		Environ:    staticEnviron("PATH=" + fullPath),
	}
	result, err := RunObserver(context.Background(), cfg, "prompt", "digest")
	if err != nil {
		t.Fatalf("RunObserver: %v", err)
	}
	if result.ResultText != "observer answer text" {
		t.Errorf("ResultText = %q, want the canned result text (the relative claude actually ran)", result.ResultText)
	}
}

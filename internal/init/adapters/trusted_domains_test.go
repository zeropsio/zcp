package adapters

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeTrustedDomains_EmptyInput_AddsAllWantedDomains(t *testing.T) {
	t.Parallel()
	out, changed, err := mergeTrustedDomains(nil, []string{"https://zerops.io", "https://github.com"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatal("want changed=true on empty input")
	}
	got := stringList(mustUnmarshalYAML(t, out)[trustedDomainsKey])
	want := []string{"https://zerops.io", "https://github.com"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeTrustedDomains_PreservesExistingUserDomains(t *testing.T) {
	t.Parallel()
	raw := []byte(trustedDomainsKey + ":\n  - https://my-own-domain.example\n")
	out, changed, err := mergeTrustedDomains(raw, []string{"https://zerops.io"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatal("want changed=true when a new domain is added")
	}
	got := stringList(mustUnmarshalYAML(t, out)[trustedDomainsKey])
	want := []string{"https://my-own-domain.example", "https://zerops.io"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v (existing entries must be preserved, new appended)", got, want)
	}
}

func TestMergeTrustedDomains_SkipsDomainsAlreadyPresent(t *testing.T) {
	t.Parallel()
	raw := []byte(trustedDomainsKey + ":\n  - https://zerops.io\n  - https://github.com\n")
	out, changed, err := mergeTrustedDomains(raw, []string{"https://zerops.io", "https://github.com"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if changed {
		t.Errorf("want changed=false when all wanted domains already present, got output: %s", out)
	}
	if string(out) != string(raw) {
		t.Errorf("unchanged merge must return the original bytes verbatim:\ngot:  %s\nwant: %s", out, raw)
	}
}

func TestMergeTrustedDomains_IdempotentOnRerun(t *testing.T) {
	t.Parallel()
	first, changed1, err := mergeTrustedDomains(nil, DefaultTrustedDomains)
	if err != nil {
		t.Fatalf("first merge: %v", err)
	}
	if !changed1 {
		t.Fatal("want changed=true on first merge into empty config")
	}
	second, changed2, err := mergeTrustedDomains(first, DefaultTrustedDomains)
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if changed2 {
		t.Errorf("second merge over already-merged output must be a no-op, got: %s", second)
	}
	if string(first) != string(second) {
		t.Errorf("re-merge must round-trip byte-for-byte:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestMergeTrustedDomains_PreservesOtherConfigKeys(t *testing.T) {
	t.Parallel()
	raw := []byte("bind-addr: 127.0.0.1:8080\nauth: password\npassword: abc123\ncert: false\n")
	out, changed, err := mergeTrustedDomains(raw, []string{"https://zerops.io"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatal("want changed=true")
	}
	data := mustUnmarshalYAML(t, out)
	for key, want := range map[string]string{
		"bind-addr": "127.0.0.1:8080",
		"auth":      "password",
		"password":  "abc123",
	} {
		if got, _ := data[key].(string); got != want {
			t.Errorf("key %q: got %q, want %q (must survive the merge untouched)", key, got, want)
		}
	}
	if got, _ := data["cert"].(bool); got != false {
		t.Errorf("key %q: got %v, want false", "cert", data["cert"])
	}
}

func TestMergeTrustedDomains_MalformedYAML_ReturnsError(t *testing.T) {
	t.Parallel()
	raw := []byte("this: is: not: valid: yaml: [[[")
	_, _, err := mergeTrustedDomains(raw, []string{"https://zerops.io"})
	if err == nil {
		t.Fatal("want error on malformed YAML, got nil (must never silently clobber a broken config)")
	}
}

func TestMergeTrustedDomains_ScalarExistingValue_Wrapped(t *testing.T) {
	t.Parallel()
	// A hand-edited config.yaml might carry a bare scalar instead of a
	// list — code-server itself expects string[] here, but the merge
	// must not crash or drop the existing value.
	raw := []byte(trustedDomainsKey + ": https://my-own-domain.example\n")
	out, changed, err := mergeTrustedDomains(raw, []string{"https://zerops.io"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatal("want changed=true")
	}
	got := stringList(mustUnmarshalYAML(t, out)[trustedDomainsKey])
	want := []string{"https://my-own-domain.example", "https://zerops.io"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEnsureTrustedDomains_SkipsWhenCodeServerDirAbsent(t *testing.T) {
	t.Parallel()
	home := t.TempDir() // no .config/code-server under here — plain laptop

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("want nil error outside a code-server environment, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Errorf("must not create .config when code-server isn't present (err=%v)", err)
	}
}

func TestEnsureTrustedDomains_CreatesConfigWhenDirExistsButFileMissing(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "code-server"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("EnsureTrustedDomains: %v", err)
	}

	path := filepath.Join(home, ".config", "code-server", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	data := mustUnmarshalYAML(t, raw)
	got := stringList(data[trustedDomainsKey])
	// Pinned to explicit literals (not DefaultTrustedDomains) so an
	// accidental edit to the registry itself can't slip through unnoticed.
	want := []string{
		"https://zerops.io",
		"https://*.zerops.io",
		"https://www.youtube.com",
		"https://github.com",
	}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config.yaml: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("new config.yaml mode = %o, want 0600 (this file carries the login password)", perm)
	}
}

func TestEnsureTrustedDomains_MergesIntoExistingConfig_PreservesUserKeys(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "code-server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	original := "bind-addr: 127.0.0.1:8080\nauth: password\npassword: abc123\ncert: false\n" +
		trustedDomainsKey + ":\n  - https://my-own-domain.example\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("EnsureTrustedDomains: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	data := mustUnmarshalYAML(t, raw)
	if got, _ := data["password"].(string); got != "abc123" {
		t.Errorf("password: got %q, want %q (user config must survive)", got, "abc123")
	}
	got := stringList(data[trustedDomainsKey])
	if len(got) == 0 || got[0] != "https://my-own-domain.example" {
		t.Errorf("want the user's own domain preserved first, got %v", got)
	}
	for _, want := range DefaultTrustedDomains {
		if !slices.Contains(got, want) {
			t.Errorf("want %q merged in, got %v", want, got)
		}
	}
}

// TestEnsureTrustedDomains_PreservesExisting0600Mode pins the HIGH-severity
// fix: config.yaml carries code-server's login password (auth: password /
// password: ...), so a merge must never widen an existing 0600 file to
// something more permissive.
func TestEnsureTrustedDomains_PreservesExisting0600Mode(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "code-server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("auth: password\npassword: abc123\n"), 0o600); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("EnsureTrustedDomains: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 0600 preserved (never widen a secrets file's permissions)", perm)
	}
}

// TestEnsureTrustedDomains_Preserves0644Mode proves the mode-preservation
// logic is symmetric: an existing file's mode is kept AS-IS in either
// direction, not forced to a fixed value.
func TestEnsureTrustedDomains_Preserves0644Mode(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "code-server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("auth: none\n"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("EnsureTrustedDomains: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %o, want 0644 preserved", perm)
	}
}

// TestEnsureTrustedDomains_SymlinkedConfig_WritesThroughToTarget proves the
// write path resolves a symlinked config.yaml and writes the merge into the
// TARGET file (preserving the target's mode), rather than replacing the
// symlink itself with a plain file — a symlinked config.yaml is typically a
// deliberate operator choice (e.g. secrets mounted from elsewhere).
func TestEnsureTrustedDomains_SymlinkedConfig_WritesThroughToTarget(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "code-server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "actual-config.yaml")
	if err := os.WriteFile(targetPath, []byte("auth: password\npassword: abc123\n"), 0o600); err != nil {
		t.Fatalf("setup target write: %v", err)
	}

	linkPath := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("EnsureTrustedDomains: %v", err)
	}

	// The symlink itself must survive — never replaced by a plain file.
	linkInfo, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("config.yaml is no longer a symlink — the write replaced the link instead of writing through it")
	}
	// Canonicalize both sides through EvalSymlinks before comparing — on
	// macOS, t.TempDir() itself lives under a symlinked prefix
	// (/tmp -> /private/tmp, /var -> /private/var), so a raw string
	// comparison against targetPath would spuriously fail here even
	// though the write landed on the correct file.
	wantTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("resolve want target: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil || resolved != wantTarget {
		t.Fatalf("symlink target changed: resolved=%q err=%v, want %q", resolved, err, wantTarget)
	}

	// The TARGET must carry the merge, with its own mode preserved.
	raw, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	data := mustUnmarshalYAML(t, raw)
	if got, _ := data["password"].(string); got != "abc123" {
		t.Errorf("password: got %q, want %q", got, "abc123")
	}
	got := stringList(data[trustedDomainsKey])
	if !equalStrings(got, DefaultTrustedDomains) {
		t.Errorf("got %v, want %v", got, DefaultTrustedDomains)
	}
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if perm := targetInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("target mode = %o, want 0600 preserved", perm)
	}
}

// TestEnsureTrustedDomains_BrokenSymlink_SkipsWithoutError documents the
// "any doubt, skip" posture for a config.yaml this sensitive: a symlink
// that can't be resolved (broken link) must not be replaced by a plain
// file, and must not fail `zcp init` — just a stderr warning and a no-op.
func TestEnsureTrustedDomains_BrokenSymlink_SkipsWithoutError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "code-server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	linkPath := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist.yaml"), linkPath); err != nil {
		t.Fatalf("setup broken symlink: %v", err)
	}

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("want nil error on a broken symlink (skip, don't fail init), got: %v", err)
	}

	linkInfo, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("broken symlink was replaced instead of skipped")
	}
}

func TestEnsureTrustedDomains_IdempotentSecondRunIsNoOp(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "code-server"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("first run: %v", err)
	}
	path := filepath.Join(home, ".config", "code-server", "config.yaml")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first run: %v", err)
	}

	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second run: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("second run must be a byte-for-byte no-op:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestEnsureTrustedDomains_RespectsCodeServerConfigEnvOverride(t *testing.T) {
	home := t.TempDir()
	altDir := t.TempDir()
	altPath := filepath.Join(altDir, "custom-config.yaml")
	t.Setenv("CODE_SERVER_CONFIG", altPath)
	if err := os.MkdirAll(filepath.Join(home, ".config", "code-server"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// The override path's directory doesn't exist yet — codeServerConfigPath
	// honors $CODE_SERVER_CONFIG exactly as code-server itself does, so
	// EnsureTrustedDomains must write there, not to ~/.config/code-server.
	if err := EnsureTrustedDomains(home); err != nil {
		t.Fatalf("EnsureTrustedDomains: %v", err)
	}
	if _, err := os.Stat(altPath); err != nil {
		t.Errorf("want config written at $CODE_SERVER_CONFIG override %s, got err=%v", altPath, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "code-server", "config.yaml")); !os.IsNotExist(err) {
		t.Errorf("must not write the default path when $CODE_SERVER_CONFIG is set (err=%v)", err)
	}
}

func TestCodeServerConfigPath_DefaultsUnderDotConfig(t *testing.T) {
	t.Setenv("CODE_SERVER_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home := "/home/zerops"
	got := codeServerConfigPath(home)
	want := filepath.Join(home, ".config", "code-server", "config.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCodeServerConfigPath_HonorsXDGConfigHome(t *testing.T) {
	t.Setenv("CODE_SERVER_CONFIG", "")
	xdg := "/custom/xdg"
	t.Setenv("XDG_CONFIG_HOME", xdg)
	got := codeServerConfigPath("/home/zerops")
	want := filepath.Join(xdg, "code-server", "config.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCodeServerConfigPath_HonorsCodeServerConfigEnv(t *testing.T) {
	override := "/some/where/config.yaml"
	t.Setenv("CODE_SERVER_CONFIG", override)
	got := codeServerConfigPath("/home/zerops")
	if got != override {
		t.Errorf("got %q, want %q", got, override)
	}
}

// mustUnmarshalYAML parses YAML bytes into a generic map for assertions,
// failing the test on error rather than returning it.
func mustUnmarshalYAML(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal yaml: %v\n%s", err, raw)
	}
	return data
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

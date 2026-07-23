package adapters

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr for the duration of fn and returns
// what was written. Not safe for t.Parallel() (os.Stderr is global).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = orig
	data, _ := io.ReadAll(r)
	return string(data)
}

// TestInstallBootstrap_VersionedDirNoOp locks the P0 contract that
// re-running installBootstrapExtension against an already-current
// install does not rewrite any file. Without this, every `zcp init`
// re-run (container restart, zcli replay) would overwrite live files
// under a possibly-running extension host, risking a mixed old/new
// module state mid-activation.
func TestInstallBootstrap_VersionedDirNoOp(t *testing.T) {
	home := t.TempDir()

	if err := installBootstrapExtension(home); err != nil {
		t.Fatalf("first install: %v", err)
	}

	extPath := filepath.Join(home, ".local", "share", "code-server", "extensions", bootstrapExtDirName(), "extension.js")
	original, err := os.ReadFile(extPath)
	if err != nil {
		t.Fatalf("read extension.js after first install: %v", err)
	}
	tampered := append(append([]byte{}, original...), []byte("\n// tampered")...)
	if err := os.WriteFile(extPath, tampered, 0o644); err != nil {
		t.Fatalf("tamper extension.js: %v", err)
	}

	out := captureStderr(t, func() {
		if err := installBootstrapExtension(home); err != nil {
			t.Fatalf("second install: %v", err)
		}
	})

	after, err := os.ReadFile(extPath)
	if err != nil {
		t.Fatalf("read extension.js after second install: %v", err)
	}
	if string(after) != string(tampered) {
		t.Errorf("same-version re-install rewrote extension.js (expected no-op):\n  tampered: %s\n  after:    %s", tampered, after)
	}
	if out != "" {
		t.Errorf("no-op install should print nothing to stderr, got: %q", out)
	}
}

// TestInstallBootstrap_UpgradeKeepsOldDir locks that installing a new
// bootstrap version never touches an older versioned (or legacy
// unversioned) install directory — an already-running extension host
// may still be serving files out of it. It also locks that an upgrade
// (unlike the no-op path) prints a reload notice naming the new version.
func TestInstallBootstrap_UpgradeKeepsOldDir(t *testing.T) {
	home := t.TempDir()

	// Seed a legacy (pre-P0, unversioned) install + matching index entry
	// at an old version, exactly as a container upgrading from before
	// this change would have on disk.
	extensionsDir := filepath.Join(home, ".local", "share", "code-server", "extensions")
	legacyDir := filepath.Join(extensionsDir, "zcp-bootstrap")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	legacyFiles := map[string]string{
		"package.json": `{"name":"zcp-bootstrap","version":"0.1.1"}`,
		"extension.js": "// legacy 0.1.1 extension.js",
		"logo.svg":     `<svg viewBox="0 0 1 1"></svg>`,
	}
	for name, content := range legacyFiles {
		if err := os.WriteFile(filepath.Join(legacyDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("seed legacy %s: %v", name, err)
		}
	}
	indexPath := filepath.Join(extensionsDir, "extensions.json")
	legacyIndex := `[{"identifier":{"id":"zerops.zcp-bootstrap"},"version":"0.1.1","relativeLocation":"zcp-bootstrap","metadata":{"installedTimestamp":1700000000000,"pinned":true,"source":"vsix"}}]`
	if err := os.WriteFile(indexPath, []byte(legacyIndex), 0o644); err != nil {
		t.Fatalf("seed legacy index: %v", err)
	}

	out := captureStderr(t, func() {
		if err := installBootstrapExtension(home); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	if !strings.Contains(out, "zcp-bootstrap "+BootstrapExtVersion+" installed") {
		t.Errorf("expected reload notice naming %s, got: %q", BootstrapExtVersion, out)
	}

	// Legacy dir + its files are untouched, byte-identical.
	for name, want := range legacyFiles {
		got, err := os.ReadFile(filepath.Join(legacyDir, name))
		if err != nil {
			t.Fatalf("read legacy %s after upgrade: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("legacy %s mutated by upgrade:\n  want: %s\n  got:  %s", name, want, got)
		}
	}

	// New versioned dir exists with the current install.
	newDir := filepath.Join(extensionsDir, bootstrapExtDirName())
	if _, err := os.Stat(filepath.Join(newDir, "extension.js")); err != nil {
		t.Errorf("expected new versioned dir to exist: %v", err)
	}

	// Index carries exactly one zerops.zcp-bootstrap entry, pointing at
	// the new version/dir, and the pre-existing installedTimestamp
	// survived the upgrade.
	entries, err := readExtensionsIndex(indexPath)
	if err != nil {
		t.Fatalf("read index after upgrade: %v", err)
	}
	var bootstrapEntries []map[string]any
	for _, e := range entries {
		if extensionEntryID(e) == bootstrapExtID {
			bootstrapEntries = append(bootstrapEntries, e)
		}
	}
	if len(bootstrapEntries) != 1 {
		t.Fatalf("expected exactly one bootstrap entry after upgrade, got %d; entries=%v", len(bootstrapEntries), entries)
	}
	entry := bootstrapEntries[0]
	if v, _ := entry["version"].(string); v != BootstrapExtVersion {
		t.Errorf("bootstrap entry version = %q, want %q", v, BootstrapExtVersion)
	}
	if rl, _ := entry["relativeLocation"].(string); rl != bootstrapExtDirName() {
		t.Errorf("bootstrap entry relativeLocation = %q, want %q", rl, bootstrapExtDirName())
	}
	if ts := extensionEntryTimestamp(entry); ts != 1700000000000 {
		t.Errorf("installedTimestamp churned across upgrade: got %d, want 1700000000000", ts)
	}
}

func TestInstallBootstrap_WritesStartupPolicyFromZeropsSubdomain(t *testing.T) {
	tests := []struct {
		name            string
		zeropsSubdomain string
		bridgeOrigins   string
		wantAutoOpen    bool
	}{
		{
			name:            "tatami service URL enables welcome",
			zeropsSubdomain: "https://zcp-24cb-8080.prg1.zerops.app",
			wantAutoOpen:    true,
		},
		{
			name:            "app origin preserves launcher",
			zeropsSubdomain: "https://app.zerops.io",
			wantAutoOpen:    false,
		},
		{
			name:            "app.zerops.io embedding GUI preserves launcher",
			zeropsSubdomain: "https://zcp-24cb-8080.prg1.zerops.app",
			bridgeOrigins:   "https://app.zerops.io",
			wantAutoOpen:    false,
		},
		{
			name:            "custom embed still enables welcome",
			zeropsSubdomain: "https://zcp-24cb-8080.prg1.zerops.app",
			bridgeOrigins:   "https://febridge-24cb.prg1.zerops.app",
			wantAutoOpen:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("zeropsSubdomain", tt.zeropsSubdomain)
			t.Setenv("ZCP_WELCOME_BRIDGE_ORIGINS", tt.bridgeOrigins)
			home := t.TempDir()

			if err := installBootstrapExtension(home); err != nil {
				t.Fatalf("install: %v", err)
			}

			path := filepath.Join(
				home,
				".local",
				"share",
				"code-server",
				"extensions",
				bootstrapExtDirName(),
				"startup.json",
			)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read startup policy: %v", err)
			}
			var config struct {
				AutoOpenWelcome bool `json:"autoOpenWelcome"`
			}
			if err := json.Unmarshal(raw, &config); err != nil {
				t.Fatalf("parse startup policy: %v", err)
			}
			if config.AutoOpenWelcome != tt.wantAutoOpen {
				t.Errorf("startup.json autoOpenWelcome = %v, want %v", config.AutoOpenWelcome, tt.wantAutoOpen)
			}
		})
	}
}

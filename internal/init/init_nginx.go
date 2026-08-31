package init

import (
	"fmt"
	"os"
	"text/template"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/z3"
)

// NginxConfig holds values for nginx.conf template rendering.
//
// Password is the raw VSCODE_PASSWORD value, used verbatim as both the
// auth-cookie value and the path component of `/zcp-auth/<token>`.
// Generated passwords are alphanumeric so they're URL-safe and
// cookie-safe as-is — no hashing needed (an earlier design ran sha256
// over the env value to coerce special characters into hex).
//
// The z3 fields carry internal/z3's constants into the template so the public
// path prefix, the loopback port and the readiness marker have one definition
// each — moving any of them stays a single edit.
//
// Z3Enabled gates every z3-shaped location the template can render — the
// {{.Z3BasePath}}/ proxy, the closed door on {{.Z3Port}}, and the
// {{.Z3BasePath}}/healthz readiness route — behind runtime.Info.Z3Enabled
// (ZCP_Z3_ENABLED). False renders none of them: the loopback port stays
// reachable only through code-server's own /proxy/<port>/ door, and no
// route in this config answers /healthz.
type NginxConfig struct {
	HasAuth  bool
	Password string

	Z3Enabled      bool
	Z3BasePath     string
	Z3Port         int
	InitMarkerPath string
}

// zeropsUID/zeropsGID are the Zerops container service user nginx runs as.
// `zcp init nginx` runs ONLY in the zcp@1 service type, always via `sudo -E`
// (root) in run.initCommands — so chowning the nginx dirs and logs to this
// fixed uid/gid always succeeds and needs no /etc/passwd lookup. (A
// user.Lookup("zerops") miss during the init phase was the fragility behind
// the reverted 2026-04 chown attempt; a fixed numeric target removes it.)
// Worker-owned dirs are what let us use 0755 instead of world-writable 0777:
// nginx writes because it owns the path, not because everyone can.
const (
	zeropsUID = 2023
	zeropsGID = 2023
)

var (
	defaultNginxOutputPath = "/etc/nginx/nginx.conf"
	defaultNginxDirs       = []string{"/var/log/nginx", "/var/lib/nginx/tmp", "/var/lib/nginx/body", "/var/lib/nginx/proxy", "/var/lib/nginx/fastcgi", "/var/lib/nginx/uwsgi", "/var/lib/nginx/scgi", "/var/cache/nginx"}
	defaultNginxLogFiles   = []string{"/var/log/nginx/error.log", "/var/log/nginx/access.log"}

	nginxOutputPath = defaultNginxOutputPath
	nginxDirs       = append([]string{}, defaultNginxDirs...)
	nginxLogFiles   = append([]string{}, defaultNginxLogFiles...)

	// nginxOwnerUID/nginxOwnerGID are the chown target for nginx dirs + logs.
	// Overridable so tests (which run as a non-root, non-zerops user) chown to
	// themselves — chowning to self always succeeds, whereas chowning to 2023
	// would EPERM off the Zerops container.
	nginxOwnerUID = zeropsUID
	nginxOwnerGID = zeropsGID
)

// RunNginx generates /etc/nginx/nginx.conf and creates required directories.
// Authentication is enabled when VSCODE_PASSWORD env var is set.
func RunNginx() error {
	fmt.Fprintln(os.Stderr, "  → Nginx directories")
	if err := createNginxDirs(); err != nil {
		return fmt.Errorf("nginx dirs: %w", err)
	}

	fmt.Fprintln(os.Stderr, "  → Nginx config")
	password := os.Getenv("VSCODE_PASSWORD")
	z3Enabled := runtime.Detect().Z3Enabled
	if err := renderNginxConfig(nginxOutputPath, password, z3Enabled); err != nil {
		return fmt.Errorf("nginx config: %w", err)
	}

	if password != "" {
		fmt.Fprintln(os.Stderr, "  ✓ Nginx init complete (auth enabled)")
	} else {
		fmt.Fprintln(os.Stderr, "  ✓ Nginx init complete (no auth)")
	}
	return nil
}

// createNginxDirs creates the directories nginx needs and gives them the
// permissions nginx expects in the Zerops container: each dir is owned by the
// service user nginx runs as (zerops) at 0755, and pre-existing log files
// (apt installs them root:adm 0640) are chowned to that user at 0644.
// Ownership — not world-writable 0777 — is what lets the non-root worker
// write. `zcp init nginx` runs as root (sudo -E), so the chowns always apply.
func createNginxDirs() error {
	for _, d := range nginxDirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
		// Chmod explicitly so a pre-existing dir (apt-created /var/lib/nginx/*
		// or the persistent /var/log/nginx) lands at exactly 0755 regardless
		// of umask or its prior mode.
		if err := os.Chmod(d, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", d, err)
		}
		if err := os.Chown(d, nginxOwnerUID, nginxOwnerGID); err != nil {
			return fmt.Errorf("chown %s: %w", d, err)
		}
	}

	// Pre-existing log files (apt installs nginx with root:adm 0640) must be
	// owned by the service user so the worker can append; 0644 lets others
	// read (debugging, log shipper) but only the owner write.
	for _, f := range nginxLogFiles {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		if err := os.Chown(f, nginxOwnerUID, nginxOwnerGID); err != nil {
			return fmt.Errorf("chown %s: %w", f, err)
		}
		if err := os.Chmod(f, 0o644); err != nil {
			return fmt.Errorf("chmod %s: %w", f, err)
		}
	}
	return nil
}

// renderNginxConfig renders the nginx.conf template to outputPath. If
// password is non-empty, auth is enabled and the raw password is baked
// into the rendered config as both the cookie value and the
// `/zcp-auth/<token>` path component. z3Enabled gates the z3-shaped
// locations — see NginxConfig.Z3Enabled.
func renderNginxConfig(outputPath, password string, z3Enabled bool) error {
	cfg := NginxConfig{
		Z3Enabled:      z3Enabled,
		Z3BasePath:     z3.BasePath,
		Z3Port:         z3.ServePort,
		InitMarkerPath: z3.InitMarkerPath,
	}
	if password != "" {
		cfg.HasAuth = true
		cfg.Password = password
	}

	raw, err := content.GetTemplate("nginx.conf.tmpl")
	if err != nil {
		return fmt.Errorf("load nginx template: %w", err)
	}

	tmpl, err := template.New("nginx").Parse(raw)
	if err != nil {
		return fmt.Errorf("parse nginx template: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outputPath, err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, cfg); err != nil {
		return fmt.Errorf("render nginx template: %w", err)
	}
	return nil
}

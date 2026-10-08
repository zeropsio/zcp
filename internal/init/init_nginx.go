package init

import (
	"fmt"
	"os"
	"text/template"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// NginxConfig holds values for nginx.conf template rendering.
//
// Password is the raw VSCODE_PASSWORD value, used verbatim as both the
// auth-cookie value and the path component of `/zcp-auth/<token>`.
// Generated passwords are alphanumeric so they're URL-safe and
// cookie-safe as-is — no hashing needed (an earlier design ran sha256
// over the env value to coerce special characters into hex).
//
// The mate fields carry internal/mate's constants into the template so the public
// path prefix, the loopback port and the readiness marker have one definition
// each — moving any of them stays a single edit.
//
// MateEnabled gates every mate-shaped location the template can render — the
// {{.MateBasePath}}/ proxy, the closed door on {{.MatePort}}, and the
// {{.MateBasePath}}/healthz readiness route — behind runtime.Info.MateEnabled
// (ZCP_MATE_ENABLED). False renders none of them: the loopback port stays
// reachable only through code-server's own /proxy/<port>/ door, and no
// route in this config answers /healthz.
type NginxConfig struct {
	HasAuth  bool
	Password string

	MateEnabled    bool
	MateBasePath   string
	MatePort       int
	InitMarkerPath string
}

// zeropsUID/zeropsGID are the Zerops container service user nginx runs as.
// `zcp init nginx` runs ONLY in the zcp@1 service type, always via `sudo -E`
// (root) in run.initCommands — so chowning the nginx cache and temporary dirs to this
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
	defaultNginxDirs       = []string{"/var/lib/nginx/tmp", "/var/lib/nginx/body", "/var/lib/nginx/proxy", "/var/lib/nginx/fastcgi", "/var/lib/nginx/uwsgi", "/var/lib/nginx/scgi", "/var/cache/nginx"}

	nginxOutputPath = defaultNginxOutputPath
	nginxDirs       = append([]string{}, defaultNginxDirs...)

	// nginxOwnerUID/nginxOwnerGID are the chown target for nginx cache and temporary dirs.
	// Overridable so tests (which run as a non-root, non-zerops user) chown to
	// themselves — chowning to self always succeeds, whereas chowning to 2023
	// would EPERM off the Zerops container.
	nginxOwnerUID = zeropsUID
	nginxOwnerGID = zeropsGID
)

// RunNginx generates /etc/nginx/nginx.conf and creates required directories.
// Authentication is enabled when VSCODE_PASSWORD env var is set.
func RunNginx() error {
	return runNginx("/etc/logrotate.d/nginx")
}

// runNginx takes the distribution policy path explicitly so tests cannot retire
// a host policy and need no mutable global path override.
func runNginx(logrotatePath string) error {
	fmt.Fprintln(os.Stderr, "  → Nginx directories")
	if err := createNginxDirs(); err != nil {
		return fmt.Errorf("nginx dirs: %w", err)
	}

	fmt.Fprintln(os.Stderr, "  → Nginx config")
	password := os.Getenv("VSCODE_PASSWORD")
	mateEnabled := runtime.Detect().MateEnabled
	if err := renderNginxConfig(nginxOutputPath, password, mateEnabled); err != nil {
		return fmt.Errorf("nginx config: %w", err)
	}

	// File logging has been replaced by the system journal. Retire only the
	// distribution's obsolete policy, after the new config is in place; leave
	// historical logs and unrelated rotation policies untouched.
	if err := os.Remove(logrotatePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("nginx log rotation: remove %s: %w", logrotatePath, err)
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
// service user nginx runs as (zerops) at 0755. Logs go to the system journal,
// so historical log directories and files are neither needed nor touched.
// Ownership — not world-writable 0777 — is what lets the non-root worker
// write. `zcp init nginx` runs as root (sudo -E), so the chowns always apply.
func createNginxDirs() error {
	for _, d := range nginxDirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
		// Chmod explicitly so a pre-existing dir (apt-created /var/lib/nginx/*)
		// lands at exactly 0755 regardless
		// of umask or its prior mode.
		if err := os.Chmod(d, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", d, err)
		}
		if err := os.Chown(d, nginxOwnerUID, nginxOwnerGID); err != nil {
			return fmt.Errorf("chown %s: %w", d, err)
		}
	}

	return nil
}

// renderNginxConfig renders the nginx.conf template to outputPath. If
// password is non-empty, auth is enabled and the raw password is baked
// into the rendered config as both the cookie value and the
// `/zcp-auth/<token>` path component. mateEnabled gates the mate-shaped
// locations — see NginxConfig.MateEnabled.
func renderNginxConfig(outputPath, password string, mateEnabled bool) error {
	cfg := NginxConfig{
		MateEnabled:    mateEnabled,
		MateBasePath:   mate.BasePath,
		MatePort:       mate.ServePort,
		InitMarkerPath: mate.InitMarkerPath,
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

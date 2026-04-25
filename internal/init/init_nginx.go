package init

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"text/template"

	"github.com/zeropsio/zcp/internal/content"
)

// NginxConfig holds values for nginx.conf template rendering.
type NginxConfig struct {
	HasAuth      bool
	PasswordHash string
}

// containerServiceUser is the canonical Zerops container user nginx must run
// as — `zcp serve` runs as this user, and `zcp service start nginx` inherits
// that uid (no privilege drop in service.Start). Hardcoding this name pins
// the platform invariant; making it configurable would let a misconfigured
// env var quietly chown logs to the wrong user and break the start path.
const containerServiceUser = "zerops"

var (
	defaultNginxOutputPath = "/etc/nginx/nginx.conf"
	defaultNginxDirs       = []string{"/var/log/nginx", "/var/lib/nginx/tmp", "/var/lib/nginx/body", "/var/lib/nginx/proxy", "/var/lib/nginx/fastcgi", "/var/lib/nginx/uwsgi", "/var/lib/nginx/scgi"}
	defaultNginxLogFiles   = []string{"/var/log/nginx/error.log", "/var/log/nginx/access.log"}

	nginxOutputPath = defaultNginxOutputPath
	nginxDirs       = append([]string{}, defaultNginxDirs...)
	nginxLogFiles   = append([]string{}, defaultNginxLogFiles...)

	// lookupUser is overridable so tests can inject a synthetic service user
	// (CI containers don't have a `zerops` user). Defaults to user.Lookup.
	lookupUser = user.Lookup
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
	if err := renderNginxConfig(nginxOutputPath, password); err != nil {
		return fmt.Errorf("nginx config: %w", err)
	}

	if password != "" {
		fmt.Fprintln(os.Stderr, "  ✓ Nginx init complete (auth enabled)")
	} else {
		fmt.Fprintln(os.Stderr, "  ✓ Nginx init complete (no auth)")
	}
	return nil
}

// createNginxDirs creates directories needed by nginx and chowns them to the
// container service user (`zerops`). Target ownership is fixed regardless of
// who runs init: in Zerops `RUN.INIT` this command runs via sudo (euid=0),
// while nginx itself runs later as `zerops` (the user `zcp serve` runs as).
// Chowning to euid would leave dirs root-owned and break nginx startup.
func createNginxDirs() error {
	uid, gid, err := serviceUserIDs()
	if err != nil {
		return err
	}
	if err := guardCanChown(uid); err != nil {
		return err
	}

	for _, d := range nginxDirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
		if err := os.Chown(d, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", d, err)
		}
	}

	// Pre-existing log files (apt installs nginx with www-data:adm 0640) need
	// to be owned by the service user so nginx can append. 0644 lets others
	// read (debugging, log shipper) but only owner write.
	for _, f := range nginxLogFiles {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		if err := os.Chown(f, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", f, err)
		}
		if err := os.Chmod(f, 0o644); err != nil {
			return fmt.Errorf("chmod %s: %w", f, err)
		}
	}
	return nil
}

// serviceUserIDs resolves the container service user's uid/gid.
func serviceUserIDs() (int, int, error) {
	u, err := lookupUser(containerServiceUser)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup %q user: %w (init nginx is container-only)", containerServiceUser, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid %q: %w", u.Uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid %q: %w", u.Gid, err)
	}
	return uid, gid, nil
}

// guardCanChown returns an actionable error if the running user can neither
// chown system files (root) nor chown to themselves (the service user). The
// kernel would reject the chown later anyway with EPERM, but a clear message
// up front saves the operator from chasing a permission error mid-init.
func guardCanChown(targetUID int) error {
	euid := os.Geteuid()
	if euid == 0 || euid == targetUID {
		return nil
	}
	return fmt.Errorf("init nginx: must run as root (sudo) or as %s user; running as uid=%d", containerServiceUser, euid)
}

// renderNginxConfig renders the nginx.conf template to outputPath.
// If password is non-empty, auth is enabled with SHA256 hash of the password.
func renderNginxConfig(outputPath, password string) error {
	cfg := NginxConfig{}
	if password != "" {
		hash := sha256.Sum256([]byte(password))
		cfg.HasAuth = true
		cfg.PasswordHash = fmt.Sprintf("%x", hash)
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

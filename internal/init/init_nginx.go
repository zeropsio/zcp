package init

import (
	"crypto/sha256"
	"fmt"
	"os"
	"text/template"

	"github.com/zeropsio/zcp/internal/content"
)

// NginxConfig holds values for nginx.conf template rendering.
type NginxConfig struct {
	HasAuth      bool
	PasswordHash string
}

var (
	defaultNginxOutputPath = "/etc/nginx/nginx.conf"
	defaultNginxDirs       = []string{"/var/log/nginx", "/var/lib/nginx/tmp", "/var/lib/nginx/body", "/var/lib/nginx/proxy", "/var/lib/nginx/fastcgi", "/var/lib/nginx/uwsgi", "/var/lib/nginx/scgi"}
	defaultNginxLogFiles   = []string{"/var/log/nginx/error.log", "/var/log/nginx/access.log"}

	nginxOutputPath = defaultNginxOutputPath
	nginxDirs       = append([]string{}, defaultNginxDirs...)
	nginxLogFiles   = append([]string{}, defaultNginxLogFiles...)
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

// createNginxDirs creates directories needed by nginx and ensures
// any pre-existing log files are writable by the non-root worker.
func createNginxDirs() error {
	for _, d := range nginxDirs {
		if err := os.MkdirAll(d, 0777); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
		if err := os.Chmod(d, 0777); err != nil {
			return fmt.Errorf("chmod %s: %w", d, err)
		}
	}

	// Fix perms on pre-existing log files (created by apt as www-data:adm 0640).
	for _, f := range nginxLogFiles {
		if _, err := os.Stat(f); err == nil {
			if err := os.Chmod(f, 0666); err != nil {
				return fmt.Errorf("chmod %s: %w", f, err)
			}
		}
	}
	return nil
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

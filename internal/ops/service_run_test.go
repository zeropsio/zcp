package ops

import (
	"strings"
	"testing"
)

// A cwd in a folder of host's mount names that folder of the service; the
// mount's root, another service's mount, a mount whose name only starts with
// the host's, and a path that climbs out of the mount name the checkout.
// witness: internal/ops/service_run.go | strings.CutPrefix(path.Clean(cwd), mount+"/") → strings.CutPrefix(path.Clean(cwd), mount)
func TestServiceRunDir_FolderOfTheMount_IsThatFolderOfTheService(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, host, checkout, cwd, want string
	}{
		{"a folder of the mount", "appdev", "/var/www", "/var/www/appdev/apps/web", "/var/www/apps/web"},
		{"a folder of a lease's mount", "webpool-7", "/var/www", "/var/www/webpool-7/packages/shared", "/var/www/packages/shared"},
		{"the mount's root", "appdev", "/var/www", "/var/www/appdev", "/var/www"},
		{"outside every mount", "appdev", "/var/www", "/tmp", "/var/www"},
		{"outside, the code on a volume", "appdev", "/var/www/code", "/tmp", "/var/www/code"},
		{"another service's mount", "appdev", "/var/www", "/var/www/apidev/src", "/var/www"},
		{"a mount whose name starts with the host's", "appdev", "/var/www", "/var/www/appdev2/src", "/var/www"},
		{"a path that climbs out of the mount", "appdev", "/var/www", "/var/www/appdev/../../etc", "/var/www"},
		{"no folder read", "appdev", "/var/www", "", "/var/www"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ServiceRunDir(tc.host, tc.checkout, tc.cwd); got != tc.want {
				t.Errorf("ServiceRunDir(%q, %q, %q) = %q, want %q", tc.host, tc.checkout, tc.cwd, got, tc.want)
			}
		})
	}
}

// Every argument reaches the command as data: a space, a quote, `;`, `$` and
// `*` are bytes of one argument, never the shell's.
// witness: internal/ops/service_run.go | b.WriteString(ShellQuote(a)) → b.WriteString(a)
func TestBuildServiceRunCommand_Arguments_ReachTheCommandAsData(t *testing.T) {
	t.Parallel()
	// The script the service's /bin/sh runs, POSIX single-quoted by hand …
	script := `cd '/var/www/apps/web' && 'printf' '%s' 'a b' 'it'\''s' ';ls' '$HOME' '*'; exit $?`
	// … and that script as the one -c argument of the login shell's command.
	want := "/bin/sh -c '" + strings.ReplaceAll(script, `'`, `'\''`) + "'"
	got := BuildServiceRunCommand("/var/www/apps/web", []string{"printf", "%s", "a b", "it's", ";ls", "$HOME", "*"})
	if got != want {
		t.Errorf("remote command\n got: %s\nwant: %s", got, want)
	}
}

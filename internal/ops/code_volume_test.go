package ops

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestFillCodeVolume_FillsOnceFromTheCheckoutLeavingTheVolumeOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mountPath  string
		answer     string
		wantFilled bool
		wantErr    string
	}{
		{name: "an empty volume is filled", mountPath: "/var/www/ws", answer: "@@zcp code-volume filled\n", wantFilled: true},
		{name: "a volume holding a repository is left as it is", mountPath: "/var/www/ws", answer: "@@zcp code-volume already\n"},
		{name: "a silent container is no fill", mountPath: "/var/www/ws", answer: "", wantErr: "no answer"},
		{name: "a mount outside /var/www is refused before SSH", mountPath: "/mnt/vol", wantErr: "no folder under /var/www"},
		{name: "/var/www itself is refused before SSH", mountPath: "/var/www", wantErr: "no folder under /var/www"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssh := &mockSSHDeployer{output: []byte(tt.answer)}
			filled, err := FillCodeVolume(context.Background(), ssh, "webdev", "vol", tt.mountPath)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || filled != tt.wantFilled {
				t.Fatalf("filled = %v, err = %v; want %v", filled, err, tt.wantFilled)
			}
			if len(ssh.calls) != 1 {
				t.Fatalf("ssh calls = %d", len(ssh.calls))
			}
			cmd := ssh.calls[0].command
			for _, want := range []string{`[ -e "$vol/.git" ]`, "cd '/var/www'", "! -name 'ws'", "vol='/var/www/ws'"} {
				if !strings.Contains(cmd, want) {
					t.Errorf("command lacks %q: %s", want, cmd)
				}
			}
		})
	}
}

// TestZeropsYmlEntry_WorksIn_OnlyARunWorkingInTheMountTakesItForItsCodeRoot:
// a dev setup treats a volume's mount as its working dir only when its file
// says so — every run command is a no-op or names the mount, and a built-in
// web server serves a documentRoot inside it. A run working in /var/www —
// a Laravel app and worker beside a storage volume — does not.
func TestZeropsYmlEntry_WorksIn_OnlyARunWorkingInTheMountTakesItForItsCodeRoot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		run   string
		mount string
		want  bool
	}{
		{name: "an idle dev container works where zcp deploys it from", run: "base: bun@1.3\n      start: zsc noop --silent", mount: "/var/www/ws", want: true},
		{name: "the supervisor rooted on the mount", run: "base: go@1\n      start: zcp-devsup run --root /var/www/ws", mount: "/var/www/ws", want: true},
		{name: "a start that changes into the mount", run: "base: bun@1.3\n      start: cd /var/www/ws/apps/web && bun dev", mount: "/var/www/ws", want: true},
		{name: "a built-in web server serving a root inside the mount", run: "base: php-nginx@8.4\n      documentRoot: ws/public", mount: "/var/www/ws", want: true},
		{name: "a Laravel worker started in /var/www", run: "base: php@8.4\n      start: php artisan queue:work", mount: "/var/www/storage", want: false},
		{name: "a Laravel app whose web server serves /var/www/public", run: "base: php-nginx@8.4\n      documentRoot: public", mount: "/var/www/storage", want: false},
		{name: "a built-in web server serving /var/www itself", run: "base: nginx@1.22", mount: "/var/www/storage", want: false},
		{name: "the supervisor on /var/www", run: "base: bun@1.3\n      start: zcp-devsup run", mount: "/var/www/ws", want: false},
		{name: "an init command run in /var/www", run: "base: php@8.4\n      initCommands:\n        - php artisan migrate --force\n      start: zsc noop --silent", mount: "/var/www/storage", want: false},
		{name: "a sibling folder sharing the mount's prefix", run: "base: bun@1.3\n      start: cd /var/www/wsx && bun dev", mount: "/var/www/ws", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, err := ParseZeropsYmlContent([]byte("zerops:\n  - setup: dev\n    run:\n      "+tt.run+"\n"), "zerops.yaml")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := doc.Zerops[0].WorksIn(tt.mount); got != tt.want {
				t.Errorf("WorksIn(%s) = %v, want %v", tt.mount, got, tt.want)
			}
		})
	}
}

// localShell runs zcp's container commands on this machine with /var/www
// moved under root, so the volume scripts are proven on real folders.
type localShell struct{ root string }

func (l localShell) ExecSSH(ctx context.Context, _, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(command, "'/var/www", "'"+l.root)) //nolint:gosec // G204: the commands under test, in a temp checkout
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, out)
	}
	return out, nil
}

func (l localShell) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestCodeVolume_OnlyAnEmptyVolumeIsFilledAndOnlyThisRepositoryCountsAsItsCheckout:
// zcp never writes code into a volume it did not prove is a code volume. The
// probe and the fill read the volume the same way: nothing there, an empty
// folder, a checkout sharing the dev service's history, or foreign content —
// a data folder or another repository — which the fill refuses, naming the
// volume, and leaves untouched.
func TestCodeVolume_OnlyAnEmptyVolumeIsFilledAndOnlyThisRepositoryCountsAsItsCheckout(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	gitRepo := func(t *testing.T, dir, file string) {
		t.Helper()
		run := func(args ...string) {
			t.Helper()
			cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...) //nolint:gosec // G204: a test's own git
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
			t.Fatal(err)
		}
		run("init", "-q")
		run("add", "-A")
		run("commit", "-q", "-m", file)
	}
	tests := []struct {
		name       string
		volume     func(t *testing.T, root, vol string)
		want       topology.VolumeContent
		wantFilled bool
		wantErr    string
	}{
		{name: "a volume not mounted yet", volume: func(*testing.T, string, string) {}, want: topology.VolumeAbsent, wantErr: "no "},
		{name: "an empty volume", volume: func(t *testing.T, _, vol string) { t.Helper(); mustMkdir(t, vol) }, want: topology.VolumeEmpty, wantFilled: true},
		{name: "a copy of the dev service's repository", volume: func(t *testing.T, root, vol string) {
			t.Helper()
			// Copied beside the volume, then moved in: GNU cp refuses to copy a directory into itself.
			copied := filepath.Join(t.TempDir(), "www")
			if out, err := exec.CommandContext(context.Background(), "cp", "-a", root, copied).CombinedOutput(); err != nil {
				t.Fatalf("cp: %v %s", err, out)
			}
			if err := os.Rename(copied, vol); err != nil {
				t.Fatal(err)
			}
		}, want: topology.VolumeCheckout},
		{name: "Laravel's storage folder", volume: func(t *testing.T, _, vol string) {
			t.Helper()
			mustMkdir(t, filepath.Join(vol, "logs"))
			if err := os.WriteFile(filepath.Join(vol, "logs", "laravel.log"), []byte("user data"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: topology.VolumeForeign, wantErr: "the code volume vol at /var/www/storage holds files"},
		{name: "another repository", volume: func(t *testing.T, _, vol string) { t.Helper(); gitRepo(t, vol, "other.txt") }, want: topology.VolumeForeign, wantErr: "the code volume vol at /var/www/storage holds files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "www")
			gitRepo(t, root, "app.php")
			vol := filepath.Join(root, "storage")
			tt.volume(t, root, vol)
			before := listTree(t, vol)
			ssh := localShell{root: root}

			got, err := CodeVolumeContent(context.Background(), ssh, "app", "/var/www/storage")
			if err != nil || got != tt.want {
				t.Fatalf("CodeVolumeContent = %q, %v; want %q", got, err, tt.want)
			}
			filled, err := FillCodeVolume(context.Background(), ssh, "app", "vol", "/var/www/storage")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("fill err = %v, want %q", err, tt.wantErr)
				}
				if after := listTree(t, vol); after != before {
					t.Errorf("a refused volume changed:\nbefore %s\nafter  %s", before, after)
				}
				return
			}
			if err != nil || filled != tt.wantFilled {
				t.Fatalf("filled = %v, err = %v; want %v", filled, err, tt.wantFilled)
			}
			if _, err := os.Stat(filepath.Join(vol, "app.php")); err != nil {
				t.Errorf("the volume lacks the repository: %v", err)
			}
		})
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func listTree(t *testing.T, dir string) string {
	t.Helper()
	var names []string
	_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err == nil {
			names = append(names, strings.TrimPrefix(p, dir))
		}
		return nil
	})
	return strings.Join(names, " ")
}

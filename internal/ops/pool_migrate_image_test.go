// Tests for: ops/pool_migrate_image.go — the image step's checks of a part's
// pool setup in the commit its deploy ships, and of the app version the pool
// runs (docs/spec-workflows.md §13 PM-11). The read runs the real script
// against a real git repository in t.TempDir(); the checks are table-driven
// over what that read returns.
package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// imageDevsupLine is the zcp-devsup install line a dev setup under the
// supervisor carries.
var imageDevsupLine = DevsupPrepareCommand("https://github.com/zeropsio/zcp/releases/download/v1.2.3/" + DevsupAsset)

// imageReadyYAML is a zerops.yaml whose pool setup passes every check for a
// pool of three containers and a previewPortBase of 4000.
func imageReadyYAML() string {
	return `zerops:
  - setup: appdev
    build:
      base: nodejs@22
      buildCommands:
        - zsc noop
      deployFiles: ./
    run:
      base: nodejs@22
      prepareCommands:
        - "` + strings.ReplaceAll(imageDevsupLine, `"`, `\"`) + `"
      start: zcp-devsup run
  - setup: apppool
    build:
      base: nodejs@22
      addToRunPrepare:
        - package.json
        - package-lock.json
    run:
      base: nodejs@22
      ports:
        - port: 4001
          httpSupport: true
        - port: 4002
          httpSupport: true
        - port: 4003
          httpSupport: true
      prepareCommands:
        - "` + strings.ReplaceAll(imageDevsupLine, `"`, `\"`) + `"
        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci
      start: zcp-devsup run
`
}

const imageDeclaration = `{"version": 1, "previewPortBase": 4000, "servers": [{"name": "web", "port": 3000, "command": "npm run dev"}]}`

const imageHead = "1111111111111111111111111111111111111111"

// imagePool is the part's pool as the direct service list reads it: at most
// max containers, running active (nil for none).
func imagePool(maxContainers int32, active *platform.ActiveAppVersionDigest) platform.ServiceStack {
	return platform.ServiceStack{
		ID:   "svc-pool",
		Name: "apppool",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER",
		},
		Status:             "ACTIVE",
		CurrentAutoscaling: &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: maxContainers},
		ActiveAppVersion:   active,
	}
}

// imageBuilt is an app version created at created that carries a build.
func imageBuilt(created string) *platform.ActiveAppVersionDigest {
	return &platform.ActiveAppVersionDigest{ID: "av-1", Created: created, Source: "CLI", Built: true}
}

func imageInput(pool platform.ServiceStack) PoolMigrateImageInput {
	return PoolMigrateImageInput{
		Dev:                   "appdev",
		Root:                  "/var/www",
		Head:                  PoolMigrateTree{Path: "/var/www", Head: imageHead, Branch: "main"},
		Pool:                  pool,
		DevSetup:              "appdev",
		FallbackMaxContainers: 3,
	}
}

// imageRead is what the commit holds: the config file under name with its
// last change at changed, and the declaration ("" for none).
func imageRead(name, config, declaration, changed string) poolMigrateCommitRead {
	r := poolMigrateCommitRead{configFile: name, config: config, configChangedAt: changed}
	if declaration != "" {
		r.declaration, r.hasDeclaration = declaration, true
	}
	return r
}

func imageReadyRead() poolMigrateCommitRead {
	return imageRead("zerops.yaml", imageReadyYAML(), imageDeclaration, "2026-10-24T10:00:00+02:00")
}

func imageChecks(list []PoolMigrateImageCheck) []string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.Check)
	}
	return out
}

func imageMessage(list []PoolMigrateImageCheck, check string) string {
	for _, c := range list {
		if c.Check == check {
			return c.Message
		}
	}
	return ""
}

func TestPoolMigrateImage_SetupChecks_ReadySetup_NothingMissing(t *testing.T) {
	t.Parallel()
	got := checkPoolMigrateImage(imageInput(imagePool(3, nil)), imageReadyRead())

	if len(got.Missing) != 0 || len(got.Findings) != 0 {
		t.Fatalf("missing %v findings %v, want none for a ready setup", got.Missing, got.Findings)
	}
	want := PoolMigrateImage{
		Commit: imageHead, Branch: "main", File: "zerops.yaml", Setup: "apppool",
		PreviewPortBase: 4000, MaxContainers: 3,
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("image = %+v\nwant    %+v", *got, want)
	}
}

func TestPoolMigrateImage_SetupChecks_EachGapNamed(t *testing.T) {
	t.Parallel()
	ready := imageReadyYAML()
	devPart, poolPart, _ := strings.Cut(ready, "  - setup: apppool")
	poolSetup := "  - setup: apppool" + poolPart
	tests := []struct {
		name      string
		read      poolMigrateCommitRead
		head      string
		wantCheck string
		// wantIn are phrases the message names.
		wantIn []string
	}{
		{
			name: "no commit yet", head: "0000000000000000000000000000000000000000",
			wantCheck: PoolMigrateImageCommit, wantIn: []string{"no commit"},
		},
		{
			name: "the commit is gone", read: poolMigrateCommitRead{noCommit: true},
			wantCheck: PoolMigrateImageCommit, wantIn: []string{"1111111111"},
		},
		{
			name: "no config file", read: poolMigrateCommitRead{declaration: imageDeclaration, hasDeclaration: true},
			wantCheck: PoolMigrateImageConfig, wantIn: []string{"zerops.yaml", "1111111111"},
		},
		{
			name:      "the config file does not parse",
			read:      imageRead("zerops.yaml", "zerops:\n  - setup: [\n", imageDeclaration, "2026-10-24T10:00:00Z"),
			wantCheck: PoolMigrateImageConfig, wantIn: []string{"zerops.yaml"},
		},
		{
			name:      "no pool setup",
			read:      imageRead("zerops.yml", strings.Replace(ready, "setup: apppool", "setup: worker", 1), imageDeclaration, "2026-10-24T10:00:00Z"),
			wantCheck: PoolMigrateImageSetup, wantIn: []string{`"apppool"`, "appdev, worker", "zerops.yml"},
		},
		{
			name:      "run.start is not the supervisor",
			read:      imageRead("zerops.yaml", strings.Replace(ready, "        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci\n      start: zcp-devsup run", "        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci\n      start: npm run dev", 1), imageDeclaration, "2026-10-24T10:00:00Z"),
			wantCheck: PoolMigrateImageStart, wantIn: []string{"npm run dev", "zcp-devsup run"},
		},
		{
			name:      "no install line",
			read:      imageRead("zerops.yaml", devPart+strings.Replace(poolSetup, "        - \""+strings.ReplaceAll(imageDevsupLine, `"`, `\"`)+"\"\n", "", 1), imageDeclaration, "2026-10-24T10:00:00Z"),
			wantCheck: PoolMigrateImageInstall, wantIn: []string{"zcp-devsup", `"appdev"`, imageDevsupLine},
		},
		{
			name:      "no declaration",
			read:      imageRead("zerops.yaml", ready, "", "2026-10-24T10:00:00Z"),
			wantCheck: PoolMigrateImagePreviewBase, wantIn: []string{".zcp/dev-servers.json", "previewPortBase"},
		},
		{
			name:      "the declaration does not parse",
			read:      imageRead("zerops.yaml", ready, `{"version": 1, "servers": [{"name": "web", "command": "a"}, {"name": "web", "command": "b"}]}`, "2026-10-24T10:00:00Z"),
			wantCheck: PoolMigrateImagePreviewBase, wantIn: []string{".zcp/dev-servers.json", "twice"},
		},
		{
			name:      "previewPortBase unset",
			read:      imageRead("zerops.yaml", ready, `{"version": 1, "servers": []}`, "2026-10-24T10:00:00Z"),
			wantCheck: PoolMigrateImagePreviewBase, wantIn: []string{"previewPortBase"},
		},
		{
			name: "preview ports missing or without httpSupport",
			read: imageRead("zerops.yaml",
				strings.Replace(strings.Replace(ready, "        - port: 4003\n          httpSupport: true\n", "", 1), "        - port: 4002\n          httpSupport: true\n", "        - port: 4002\n", 1),
				imageDeclaration, "2026-10-24T10:00:00Z"),
			wantCheck: PoolMigrateImagePreviewPorts, wantIn: []string{"4003", "4002", "httpSupport: true", "3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := imageInput(imagePool(3, nil))
			if tt.head != "" {
				in.Head.Head = tt.head
			}
			got := checkPoolMigrateImage(in, tt.read)
			if checks := imageChecks(got.Missing); !slices.Contains(checks, tt.wantCheck) {
				t.Fatalf("missing = %v, want %s", checks, tt.wantCheck)
			}
			msg := imageMessage(got.Missing, tt.wantCheck)
			for _, want := range tt.wantIn {
				if !strings.Contains(msg, want) {
					t.Errorf("%s message %q does not name %q", tt.wantCheck, msg, want)
				}
			}
			if got.Done {
				t.Error("a setup with a gap is never done")
			}
		})
	}
}

// TestPoolMigrateImage_SetupChecks_DependencyRule_FindingsNeverHoldTheDeploy
// pins the dependency rule as findings: the image bakes the dependencies into
// /var/www in run.prepareCommands from the files build.addToRunPrepare lists,
// and what build.deployFiles ships is wiped by every reset — each named, none
// keeping the deploy from being named.
func TestPoolMigrateImage_SetupChecks_DependencyRule_FindingsNeverHoldTheDeploy(t *testing.T) {
	t.Parallel()
	ready := imageReadyYAML()
	noLockfile := strings.Replace(ready, "      addToRunPrepare:\n        - package.json\n        - package-lock.json\n", "", 1)
	notBaked := strings.Replace(ready, "        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci\n", "", 1)
	shipped := strings.Replace(notBaked, "      addToRunPrepare:", "      buildCommands:\n        - npm ci\n      deployFiles:\n        - ./\n      addToRunPrepare:", 1)
	tests := []struct {
		name        string
		config      string
		declaration string
		want        []string
		wantIn      map[string][]string
		wantNotIn   map[string][]string
	}{
		{
			name: "no lockfile in addToRunPrepare", config: noLockfile, declaration: imageDeclaration,
			want:   []string{PoolMigrateImageDepsLockfile},
			wantIn: map[string][]string{PoolMigrateImageDepsLockfile: {"build.addToRunPrepare", "lockfile"}},
		},
		{
			name: "nothing baked into /var/www", config: notBaked, declaration: imageDeclaration,
			want:      []string{PoolMigrateImageDepsBaked},
			wantIn:    map[string][]string{PoolMigrateImageDepsBaked: {"/var/www", "run.prepareCommands"}},
			wantNotIn: map[string][]string{PoolMigrateImageDepsBaked: {"deployFiles"}},
		},
		{
			name: "dependencies only in deployFiles", config: shipped, declaration: imageDeclaration,
			want:   []string{PoolMigrateImageDepsBaked},
			wantIn: map[string][]string{PoolMigrateImageDepsBaked: {"build.deployFiles", "wiped by every reset"}},
		},
		{
			name: "a branch prepare installs them instead", config: notBaked,
			declaration: `{"version": 1, "previewPortBase": 4000, "prepare": "npm ci", "servers": []}`,
			want:        []string{PoolMigrateImageDepsBaked},
			wantIn:      map[string][]string{PoolMigrateImageDepsBaked: {"npm ci", "every acquire"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkPoolMigrateImage(imageInput(imagePool(3, nil)), imageRead("zerops.yaml", tt.config, tt.declaration, "2026-10-24T10:00:00Z"))
			if len(got.Missing) != 0 {
				t.Fatalf("missing = %v: the dependency rule never holds the deploy", got.Missing)
			}
			if checks := imageChecks(got.Findings); !reflect.DeepEqual(checks, tt.want) {
				t.Fatalf("findings = %v, want %v", checks, tt.want)
			}
			for check, phrases := range tt.wantIn {
				msg := imageMessage(got.Findings, check)
				for _, p := range phrases {
					if !strings.Contains(msg, p) {
						t.Errorf("%s message %q does not name %q", check, msg, p)
					}
				}
			}
			for check, phrases := range tt.wantNotIn {
				msg := imageMessage(got.Findings, check)
				for _, p := range phrases {
					if strings.Contains(msg, p) {
						t.Errorf("%s message %q names %q", check, msg, p)
					}
				}
			}
		})
	}
}

func TestPoolMigrateImage_SetupChecks_PreviewPortsFollowMaxContainers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		pool        platform.ServiceStack
		wantMax     int
		wantMissing bool
	}{
		{name: "three containers, three ports", pool: imagePool(3, nil), wantMax: 3},
		{name: "five containers, three ports", pool: imagePool(5, nil), wantMax: 5, wantMissing: true},
		{name: "a maximum over ten holds ten", pool: imagePool(12, nil), wantMax: 10, wantMissing: true},
		{name: "unread maximum takes the fallback", pool: func() platform.ServiceStack {
			p := imagePool(0, nil)
			p.CurrentAutoscaling = nil
			return p
		}(), wantMax: 3},
		{name: "two containers need two of the three", pool: imagePool(2, nil), wantMax: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkPoolMigrateImage(imageInput(tt.pool), imageReadyRead())
			if got.MaxContainers != tt.wantMax {
				t.Errorf("maxContainers = %d, want %d", got.MaxContainers, tt.wantMax)
			}
			if has := slices.Contains(imageChecks(got.Missing), PoolMigrateImagePreviewPorts); has != tt.wantMissing {
				t.Errorf("preview-ports missing = %v, want %v (%v)", has, tt.wantMissing, got.Missing)
			}
			if tt.wantMissing && !strings.Contains(imageMessage(got.Missing, PoolMigrateImagePreviewPorts), "4004") {
				t.Errorf("message %q does not name the first port the pool lacks", imageMessage(got.Missing, PoolMigrateImagePreviewPorts))
			}
		})
	}
}

// TestPoolMigrateImage_Done_PoolRunsAnImageNewerThanTheSetup pins when the
// image step is done: the pool runs an app version with code, created at or
// after the commit's last change of its config file, and the setup passes
// every check.
func TestPoolMigrateImage_Done_PoolRunsAnImageNewerThanTheSetup(t *testing.T) {
	t.Parallel()
	placeholder := &platform.ActiveAppVersionDigest{ID: "av-0", Created: "2026-10-24T09:00:00Z", Source: platform.AppVersionSourceNone}
	tests := []struct {
		name         string
		active       *platform.ActiveAppVersionDigest
		read         poolMigrateCommitRead
		wantDeployed bool
		wantCurrent  bool
		wantDone     bool
	}{
		{name: "the pool runs nothing", read: imageReadyRead()},
		{name: "the startWithoutCode placeholder", active: placeholder, read: imageReadyRead()},
		{name: "built before the setup changed", active: imageBuilt("2026-10-24T07:59:59Z"), read: imageReadyRead(), wantDeployed: true},
		{name: "built after the setup changed", active: imageBuilt("2026-10-24T08:00:05.123Z"), read: imageReadyRead(), wantDeployed: true, wantCurrent: true, wantDone: true},
		{name: "built the second it changed", active: imageBuilt("2026-10-24T08:00:00Z"), read: imageReadyRead(), wantDeployed: true, wantCurrent: true, wantDone: true},
		{
			name: "current but the setup has a gap", active: imageBuilt("2026-10-24T09:00:00Z"),
			read:         imageRead("zerops.yaml", strings.Replace(imageReadyYAML(), "start: zcp-devsup run\n", "start: npm start\n", 2), imageDeclaration, "2026-10-24T10:00:00+02:00"),
			wantDeployed: true, wantCurrent: true,
		},
		{name: "created time unreadable", active: imageBuilt(""), read: imageReadyRead(), wantDeployed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkPoolMigrateImage(imageInput(imagePool(3, tt.active)), tt.read)
			if (got.Deployed != nil) != tt.wantDeployed {
				t.Fatalf("deployed = %+v, want present=%v", got.Deployed, tt.wantDeployed)
			}
			if got.Deployed != nil {
				if got.Deployed.Current != tt.wantCurrent {
					t.Errorf("deployed.current = %v, want %v (%+v)", got.Deployed.Current, tt.wantCurrent, got.Deployed)
				}
				if got.Deployed.ConfigCommittedAt != "2026-10-24T10:00:00+02:00" {
					t.Errorf("deployed.configCommittedAt = %q", got.Deployed.ConfigCommittedAt)
				}
			}
			if got.Done != tt.wantDone {
				t.Errorf("done = %v, want %v", got.Done, tt.wantDone)
			}
		})
	}
}

func TestPoolMigrateImage_Uncommitted_Named(t *testing.T) {
	t.Parallel()
	in := imageInput(imagePool(3, nil))
	in.Head.Modified = &PoolMigratePaths{Count: 2, Paths: []string{"app.js", "zerops.yaml"}}
	in.Head.Untracked = &PoolMigratePaths{Count: 1, Paths: []string{".zcp/"}}
	got := checkPoolMigrateImage(in, imageReadyRead())
	if want := []string{"zerops.yaml", ".zcp/dev-servers.json"}; !reflect.DeepEqual(got.Uncommitted, want) {
		t.Fatalf("uncommitted = %v, want %v", got.Uncommitted, want)
	}

	in.Head.Modified, in.Head.Untracked = nil, nil
	in.Head.Staged = &PoolMigratePaths{Count: 1, Paths: []string{"lib.js"}}
	if got := checkPoolMigrateImage(in, imageReadyRead()); len(got.Uncommitted) != 0 {
		t.Fatalf("uncommitted = %v, want none", got.Uncommitted)
	}
}

// imageRepo is a repository whose HEAD commits a ready pool setup and its
// declaration, then changes both files in the index and the tree without
// committing them.
func imageRepo(t *testing.T) (base, root, head string) {
	t.Helper()
	base = censusBase(t)
	root = censusCleanRepo(t, base)
	censusWrite(t, filepath.Join(root, "zerops.yaml"), imageReadyYAML())
	censusWrite(t, filepath.Join(root, ".zcp", "dev-servers.json"), imageDeclaration)
	censusGit(t, root, "add", "-A")
	censusGit(t, root, "commit", "-q", "-m", "pool setup")
	head = strings.TrimSpace(censusMust(t, root, "rev-parse", "HEAD"))
	censusAppend(t, filepath.Join(root, "zerops.yaml"), "# staged edit\n")
	censusGit(t, root, "add", "zerops.yaml")
	censusWrite(t, filepath.Join(root, ".zcp", "dev-servers.json"), `{"version": 1, "previewPortBase": 9000, "servers": []}`)
	return base, root, head
}

func censusMust(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := censusGitRun(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func TestReadPoolMigrateImage_ReadsTheCommitNotTheTree(t *testing.T) {
	t.Parallel()
	_, root, head := imageRepo(t)
	in := imageInput(imagePool(3, nil))
	in.Root, in.Head.Head = root, head
	ssh := &censusLocalSSH{}
	got, err := ReadPoolMigrateImage(context.Background(), ssh, in)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.Missing) != 0 || got.File != "zerops.yaml" || got.PreviewPortBase != 4000 || got.Commit != head {
		t.Fatalf("image = %+v, want the commit's ready setup and its previewPortBase 4000", got)
	}
	if len(ssh.calls) != 1 || ssh.calls[0] != "appdev: "+PoolMigrateImageCommand(root, head) {
		t.Fatalf("ssh calls = %v, want one round trip", ssh.calls)
	}

	// The commit's last change of zerops.yaml is when its committer said.
	read, err := parsePoolMigrateImageRead(string(mustExec(t, ssh, PoolMigrateImageCommand(root, head))))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSpace(censusMust(t, root, "log", "-1", "--format=%cI", head, "--", "zerops.yaml")); read.configChangedAt != want {
		t.Errorf("configChangedAt = %q, want %q", read.configChangedAt, want)
	}
	if read.config != imageReadyYAML() || read.declaration != imageDeclaration {
		t.Errorf("the read did not return the commit's files byte for byte")
	}
}

func mustExec(t *testing.T, ssh *censusLocalSSH, command string) []byte {
	t.Helper()
	out, err := ssh.ExecSSH(context.Background(), "appdev", command)
	if err != nil {
		t.Fatalf("exec: %v\n%s", err, out)
	}
	return out
}

func TestReadPoolMigrateImage_ZeropsYmlFallbackAndNoDeclaration(t *testing.T) {
	t.Parallel()
	base := censusBase(t)
	root := censusCleanRepo(t, base)
	censusWrite(t, filepath.Join(root, "zerops.yml"), imageReadyYAML())
	// A directory named like the declaration is no declaration.
	if err := os.MkdirAll(filepath.Join(root, ".zcp", "dev-servers.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	censusWrite(t, filepath.Join(root, ".zcp", "dev-servers.json", "x"), "x\n")
	censusGit(t, root, "add", "-A")
	censusGit(t, root, "commit", "-q", "-m", "legacy name")
	head := strings.TrimSpace(censusMust(t, root, "rev-parse", "HEAD"))

	in := imageInput(imagePool(3, nil))
	in.Root, in.Head.Head = root, head
	got, err := ReadPoolMigrateImage(context.Background(), &censusLocalSSH{}, in)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.File != "zerops.yml" {
		t.Errorf("file = %q, want zerops.yml", got.File)
	}
	if checks := imageChecks(got.Missing); !reflect.DeepEqual(checks, []string{PoolMigrateImagePreviewBase}) {
		t.Errorf("missing = %v, want only the declaration", checks)
	}
}

func TestReadPoolMigrateImage_UnknownCommit_Missing(t *testing.T) {
	t.Parallel()
	base := censusBase(t)
	root := censusCleanRepo(t, base)
	in := imageInput(imagePool(3, nil))
	in.Root = root
	got, err := ReadPoolMigrateImage(context.Background(), &censusLocalSSH{}, in)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if checks := imageChecks(got.Missing); !reflect.DeepEqual(checks, []string{PoolMigrateImageCommit}) {
		t.Fatalf("missing = %v, want the commit", checks)
	}
}

func TestReadPoolMigrateImage_NoCommitYet_NoSSH(t *testing.T) {
	t.Parallel()
	in := imageInput(imagePool(3, nil))
	in.Head.Head = ""
	ssh := &censusLocalSSH{}
	got, err := ReadPoolMigrateImage(context.Background(), ssh, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(ssh.calls) != 0 || !slices.Contains(imageChecks(got.Missing), PoolMigrateImageCommit) {
		t.Fatalf("calls %v missing %v, want no ssh and the commit named", ssh.calls, got.Missing)
	}
}

func TestReadPoolMigrateImage_WritesNothing(t *testing.T) {
	t.Parallel()
	base, root, head := imageRepo(t)
	in := imageInput(imagePool(3, nil))
	in.Root, in.Head.Head = root, head
	before := censusSnapshot(t, base)
	for range 2 {
		if _, err := ReadPoolMigrateImage(context.Background(), &censusLocalSSH{}, in); err != nil {
			t.Fatal(err)
		}
	}
	if diff := censusSnapshotDiff(before, censusSnapshot(t, base)); len(diff) > 0 {
		t.Fatalf("the read changed the repository:\n%s", strings.Join(diff, "\n"))
	}
}

func TestReadPoolMigrateImage_SSHFailure_KeepsTheSSHError(t *testing.T) {
	t.Parallel()
	in := imageInput(imagePool(3, nil))
	in.Root = filepath.Join(t.TempDir(), "gone")
	_, err := ReadPoolMigrateImage(context.Background(), &censusLocalSSH{}, in)
	var sshErr *platform.SSHExecError
	if !errors.As(err, &sshErr) {
		t.Fatalf("err = %v, want the ssh error kept", err)
	}
}

// TestReadPoolMigrateImage_ScriptRunsOnlyReadOnlyGit pins the read to git
// subcommands that read objects: nothing in it can move a ref, write an
// object, take a lock or touch the index or the tree.
func TestReadPoolMigrateImage_ScriptRunsOnlyReadOnlyGit(t *testing.T) {
	t.Parallel()
	script := PoolMigrateImageCommand("/var/www", imageHead)
	readOnly := map[string]bool{"cat-file": true, "log": true}
	matches := regexp.MustCompile(`\bgit\s+([a-z-]+)`).FindAllStringSubmatch(script, -1)
	if len(matches) == 0 {
		t.Fatal("the read runs no git at all")
	}
	for _, m := range matches {
		if !readOnly[m[1]] {
			t.Errorf("the read runs `git %s`, which is not a read-only subcommand", m[1])
		}
	}
	if !strings.Contains(script, "GIT_OPTIONAL_LOCKS=0") {
		t.Error("the read must run git with optional locks off")
	}
	if !strings.HasPrefix(script, "cd '/var/www'") || !strings.Contains(script, "'"+imageHead+"'") {
		t.Errorf("the read must start in the shell-quoted root and name the shell-quoted commit: %q", script)
	}
}

// TestParsePoolMigrateImageRead_CutShortOrFailed_Error pins that a read the
// script could not finish is no read: a failed step, a file cut short, a
// missing end and no header all fail it.
func TestParsePoolMigrateImageRead_CutShortOrFailed_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		out     string
		wantErr string
	}{
		{name: "a step failed", out: "zcp-image 1\nfatal: bad object\nzcp-image-error log\n", wantErr: "bad object"},
		{name: "a file cut short", out: "zcp-image 1\nF 400 zerops.yaml\nzerops:\n", wantErr: "zerops.yaml"},
		{name: "no end", out: "zcp-image 1\nF 3 zerops.yaml\nabc\nC 2026-10-24T10:00:00Z\n", wantErr: "incomplete"},
		{name: "no header", out: "bash: git: command not found\n", wantErr: "command not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parsePoolMigrateImageRead(tt.out); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one naming %q", err, tt.wantErr)
			}
		})
	}
	// A file whose content holds the framing's own lines is read by its size.
	body := "zcp-image-end\nC 2000-01-01T00:00:00Z\n"
	out := fmt.Sprintf("zcp-image 1\nF %d zerops.yaml\n%s\nC 2026-10-24T10:00:00Z\nzcp-image-end\n", len(body), body)
	read, err := parsePoolMigrateImageRead(out)
	if err != nil {
		t.Fatal(err)
	}
	if read.config != body || read.configChangedAt != "2026-10-24T10:00:00Z" {
		t.Fatalf("read = %+v", read)
	}
}

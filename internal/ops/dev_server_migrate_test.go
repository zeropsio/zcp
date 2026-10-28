package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testDevsupURL = "https://github.com/zeropsio/zcp/releases/download/v9.300.0/zcp-devsup-linux-amd64"

func TestDevsupURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, latest, override, want, wantErr string
	}{
		{name: "the newest release's asset", latest: testDevsupURL, want: testDevsupURL},
		{name: "no release ships it yet", wantErr: "no published zcp release ships"},
		{name: "the override wins", latest: testDevsupURL, override: "https://lab.example/b/zcp-devsup-linux-amd64", want: "https://lab.example/b/zcp-devsup-linux-amd64"},
		{name: "an override that would break the quoted line", override: "https://x/a'b/zcp-devsup-linux-amd64", wantErr: "not an http(s) URL"},
		{name: "an override for another asset", override: "https://x/zcp-linux-amd64", wantErr: "not an http(s) URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DevsupURL(tt.latest, tt.override)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("= %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

// devSetupRun decodes setup's run.prepareCommands and run.start.
func devSetupRun(t *testing.T, data []byte, setup string) (prepare []string, start string) {
	t.Helper()
	var doc struct {
		Zerops []struct {
			Setup string `yaml:"setup"`
			Run   struct {
				PrepareCommands []string `yaml:"prepareCommands"`
				Start           string   `yaml:"start"`
			} `yaml:"run"`
		} `yaml:"zerops"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("result is not YAML: %v\n%s", err, data)
	}
	for _, e := range doc.Zerops {
		if e.Setup == setup {
			return e.Run.PrepareCommands, e.Run.Start
		}
	}
	t.Fatalf("no setup %q in\n%s", setup, data)
	return nil, ""
}

// TestAdoptSupervisorYAML: the dev setup gains the install line and
// run.start `zcp-devsup run` — `zcp-devsup run --root <mountPath>` for a
// variant-B dev service, whose repository is on the code volume;
// everything else in the file stays.
func TestAdoptSupervisorYAML(t *testing.T) {
	t.Parallel()
	line := DevsupPrepareCommand(testDevsupURL)
	const olderLine = "sudo sh -c 'curl -fsSL https://github.com/zeropsio/zcp/releases/download/v9.299.0/zcp-devsup-linux-amd64 -o /usr/local/bin/zcp-devsup && chmod 0755 /usr/local/bin/zcp-devsup'"
	tests := []struct {
		name, yaml, setup, hostname string
		// root is the repository root the supervisor runs on, "" for /var/www.
		root        string
		wantSetup   string
		wantPrepare []string
		// wantStart is run.start after the edit, "" for `zcp-devsup run`.
		wantStart   string
		wantChanged bool
		wantErr     string
	}{
		{
			name:     "a single setup with today's noop start",
			hostname: "appdev",
			yaml: `zerops:
  - setup: dev
    build:
      base: bun@1.3
      deployFiles: ./
    run:
      base: bun@1.3
      ports:
        - port: 3000
          httpSupport: true
      start: zsc noop --silent
`,
			wantSetup: "dev", wantPrepare: []string{line}, wantChanged: true,
		},
		{
			name: "the setup named after the hostname among several, keeping its prepareCommands",
			yaml: `zerops:
  - setup: appdev
    run:
      base: nodejs@22
      prepareCommands:
        - sudo apk add jq
  - setup: prod
    run:
      start: node dist/main.js
`,
			hostname:  "appdev",
			wantSetup: "appdev", wantPrepare: []string{"sudo apk add jq", line}, wantChanged: true,
		},
		{
			name: "the setup the service meta names",
			yaml: `zerops:
  - setup: dev
    run: {start: zsc noop}
  - setup: dev-api
    run: {start: zsc noop}
`,
			setup: "dev-api", hostname: "apidev",
			wantSetup: "dev-api", wantPrepare: []string{line}, wantChanged: true,
		},
		{
			// The release a dev setup already installs stays whichever it is:
			// a newer one out since is no reason to change the person's file.
			name: "a zcp-devsup line the dev setup carries stays as written, whichever release it names",
			yaml: `zerops:
  - setup: dev
    run:
      prepareCommands:
        - sudo sh -c 'curl -fsSL https://github.com/zeropsio/zcp/releases/download/v9.299.0/zcp-devsup-linux-amd64 -o /usr/local/bin/zcp-devsup && chmod 0755 /usr/local/bin/zcp-devsup'
      start: zcp-devsup run
`,
			hostname:  "appdev",
			wantSetup: "dev", wantPrepare: []string{olderLine}, wantChanged: false,
		},
		{
			name: "a zcp-devsup line without the supervisor's start keeps its release and gains the start",
			yaml: `zerops:
  - setup: dev
    run:
      prepareCommands:
        - sudo sh -c 'curl -fsSL https://github.com/zeropsio/zcp/releases/download/v9.299.0/zcp-devsup-linux-amd64 -o /usr/local/bin/zcp-devsup && chmod 0755 /usr/local/bin/zcp-devsup'
      start: zsc noop --silent
`,
			hostname:  "appdev",
			wantSetup: "dev", wantPrepare: []string{olderLine}, wantChanged: true,
		},
		{
			name:      "already on the supervisor",
			yaml:      "zerops:\n  - setup: dev\n    run:\n      prepareCommands:\n        - " + yamlQuote(line) + "\n      start: zcp-devsup run\n",
			hostname:  "appdev",
			wantSetup: "dev", wantPrepare: []string{line}, wantChanged: false,
		},
		{
			name: "a variant-B dev service's supervisor runs on the code volume",
			yaml: `zerops:
  - setup: dev-api
    run:
      base: go@1
      volume:
        hostname: vol
        mountPath: /var/www/ws
      start: zsc noop --silent
`,
			setup: "dev-api", hostname: "apidev", root: "/var/www/ws",
			wantSetup: "dev-api", wantPrepare: []string{line}, wantStart: "zcp-devsup run --root /var/www/ws", wantChanged: true,
		},
		{
			name:      "a supervisor on /var/www moves onto the code volume",
			yaml:      "zerops:\n  - setup: dev\n    run:\n      prepareCommands:\n        - " + yamlQuote(line) + "\n      start: zcp-devsup run\n",
			hostname:  "webdev",
			root:      "/var/www/ws",
			wantSetup: "dev", wantPrepare: []string{line}, wantStart: "zcp-devsup run --root /var/www/ws", wantChanged: true,
		},
		{
			name:      "already on the supervisor on the code volume",
			yaml:      "zerops:\n  - setup: dev\n    run:\n      prepareCommands:\n        - " + yamlQuote(line) + "\n      start: zcp-devsup run --root /var/www/ws\n",
			hostname:  "webdev",
			root:      "/var/www/ws",
			wantSetup: "dev", wantPrepare: []string{line}, wantStart: "zcp-devsup run --root /var/www/ws", wantChanged: false,
		},
		{
			name:     "several setups and none is the dev one",
			yaml:     "zerops:\n  - setup: web\n  - setup: api\n",
			hostname: "appdev",
			wantErr:  "cannot tell the dev setup",
		},
		{
			name:     "a setup the file lacks",
			yaml:     "zerops:\n  - setup: dev\n",
			setup:    "dev-api",
			hostname: "apidev",
			wantErr:  `no setup "dev-api"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, used, changed, err := AdoptSupervisorYAML([]byte(tt.yaml), tt.setup, tt.hostname, testDevsupURL, tt.root)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("AdoptSupervisorYAML: %v", err)
			}
			if used != tt.wantSetup || changed != tt.wantChanged {
				t.Errorf("setup, changed = %q, %v; want %q, %v", used, changed, tt.wantSetup, tt.wantChanged)
			}
			if !changed && string(out) != tt.yaml {
				t.Errorf("an unchanged file was rewritten:\n%s", out)
			}
			prepare, start := devSetupRun(t, out, used)
			wantStart := tt.wantStart
			if wantStart == "" {
				wantStart = "zcp-devsup run"
			}
			if start != wantStart {
				t.Errorf("run.start = %q, want %q", start, wantStart)
			}
			if strings.Join(prepare, "\n") != strings.Join(tt.wantPrepare, "\n") {
				t.Errorf("prepareCommands = %q, want %q", prepare, tt.wantPrepare)
			}
			if strings.Contains(tt.yaml, "base: bun@1.3") && !strings.Contains(string(out), "httpSupport: true") {
				t.Errorf("the rest of the setup was lost:\n%s", out)
			}
		})
	}
}

func yamlQuote(s string) string {
	b, _ := yaml.Marshal(s)
	return strings.TrimSpace(string(b))
}

// TestAdoptSupervisorYAML_ChangesNothingButTheSupervisorLines (orchestrator
// decision A, review hole 4a): moving a dev setup onto the supervisor splices
// the install line and run.start into the user's own bytes — blank lines,
// comments, zero-indented lists and flow maps stay exactly as written.
func TestAdoptSupervisorYAML_ChangesNothingButTheSupervisorLines(t *testing.T) {
	t.Parallel()
	item := DevsupPrepareCommand(testDevsupURL)
	recipe, err := os.ReadFile(filepath.Join("..", "authoring", "recipe", "testdata", "goldens", "showcase-zerops.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, yaml, setup string
		removed, added    []string
	}{
		{
			name: "a hand-written file with blank lines, comments, a zero-indented cache list and flow maps",
			yaml: `# zerops.yml for the app

zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
      - npm ci   # zero-indented
      cache:
      - node_modules

      deployFiles: ./
    run:
      base: nodejs@22
      ports: [{port: 3000, httpSupport: true}]
      envVariables: {NODE_ENV: development, LOG: "debug"}
      prepareCommands:
        - sudo apk add jq   # tools

        # more tools later
      start: zsc noop --silent   # keep alive
  - setup: prod
    run: {start: node dist/main.js}
`,
			setup:   "dev",
			removed: []string{"      start: zsc noop --silent   # keep alive"},
			added:   []string{"        - " + item, "      start: zcp-devsup run   # keep alive"},
		},
		{
			name: "a zero-indented prepareCommands list and no start",
			yaml: `zerops:
- setup: dev
  run:
    prepareCommands:
    - sudo apk add jq
    base: bun@1.3

- setup: prod
`,
			setup: "dev",
			added: []string{"    - " + item, "    start: zcp-devsup run"},
		},
		{
			name: "a run without prepareCommands or start",
			yaml: `zerops:
  - setup: dev
    run:
      base: go@1
      # ports later
`,
			setup: "dev",
			added: []string{"      prepareCommands:", "        - " + item, "      start: zcp-devsup run"},
		},
		{
			name: "a block-scalar start followed by a comment",
			yaml: `zerops:
  - setup: dev
    run:
      prepareCommands: [sudo apk add jq]
      start: |
        npm run dev

      # ports below
      ports:
        - port: 3000
`,
			setup:   "dev",
			removed: []string{"      prepareCommands: [sudo apk add jq]", "      start: |", "        npm run dev"},
			added:   []string{"      prepareCommands: [sudo apk add jq, " + item + "]", "      start: zcp-devsup run"},
		},
		{
			name: "a flow-map run",
			yaml: `zerops:
  - setup: dev
    run: {start: zsc noop}
  - setup: dev-api
    run: {start: zsc noop}
`,
			setup:   "dev-api",
			removed: []string{"    run: {start: zsc noop}"},
			added:   []string{"    run: {prepareCommands: [" + item + "], start: zcp-devsup run}"},
		},
		{
			name:  "the showcase recipe's zerops.yaml",
			yaml:  string(recipe),
			setup: "dev",
			added: []string{"        - " + item, "      start: zcp-devsup run"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, _, changed, err := AdoptSupervisorYAML([]byte(tt.yaml), tt.setup, "appdev", testDevsupURL, "")
			if err != nil || !changed {
				t.Fatalf("AdoptSupervisorYAML: changed=%v err=%v", changed, err)
			}
			removed, added := lineDiff(strings.Split(tt.yaml, "\n"), strings.Split(string(out), "\n"))
			if strings.Join(removed, "\n") != strings.Join(tt.removed, "\n") || strings.Join(added, "\n") != strings.Join(tt.added, "\n") {
				t.Errorf("diff:\n- %q\n+ %q\nwant:\n- %q\n+ %q\nfile:\n%s", removed, added, tt.removed, tt.added, out)
			}
			prepare, start := devSetupRun(t, out, tt.setup)
			if start != DevsupStart || len(prepare) == 0 || prepare[len(prepare)-1] != item {
				t.Errorf("run.start = %q, prepareCommands = %q; want the supervisor's", start, prepare)
			}
		})
	}
}

// lineDiff is the lines a removed and b added (a longest common subsequence).
func lineDiff(a, b []string) (removed, added []string) {
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i, j = i+1, j+1
		case j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]):
			added = append(added, b[j])
			j++
		default:
			removed = append(removed, a[i])
			i++
		}
	}
	return removed, added
}

// TestDevsupPrepareCommand_FallsBackToWgetWhenCurlIsAbsent (docs/spec-mate.md
// §2.8a.2): an Alpine image without curl still installs the supervisor.
func TestDevsupPrepareCommand_FallsBackToWgetWhenCurlIsAbsent(t *testing.T) {
	t.Parallel()
	got := DevsupPrepareCommand(testDevsupURL)
	want := `sudo sh -c 'u=` + testDevsupURL + `; f=/usr/local/bin/zcp-devsup; if command -v curl >/dev/null; then curl -fsSL "$u" -o "$f"; else wget -qO "$f" "$u"; fi && chmod 0755 "$f"'`
	if got != want {
		t.Errorf("DevsupPrepareCommand = %q, want %q", got, want)
	}
}

func TestAdoptSupervisorInDevSetup_ZeropsYmlRepo_ErrorNamesZeropsYmlNeverZeropsYaml(t *testing.T) {
	t.Parallel()
	ssh := &mockSSHDeployer{output: []byte("@@zcp-file zerops.yml\nzerops:\n  - setup: a\n    run: {}\n  - setup: b\n    run: {}\n")}
	file, _, _, _, err := AdoptSupervisorInDevSetup(context.Background(), ssh, "appdev", "", "http://x", "", "")
	if file != "zerops.yml" {
		t.Fatalf("file = %q", file)
	}
	if err == nil {
		t.Fatal("expected an error: several setups, none the dev one")
	}
	if !strings.Contains(err.Error(), "zerops.yml") || strings.Contains(err.Error(), "zerops.yaml") {
		t.Fatalf("error must name the repo's zerops.yml and never zerops.yaml: %v", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatalf("the named error must still wrap the setup error: %v", err)
	}
}

// TestReadRepoConfigFile_NamesTheFileItRead: the repo config file read —
// zerops.yaml first, else zerops.yml — names the file it read, run by a real
// shell; a folder with neither reads as no file.
func TestReadRepoConfigFile_NamesTheFileItRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                  string
		files                 []string
		wantFile, wantContent string
	}{
		{name: "zerops.yaml", files: []string{"zerops.yaml"}, wantFile: "zerops.yaml", wantContent: "zerops: # zerops.yaml\n"},
		{name: "zerops.yml", files: []string{"zerops.yml"}, wantFile: "zerops.yml", wantContent: "zerops: # zerops.yml\n"},
		{name: "both", files: []string{"zerops.yaml", "zerops.yml"}, wantFile: "zerops.yaml", wantContent: "zerops: # zerops.yaml\n"},
		{name: "neither"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("zerops: # "+f+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			file, content, err := ReadRepoConfigFile(context.Background(), localShell{root: dir}, "app", dir)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if file != tc.wantFile || content != tc.wantContent {
				t.Errorf("read = (%q, %q), want (%q, %q)", file, content, tc.wantFile, tc.wantContent)
			}
		})
	}
}

func TestAdoptSupervisorYAML_WritesNoEnvVariableNeverAZeropsPrefixedOne(t *testing.T) {
	t.Parallel()
	in := "zerops:\n  - setup: dev\n    run:\n      base: nodejs@24\n      envVariables:\n        NODE_ENV: development\n      start: npm run dev\n"
	out, _, changed, err := AdoptSupervisorYAML([]byte(in), "dev", "appdev", testDevsupURL, "")
	if err != nil || !changed {
		t.Fatalf("AdoptSupervisorYAML: changed=%v err=%v", changed, err)
	}
	if strings.Contains(string(out), "ZEROPS_") {
		t.Errorf("the supervisor wiring wrote a ZEROPS_ name, which the platform refuses on deploy:\n%s", out)
	}
	_, added := lineDiff(strings.Split(in, "\n"), strings.Split(string(out), "\n"))
	for _, l := range added {
		if strings.Contains(l, "envVariables") || strings.Contains(l, ": ") && !strings.Contains(l, "start:") && !strings.Contains(l, "prepare") && !strings.HasPrefix(strings.TrimSpace(l), "- ") {
			t.Errorf("wiring added an env-like line %q", l)
		}
	}
}

// TestRepinSupervisorYAML_ReplacesOnlyTheInstallLineTheDevSetupCarries
// (docs/spec-mate.md §2.8a.2): re-pinning swaps the zcp-devsup line of the dev
// setup for the newest one and names the download it replaced; the rest of
// the file, other setups and run.start stay as written, and a setup without
// the line or already on the newest pin is not touched.
func TestRepinSupervisorYAML_ReplacesOnlyTheInstallLineTheDevSetupCarries(t *testing.T) {
	t.Parallel()
	const older = "https://github.com/zeropsio/zcp/releases/download/v9.200.0/zcp-devsup-linux-amd64"
	setups := func(devURL, prodURL string) string {
		return `zerops:
  - setup: dev
    run:
      base: bun@1.3
      prepareCommands:
        - echo before   # kept
        - ` + yamlQuote(DevsupPrepareCommand(devURL)) + `
      start: zcp-devsup run --root /var/www/ws
  - setup: prod
    run:
      prepareCommands: [` + yamlQuote(DevsupPrepareCommand(prodURL)) + `]
      start: bun index.ts
`
	}
	tests := []struct {
		name, yaml, want, wantFrom string
	}{
		{name: "an older pin", yaml: setups(older, older), want: setups(testDevsupURL, older), wantFrom: older},
		{name: "the newest pin", yaml: setups(testDevsupURL, older), want: setups(testDevsupURL, older)},
		{name: "no supervisor line", yaml: "zerops:\n  - setup: dev\n    run:\n      start: bun index.ts\n", want: "zerops:\n  - setup: dev\n    run:\n      start: bun index.ts\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, used, from, err := RepinSupervisorYAML([]byte(tt.yaml), "dev", "appdev", testDevsupURL)
			if err != nil {
				t.Fatalf("RepinSupervisorYAML: %v", err)
			}
			if string(out) != tt.want || from != tt.wantFrom || used != "dev" {
				t.Errorf("RepinSupervisorYAML = from %q, setup %q:\n%s\nwant from %q:\n%s", from, used, out, tt.wantFrom, tt.want)
			}
		})
	}
}

package ops

import (
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
// run.start `zcp-devsup run`; everything else in the file stays.
func TestAdoptSupervisorYAML(t *testing.T) {
	t.Parallel()
	line := DevsupPrepareCommand(testDevsupURL)
	tests := []struct {
		name, yaml, setup, hostname string
		wantSetup                   string
		wantPrepare                 []string
		wantChanged                 bool
		wantErr                     string
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
			name: "an older zcp-devsup line is replaced, not doubled",
			yaml: `zerops:
  - setup: dev
    run:
      prepareCommands:
        - sudo sh -c 'curl -fsSL https://github.com/zeropsio/zcp/releases/download/v9.299.0/zcp-devsup-linux-amd64 -o /usr/local/bin/zcp-devsup && chmod 0755 /usr/local/bin/zcp-devsup'
      start: zcp-devsup run
`,
			hostname:  "appdev",
			wantSetup: "dev", wantPrepare: []string{line}, wantChanged: true,
		},
		{
			name:      "already on the supervisor",
			yaml:      "zerops:\n  - setup: dev\n    run:\n      prepareCommands:\n        - " + yamlQuote(line) + "\n      start: zcp-devsup run\n",
			hostname:  "appdev",
			wantSetup: "dev", wantPrepare: []string{line}, wantChanged: false,
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
			out, used, changed, err := AdoptSupervisorYAML([]byte(tt.yaml), tt.setup, tt.hostname, testDevsupURL)
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
			if start != DevsupStart {
				t.Errorf("run.start = %q, want %q", start, DevsupStart)
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

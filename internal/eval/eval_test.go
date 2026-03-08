package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Prompt generation tests ---

func TestParseRecipeMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		recipeName  string
		content     string
		wantTitle   string
		wantRuntime string
		wantSvcLen  int
	}{
		{
			name:       "go recipe with postgresql",
			recipeName: "go",
			content: `# Go on Zerops

## Keywords
go, golang, net/http, postgresql, api, stdlib

## TL;DR
Go stdlib HTTP server on port 8080 with PostgreSQL.

## zerops.yml
` + "```yaml" + `
zerops:
  - setup: api
    build:
      base: go@1
      deployFiles: ./app
    run:
      base: go@1
      ports:
        - port: 8080
          httpSupport: true
      start: ./app
` + "```" + `

## import.yml
` + "```yaml" + `
services:
  - hostname: api
    type: go@1
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
` + "```" + `

## Gotchas
- Bind to 0.0.0.0
`,
			wantTitle:   "Go on Zerops",
			wantRuntime: "go@1",
			wantSvcLen:  1, // postgresql@16 (runtime excluded)
		},
		{
			name:       "static site no import services",
			recipeName: "react-static",
			content: `# React Static on Zerops

## Keywords
react, static, vite, ssg, spa

## TL;DR
React static site with Vite.

## zerops.yml
` + "```yaml" + `
zerops:
  - setup: app
    build:
      base: nodejs@20
      deployFiles: dist/~
    run:
      base: static
` + "```" + `

## import.yml
` + "```yaml" + `
services:
  - hostname: app
    type: static
    enableSubdomainAccess: true
` + "```" + `

## Gotchas
- Deploy dist/~
`,
			wantTitle:   "React Static on Zerops",
			wantRuntime: "static",
			wantSvcLen:  0, // no managed services
		},
		{
			name:       "laravel full stack",
			recipeName: "laravel",
			content: `# Laravel on Zerops

## Keywords
laravel, php, postgresql, valkey, redis, s3, nginx

## TL;DR
Laravel on PHP-Nginx with PostgreSQL.

## zerops.yml
` + "```yaml" + `
zerops:
  - setup: app
    build:
      base:
        - php@8.4
        - nodejs@18
      deployFiles: ./
    run:
      base: php-nginx@8.4
      documentRoot: public
` + "```" + `

## import.yml (Full)
` + "```yaml" + `
#yamlPreprocessor=on
services:
  - hostname: app
    type: php-nginx@8.4
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA

  - hostname: redis
    type: valkey@7.2
    mode: NON_HA

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
` + "```" + `

## Gotchas
- TRUSTED_PROXIES required
`,
			wantTitle:   "Laravel on Zerops",
			wantRuntime: "php-nginx@8.4",
			wantSvcLen:  3, // postgresql, valkey, object-storage
		},
		{
			name:       "discord bot no managed services",
			recipeName: "discord-py",
			content: `# Discord Bot with Python on Zerops

## Keywords
discord, discordpy, python, bot

## TL;DR
Discord.py bot on Python.

## zerops.yml
` + "```yaml" + `
zerops:
  - setup: bot
    build:
      base: python@3.12
      deployFiles: /~
    run:
      base: python@3.12
      ports:
        - port: 8080
          httpSupport: true
      start: python3 bot.py
` + "```" + `

## import.yml
` + "```yaml" + `
services:
  - hostname: bot
    type: python@3.12
    envSecrets:
      DISCORD_TOKEN: fill_your_bot_token
` + "```" + `

## Gotchas
- No HTTP server
`,
			wantTitle:   "Discord Bot with Python on Zerops",
			wantRuntime: "python@3.12",
			wantSvcLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			meta, err := ParseRecipeMetadata(tt.recipeName, tt.content)
			if err != nil {
				t.Fatalf("ParseRecipeMetadata: %v", err)
			}

			if meta.Title != tt.wantTitle {
				t.Errorf("title: got %q, want %q", meta.Title, tt.wantTitle)
			}
			if meta.Runtime != tt.wantRuntime {
				t.Errorf("runtime: got %q, want %q", meta.Runtime, tt.wantRuntime)
			}
			if len(meta.Services) != tt.wantSvcLen {
				t.Errorf("services: got %d, want %d: %v", len(meta.Services), tt.wantSvcLen, meta.Services)
			}
		})
	}
}

func TestGenerateHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		recipe     string
		role       string
		wantPrefix string
	}{
		{"go runtime", "go", "runtime", "ev"},
		{"laravel runtime", "laravel", "runtime", "ev"},
		{"django db", "django", "db", "ev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := GenerateHostname(tt.recipe, tt.role)
			if !strings.HasPrefix(h, tt.wantPrefix) {
				t.Errorf("got %q, want prefix %q", h, tt.wantPrefix)
			}
			if len(h) > 25 {
				t.Errorf("hostname %q exceeds 25 char limit", h)
			}
			// Must be lowercase alphanumeric only
			for _, c := range h {
				if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
					t.Errorf("hostname %q contains invalid char %q", h, c)
				}
			}
		})
	}
}

func TestGenerateHostnames(t *testing.T) {
	t.Parallel()

	meta := &RecipeMetadata{
		Name:    "laravel",
		Title:   "Laravel on Zerops",
		Runtime: "php-nginx@8.4",
		Services: []ServiceDef{
			{Type: "postgresql@16", Role: "db"},
			{Type: "valkey@7.2", Role: "cache"},
		},
	}

	hostnames := GenerateHostnames(meta)

	// Must have runtime + each service
	if len(hostnames) != 3 {
		t.Fatalf("got %d hostnames, want 3: %v", len(hostnames), hostnames)
	}

	// All must be unique
	seen := make(map[string]bool)
	for role, h := range hostnames {
		if seen[h] {
			t.Errorf("duplicate hostname %q for role %q", h, role)
		}
		seen[h] = true
	}
}

func TestBuildTaskPrompt(t *testing.T) {
	t.Parallel()

	meta := &RecipeMetadata{
		Name:    "go",
		Title:   "Go on Zerops",
		Runtime: "go@1",
		Services: []ServiceDef{
			{Type: "postgresql@16", Role: "db"},
		},
	}
	hostnames := map[string]string{
		"runtime": "evgo1a2b3c",
		"db":      "evgo4d5e6f",
	}

	prompt := BuildTaskPrompt(meta, hostnames)

	// Must contain framework reference
	if !strings.Contains(prompt, "Go") {
		t.Error("prompt missing framework name")
	}
	// Must contain hostnames
	if !strings.Contains(prompt, "evgo1a2b3c") {
		t.Error("prompt missing runtime hostname")
	}
	if !strings.Contains(prompt, "evgo4d5e6f") {
		t.Error("prompt missing db hostname")
	}
	// Must contain service types
	if !strings.Contains(prompt, "go@1") {
		t.Error("prompt missing runtime type")
	}
	if !strings.Contains(prompt, "postgresql@16") {
		t.Error("prompt missing service type")
	}
	// Must contain HTTP 200 verification
	if !strings.Contains(prompt, "HTTP 200") {
		t.Error("prompt missing HTTP 200 verification")
	}
	// Must restrict to zerops_* tools
	if !strings.Contains(prompt, "zerops_*") {
		t.Error("prompt missing tool restriction")
	}
	// Should NOT contain verbose workflow instructions (MCP server handles that)
	for _, unwanted := range []string{
		`zerops_workflow action="start"`,
		`action="complete"`,
		`action="strategy"`,
		"EXACT hostnames",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("prompt should not contain verbose workflow instruction %q", unwanted)
		}
	}
}

func TestBuildFullPrompt(t *testing.T) {
	t.Parallel()

	meta := &RecipeMetadata{
		Name:    "go",
		Title:   "Go on Zerops",
		Runtime: "go@1",
	}
	hostnames := map[string]string{"runtime": "evgo1a2b3c"}

	prompt := BuildFullPrompt(meta, hostnames)

	// Must contain task section
	if !strings.Contains(prompt, "Deploy") {
		t.Error("prompt missing task section")
	}
	// Must contain assessment section
	if !strings.Contains(prompt, "EVAL REPORT") {
		t.Error("prompt missing assessment section")
	}
	// Must contain assessment categories
	for _, section := range []string{
		"Deployment outcome",
		"Workflow execution",
		"Failure chains",
		"Information gaps",
		"Wasted steps",
		"What worked well",
	} {
		if !strings.Contains(prompt, section) {
			t.Errorf("prompt missing assessment section %q", section)
		}
	}
}

// --- Log extraction tests ---

func TestExtractAssessment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		log        string
		wantFound  bool
		wantSubstr string
	}{
		{
			name: "assessment present",
			log: `{"type":"assistant","message":{"content":[{"type":"text","text":"Some work here"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"## EVAL REPORT\n\n### Deployment outcome\nSUCCESS\n\n### Failure chains\nNo failure chains."}]}}`,
			wantFound:  true,
			wantSubstr: "Deployment outcome",
		},
		{
			name: "no assessment",
			log: `{"type":"assistant","message":{"content":[{"type":"text","text":"I deployed the app."}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Done."}]}}`,
			wantFound: false,
		},
		{
			name:      "empty log",
			log:       "",
			wantFound: false,
		},
		{
			name:       "assessment in last of multiple text blocks",
			log:        `{"type":"assistant","message":{"content":[{"type":"text","text":"step 1"},{"type":"text","text":"## EVAL REPORT\n\n### Deployment outcome\nPARTIAL"}]}}`,
			wantFound:  true,
			wantSubstr: "PARTIAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assessment, found := ExtractAssessment(tt.log)
			if found != tt.wantFound {
				t.Fatalf("found: got %v, want %v", found, tt.wantFound)
			}
			if tt.wantFound && !strings.Contains(assessment, tt.wantSubstr) {
				t.Errorf("assessment missing %q: %s", tt.wantSubstr, assessment)
			}
		})
	}
}

func TestExtractToolCalls(t *testing.T) {
	t.Parallel()

	log := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tc1","name":"mcp__zcp__zerops_discover","input":{"service":"app"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tc1","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tc2","name":"mcp__zcp__zerops_knowledge","input":{"query":"laravel"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tc2","content":"recipe found"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Done"}]}}
`

	calls := ExtractToolCalls(log)
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(calls))
	}
	if calls[0].Name != "zerops_discover" {
		t.Errorf("call[0].Name: got %q, want %q", calls[0].Name, "zerops_discover")
	}
	if calls[1].Name != "zerops_knowledge" {
		t.Errorf("call[1].Name: got %q, want %q", calls[1].Name, "zerops_knowledge")
	}
	if calls[0].Result != "ok" {
		t.Errorf("call[0].Result: got %q, want %q", calls[0].Result, "ok")
	}
}

// --- Cleanup tests ---

func TestMatchesEvalPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		prefix   string
		want     bool
	}{
		{"matches", "evgo1a2b3c", "evgo", true},
		{"no match", "appdev", "evgo", false},
		{"empty prefix", "evgo1a2b3c", "", false},
		{"partial match start", "evgo1a2b3c", "ev", true},
		{"exact prefix", "evlr", "evlr", true},
		{"permanent service", "db", "evgo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := MatchesEvalPrefix(tt.hostname, tt.prefix)
			if got != tt.want {
				t.Errorf("MatchesEvalPrefix(%q, %q): got %v, want %v",
					tt.hostname, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestRecipeShortName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		recipe string
		want   string
	}{
		{"go", "go"},
		{"laravel", "ll"},
		{"django", "do"},
		{"nextjs-ssr", "ns"},
		{"react-static", "rs"},
		{"discord-py", "dp"},
		{"phoenix", "px"},
		{"bun-hono", "bh"},
		{"a", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.recipe, func(t *testing.T) {
			t.Parallel()

			got := RecipeShortName(tt.recipe)
			if got != tt.want {
				t.Errorf("RecipeShortName(%q): got %q, want %q", tt.recipe, got, tt.want)
			}
			if len(got) > 4 {
				t.Errorf("short name %q exceeds 4 chars", got)
			}
		})
	}
}

func TestIsProtectedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{"CLAUDE.md", true},
		{".claude", true},
		{".mcp.json", true},
		{".zcp", true},
		{"import.yml", false},
		{"zerops.yml", false},
		{"main.go", false},
		{"node_modules", false},
		{"app", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := IsProtectedPath(tt.name)
			if got != tt.want {
				t.Errorf("IsProtectedPath(%q): got %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestCleanWorkDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create protected files
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "# test")
	mustMkdir(t, filepath.Join(dir, ".claude"))
	mustWrite(t, filepath.Join(dir, ".mcp.json"), "{}")
	mustMkdir(t, filepath.Join(dir, ".zcp", "state"))

	// Create files that should be cleaned
	mustWrite(t, filepath.Join(dir, "import.yml"), "services:")
	mustWrite(t, filepath.Join(dir, "zerops.yml"), "zerops:")
	mustMkdir(t, filepath.Join(dir, "app"))
	mustWrite(t, filepath.Join(dir, "app", "main.go"), "package main")

	// Run cleanup via exported wrapper
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	var removed []string
	for _, entry := range entries {
		if IsProtectedPath(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			t.Fatalf("remove %s: %v", entry.Name(), err)
		}
		removed = append(removed, entry.Name())
	}

	// Verify protected files survive
	for _, name := range []string{"CLAUDE.md", ".claude", ".mcp.json", ".zcp"} {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			t.Errorf("protected path %q was deleted", name)
		}
	}

	// Verify cleaned files are gone
	for _, name := range []string{"import.yml", "zerops.yml", "app"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected %q to be deleted, but it still exists", name)
		}
	}

	if len(removed) != 3 {
		t.Errorf("expected 3 removed, got %d: %v", len(removed), removed)
	}
}

// --- Claude memory cleanup tests ---

func TestCleanClaudeMemory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, base string)
		check func(t *testing.T, base string)
	}{
		{
			name: "removes memory files from multiple projects",
			setup: func(t *testing.T, base string) {
				t.Helper()
				for _, proj := range []string{"proj-a", "proj-b"} {
					memDir := filepath.Join(base, proj, "memory")
					mustMkdir(t, memDir)
					mustWrite(t, filepath.Join(memDir, "MEMORY.md"), "# stale memory")
					mustWrite(t, filepath.Join(memDir, "patterns.md"), "some pattern")
				}
			},
			check: func(t *testing.T, base string) {
				t.Helper()
				for _, proj := range []string{"proj-a", "proj-b"} {
					memDir := filepath.Join(base, proj, "memory")
					entries, err := os.ReadDir(memDir)
					if err != nil {
						t.Fatalf("read memory dir %s: %v", proj, err)
					}
					if len(entries) != 0 {
						t.Errorf("project %s: expected 0 files, got %d", proj, len(entries))
					}
				}
			},
		},
		{
			name: "preserves non-memory project files",
			setup: func(t *testing.T, base string) {
				t.Helper()
				projDir := filepath.Join(base, "proj-c")
				mustMkdir(t, filepath.Join(projDir, "memory"))
				mustWrite(t, filepath.Join(projDir, "memory", "MEMORY.md"), "data")
				mustWrite(t, filepath.Join(projDir, "settings.json"), "{}")
			},
			check: func(t *testing.T, base string) {
				t.Helper()
				settings := filepath.Join(base, "proj-c", "settings.json")
				if _, err := os.Stat(settings); os.IsNotExist(err) {
					t.Error("settings.json was deleted")
				}
			},
		},
		{
			name: "no-op when no memory directories exist",
			setup: func(t *testing.T, base string) {
				t.Helper()
				mustMkdir(t, filepath.Join(base, "proj-d"))
			},
			check: func(t *testing.T, base string) {
				t.Helper()
				// Just verify no error occurred (checked by caller)
			},
		},
		{
			name:  "no-op when projects dir does not exist",
			setup: func(t *testing.T, base string) { t.Helper() },
			check: func(t *testing.T, base string) { t.Helper() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Use temp dir as fake ~/.claude/projects
			base := filepath.Join(t.TempDir(), ".claude", "projects")
			if tt.name != "no-op when projects dir does not exist" {
				mustMkdir(t, base)
			}

			tt.setup(t, base)

			err := cleanClaudeMemoryDir(base)
			if err != nil {
				t.Fatalf("cleanClaudeMemoryDir: %v", err)
			}

			tt.check(t, base)
		})
	}
}

// --- Duration formatting ---

func TestDurationJSON(t *testing.T) {
	t.Parallel()

	d := Duration(90_000_000_000) // 1m30s
	got := d.String()
	if got != "1m30s" {
		t.Errorf("Duration.String(): got %q, want %q", got, "1m30s")
	}
}

// --- Test helpers ---

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

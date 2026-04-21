package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// runSymbolContractCheck invokes checkSymbolContractEnvVarConsistency and
// converts output to the test-local shim type.
func runSymbolContractCheck(t *testing.T, projectRoot string, contract workflow.SymbolContract) []workflowStepCheckShim {
	t.Helper()
	checks := checkSymbolContractEnvVarConsistency(t.Context(), projectRoot, contract)
	out := make([]workflowStepCheckShim, 0, len(checks))
	for _, c := range checks {
		out = append(out, workflowStepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
	}
	return out
}

// writeCodebaseFiles seeds a codebase mount at projectRoot/{host}dev/ with
// the given relative files + contents.
func writeCodebaseFiles(t *testing.T, projectRoot, host string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(projectRoot, host+"dev", rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// minimalDBContract returns a contract declaring a `db` kind with hostname
// "db" and the canonical postgres-style env var names. Exercises the v34
// class surface (DB_PASSWORD vs DB_PASS).
func minimalDBContract() workflow.SymbolContract {
	return workflow.SymbolContract{
		EnvVarsByKind: map[string]map[string]string{
			"db": {
				"host": "DB_HOST",
				"port": "DB_PORT",
				"user": "DB_USER",
				"pass": "DB_PASSWORD",
				"name": "DB_DBNAME",
			},
		},
	}
}

func TestSymbolContractEnvVarConsistency_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		contract   workflow.SymbolContract
		files      map[string]map[string]string // host → (rel → content)
		wantStatus string
		wantDetail []string
	}{
		{
			name:     "all codebases use canonical DB_PASSWORD passes",
			contract: minimalDBContract(),
			files: map[string]map[string]string{
				"api": {
					"src/db.ts":    "const p = process.env.DB_PASSWORD;\n",
					".env.example": "DB_PASSWORD=\n",
				},
				"worker": {
					"src/db.ts": "const p = process.env.DB_PASSWORD;\n",
				},
			},
			wantStatus: "pass",
		},
		{
			name:     "codebase uses sibling DB_PASS fails (v34 class)",
			contract: minimalDBContract(),
			files: map[string]map[string]string{
				"api": {
					"src/db.ts": "const p = process.env.DB_PASS;\n",
				},
				"worker": {
					"src/db.ts": "const p = process.env.DB_PASSWORD;\n",
				},
			},
			wantStatus: "fail",
			wantDetail: []string{"DB_PASS", "DB_PASSWORD", "api"},
		},
		{
			name:     "sibling in .env.example caught",
			contract: minimalDBContract(),
			files: map[string]map[string]string{
				"api": {
					".env.example": "DB_PWD=secret\n",
				},
			},
			wantStatus: "fail",
			wantDetail: []string{"DB_PWD", "DB_PASSWORD"},
		},
		{
			name:     "sibling in zerops.yaml caught",
			contract: minimalDBContract(),
			files: map[string]map[string]string{
				"api": {
					"zerops.yaml": "zerops:\n  - setup: prod\n    run:\n      envVariables:\n        DB_PASS: ${db_password}\n",
				},
			},
			wantStatus: "fail",
			wantDetail: []string{"DB_PASS"},
		},
		{
			name:     "DB_USER vs DB_USERNAME sibling detected",
			contract: minimalDBContract(),
			files: map[string]map[string]string{
				"api": {
					"src/config.ts": "const u = process.env.DB_USERNAME;\n",
				},
			},
			wantStatus: "fail",
			wantDetail: []string{"DB_USERNAME", "DB_USER"},
		},
		{
			name:     "non-matching tokens ignored (IG has TOKEN but not DB_ prefix)",
			contract: minimalDBContract(),
			files: map[string]map[string]string{
				"api": {
					"src/jwt.ts": "const t = process.env.JWT_SECRET;\n",
				},
			},
			wantStatus: "pass",
		},
		{
			name:     "empty contract passes vacuously",
			contract: workflow.SymbolContract{},
			files: map[string]map[string]string{
				"api": {
					"src/db.ts": "const p = process.env.ANYTHING;\n",
				},
			},
			wantStatus: "pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for host, files := range tt.files {
				writeCodebaseFiles(t, root, host, files)
			}
			got := runSymbolContractCheck(t, root, tt.contract)
			check := findCheckByName(got, "symbol_contract_env_var_consistency")
			if check == nil {
				t.Fatalf("expected symbol_contract_env_var_consistency check, got %+v", got)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
			for _, w := range tt.wantDetail {
				if !strings.Contains(check.Detail, w) {
					t.Errorf("detail missing %q; full: %s", w, check.Detail)
				}
			}
		})
	}
}

// TestSymbolContractEnvVarConsistency_EmptyProjectRoot: a missing /
// inaccessible project root is a graceful pass (upstream concern).
func TestSymbolContractEnvVarConsistency_EmptyProjectRoot(t *testing.T) {
	t.Parallel()
	got := runSymbolContractCheck(t, "/nothing/here", minimalDBContract())
	check := findCheckByName(got, "symbol_contract_env_var_consistency")
	if check == nil {
		t.Fatal("expected check emitted on missing root")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass on missing root, got %q", check.Status)
	}
}

// TestSymbolContractEnvVarConsistency_NoCodebaseDirs: a projectRoot with no
// `*dev` subdirectories (fresh tempdir) should pass — nothing to scan.
func TestSymbolContractEnvVarConsistency_NoCodebaseDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := runSymbolContractCheck(t, root, minimalDBContract())
	check := findCheckByName(got, "symbol_contract_env_var_consistency")
	if check == nil {
		t.Fatal("expected check emitted")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass with no codebases, got %q (detail: %s)", check.Status, check.Detail)
	}
}

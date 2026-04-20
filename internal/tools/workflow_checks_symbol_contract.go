package tools

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// sourceFileExtensions are the filename suffixes scanned inside a codebase
// mount's src/ tree. Lowercased; filepath.Ext(...) is lowercased before
// membership check so the scan is case-insensitive.
var sourceFileExtensions = map[string]bool{
	".ts":   true,
	".tsx":  true,
	".js":   true,
	".jsx":  true,
	".mjs":  true,
	".cjs":  true,
	".php":  true,
	".go":   true,
	".py":   true,
	".rb":   true,
	".java": true,
	".kt":   true,
	".cs":   true,
}

// envVarTokenRegexp extracts bare ALL_CAPS_UPPERCASE tokens with underscores
// from a source line. Matches `DB_PASS`, `DB_PASSWORD`, `DB_apiUrl` is
// excluded (mixed case) — object-storage contracts use the `H_apiUrl`
// pattern so the scan is case-insensitive but anchored at an uppercase
// letter or digit for the first character to avoid matching ordinary
// identifiers.
var envVarTokenRegexp = regexp.MustCompile(`\b[A-Z][A-Za-z0-9_]{2,}\b`)

// siblingSuffixes maps a canonical slot suffix to confusable aliases. These
// are the empirical v20–v34 cross-scaffold divergences: different codebases
// on the same recipe picked different names for the same conceptual slot
// because the scaffold briefs didn't share a symbol contract.
var siblingSuffixes = map[string][]string{
	"PASSWORD": {"PASS", "PWD", "SECRET"},
	"PASS":     {"PASSWORD", "PWD", "SECRET"},
	"USER":     {"USERNAME", "UNAME"},
	"USERNAME": {"USER", "UNAME"},
	"DBNAME":   {"DB", "DATABASE", "DB_NAME"},
	"DATABASE": {"DB", "DBNAME", "DB_NAME"},
	"HOST":     {"HOSTNAME", "SERVER"},
	"HOSTNAME": {"HOST", "SERVER"},
	"apiUrl":   {"API_URL", "URL", "ENDPOINT"},
	"apiHost":  {"API_HOST", "HOSTNAME"},
}

// maxEnvVarConsistencyExamples caps the number of offending (host, file,
// token) triples in Detail so the message stays readable when a scaffold
// sprays the wrong name across an entire codebase.
const maxEnvVarConsistencyExamples = 10

// checkSymbolContractEnvVarConsistency asserts that every scaffolded
// codebase references the canonical env-var names declared in
// plan.SymbolContract.EnvVarsByKind. Closes the v34 class where apidev used
// `DB_PASSWORD` while workerdev used `DB_PASS` because the scaffold briefs
// didn't share a symbol contract — both codebases crashed at startup until
// one was rewritten to match.
//
// Scope per check-rewrite.md §16: `{host}/src/**/*.{ts,js,php,go,...}` +
// `{host}/.env.example` + `{host}/zerops.yaml`. `{host}` resolves to any
// `*dev` subdirectory of projectRoot — the scaffold convention. Passes
// vacuously when the contract declares no env-var expectations (e.g. a
// hello-world recipe with no managed services).
func checkSymbolContractEnvVarConsistency(projectRoot string, contract workflow.SymbolContract) []workflow.StepCheck {
	if info, err := os.Stat(projectRoot); err != nil || !info.IsDir() {
		return []workflow.StepCheck{{
			Name:   "symbol_contract_env_var_consistency",
			Status: statusPass,
		}}
	}
	canonical := canonicalEnvVarSet(contract)
	if len(canonical) == 0 {
		return []workflow.StepCheck{{
			Name:   "symbol_contract_env_var_consistency",
			Status: statusPass,
		}}
	}
	siblings := buildSiblingMap(canonical)
	if len(siblings) == 0 {
		return []workflow.StepCheck{{
			Name:   "symbol_contract_env_var_consistency",
			Status: statusPass,
		}}
	}

	type violation struct {
		host     string
		file     string
		sibling  string
		expected string
	}
	var viols []violation

	entries, _ := os.ReadDir(projectRoot)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), "dev") {
			continue
		}
		host := strings.TrimSuffix(entry.Name(), "dev")
		codebaseRoot := filepath.Join(projectRoot, entry.Name())
		files := collectSymbolContractScanFiles(codebaseRoot)
		for _, fp := range files {
			data, err := os.ReadFile(fp)
			if err != nil {
				continue
			}
			for _, tok := range envVarTokenRegexp.FindAllString(string(data), -1) {
				canon, isSibling := siblings[tok]
				if !isSibling || canon == tok {
					continue
				}
				rel, relErr := filepath.Rel(projectRoot, fp)
				if relErr != nil {
					rel = fp
				}
				viols = append(viols, violation{
					host:     host,
					file:     filepath.ToSlash(rel),
					sibling:  tok,
					expected: canon,
				})
			}
		}
	}

	if len(viols) == 0 {
		return []workflow.StepCheck{{
			Name:   "symbol_contract_env_var_consistency",
			Status: statusPass,
		}}
	}

	dedup := map[string]violation{}
	for _, v := range viols {
		key := v.host + "|" + v.file + "|" + v.sibling
		if _, exists := dedup[key]; !exists {
			dedup[key] = v
		}
	}
	unique := make([]violation, 0, len(dedup))
	for _, v := range dedup {
		unique = append(unique, v)
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].host != unique[j].host {
			return unique[i].host < unique[j].host
		}
		if unique[i].file != unique[j].file {
			return unique[i].file < unique[j].file
		}
		return unique[i].sibling < unique[j].sibling
	})
	examples := make([]string, 0, maxEnvVarConsistencyExamples)
	for i, v := range unique {
		if i >= maxEnvVarConsistencyExamples {
			break
		}
		examples = append(examples, fmt.Sprintf("%s uses %s (expected %s) in %s", v.host, v.sibling, v.expected, v.file))
	}
	more := ""
	if len(unique) > maxEnvVarConsistencyExamples {
		more = fmt.Sprintf(" (+%d more)", len(unique)-maxEnvVarConsistencyExamples)
	}
	return []workflow.StepCheck{{
		Name:   "symbol_contract_env_var_consistency",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%d env-var divergence(s) from plan.SymbolContract.EnvVarsByKind: %s%s. v34 class: apidev used DB_PASSWORD while workerdev used DB_PASS — both crashed at startup until one was rewritten. Fix: replace the sibling token(s) with the canonical name declared in the contract; all scaffolded codebases must agree.",
			len(unique), strings.Join(examples, "; "), more,
		),
	}}
}

// canonicalEnvVarSet flattens contract.EnvVarsByKind into a set of canonical
// env-var names. Order not preserved (the siblings map keys on string).
func canonicalEnvVarSet(contract workflow.SymbolContract) map[string]bool {
	set := map[string]bool{}
	for _, byKind := range contract.EnvVarsByKind {
		for _, name := range byKind {
			if name != "" {
				set[name] = true
			}
		}
	}
	return set
}

// buildSiblingMap derives a sibling→canonical lookup from the canonical
// env-var set. For each canonical name, it generates the sibling-suffix
// variants (e.g. DB_PASSWORD → DB_PASS / DB_PWD / DB_SECRET) and maps each
// variant back to the canonical. Canonical tokens themselves are included
// in the map (value = itself) so the caller can cheaply detect "token IS
// canonical" vs "token is a known sibling of a different canonical".
func buildSiblingMap(canonical map[string]bool) map[string]string {
	out := map[string]string{}
	for name := range canonical {
		out[name] = name
	}
	for name := range canonical {
		for suffix, aliases := range siblingSuffixes {
			if !strings.HasSuffix(name, suffix) {
				continue
			}
			prefix := strings.TrimSuffix(name, suffix)
			for _, alt := range aliases {
				sibling := prefix + alt
				if sibling == name {
					continue
				}
				if canonical[sibling] {
					// Another canonical claims the sibling — don't flag
					// as a violation; the scaffolded contract already
					// considers both names first-class (e.g. storage
					// contracts carry both apiUrl and apiHost).
					continue
				}
				out[sibling] = name
			}
		}
	}
	return out
}

// collectSymbolContractScanFiles walks a codebase root for files the
// symbol-contract check scans: src/** with a source-file extension, plus
// .env.example and zerops.yaml/.yml at the root.
func collectSymbolContractScanFiles(codebaseRoot string) []string {
	var files []string
	srcRoot := filepath.Join(codebaseRoot, "src")
	if info, err := os.Stat(srcRoot); err == nil && info.IsDir() {
		_ = filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil //nolint:nilerr // best-effort scan; unreadable subtree means "no divergence there", not a check failure
			}
			if d.IsDir() {
				return nil
			}
			if sourceFileExtensions[strings.ToLower(filepath.Ext(path))] {
				files = append(files, path)
			}
			return nil
		})
	}
	for _, name := range []string{".env.example", "zerops.yaml", "zerops.yml"} {
		full := filepath.Join(codebaseRoot, name)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			files = append(files, full)
		}
	}
	return files
}

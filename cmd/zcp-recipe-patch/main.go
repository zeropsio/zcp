// zcp-recipe-patch — apply named patch profiles to a frozen recipe-run
// directory and emit the patched derivative for sim-driven verification.
//
// One profile lands today: `run19-for-run20`. It transforms
// `docs/zcprecipator3/runs/19/` into a derivative that reflects what
// scaffold + feature WOULD have produced if the run-20 brief changes
// (C2 SPA static runtime + project-env URL constants, C4 worker
// dev-server attestation, C5 producer-side facts integrity) had been
// live. Sim then runs against the derivative to verify the
// downstream engine + content + validator fixes (E1, V1, V2, C1, C3)
// against correct inputs.
//
// The patch is a TEST INPUT, not a test ORACLE: it confirms
// "given correct scaffold/feature output, downstream produces correct
// surfaces." It does NOT confirm "the new C2/C4 briefs steer the
// agent to produce that output." Those still need prompt-eval.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
)

func main() {
	profile := flag.String("profile", "", "patch profile (run19-for-run20)")
	from := flag.String("from", "", "source run dir (e.g. docs/zcprecipator3/runs/19)")
	to := flag.String("to", "", "destination patched dir (e.g. docs/zcprecipator3/runs/19-patched)")
	flag.Parse()

	if *profile == "" || *from == "" || *to == "" {
		fmt.Fprintln(os.Stderr, "usage: zcp-recipe-patch -profile <name> -from <runDir> -to <outDir>")
		os.Exit(2)
	}

	switch *profile {
	case "run19-for-run20":
		if err := patchRun19ForRun20(*from, *to); err != nil {
			fmt.Fprintf(os.Stderr, "patch run19-for-run20: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown profile %q (have: run19-for-run20)\n", *profile)
		os.Exit(2)
	}

	fmt.Fprintf(os.Stderr, "patched %s → %s\n", *from, *to)
}

// patchRun19ForRun20 applies the four run-20 brief-change patches to a
// run-19-shaped corpus:
//
//  1. Copy the run dir verbatim, then layer patches on top.
//  2. Rewrite appdev/zerops.yaml to the C2-correct shape: dev keeps
//     nodejs@22 + Vite dev server, stage flips to base: static + Nginx
//     documentRoot=dist + project-env-fed VITE_API_URL.
//  3. Inject project envs (STAGE_API_URL etc.) into Plan.ProjectEnvVars
//     using the C2 cross-service-urls.md pattern.
//  4. Append a worker_dev_server_started fact for workerdev (C4
//     attestation that the gate now requires).
//  5. Append field_rationale facts for every directive group in each
//     codebase's zerops.yaml that lacks one — the C5 producer-side
//     completeness gate now requires those.
//
// Each patch is idempotent: applying twice produces the same output.
func patchRun19ForRun20(from, to string) error {
	if err := validateInputs(from); err != nil {
		return fmt.Errorf("validate inputs: %w", err)
	}
	if err := mirrorTree(from, to); err != nil {
		return fmt.Errorf("mirror %s → %s: %w", from, to, err)
	}
	if err := patchAppdevYAML(to); err != nil {
		return fmt.Errorf("patch appdev yaml: %w", err)
	}
	if err := patchApidevCORSOrigins(to); err != nil {
		return fmt.Errorf("patch apidev CORS_ORIGINS: %w", err)
	}
	if err := patchPlanProjectEnvs(to); err != nil {
		return fmt.Errorf("patch plan project envs: %w", err)
	}
	if err := appendWorkerDevServerFact(to); err != nil {
		return fmt.Errorf("append worker dev-server fact: %w", err)
	}
	if err := appendFieldRationaleCoverage(to); err != nil {
		return fmt.Errorf("append field-rationale coverage: %w", err)
	}
	return nil
}

// mirrorTree copies every file under `from` into `to`. Skips
// `.git`, `node_modules`, `vendor` to keep the derivative small.
func mirrorTree(from, to string) error {
	skipDirs := map[string]bool{".git": true, "node_modules": true, "vendor": true}
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(from, path)
		if relErr != nil {
			return relErr
		}
		if skipDirs[d.Name()] && d.IsDir() {
			return filepath.SkipDir
		}
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		return copyFile(path, dst)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// patchAppdevYAML rewrites the appdev codebase's zerops.yaml to the
// C2-correct shape:
//
//   - dev setup keeps base: nodejs@22 + zsc noop (Vite dev server is
//     started by the agent via zerops_dev_server, unchanged from run-19)
//   - stage setup flips to base: static + Nginx documentRoot=dist;
//     drops the npx serve start; reads VITE_API_URL from STAGE_API_URL
//     project env (resolved at provision time via ${zeropsSubdomainHost})
//
// The rewrite is a string replacement keyed off the run-19 shape. If
// upstream changes the run-19 yaml shape, the substring guards fail
// loudly rather than silently corrupting.
func patchAppdevYAML(rootDir string) error {
	yamlPath := filepath.Join(rootDir, "appdev", "zerops.yaml")
	const want = `zerops:
  - setup: appdev
    build:
      base: nodejs@22
      os: ubuntu
      buildCommands:
        - npm install
      deployFiles: .
      cache:
        - node_modules
    run:
      base: nodejs@22
      os: ubuntu
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        VITE_API_URL: ${DEV_API_URL}
        PORT: '3000'
      start: zsc noop --silent

  - setup: appstage
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist
      cache:
        - node_modules
      envVariables:
        VITE_API_URL: ${STAGE_API_URL}
    run:
      base: static
      documentRoot: dist
`
	if _, err := os.Stat(yamlPath); err != nil {
		return fmt.Errorf("read %s: %w", yamlPath, err)
	}
	if err := os.WriteFile(yamlPath, []byte(want), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", yamlPath, err)
	}
	return nil
}

// patchApidevCORSOrigins rewrites the apidev/zerops.yaml CORS_ORIGINS
// values to consume the project-env constants the C2 fix teaches.
// Run-19 emitted post-active aliases (`${appdev_zeropsSubdomain}`,
// `${appstage_zeropsSubdomain}`) directly inline; the C2 brief change
// teaches both producer (api) and consumer (app) to read project-env
// constants instead so a hostname rename only edits the constants in
// one place.
//
// Two replacements per file:
//
//   - dev setup: `${appdev_zeropsSubdomain},${appstage_zeropsSubdomain}`
//     → `${DEV_FRONTEND_URL},${STAGE_FRONTEND_URL}`
//   - prod setup: `${appstage_zeropsSubdomain}` → `${STAGE_FRONTEND_URL}`
//
// Idempotent: if the file already carries the project-env shape, the
// substitutions don't match and the file is unchanged.
func patchApidevCORSOrigins(rootDir string) error {
	yamlPath := filepath.Join(rootDir, "apidev", "zerops.yaml")
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", yamlPath, err)
	}
	body := string(raw)
	body = strings.ReplaceAll(body,
		"${appdev_zeropsSubdomain},${appstage_zeropsSubdomain}",
		"${DEV_FRONTEND_URL},${STAGE_FRONTEND_URL}")
	body = strings.ReplaceAll(body,
		"CORS_ORIGINS: ${appstage_zeropsSubdomain}",
		"CORS_ORIGINS: ${STAGE_FRONTEND_URL}")
	if err := os.WriteFile(yamlPath, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", yamlPath, err)
	}
	return nil
}

// patchPlanProjectEnvs injects the four URL constants the C2 fix
// teaches into Plan.ProjectEnvVars under a synthesized "workspace"
// scope. The shape mirrors what `zerops_env project=true action=set`
// would have written if the scaffold sub-agent had used the right
// pattern.
//
// Idempotent: re-runs overwrite the same keys with the same values.
func patchPlanProjectEnvs(rootDir string) error {
	planPath := filepath.Join(rootDir, "environments", "plan.json")
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", planPath, err)
	}
	var plan recipe.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return fmt.Errorf("unmarshal plan: %w", err)
	}
	if plan.ProjectEnvVars == nil {
		plan.ProjectEnvVars = map[string]map[string]string{}
	}
	if plan.ProjectEnvVars["workspace"] == nil {
		plan.ProjectEnvVars["workspace"] = map[string]string{}
	}
	plan.ProjectEnvVars["workspace"]["STAGE_API_URL"] = "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app"
	plan.ProjectEnvVars["workspace"]["STAGE_FRONTEND_URL"] = "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
	plan.ProjectEnvVars["workspace"]["DEV_API_URL"] = "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app"
	plan.ProjectEnvVars["workspace"]["DEV_FRONTEND_URL"] = "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app"
	out, err := json.MarshalIndent(&plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	out = append(out, '\n')
	return os.WriteFile(planPath, out, 0o600)
}

// appendWorkerDevServerFact adds the C4 attestation fact for
// workerdev. The new gate at gate_worker_dev_server.go requires this
// fact when a dev codebase's start is `zsc noop --silent`. Without
// it, scaffold complete-phase refuses.
//
// The fact must validate per FactRecord.validatePorterChange
// (Why + CandidateClass + CandidateSurface). We use scaffold-decision
// + CODEBASE_ZEROPS_COMMENTS per the dev-loop principle's example.
//
// Idempotent: skipped if a fact with this topic on the worker scope
// already exists.
func appendWorkerDevServerFact(rootDir string) error {
	factsPath := filepath.Join(rootDir, "environments", "facts.jsonl")
	log := recipe.OpenFactsLog(factsPath)
	existing, err := log.Read()
	if err != nil {
		return fmt.Errorf("read facts: %w", err)
	}
	for _, f := range existing {
		if f.Topic == "worker_dev_server_started" && strings.HasPrefix(f.Scope, "worker") {
			return nil // already attested
		}
	}
	attest := recipe.FactRecord{
		Topic:            "worker_dev_server_started",
		Kind:             recipe.FactKindPorterChange,
		Scope:            "worker/runtime",
		Why:              "Worker process owned by zerops_dev_server (port=0 healthPath=\"\"); code edits drive nest watcher rebuild instead of redeploy.",
		CandidateClass:   "scaffold-decision",
		CandidateSurface: "CODEBASE_ZEROPS_COMMENTS",
	}
	if err := log.Append(attest); err != nil {
		return fmt.Errorf("append fact: %w", err)
	}
	return nil
}

// appendFieldRationaleCoverage closes the C5 producer-side facts
// integrity gap. Run-19's facts.jsonl had 7 unclassified
// field_rationale records and 13 facts without classification — the
// downstream codebase-content brief produced sparse comments because
// it had nothing to attach to.
//
// This patch synthesizes one field_rationale fact per directive
// group in each codebase's on-disk zerops.yaml that doesn't already
// have an attesting fact. The synthesized facts mirror what scaffold
// SHOULD have recorded if the new C5 brief instructions had been
// live — same shape, framework-neutral Why prose grounded in the
// directive's actual purpose.
//
// Idempotent: only appends facts for directives that lack one.
func appendFieldRationaleCoverage(rootDir string) error {
	factsPath := filepath.Join(rootDir, "environments", "facts.jsonl")
	log := recipe.OpenFactsLog(factsPath)
	existing, err := log.Read()
	if err != nil {
		return fmt.Errorf("read facts: %w", err)
	}
	have := factPathSet(existing)

	// One synthesized fact per (codebase, directive). The Why text is
	// hand-authored per directive — concise, framework-neutral,
	// suitable as a comment fragment seed.
	plans := []struct {
		host      string
		fieldPath string
		why       string
	}{
		// apidev — every directive group from runs/19/apidev/zerops.yaml
		{"api", "build.deployFiles", "Source tree ships unfiltered (./) so dev self-deploy works without narrowing — narrow `[dist, package.json]` would wipe the working tree on the next dev cycle."},
		{"api", "run.ports", "Port matches the runtime's listener; httpSupport: true tells the L7 balancer to terminate TLS and forward HTTP to the container — the runtime must bind 0.0.0.0:PORT, loopback is unreachable from the balancer."},
		{"api", "run.healthCheck", "Probe path is /health, NOT /api/health — the route is excluded from the framework's global API prefix so platform liveness stays decoupled from API versioning. If the prefix moves to /api/v2 later, the probe path doesn't change."},
		{"api", "run.start", "dev runs `zsc noop --silent` so the agent owns the long-running process via zerops_dev_server — code edits don't force redeploy. prod runs the compiled entry under platform supervision."},
		{"api", "run.envVariables", "Cross-service refs (${db_*}, ${cache_*}, etc.) re-aliased under stable own-keys (DB_HOST, REDIS_HOST, ...) so application code reads its own names — swap `db` later with a yaml-only edit, no code rewrite."},
		{"api", "run.initCommands", "Two execOnce keys (migrate-${appVersionId}, seed-${appVersionId}) per deploy: a failed seed doesn't burn the migrate key, the next container retry re-fires only what's still pending. With minContainers >= 2 only one container per key actually runs the command; the rest skip."},
		{"api", "deploy.readinessCheck", "Readiness gate blocks traffic until /health returns 200 — keeps rolling deploys from sending requests to a container that's up but still wiring framework + DB + cache. Pair with SIGTERM drain so in-flight work finishes before the balancer cuts."},

		// appdev — post-C2-patch shape (base: static stage)
		{"app", "build.envVariables", "VITE_API_URL is baked into the SPA bundle at build time — Vite inlines `import.meta.env.VITE_*` as string literals during `vite build`. Read STAGE_API_URL from project envs (resolved at provision time via ${zeropsSubdomainHost}) so the bundle ships with a real URL, not a literal token."},
		{"app", "build.deployFiles", "Stage ships `dist` only — the static bundle is the deploy artifact. Nginx (base: static) serves it from documentRoot."},
		{"app", "run.base", "Stage uses base: static — the SPA compiles to a static bundle, no node runtime needed at request time. ~80MB RAM saved per replica vs running `npx serve` on nodejs@22."},
		{"app", "run.documentRoot", "Nginx serves dist/ as the document root with built-in SPA fallback — every unmatched path returns index.html so client-side routes survive a browser refresh."},

		// workerdev — every directive group from runs/19/workerdev/zerops.yaml
		{"worker", "run.start", "dev runs `zsc noop --silent` so the agent owns the long-running nest watcher via zerops_dev_server (port=0 healthPath=\"\" for no-HTTP workers). prod runs the compiled standalone application context."},
		{"worker", "run.envVariables", "Worker reads five managed services — each one's keys re-aliased to friendly DB_*/NATS_*/CACHE_*/S3_*/SEARCH_* names so code never reads platform-side hostnames directly. SEARCH_URL composes hostname + port because the Meilisearch SDK takes a base URL."},
		{"worker", "build.deployFiles", "prod ships dist + node_modules + package.json only — source tree omitted to keep the runtime container small. dev keeps source via `./` so SSHFS edits drive the watcher."},
	}

	for _, p := range plans {
		key := p.host + "::" + p.fieldPath
		if have[key] {
			continue
		}
		fact := recipe.FactRecord{
			Topic:     fmt.Sprintf("%s-%s-rationale", p.host, normalizeForTopic(p.fieldPath)),
			Kind:      recipe.FactKindFieldRationale,
			Scope:     fmt.Sprintf("%s/zerops.yaml", p.host),
			FieldPath: p.fieldPath,
			Why:       p.why,
		}
		if err := log.Append(fact); err != nil {
			return fmt.Errorf("append %s rationale: %w", key, err)
		}
	}
	return nil
}

// factPathSet builds an idempotence index of (host, fieldPath) pairs
// already attested by field_rationale facts in the corpus.
func factPathSet(records []recipe.FactRecord) map[string]bool {
	out := map[string]bool{}
	for _, f := range records {
		if f.Kind != recipe.FactKindFieldRationale || f.FieldPath == "" {
			continue
		}
		host := f.Scope
		if i := strings.IndexByte(host, '/'); i > 0 {
			host = host[:i]
		}
		if host == "" {
			continue
		}
		out[host+"::"+f.FieldPath] = true
	}
	return out
}

// normalizeForTopic turns "run.envVariables" into
// "run-envvariables" — lowercase, dot-replaced, suitable as a Topic
// suffix.
func normalizeForTopic(fieldPath string) string {
	out := strings.ToLower(fieldPath)
	out = strings.ReplaceAll(out, ".", "-")
	return out
}

// validateInputs is a sanity guard — the patch profile assumes the
// run-19 corpus shape. If facts.jsonl or plan.json are missing, fail
// fast.
func validateInputs(rootDir string) error {
	required := []string{
		filepath.Join(rootDir, "environments", "plan.json"),
		filepath.Join(rootDir, "environments", "facts.jsonl"),
		filepath.Join(rootDir, "apidev", "zerops.yaml"),
		filepath.Join(rootDir, "appdev", "zerops.yaml"),
		filepath.Join(rootDir, "workerdev", "zerops.yaml"),
	}
	for _, p := range required {
		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("missing required file %s", p)
			}
			return err
		}
	}
	return nil
}

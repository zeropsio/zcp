package sync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// skipDirs are directories to exclude from the export archive.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// skipFiles are file names to exclude from the export archive.
var skipFiles = map[string]bool{
	".DS_Store": true,
	"Thumbs.db": true,
}

// skipPathPatterns are path segments that indicate generated/cached files.
// Used only in the walk fallback (non-git directories).
var skipPathPatterns = []string{
	"storage/framework/views/",    // Laravel compiled Blade cache
	"storage/framework/cache/",    // Laravel file cache
	"storage/framework/sessions/", // Laravel file sessions
	"storage/logs/",               // Laravel log files
	"bootstrap/cache/packages.php",
	"bootstrap/cache/services.php",
	"bootstrap/cache/events.php",
	".phpunit.cache/",
	"__pycache__/",
	".next/cache/",
}

// Environment folder layout constants.
const (
	layoutNested = "nested" // environments/ subdir
	layoutRoot   = "root"   // env folders at recipe dir root
)

// knownEnvFolders are the expected environment tier folder prefixes.
var knownEnvFolders = []string{
	"0 \u2014", "1 \u2014", "2 \u2014", "3 \u2014", "4 \u2014", "5 \u2014",
}

// ExportOpts configures the recipe export.
//
// SessionStateDir / SessionID / SkipCloseGate (v8.97 Fix 1): the close-step
// gate reads the named session's state and refuses to export unless
// step=close is complete. Precedence for session ID resolution: explicit
// SessionID field → $ZCP_SESSION_ID env var → empty (ad-hoc CLI mode,
// gate skipped). SkipCloseGate is ONLY for an explicit --force-export
// bypass and prints a stderr warning when used.
type ExportOpts struct {
	RecipeDir       string   // recipe output dir (env folders + README)
	AppDirs         []string // app source dirs (SSHFS mounts or local subdirs), optional — one per codebase
	IncludeTimeline bool     // prompt for TIMELINE.md if missing
	SessionStateDir string   // path to workflow state dir (defaults to CWD/.zcp/state)
	SessionID       string   // session ID from --session flag; falls back to $ZCP_SESSION_ID
	SkipCloseGate   bool     // ONLY for explicit --force-export — prints stderr warning
}

// enforceCloseGate implements the v8.97 Fix 1 close-step gate. Returns
// nil (gate passes) when:
//   - No session context is declared (ad-hoc CLI export), with a stderr
//     note for transparency.
//   - Session state is loadable AND step=close is complete.
//
// Returns an ErrExportBlocked error with distinct diagnostics when:
//   - A session is declared but state cannot be loaded → names the ID and
//     its source so the author knows whether to unset the flag or fix the
//     session dir.
//   - State is loaded but close is not complete → names the current
//     status so the author knows to dispatch the review and browser walk.
//
// The three branches are individually diagnosable so v32-era confusion
// ("is the gate failing because close is incomplete or the state file is
// missing?") is eliminated at the message level rather than via error-code
// proliferation.
func enforceCloseGate(opts ExportOpts) error {
	sessionID, sourceLabel := resolveSessionID(opts.SessionID)
	if sessionID == "" {
		// Cx-CLOSE-STEP-GATE-HARD: before falling through to "no session
		// context, skip gate", check the session registry for any active
		// session whose OutputDir matches the target recipe dir. If one
		// exists the invocation is in fact bound to a session — the
		// author forgot the --session flag (or the agent invented an
		// ad-hoc export as a shortcut around close). Refuse with the
		// session ID + remediation naming both the flag-based and
		// workflow-based paths forward. v36 F-8/F-11 shipped an
		// "advisory note" that was easy to skip; this turns the note
		// into an error.
		if liveSessionID, found, err := findLiveSessionForRecipe(opts); err == nil && found {
			return fmt.Errorf(
				"%s: live recipe session %q is tracking recipe-dir %q — sessionless export would bypass its close-step gate; re-run with --session=%s (to run the gate against that session), or finish `zerops_workflow action=complete step=close` inside the session first; `--force-export` bypasses with a stderr warning when the session is abandoned",
				platform.ErrExportBlocked, liveSessionID, opts.RecipeDir, liveSessionID,
			)
		}
		fmt.Fprintln(os.Stderr, "note: no session context (--session unset, $ZCP_SESSION_ID unset); skipping close-step gate.")
		return nil
	}
	state, err := loadRecipeSession(opts.SessionStateDir, sessionID)
	if err != nil {
		return fmt.Errorf(
			"%s: session %q declared (via %s) but state could not be loaded: %w — verify the session ID is correct; if exporting outside an orchestrated run, unset both --session and $ZCP_SESSION_ID; retry with --force-export to bypass (not recommended)",
			platform.ErrExportBlocked, sessionID, sourceLabel, err,
		)
	}
	status := recipeStepStatus(state, "close")
	if status != "complete" {
		shown := status
		if shown == "" {
			shown = "(step missing)"
		}
		return fmt.Errorf(
			"%s: close step is %s — dispatch the code-review subagent, run the close browser walk, then `zerops_workflow action=complete step=close` before exporting; exporting without close produces an incomplete deliverable (per-codebase READMEs + CLAUDE.md not staged, no code-review signals)",
			platform.ErrExportBlocked, shown,
		)
	}
	return nil
}

// ExportResult holds the outcome of an export operation.
type ExportResult struct {
	ArchivePath    string // path to created archive (empty if NeedsTimeline)
	NeedsTimeline  bool   // true if TIMELINE.md is missing and was requested
	TimelinePrompt string // prompt for the AI to generate TIMELINE.md
	TimelinePath   string // where to write TIMELINE.md
}

// ExportRecipe creates a .tar.gz archive combining:
//   - Environment folders (from recipeDir, at root or in environments/ subdir)
//     → always placed under environments/ in the archive
//   - App source (from each AppDirs entry, if provided) → each placed under
//     its directory's basename (e.g. appdev/, apidev/, workerdev/). Dual-runtime
//     recipes pass multiple dirs so the archive captures every codebase.
//   - TIMELINE.md (from recipeDir root, if present)
//   - README.md (from recipeDir root, if present)
//
// The archive is written to os.TempDir first, then moved to CWD.
//
// v8.97 Fix 1: before archive creation, ExportRecipe reads the workflow
// session state and refuses if step=close is not complete. Three distinct
// diagnostic paths:
//  1. No session context (both --session unset AND $ZCP_SESSION_ID unset):
//     ad-hoc CLI export, gate skipped with a stderr note.
//  2. Declared session with missing state: actionable error naming the
//     session ID and its source (--session vs env var).
//  3. State loaded but close incomplete: actionable error naming the
//     current close-step status.
//
// SkipCloseGate bypasses the gate with an explicit stderr warning — only
// for emergency debug extraction.
func ExportRecipe(opts ExportOpts) (*ExportResult, error) {
	if !opts.SkipCloseGate {
		if err := enforceCloseGate(opts); err != nil {
			return nil, err
		}
	} else if opts.SessionID != "" || os.Getenv("ZCP_SESSION_ID") != "" {
		fmt.Fprintln(os.Stderr, "warning: --force-export bypasses the close-step gate. An exported archive may be incomplete if close is not complete (per-codebase READMEs + CLAUDE.md not staged, no code-review signals).")
	}

	recipeDir, err := filepath.Abs(opts.RecipeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve recipe dir: %w", err)
	}
	if info, err := os.Stat(recipeDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"recipe-dir %q does not exist or is not a directory — pass the absolute path to a generated recipe output (e.g. /var/www/zcprecipator/{slug}), not a bare slug. Usage: zcp sync recipe export <recipe-dir> [--app-dir <path>]... [--include-timeline]",
			opts.RecipeDir,
		)
	}

	// Reject duplicate app-dir basenames — each codebase gets its own
	// {basename}/ prefix inside the archive, so two dirs with the same
	// basename would silently clobber each other.
	seen := make(map[string]string, len(opts.AppDirs))
	for _, d := range opts.AppDirs {
		name := filepath.Base(d)
		if prev, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate app-dir basename %q: %s and %s", name, prev, d)
		}
		seen[name] = d
	}

	// Find environment folders — either at root or inside environments/.
	envsDir, envLayout := findEnvFolders(recipeDir)
	if envsDir == "" {
		fmt.Fprintln(os.Stderr, "  warning: no environment folders found")
	}

	// Check TIMELINE.md when requested.
	timelinePath := filepath.Join(recipeDir, "TIMELINE.md")
	if opts.IncludeTimeline {
		if _, err := os.Stat(timelinePath); os.IsNotExist(err) {
			return &ExportResult{
				NeedsTimeline:  true,
				TimelinePrompt: buildTimelinePrompt(recipeDir),
				TimelinePath:   timelinePath,
			}, nil
		}
	}

	baseName := filepath.Base(recipeDir)
	archivePrefix := baseName + "-zcprecipator"
	finalName := archivePrefix + ".tar.gz"

	// Write to temp dir first to avoid self-inclusion.
	tmpFile, err := os.CreateTemp("", archivePrefix+"-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	gw := gzip.NewWriter(tmpFile)
	tw := tar.NewWriter(gw)

	// 1. Add root files (TIMELINE.md, README.md, ZCP_CONTENT_MANIFEST.json).
	//
	// The manifest is the writer sub-agent's honesty declaration — which
	// gotchas carry which citation-map topics, which IG items paraphrase
	// which guides. Cx-4 MANIFEST-OVERLAY in v8.112.0 stages it into the
	// recipe output directory; without inclusion in this whitelist it
	// never reaches the tarball the user ships (v38 F-23 root cause).
	for _, name := range []string{"TIMELINE.md", "README.md", "ZCP_CONTENT_MANIFEST.json"} {
		p := filepath.Join(recipeDir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			if addErr := addFileToTar(tw, p, filepath.Join(archivePrefix, name), fi); addErr != nil {
				tw.Close()
				gw.Close()
				tmpFile.Close()
				return nil, addErr
			}
		}
	}

	// 2. Add environment folders → always under environments/ in archive.
	if envsDir != "" {
		if err := exportEnvFolders(tw, envsDir, envLayout, archivePrefix); err != nil {
			tw.Close()
			gw.Close()
			tmpFile.Close()
			return nil, err
		}
	}

	// 3. Add each app source dir — one archive subdir per codebase.
	//    Cx-CLOSE-STEP-STAGING adds a post-source overlay: after the
	//    git-tracked source lands in `{archivePrefix}/{appName}/`, any
	//    writer-staged files at `{recipeDir}/{appName}/*.md` overlay
	//    into the same archive path. This is how the per-codebase
	//    README.md + CLAUDE.md the close-step stages reach the tarball
	//    even when the writer sub-agent never git-committed them
	//    (v36 F-10 fix — export no longer strips uncommitted writer
	//    output when close-step has staged it into the output tree).
	for _, rawAppDir := range opts.AppDirs {
		appDir, absErr := filepath.Abs(rawAppDir)
		if absErr != nil {
			tw.Close()
			gw.Close()
			tmpFile.Close()
			return nil, fmt.Errorf("resolve app dir %s: %w", rawAppDir, absErr)
		}
		appName := filepath.Base(appDir)
		archiveAppDir := filepath.Join(archivePrefix, appName)
		// Run-11 Q-3: warn (don't block) when SourceRoot has no .git/
		// directory. The apps-repo publish path needs a clean git
		// history; missing .git is a sign scaffold's git-init mandate
		// (Q-1) was skipped.
		if !hasGitDir(appDir) {
			fmt.Fprintf(os.Stderr,
				"warning: %s has no .git/ — run `git init && git add -A && git commit -m 'scaffold: initial structure + zerops.yaml'` from the SourceRoot before export to give the apps-repo a clean history (run-11 Q-3)\n",
				appDir)
			err = exportSubdirWalk(tw, appDir, archiveAppDir, "")
		} else {
			err = exportGitSubtree(tw, appDir, archiveAppDir, "")
		}
		if err != nil {
			tw.Close()
			gw.Close()
			tmpFile.Close()
			return nil, fmt.Errorf("export app dir %s: %w", rawAppDir, err)
		}
		// Overlay per-codebase markdown from the SourceRoot (run-11 M-3).
		// Pre-§L the writer staged README/CLAUDE under <recipeDir>/<appName>/;
		// post-§L stitch writes them at <cb.SourceRoot>/ directly. The
		// overlay covers the case where uncommitted README/CLAUDE land
		// in the SourceRoot after stitch but before any git commit, so
		// exportGitSubtree (committed-only) misses them.
		if err := overlayStagedWriterContent(tw, appDir, archiveAppDir); err != nil {
			tw.Close()
			gw.Close()
			tmpFile.Close()
			return nil, fmt.Errorf("overlay staged content for %s: %w", appName, err)
		}
	}

	tw.Close()
	gw.Close()
	tmpFile.Close()

	// Move to CWD.
	if err := os.Rename(tmpPath, finalName); err != nil {
		if cpErr := copyFile(tmpPath, finalName); cpErr != nil {
			return nil, fmt.Errorf("move archive: %w", cpErr)
		}
		os.Remove(tmpPath)
	}

	success = true
	return &ExportResult{ArchivePath: finalName}, nil
}

// findEnvFolders locates environment tier folders.
// Returns (dir containing them, layout) where layout is:
//   - layoutNested if found in {recipeDir}/environments/
//   - layoutRoot if found at {recipeDir}/ root level
//   - "" if not found
func findEnvFolders(recipeDir string) (string, string) {
	// Check environments/ subdir first.
	envsDir := filepath.Join(recipeDir, "environments")
	if entries, err := os.ReadDir(envsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && isEnvFolder(e.Name()) {
				return envsDir, layoutNested
			}
		}
	}
	// Check root level.
	if entries, err := os.ReadDir(recipeDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && isEnvFolder(e.Name()) {
				return recipeDir, layoutRoot
			}
		}
	}
	return "", ""
}

// exportEnvFolders adds environment folders to the archive under environments/.
func exportEnvFolders(tw *tar.Writer, envsDir, layout, archivePrefix string) error {
	envArchiveDir := filepath.Join(archivePrefix, "environments")

	return filepath.Walk(envsDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == envsDir && fi.IsDir() {
			return nil // skip the root dir itself
		}
		// For root layout, skip non-env directories (appdev/, etc.).
		if layout == layoutRoot && fi.IsDir() && !isEnvFolder(fi.Name()) {
			// Allow subdirs within env folders.
			rel, _ := filepath.Rel(envsDir, path)
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if !isEnvFolder(parts[0]) {
				return filepath.SkipDir
			}
		}
		if fi.IsDir() {
			return nil
		}
		if skipFiles[fi.Name()] {
			return nil
		}

		rel, err := filepath.Rel(envsDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		// For root layout, only include files inside env folders (+ README.md at root).
		if layout == layoutRoot {
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if !isEnvFolder(parts[0]) && fi.Name() != "README.md" {
				return nil
			}
		}

		archivePath := filepath.Join(envArchiveDir, rel)
		return addFileToTar(tw, path, archivePath, fi)
	})
}

// exportGitSubtree exports a directory's git-tracked files to the tar.
func exportGitSubtree(tw *tar.Writer, dir, archivePrefix, relPrefix string) error {
	cmd := exec.Command("git", "ls-files", "-z") //nolint:noctx // controlled path
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: git ls-files failed in %s, falling back to walk\n", dir)
		return exportSubdirWalk(tw, dir, archivePrefix, relPrefix)
	}

	for f := range strings.SplitSeq(stdout.String(), "\x00") {
		if f == "" {
			continue
		}
		fullPath := filepath.Join(dir, f)
		fi, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if fi.IsDir() || skipFiles[fi.Name()] {
			continue
		}

		archivePath := filepath.Join(archivePrefix, relPrefix, f)
		if err := addFileToTar(tw, fullPath, archivePath, fi); err != nil {
			return err
		}
	}
	return nil
}

// exportSubdirWalk walks a subdirectory with skip lists (fallback when git fails).
func exportSubdirWalk(tw *tar.Writer, dir, archivePrefix, relPrefix string) error {
	return filepath.Walk(dir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() && skipDirs[fi.Name()] {
			return filepath.SkipDir
		}
		if fi.IsDir() {
			return nil
		}
		if skipFiles[fi.Name()] {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		relSlash := strings.ReplaceAll(rel, string(filepath.Separator), "/")
		if matchesSkipPattern(relSlash) {
			return nil
		}

		archivePath := filepath.Join(archivePrefix, relPrefix, rel)
		return addFileToTar(tw, path, archivePath, fi)
	})
}

// matchesSkipPattern checks if a relative path matches any skip pattern.
func matchesSkipPattern(rel string) bool {
	for _, pattern := range skipPathPatterns {
		if strings.Contains(rel, pattern) {
			return true
		}
	}
	return false
}

// addFileToTar adds a single file to the tar writer.
func addFileToTar(tw *tar.Writer, fullPath, archivePath string, fi os.FileInfo) error {
	header, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return fmt.Errorf("file header %s: %w", archivePath, err)
	}
	header.Name = archivePath

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write header %s: %w", archivePath, err)
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer f.Close()

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("copy %s: %w", archivePath, err)
	}
	return nil
}

// overlayStagedWriterContent walks the writer-staging destination for
// one codebase and adds its .md files to the tar at archiveAppDir.
// Missing stagedDir → no-op (writer sub-agent may not have run, or
// close-step staging is disabled). Close-step staging is the
// producer; this is the consumer — keeping the paths aligned avoids
// a cross-package dependency on internal/workflow.
func overlayStagedWriterContent(tw *tar.Writer, stagedDir, archiveAppDir string) error {
	entries, err := os.ReadDir(stagedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		src := filepath.Join(stagedDir, e.Name())
		fi, statErr := os.Stat(src)
		if statErr != nil {
			continue
		}
		dst := filepath.Join(archiveAppDir, e.Name())
		if addErr := addFileToTar(tw, src, dst, fi); addErr != nil {
			return addErr
		}
	}
	return nil
}

// isEnvFolder checks if a directory name matches an environment tier folder.
func isEnvFolder(name string) bool {
	for _, prefix := range knownEnvFolders {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// hasGitDir checks if a directory contains a .git directory.
func hasGitDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

// copyFile copies src to dst via read/write (for cross-device moves).
func copyFile(src, dst string) error {
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

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// buildTimelinePrompt returns instructions for the AI to create TIMELINE.md.
func buildTimelinePrompt(sourceDir string) string {
	return fmt.Sprintf(`TIMELINE.md is missing from %s.

Write a TIMELINE.md documenting the recipe creation session. Include:

## Format
- Step-by-step build history with timestamps or durations
- Each step: what was done, decisions made, issues encountered
- Issues table: severity, description, resolution
- Final output section: URLs, file locations, archive path

## Steps to document
1. **Research** — framework analysis, recipe plan decisions
2. **Provision** — services created, env vars discovered
3. **Generate** — app scaffolded, code changes made, zerops.yaml written
4. **Deploy** — build durations, failures and fixes, verification results
5. **Finalize** — environment tiers generated, check results
6. **Close** — verification sub-agent findings, final fixes

## Rules
- Be factual — document what happened, not what should happen
- Include error messages and fix descriptions for any failures
- Note build durations where available
- List all URLs (dev, stage subdomains)

Write TIMELINE.md to: %s/TIMELINE.md
Then call export again.`, sourceDir, sourceDir)
}

// CollectRecipeFiles reads all files from a recipe environments directory
// and returns them as a map of relative paths to content.
// Paths are prefixed with slug/ and the environments/ prefix is stripped.
// Handles both layouts: environments/ subdir or env folders at root.
func CollectRecipeFiles(sourceDir, slug string) (map[string]string, error) {
	envsDir, layout := findEnvFolders(sourceDir)
	if envsDir == "" {
		return nil, fmt.Errorf("no environment folders found in %s", sourceDir)
	}

	files := make(map[string]string)

	err := filepath.Walk(envsDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == envsDir && fi.IsDir() {
			return nil
		}
		// For root layout, skip non-env directories.
		if layout == layoutRoot && fi.IsDir() && !isEnvFolder(fi.Name()) {
			rel, _ := filepath.Rel(envsDir, path)
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if !isEnvFolder(parts[0]) {
				return filepath.SkipDir
			}
		}
		if fi.IsDir() {
			return nil
		}
		if skipFiles[fi.Name()] {
			return nil
		}

		rel, err := filepath.Rel(envsDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		// For root layout, only include files inside env folders + root README.
		if layout == layoutRoot {
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if !isEnvFolder(parts[0]) && fi.Name() != "README.md" {
				return nil
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}

		targetPath := slug + "/" + strings.ReplaceAll(rel, string(filepath.Separator), "/")
		files[targetPath] = string(data)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk environments: %w", err)
	}

	return files, nil
}

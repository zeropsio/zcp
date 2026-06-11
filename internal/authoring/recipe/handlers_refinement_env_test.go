package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-34 Fix 1 — refinement-time Replace on env fragments
// (`env/<N>/intro`, `env/<N>/import-comments/<host>`) MUST persist to
// disk, mirroring the codebase-fragment re-stitch path.
//
// Pre-fix: handlers.go's refinement transactional wrapper re-stitched
// only when host != "" (codebase fragments). Refinement called
// `record-fragment mode=replace` on env/0/intro through env/5/intro,
// engine returned ok:true for each — but the published env tier READMEs
// still showed the original priorBody. Six refinement ACTs landed in
// session state but never propagated to disk.
//
// Diagnosed in plans/run-34-validation.md §"Top 5 surprises" #1.

// TestRefinementReplaceEnvIntro_PersistsToDisk pins the fix: a
// refinement-phase Replace on env/0/intro must rewrite the published
// <outputRoot>/<tier.Folder>/README.md so the new body lands on disk.
func TestRefinementReplaceEnvIntro_PersistsToDisk(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	const newIntro = "An AI-agent workspace tuned for cheap iterate-and-discard cycles."
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/intro",
		Fragment:   newIntro,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	tier, _ := TierAt(0)
	path := filepath.Join(sess.OutputRoot, tier.Folder, "README.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env README: %v", err)
	}
	if !strings.Contains(string(body), newIntro) {
		t.Errorf("env README on disk does NOT contain new intro %q after refinement Replace.\nFile: %s\nGot:\n%s",
			newIntro, path, string(body))
	}
}

// TestRefinementReplaceEnvImportComment_PersistsToDisk pins the fix
// for the second env-fragment shape: a Replace on
// `env/0/import-comments/<host>` must rewrite the published
// <outputRoot>/<tier.Folder>/import.yaml so the new comment lands on
// disk.
func TestRefinementReplaceEnvImportComment_PersistsToDisk(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	const newComment = "Postgres for iterate-and-discard agent slots."
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/import-comments/db",
		Fragment:   newComment,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	tier, _ := TierAt(0)
	path := filepath.Join(sess.OutputRoot, tier.Folder, "import.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tier import.yaml: %v", err)
	}
	if !strings.Contains(string(body), newComment) {
		t.Errorf("tier import.yaml on disk does NOT contain new comment %q after refinement Replace.\nFile: %s\nGot:\n%s",
			newComment, path, string(body))
	}
}

// TestRefinementReplaceEnvImportComment_ProjectScope_PersistsToDisk pins
// the third validateFragmentID env shape — `env/<N>/import-comments/project`.
// Project-scope comments route to plan.EnvComments[<tier>].Project (rather
// than .Service[<host>]) and surface as a leading comment block above the
// `project:` header in the tier import.yaml. The codebase-host
// `import-comments/<host>` happy path is covered above; this exercises the
// project branch specifically so a regression in either of the two
// EnvComments routing branches surfaces independently.
func TestRefinementReplaceEnvImportComment_ProjectScope_PersistsToDisk(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	const newProject = "Tier 0 project — refined orientation paragraph for the AI-agent workspace."
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/import-comments/project",
		Fragment:   newProject,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	// Plan-state landed: EnvComments[<tier>].Project carries the new body.
	if got := sess.Plan.EnvComments["0"].Project; got != newProject {
		t.Errorf("plan.EnvComments[0].Project = %q, want %q", got, newProject)
	}

	// Disk-state landed: tier import.yaml carries the comment above
	// `project:` header.
	tier, _ := TierAt(0)
	path := filepath.Join(sess.OutputRoot, tier.Folder, "import.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tier import.yaml: %v", err)
	}
	if !strings.Contains(string(body), newProject) {
		t.Errorf("tier import.yaml on disk does NOT contain new project comment %q after refinement Replace.\nFile: %s\nGot:\n%s",
			newProject, path, string(body))
	}
}

// TestRefinementReplaceMultiTier_BothTiersPersist fires refinement-replace
// on env/0/intro AND env/3/intro in the same session and asserts both
// tier READMEs reflect the new bodies independently. preStitchEnv scopes
// to one tier per call by design (so a tier-0 ACT doesn't re-emit tiers
// 1-5); this test catches any cross-tier state bleed where the second
// stitch would clobber or fail to update the first tier's prior write.
func TestRefinementReplaceMultiTier_BothTiersPersist(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	const tier0Intro = "Tier 0 — refined AI-agent workspace intro."
	const tier3Intro = "Tier 3 — refined small-team production intro."

	for _, tc := range []struct {
		fragmentID string
		body       string
	}{
		{"env/0/intro", tier0Intro},
		{"env/3/intro", tier3Intro},
	} {
		in := RecipeInput{
			Action:     "record-fragment",
			Slug:       sess.Slug,
			FragmentID: tc.fragmentID,
			Fragment:   tc.body,
			Mode:       "replace",
		}
		r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
		if r.Error != "" {
			t.Fatalf("unexpected error on %s: %s", tc.fragmentID, r.Error)
		}
	}

	// Tier 0 README carries new intro AND tier 3 README carries new intro.
	for _, tc := range []struct {
		tierIdx  int
		wantBody string
	}{
		{0, tier0Intro},
		{3, tier3Intro},
	} {
		tier, _ := TierAt(tc.tierIdx)
		path := filepath.Join(sess.OutputRoot, tier.Folder, "README.md")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read tier %d README: %v", tc.tierIdx, err)
		}
		if !strings.Contains(string(body), tc.wantBody) {
			t.Errorf("tier %d README missing %q after multi-tier refinement.\nFile: %s\nGot:\n%s",
				tc.tierIdx, tc.wantBody, path, string(body))
		}
	}

	// And the OTHER tier (the one not edited in either call) — tier 5 —
	// must NOT have either of the new intros leaked into it.
	tier5, _ := TierAt(5)
	tier5Path := filepath.Join(sess.OutputRoot, tier5.Folder, "README.md")
	tier5Body, err := os.ReadFile(tier5Path)
	if err != nil {
		t.Fatalf("read tier 5 README: %v", err)
	}
	for _, leaked := range []string{tier0Intro, tier3Intro} {
		if strings.Contains(string(tier5Body), leaked) {
			t.Errorf("tier 5 README leaked refined body %q from a sibling tier — preStitchEnv should be tier-scoped", leaked)
		}
	}
}

// TestRefinementReplaceEnv_FailureNotice_PreservesPriorBody pre-creates a
// directory at the expected env README path so writeSurfaceFile inside
// preStitchEnv fails on os.WriteFile ("is a directory"). The contract:
// the response carries an actionable `refinement-env-stitch-failed`
// notice naming the failure AND the on-disk surface is NOT corrupted —
// because writeSurfaceFile failed, the directory we planted at the
// README path is still a directory; nothing partial wrote.
//
// This is the only concrete failure-notice path in handlers.go's
// refinement env wrapper — the broader picture (priorBody preservation
// when the in-memory Replace landed but the on-disk stitch failed) is
// a known gap the wrapper documents as "best-effort" with no
// snapshot/restore gate. See handlers.go preStitchEnv comment block:
// "refinement contract for env fragments is best-effort for revert (no
// snapshot/restore gate yet), but persistence-to-disk MUST happen
// regardless." In-memory Replace HAS landed; disk did not. Re-run
// stitch-content to reconcile is the documented recovery.
func TestRefinementReplaceEnv_FailureNotice_PreservesPriorBody(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	// Capture the in-memory body so we can verify the Replace DID land
	// in plan-state (the contract documents in-memory Replace as
	// always-completing; only on-disk stitch is best-effort).
	const newIntro = "Tier 0 refined intro that the failure path will fail to persist."

	// Plant a directory at the env README path so os.WriteFile fails
	// with "is a directory" inside writeSurfaceFile. We replace the
	// stitched README file with a directory of the same name.
	tier, _ := TierAt(0)
	readmePath := filepath.Join(sess.OutputRoot, tier.Folder, "README.md")
	if err := os.Remove(readmePath); err != nil {
		t.Fatalf("remove seed README: %v", err)
	}
	if err := os.Mkdir(readmePath, 0o755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/intro",
		Fragment:   newIntro,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("record-fragment must NOT error on stitch failure (notice path); got: %s", r.Error)
	}

	// The actionable notice surfaces the failure with recovery prose.
	found := false
	for _, n := range r.Notices {
		if n.Code == "refinement-env-stitch-failed" {
			found = true
			if !strings.Contains(n.Message, "Re-run stitch-content") {
				t.Errorf("refinement-env-stitch-failed notice missing recovery prose; got %q", n.Message)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected refinement-env-stitch-failed notice; got %+v", r.Notices)
	}

	// In-memory plan-state DID land (best-effort contract: in-memory
	// Replace completes; on-disk persistence reported via notice).
	if got := sess.SnapshotFragment("env/0/intro"); got != newIntro {
		t.Errorf("plan-state should hold new body even when on-disk stitch failed; got %q", got)
	}

	// The blocker directory at readmePath is still a directory — the
	// failed write didn't corrupt it, and nothing partial leaked into
	// the surface. Pin that the path exists AND is still a directory
	// (no half-written file replacing the dir).
	st, err := os.Stat(readmePath)
	if err != nil {
		t.Fatalf("stat blocked README path: %v", err)
	}
	if !st.IsDir() {
		t.Errorf("blocked README path should still be a directory after failed write; got mode %v", st.Mode())
	}
}

// TestRefinementReplaceEnvImportComment_DoesNotTouchSiblingTierImportComments
// pins the tier-scoping contract at the comment-routing level: a Replace
// on env/0/import-comments/api must update only that tier's apidev/api
// comment slot, leaving tier-3 import.yaml untouched and (within tier 0)
// the app and worker service comments untouched. Catches any preStitchEnv
// regression that re-emits sibling tiers OR that scrambles within-tier
// EnvComments routing.
func TestRefinementReplaceEnvImportComment_DoesNotTouchSiblingTierImportComments(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	// Wipe the syntheticShowcasePlan slot-named seeds (`apidev`,
	// `apistage`) for tier 0 so the per-codebase emitter (which checks
	// `comments[host+"dev"]` first then falls back to `comments[host]`)
	// will render the bare-name comment we record. Then seed sibling-host
	// (`app`) and sibling-tier (tier 3 `api`) bare-name comments so the
	// spillover predicate has concrete strings to look for.
	const tier0AppComment = "App (tier 0) — sibling-host original comment."
	const tier3APIComment = "Api (tier 3) — sibling-tier original comment."

	// Reset tier 0 EnvComments to a clean shape (no slot-named seeds
	// shadowing the bare-name fallback path).
	tier0Clean := EnvComments{Service: map[string]string{
		"db":  "Postgres for the greetings table.",
		"app": tier0AppComment,
	}}
	sess.Plan.EnvComments["0"] = tier0Clean

	// Helper to plant a sibling comment without disturbing existing keys.
	plantComment := func(tierKey, host, comment string) {
		ec := sess.Plan.EnvComments[tierKey]
		if ec.Service == nil {
			ec.Service = map[string]string{}
		}
		ec.Service[host] = comment
		sess.Plan.EnvComments[tierKey] = ec
	}
	plantComment("3", "api", tier3APIComment)
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("re-seed stitch: %v", err)
	}

	// Snapshot tier-3 import.yaml BEFORE the refinement call so we can
	// compare byte-for-byte after.
	tier3, _ := TierAt(3)
	tier3Path := filepath.Join(sess.OutputRoot, tier3.Folder, "import.yaml")
	tier3Before, err := os.ReadFile(tier3Path)
	if err != nil {
		t.Fatalf("read tier 3 import.yaml before: %v", err)
	}

	const newAPIComment = "Tier 0 api — refined comment after rule-walk."
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/import-comments/api",
		Fragment:   newAPIComment,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	// Tier 0 import.yaml carries the new api comment.
	tier0, _ := TierAt(0)
	tier0Path := filepath.Join(sess.OutputRoot, tier0.Folder, "import.yaml")
	tier0Body, err := os.ReadFile(tier0Path)
	if err != nil {
		t.Fatalf("read tier 0 import.yaml: %v", err)
	}
	if !strings.Contains(string(tier0Body), newAPIComment) {
		t.Errorf("tier 0 import.yaml missing new api comment %q\nGot:\n%s", newAPIComment, string(tier0Body))
	}
	// Tier 0 sibling-host (app) comment is unchanged on disk.
	if !strings.Contains(string(tier0Body), tier0AppComment) {
		t.Errorf("tier 0 import.yaml lost sibling-host app comment %q after Replace on api — refinement must not scramble within-tier EnvComments routing\nGot:\n%s", tier0AppComment, string(tier0Body))
	}

	// Tier 3 import.yaml is byte-identical to its pre-refinement state.
	tier3After, err := os.ReadFile(tier3Path)
	if err != nil {
		t.Fatalf("read tier 3 import.yaml after: %v", err)
	}
	if string(tier3Before) != string(tier3After) {
		t.Errorf("tier 3 import.yaml changed after Replace on env/0/import-comments/api — preStitchEnv must be tier-scoped.\nBefore:\n%s\nAfter:\n%s",
			string(tier3Before), string(tier3After))
	}
}

// TestRefinementReplaceEnvIntro_NonRefinementPhase_StillPersistsViaStitch
// — the env-stitch path is wired only at PhaseRefinement (mirrors the
// codebase wrapper). Outside refinement, the unwrapped recordFragment
// path applies the Replace directly; on-disk persistence is the
// stitch-content path's responsibility (called by complete-phase as
// usual). This test pins that we don't accidentally re-stitch on every
// non-refinement Replace (which would be an unwarranted I/O cost).
func TestRefinementReplaceEnvIntro_NonRefinementPhase_NoDiskWrite(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)
	sess.Current = PhaseCodebaseContent

	tier, _ := TierAt(0)
	path := filepath.Join(sess.OutputRoot, tier.Folder, "README.md")
	priorOnDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prior env README: %v", err)
	}

	const newIntro = "Should not land on disk during codebase-content phase."
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/intro",
		Fragment:   newIntro,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	// Body landed in plan state.
	if got := sess.SnapshotFragment("env/0/intro"); got != newIntro {
		t.Errorf("plan state should hold new body; got %q", got)
	}
	// But on-disk file is still the prior content (no auto-stitch
	// outside refinement; complete-phase / stitch-content owns disk
	// writes for non-refinement phases).
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read env README: %v", err)
	}
	if string(current) != string(priorOnDisk) {
		t.Errorf("non-refinement env Replace should NOT auto-write disk; file changed.\nWant: %s\nGot:  %s",
			string(priorOnDisk), string(current))
	}
}

// buildRefinementSessionWithDisk builds a refinement session with the
// env READMEs + tier import.yamls already stitched to disk so the
// persist-to-disk tests can compare prior vs post-replace file bodies.
func buildRefinementSessionWithDisk(t *testing.T) *Session {
	t.Helper()
	sess := buildRefinementSession(t)

	// Seed env intros + per-host comments so AssembleEnvREADME and
	// EmitDeliverableYAML render content the test can detect changes
	// against.
	if sess.Plan.Fragments == nil {
		sess.Plan.Fragments = map[string]string{}
	}
	for i := range Tiers() {
		key := "env/" + tierIntStr(i) + "/intro"
		if _, ok := sess.Plan.Fragments[key]; !ok {
			sess.Plan.Fragments[key] = "Tier " + tierIntStr(i) + " — original intro paragraph."
		}
	}
	if sess.Plan.EnvComments == nil {
		sess.Plan.EnvComments = map[string]EnvComments{}
	}
	for i := range Tiers() {
		key := tierIntStr(i)
		ec := sess.Plan.EnvComments[key]
		if ec.Service == nil {
			ec.Service = map[string]string{}
		}
		if ec.Service["db"] == "" {
			ec.Service["db"] = "Postgres — original comment."
		}
		sess.Plan.EnvComments[key] = ec
	}

	// Stitch initial state so the file tree exists.
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch: %v", err)
	}
	return sess
}

func tierIntStr(i int) string {
	return [...]string{"0", "1", "2", "3", "4", "5"}[i]
}

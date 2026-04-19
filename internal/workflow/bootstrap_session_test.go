package workflow

import (
	"testing"
	"time"
)

func TestNewBootstrapSession_StepsPerRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		route     BootstrapRoute
		wantSteps []string
	}{
		{
			name:      "recipe",
			route:     BootstrapRouteRecipe,
			wantSteps: []string{"import", "wait-active", "verify-deploy", "verify", "close"},
		},
		{
			name:  "classic",
			route: BootstrapRouteClassic,
			wantSteps: []string{
				"plan", "import", "wait-active",
				"verify-deploy-per-runtime", "verify", "write-metas", "close",
			},
		},
		{
			name:      "adopt",
			route:     BootstrapRouteAdopt,
			wantSteps: []string{"discover", "prompt-modes", "write-metas", "verify", "close"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess := NewBootstrapSession(tt.route, "any intent", nil)
			if sess.Route != tt.route {
				t.Errorf("route = %q, want %q", sess.Route, tt.route)
			}
			if len(sess.Steps) != len(tt.wantSteps) {
				t.Fatalf("step count = %d, want %d", len(sess.Steps), len(tt.wantSteps))
			}
			for i, want := range tt.wantSteps {
				if sess.Steps[i].Name != want {
					t.Errorf("step[%d] = %q, want %q", i, sess.Steps[i].Name, want)
				}
				if !sess.Steps[i].Started.IsZero() {
					t.Errorf("step[%d] Started is set, want zero", i)
				}
				if sess.Steps[i].Finished != nil {
					t.Errorf("step[%d] Finished is set, want nil", i)
				}
			}
		})
	}
}

func TestBootstrapSession_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	original := NewBootstrapSession(
		BootstrapRouteRecipe,
		"laravel demo",
		&RecipeMatch{Slug: "laravel-jetstream", Confidence: 0.91},
	)
	if err := SaveBootstrapSession(dir, original); err != nil {
		t.Fatalf("SaveBootstrapSession: %v", err)
	}
	loaded, err := LoadBootstrapSession(dir, original.PID)
	if err != nil {
		t.Fatalf("LoadBootstrapSession: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded session is nil")
	}
	if loaded.Route != original.Route {
		t.Errorf("route = %q, want %q", loaded.Route, original.Route)
	}
	if loaded.RecipeMatch == nil || loaded.RecipeMatch.Slug != "laravel-jetstream" {
		t.Errorf("recipeMatch = %+v, want laravel-jetstream", loaded.RecipeMatch)
	}
	if loaded.Intent != original.Intent {
		t.Errorf("intent = %q, want %q", loaded.Intent, original.Intent)
	}
}

func TestBootstrapSession_LoadMissingReturnsNil(t *testing.T) {
	t.Parallel()

	sess, err := LoadBootstrapSession(t.TempDir(), 99999)
	if err != nil {
		t.Fatalf("LoadBootstrapSession: %v", err)
	}
	if sess != nil {
		t.Errorf("got non-nil session: %+v", sess)
	}
}

func TestBootstrapSession_MarkSteps(t *testing.T) {
	t.Parallel()

	sess := NewBootstrapSession(BootstrapRouteAdopt, "adopt", nil)
	now := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)

	sess.MarkStepStarted("discover", now)
	if sess.Steps[0].Started.IsZero() {
		t.Errorf("discover Started not set")
	}

	// Re-start is a no-op.
	later := now.Add(time.Minute)
	sess.MarkStepStarted("discover", later)
	if !sess.Steps[0].Started.Equal(now) {
		t.Errorf("discover Started was updated on second call: %v", sess.Steps[0].Started)
	}

	sess.MarkStepFinished("discover", later)
	if sess.Steps[0].Finished == nil || !sess.Steps[0].Finished.Equal(later) {
		t.Errorf("discover Finished = %v, want %v", sess.Steps[0].Finished, later)
	}
}

func TestBootstrapSession_FinishWithoutStartNoOp(t *testing.T) {
	t.Parallel()

	sess := NewBootstrapSession(BootstrapRouteAdopt, "", nil)
	sess.MarkStepFinished("discover", time.Now())
	if sess.Steps[0].Finished != nil {
		t.Errorf("finish without start should be no-op, got: %v", sess.Steps[0].Finished)
	}
}

func TestBootstrapSession_RecordFailure(t *testing.T) {
	t.Parallel()

	sess := NewBootstrapSession(BootstrapRouteClassic, "", nil)
	sess.RecordStepFailure("import")
	sess.RecordStepFailure("import")
	for _, step := range sess.Steps {
		if step.Name == "import" && step.Failures != 2 {
			t.Errorf("import failures = %d, want 2", step.Failures)
		}
	}
}

func TestBootstrapSession_Close(t *testing.T) {
	t.Parallel()

	sess := NewBootstrapSession(BootstrapRouteAdopt, "", nil)
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	sess.Close(now)
	if sess.ClosedAt == nil || !sess.ClosedAt.Equal(now) {
		t.Errorf("ClosedAt = %v, want %v", sess.ClosedAt, now)
	}
	// Close is idempotent.
	sess.Close(now.Add(time.Hour))
	if !sess.ClosedAt.Equal(now) {
		t.Errorf("second Close updated ClosedAt: %v", sess.ClosedAt)
	}
}

func TestBootstrapSession_IsComplete(t *testing.T) {
	t.Parallel()

	sess := NewBootstrapSession(BootstrapRouteAdopt, "", nil)
	if sess.IsComplete() {
		t.Error("fresh session reports complete")
	}
	now := time.Now()
	for _, step := range sess.Steps {
		sess.MarkStepStarted(step.Name, now)
		sess.MarkStepFinished(step.Name, now)
	}
	if !sess.IsComplete() {
		t.Error("all steps finished but IsComplete=false")
	}
}

func TestBootstrapSession_Delete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sess := NewBootstrapSession(BootstrapRouteRecipe, "", nil)
	if err := SaveBootstrapSession(dir, sess); err != nil {
		t.Fatalf("SaveBootstrapSession: %v", err)
	}
	if err := DeleteBootstrapSession(dir, sess.PID); err != nil {
		t.Fatalf("DeleteBootstrapSession: %v", err)
	}
	// Idempotent — second delete does not error.
	if err := DeleteBootstrapSession(dir, sess.PID); err != nil {
		t.Fatalf("second DeleteBootstrapSession: %v", err)
	}
	loaded, err := LoadBootstrapSession(dir, sess.PID)
	if err != nil {
		t.Fatalf("LoadBootstrapSession post-delete: %v", err)
	}
	if loaded != nil {
		t.Errorf("session still present after delete: %+v", loaded)
	}
}

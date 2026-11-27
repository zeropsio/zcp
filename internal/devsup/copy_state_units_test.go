package devsup

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// witness: internal/devsup/supervisor_unix.go | f.accepts = f.answered → f.accepts = false
func TestStartPhase_STCopyWait_TransitionsAnswerTheHeldWait(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		s, err := New(Config{Root: dir, StateDir: dir, Tasks: func() (Tasks, bool) { return Tasks{}, false }})
		if err != nil {
			t.Fatal(err)
		}
		g := &group{done: make(chan struct{})}
		sv := &server{name: "web", spec: Server{Name: "web", Port: 3000, Command: "app"}, desired: DesiredRunning, g: g, answered: true, retry: true, starts: 1, startBegun: time.Now()}
		s.servers["web"], s.previewName = sv, "web"
		results := make(chan CopySnapshot, 1)
		wait := func(after Status) {
			go func() {
				got, e := s.WaitCopy(t.Context(), after)
				if e != nil {
					t.Error(e)
					return
				}
				results <- got
			}()
			synctest.Wait()
		}
		wait(StatusRunning)
		select {
		case got := <-results:
			t.Fatalf("running wait answered early: %+v", got)
		default:
		}
		close(g.done)
		s.watchExit(sv, g)
		synctest.Wait()
		if got := <-results; got.Status != StatusCrashed {
			t.Fatalf("exit = %s, want crashed", got.Status)
		}
		wait(StatusCrashed)
		s.mu.Lock()
		sv.starting = true
		s.mu.Unlock()
		s.persist()
		synctest.Wait()
		if got := <-results; got.Status != StatusStarting {
			t.Fatalf("next start = %s, want starting", got.Status)
		}
		wait(StatusStarting)
		if err := s.Stop("web"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if got := <-results; got.Status != StatusStopped {
			t.Fatalf("stop = %s, want stopped", got.Status)
		}
		got, err := s.WaitCopy(context.Background(), StatusRunning)
		if err != nil || got.Status != StatusStopped {
			t.Fatalf("already left = %+v, %v; want stopped", got, err)
		}
	})
}

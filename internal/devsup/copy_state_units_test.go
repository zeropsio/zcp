package devsup

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

// witness: internal/devsup/control_unix.go | resp.Copy, resp.Code = &snapshot, CodeOK → resp.Copy, resp.Code = &snapshot, CodeFailed
func TestStartPhase_STCopyControl_ReportsSuccessfulSnapshots(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		after Status
	}{
		{name: "snapshot"},
		{name: "already changed", after: StatusRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			s, err := New(Config{Root: dir, StateDir: dir, Tasks: func() (Tasks, bool) { return Tasks{}, false }})
			if err != nil {
				t.Fatal(err)
			}
			client, server := net.Pipe()
			defer client.Close()
			if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			go s.handle(server)
			if err := json.NewEncoder(client).Encode(Request{Verb: VerbCopyState, After: tc.after}); err != nil {
				t.Fatal(err)
			}
			var got Response
			if err := json.NewDecoder(client).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Code != CodeOK || got.Error != "" || got.Copy == nil || got.Copy.Status != StatusStopped {
				t.Fatalf("response = %+v, want a successful stopped snapshot", got)
			}
		})
	}
}

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

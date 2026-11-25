package devsup

import (
	"testing"
	"time"
)

// witness: internal/devsup/preview.go | return !closed && listen != 0 && f.port > 0 → return !closed && listen != 0 && f.port > 0 && f.live
func TestPreview_PVForward_EveryStatusOfThePreviewServerIsForwarded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		closed bool
		listen int
		facts  statusFacts
		want   bool
	}{
		{name: "before its first start", listen: 9002, facts: statusFacts{desired: DesiredRunning, port: 5173}, want: true},
		{name: "while it starts", listen: 9002, facts: statusFacts{desired: DesiredRunning, starting: true, port: 5173}, want: true},
		{name: "while it runs", listen: 9002, facts: statusFacts{desired: DesiredRunning, live: true, port: 5173, answered: true, accepts: true}, want: true},
		{name: "after an exit", listen: 9002, facts: statusFacts{desired: DesiredRunning, port: 5173, exited: true}, want: true},
		{name: "while stopped", listen: 9002, facts: statusFacts{desired: DesiredStopped, port: 5173, exited: true}, want: true},
		{name: "after the supervisor shut down", closed: true, listen: 9002, facts: statusFacts{desired: DesiredRunning, port: 5173}},
		{name: "a container without a preview port", facts: statusFacts{desired: DesiredRunning, live: true, port: 5173}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := previewForwarded(tt.closed, tt.listen, tt.facts); got != tt.want {
				t.Fatalf("preview forward listens = %v, want %v", got, tt.want)
			}
		})
	}
}

// witness: internal/devsup/state.go | case f.port > 0 && (!f.answered || !f.accepts): → case f.port > 0 && !f.accepts:
func TestPreview_PVAnswered_AcceptingIsNotRunningUntilTheHealthPathAnswered(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		facts statusFacts
		want  Status
	}{
		{name: "accepts, healthPath not answered yet", facts: statusFacts{desired: DesiredRunning, live: true, port: 5173, accepts: true}, want: StatusStarting},
		{name: "accepts and answered", facts: statusFacts{desired: DesiredRunning, live: true, port: 5173, accepts: true, answered: true}, want: StatusRunning},
		{name: "answered, port closed again", facts: statusFacts{desired: DesiredRunning, live: true, port: 5173, answered: true}, want: StatusStarting},
		{name: "a worker without a port", facts: statusFacts{desired: DesiredRunning, live: true}, want: StatusRunning},
		{name: "no group and no exit: being stopped or about to start", facts: statusFacts{desired: DesiredRunning, port: 5173, answered: true}, want: StatusStarting},
		{name: "exited", facts: statusFacts{desired: DesiredRunning, port: 5173, exited: true}, want: StatusCrashed},
		{name: "stopped", facts: statusFacts{desired: DesiredStopped, port: 5173, exited: true}, want: StatusStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deriveStatus(tt.facts); got != tt.want {
				t.Fatalf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

// witness: internal/devsup/preview.go | if retryPending { → if !retryPending {
func TestPreview_PVPage_SaysWhetherSomethingWillStartTheServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		status       Status
		retryPending bool
		want         previewKind
	}{
		{name: "running", status: StatusRunning, want: previewForward},
		{name: "starting", status: StatusStarting, want: previewHold},
		{name: "crashed, its one more start coming", status: StatusCrashed, retryPending: true, want: previewHold},
		{name: "crashed, nothing starts it", status: StatusCrashed, want: previewFinal},
		{name: "stopped", status: StatusStopped, want: previewFinal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := previewAnswer(tt.status, tt.retryPending); got != tt.want {
				t.Fatalf("answer = %d, want %d", got, tt.want)
			}
		})
	}
}

// witness: internal/devsup/preview.go | return now.kind == previewForward || pageID(now) != served → return now.kind != previewHold
func TestPreview_PVHold_TheHeldRequestReturnsOnceThePortWouldAnswerAnotherPage(t *testing.T) {
	t.Parallel()
	exit := &Exit{Code: 1, At: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	starting := previewView{kind: previewHold, name: "web", status: StatusStarting}
	restarting := previewView{kind: previewHold, name: "web", status: StatusStarting, reason: "a restart was asked for"}
	crashedAgain := previewView{kind: previewHold, name: "web", status: StatusCrashed, exit: exit}
	crashed := previewView{kind: previewFinal, name: "web", status: StatusCrashed, exit: exit}
	stopped := previewView{kind: previewFinal, name: "web", status: StatusStopped}
	running := previewView{kind: previewForward, name: "web", status: StatusRunning}
	tests := []struct {
		name         string
		served, now  previewView
		wantAnswered bool
	}{
		{name: "a stopped page while it stays stopped", served: stopped, now: stopped},
		{name: "a crashed page while nothing starts it", served: crashed, now: crashed},
		{name: "a starting page while it starts", served: starting, now: starting},
		{name: "a stopped page once the server is started again", served: stopped, now: starting, wantAnswered: true},
		{name: "a crashed page once the server runs", served: crashed, now: running, wantAnswered: true},
		{name: "a starting page once the server runs", served: starting, now: running, wantAnswered: true},
		{name: "a starting page once the server crashed for good", served: starting, now: crashed, wantAnswered: true},
		{name: "a starting page once the server crashed and starts once more", served: starting, now: crashedAgain, wantAnswered: true},
		{name: "a starting page once the start is a restart", served: starting, now: restarting, wantAnswered: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := holdDone(pageID(tt.served), tt.now); got != tt.wantAnswered {
				t.Fatalf("held request answered = %v, want %v", got, tt.wantAnswered)
			}
		})
	}
}

// witness: internal/devsup/preview.go | return reasonExited → return ""
func TestPreview_PVOnceMore_ThePreviewServerIsStartedOnceMoreAfterAnExit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                       string
		retry, startCrash, preview bool
		want                       string
	}{
		{name: "the preview server", preview: true, want: reasonExited},
		{name: "the preview server's one more start", retry: true, preview: true, want: ""},
		{name: "another server", want: ""},
		{name: "another server's start crash", startCrash: true, want: reasonRetry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := onceMore(tt.retry, tt.startCrash, tt.preview); got != tt.want {
				t.Fatalf("one more start = %q, want %q", got, tt.want)
			}
		})
	}
}

// witness: internal/devsup/preview.go | view.kind == previewHold → view.kind == previewFinal
func TestPreview_PVMark_SaysWhetherTheServerIsComing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		view previewView
		want string
	}{
		{"starting", previewView{kind: previewHold, status: StatusStarting}, "hold"},
		{"crashed with retry", previewView{kind: previewHold, status: StatusCrashed}, "hold"},
		{"crashed", previewView{kind: previewFinal, status: StatusCrashed}, "final"},
		{"stopped", previewView{kind: previewFinal, status: StatusStopped}, "final"},
	} {
		if got := pageMark(tt.view); got != tt.want {
			t.Errorf("%s: pageMark = %q, want %q", tt.name, got, tt.want)
		}
	}
}

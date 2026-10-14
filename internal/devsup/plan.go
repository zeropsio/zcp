package devsup

// action is what a reconcile does to one server.
type action string

const (
	actNone    action = ""
	actStart   action = "start"
	actStop    action = "stop"
	actRestart action = "restart"
	// actRemove stops the server if it runs and forgets it.
	actRemove action = "remove"
)

// wanted is a server the declaration or the kept state asks for.
type wanted struct {
	server  Server
	desired Desired
}

// held is a server the supervisor knows.
type held struct {
	server Server
	// live: its process group runs under this supervisor.
	live bool
	// exited: it ended on its own (or failed to start) and stays down.
	exited bool
}

// plan decides what a reconcile does to each server. boot is the supervisor's
// first reconcile after it started (container start or restart, redeploy, a
// respawn after it was killed): only then does a server that ended on its own
// start again; otherwise it stays down until it is started.
func plan(want map[string]wanted, have map[string]held, boot bool) map[string]action {
	out := map[string]action{}
	for name := range have {
		if _, ok := want[name]; !ok {
			out[name] = actRemove
		}
	}
	for name, w := range want {
		h, ok := have[name]
		switch {
		case !ok:
			if w.desired == DesiredRunning {
				out[name] = actStart
			}
		case w.desired == DesiredStopped:
			if h.live {
				out[name] = actStop
			}
		case h.live && !h.server.SameRun(w.server):
			out[name] = actRestart
		case h.live:
		case !h.server.SameRun(w.server), !h.exited, boot:
			out[name] = actStart
		}
	}
	return out
}

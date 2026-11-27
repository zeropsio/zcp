# Start phase (`state.go`)

- ST-PHASE — A starting server's phase is the supervisor's alone: a start under way is launching even while the previous run's port had answered, and an awaited port is stalled only once this start's listen wait ran out, so no reader compares its own clock with the start or reads a restart of a healthy server as a stalled start.
- ST-AWAITED — A wait on a server lasts only while the supervisor itself still brings it up: its start under way, its port awaited within the listen wait, or its one more start to come; a running, stopped, stalled, answered-then-refusing or finally crashed server ends it at once. Breaks: one call waits out its whole bound on a server that crashed two seconds in, or answers "starting" for one that will never come.
- ST-COPY-WAIT — A held preview-state wait answers a leader exit before its next start, a stop at once, and a state already left without waiting; a live leader that has listened keeps running when its port later vanishes.

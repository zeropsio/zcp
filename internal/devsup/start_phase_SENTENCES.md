# Start phase (`state.go`)

- ST-PHASE — A starting server's phase is the supervisor's alone: a start under way is launching even while the previous run's port had answered, and an awaited port is stalled only once this start's listen wait ran out, so no reader compares its own clock with the start or reads a restart of a healthy server as a stalled start.

package topology

// StandIn marks a dev service a flow holds as its repository's stand-in
// until the flow retires it: the dev service is a step on the way, never
// where the repository ends up (docs/spec-workflows.md §13 PM-14). For names
// the flow (StandInPoolMigrate).
type StandIn struct {
	For string `json:"for"`
}

// StandInPoolMigrate: pool-migrate holds the dev service — one serving
// several parts, or a variant-B pair's — until it moves every part onto a dev
// pool of its own and retires it.
const StandInPoolMigrate = "pool-migrate"

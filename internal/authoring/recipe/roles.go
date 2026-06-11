package recipe

// Role is a codebase role within a recipe. A codebase's role determines
// the platform obligations it must satisfy (bind address, subdomain,
// process model) and the artifacts it owns (routes, static assets, jobs).
// Role contracts are framework-agnostic: they describe WHAT a role
// exposes, not HOW a framework implements it.
type Role string

const (
	// RoleMonolith serves application routes AND renders views from a
	// single codebase (full-stack framework shape).
	RoleMonolith Role = "monolith"
	// RoleAPI serves API routes, no rendered pages (API-first shape).
	RoleAPI Role = "api"
	// RoleFrontend serves a static + hydrated client application and
	// consumes an API over the network (pure-frontend shape).
	RoleFrontend Role = "frontend"
	// RoleWorker has no HTTP surface; consumes queue / broker / scheduled
	// work. May share a codebase with an API or run in its own.
	RoleWorker Role = "worker"
)

// RoleContract is the typed description of one role's platform obligations.
// Every field is a platform obligation (does it bind HTTP, does it need a
// subdomain, what zeropsSetup names apply) — never a framework instruction.
type RoleContract struct {
	Role              Role
	ServesHTTP        bool
	RequiresSubdomain bool
	ProcessModel      string
	ZeropsSetupDev    string
	ZeropsSetupProd   string
}

var roleContracts = map[Role]RoleContract{
	RoleMonolith: {
		Role: RoleMonolith, ServesHTTP: true, RequiresSubdomain: true,
		ProcessModel:   "http-server",
		ZeropsSetupDev: "dev", ZeropsSetupProd: "prod",
	},
	RoleAPI: {
		Role: RoleAPI, ServesHTTP: true, RequiresSubdomain: true,
		ProcessModel:   "http-server",
		ZeropsSetupDev: "dev", ZeropsSetupProd: "prod",
	},
	RoleFrontend: {
		Role: RoleFrontend, ServesHTTP: true, RequiresSubdomain: true,
		ProcessModel:   "http-server",
		ZeropsSetupDev: "dev", ZeropsSetupProd: "prod",
	},
	RoleWorker: {
		Role: RoleWorker, ServesHTTP: false, RequiresSubdomain: false,
		ProcessModel:   "job-consumer",
		ZeropsSetupDev: "dev", ZeropsSetupProd: "prod",
	},
}

// Contract returns the role contract, or false if the role is unknown.
func (r Role) Contract() (RoleContract, bool) {
	c, ok := roleContracts[r]
	return c, ok
}

// Roles returns all four roles in a deterministic order.
func Roles() []Role {
	return []Role{RoleMonolith, RoleAPI, RoleFrontend, RoleWorker}
}

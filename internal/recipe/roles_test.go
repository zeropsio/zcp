package recipe

import "testing"

func TestRoles_ContractPresence(t *testing.T) {
	t.Parallel()
	for _, r := range Roles() {
		if _, ok := r.Contract(); !ok {
			t.Errorf("role %q: missing contract", r)
		}
	}
}

func TestRoleContract_Shape(t *testing.T) {
	t.Parallel()

	// Role contracts describe platform obligations, not framework instructions.
	// Only four roles exist: monolith, api, frontend, worker. Each contract
	// must state whether the role serves HTTP, requires a subdomain, and what
	// process model applies.
	wantHTTP := map[Role]bool{
		RoleMonolith: true,
		RoleAPI:      true,
		RoleFrontend: true,
		RoleWorker:   false,
	}
	for r, http := range wantHTTP {
		c, ok := r.Contract()
		if !ok {
			t.Errorf("role %q: no contract", r)
			continue
		}
		if c.ServesHTTP != http {
			t.Errorf("role %q: ServesHTTP = %v, want %v", r, c.ServesHTTP, http)
		}
		if c.RequiresSubdomain != http {
			t.Errorf("role %q: RequiresSubdomain = %v, want %v", r, c.RequiresSubdomain, http)
		}
	}
	workerContract, _ := RoleWorker.Contract()
	if workerContract.ProcessModel != "job-consumer" {
		t.Errorf("worker ProcessModel = %q, want job-consumer", workerContract.ProcessModel)
	}
}

func TestRoleContract_UnknownReturnsFalse(t *testing.T) {
	t.Parallel()
	if _, ok := Role("bogus").Contract(); ok {
		t.Error("Role(\"bogus\").Contract() should return ok=false")
	}
}

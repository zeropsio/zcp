package tools

// repoRoute is where the agent runs git for one repository of this Mate's HQ.
type repoRoute struct {
	host, pool, lease string
}

// repoDevHost is where the agent runs git for appID's repo.
func repoDevHost(_, _, _ string) repoRoute { return repoRoute{} }

// inLease is r in the lease alias.
func (r repoRoute) inLease(alias string) repoRoute { return repoRoute{lease: alias, pool: r.pool} }

// git is cmds run where r runs git.
func (repoRoute) git(_ ...string) string { return "" }

// where names the place r runs git.
func (repoRoute) where() string { return "" }

// take is how branch is put in hand where r runs git.
func (repoRoute) take(_ string) string { return "" }

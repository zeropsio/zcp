package ops

// BuildProjectOriginReadCommand reads the checkout's own origin. An absent
// origin is empty; a missing checkout is an error, not permission to replace it.
func BuildProjectOriginReadCommand(workingDir string) string {
	return "cd " + shellQuote(workingDir) + " && if test -e .git; then git rev-parse --git-dir >/dev/null && { git remote get-url origin 2>/dev/null || true; }; fi"
}

// BuildProjectRepositoryCommand wires a plain project's checkout to HQ and
// joins the seeded main using the existing base-branch rules. The project
// key arrives on stdin and lives only in this process's Git environment.
// The second origin read protects a remote changed since the caller's read.
func BuildProjectRepositoryCommand(workingDir, remoteURL, hqURL string) string {
	return "cd " + shellQuote(workingDir) +
		" && (test -d .git || git init -q -b main)" +
		` && current=$(git remote get-url origin 2>/dev/null || true)` +
		` && if [ -n "$current" ] && [ "$current" != ` + shellQuote(remoteURL) + ` ]; then echo ZCP_PROJECT_ORIGIN_KEPT; exit 0; fi` +
		` && for zcp_trace in $(env | sed -n 's/^\(GIT_TRACE[A-Za-z0-9_]*\|GIT_CURL_VERBOSE\)=.*/\1/p'); do unset "$zcp_trace"; done` +
		" && IFS= read -r zcp_project_key" +
		" && export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_1=http.followRedirects GIT_CONFIG_VALUE_1=false GIT_CONFIG_KEY_0=" + shellQuote("http."+hqURL+"/.extraHeader") +
		` GIT_CONFIG_VALUE_0="Authorization: Project $zcp_project_key"` +
		` && unset zcp_project_key` +
		" && (git remote add origin " + shellQuote(remoteURL) + " 2>/dev/null || git remote set-url origin " + shellQuote(remoteURL) + ")" +
		" && " + BuildBaseBranchCommand(workingDir)
}

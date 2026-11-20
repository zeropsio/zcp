package ops

// An ssh session into a Zerops container is served by the platform's agent,
// so everything it runs lands in the agent's systemd service,
// zerops-zembed.service. Its TasksMax is systemd's DefaultTasksMax, 15% of
// the container's pids.max: 300 of 2000, while the service's own processes
// run under the container's 2000 alone (live-verified on ubuntu 24.04 dev
// and pool containers, systemd 255). A repository's checks run their stages
// at once, each with thread pools sized to the cores, and pass 300: the
// kernel refuses their threads and processes (EAGAIN), and stages fail at
// random and pass on a rerun. A scope is a unit too and DefaultTasksMax gives
// it 300 as well (live-verified), so the scope below lifts its own limit:
// the container's 2000 stays the only bound.

// Scope names of the scripts zcp runs over ssh for the repository's own work
// (ownScopeStep): what each unit's name starts with.
const (
	ownScopeRun     = "zcp-run"
	ownScopeGit     = "zcp-git"
	ownScopePrepare = "zcp-prepare"
	ownScopeDevsup  = "zcp-devsup"
)

// ownScopeWait is how long, in hundredths of a second (/proc/uptime's unit),
// a shell waits for systemd to move it into the scope it asked for.
const ownScopeWait = "100"

// ownScopeStep is the first step of a POSIX shell script zcp runs over ssh
// for the repository's own work: it moves the script's shell into a
// transient systemd scope of its own, <name>-<pid>.scope, out of the
// platform agent's service and its task limit (see above). It asks systemd's
// StartTransientUnit, through `sudo -n busctl`, for a scope holding the
// shell's own pid — the call `systemd-run --scope` makes for itself — with
// TasksMax=infinity and CollectMode=inactive-or-failed, so the unit is gone
// once its last process ends, failed or not. The shell itself moves, so what
// it runs is the process it was before: its user, environment, folder,
// descriptors, session and signals (sudo would reset the environment; only
// sudo and busctl run as root, reading /dev/null, writing nothing). systemd
// moves the pid when it runs the job, after it answered, so the shell waits
// until its own cgroup names the scope, for at most ownScopeWait by
// /proc/uptime, read by the shell's builtin read. Where sudo needs a password,
// sudo, busctl or the system bus is missing, or systemd refuses, the script
// runs where it is, as before, and says nothing. It costs a call about 25 ms,
// sudo's own start most of it (live, median of 10). Its variables are unset
// again, so the script's own commands see the environment they saw before.
func ownScopeStep(name string) string {
	unit := name + "-$$.scope"
	return `if sudo -n busctl call org.freedesktop.systemd1 /org/freedesktop/systemd1 org.freedesktop.systemd1.Manager StartTransientUnit 'ssa(sv)a(sa(sv))' ` +
		unit + ` fail 3 PIDs au 1 $$ TasksMax t 18446744073709551615 CollectMode s inactive-or-failed 0 </dev/null >/dev/null 2>&1; then ` +
		`read -r zcp_t0 zcp_ </proc/uptime; ` +
		`while read -r zcp_cg </proc/$$/cgroup && [ "${zcp_cg##*/}" != ` + unit + ` ] && read -r zcp_t zcp_ </proc/uptime && ` +
		`[ $((${zcp_t%.*}${zcp_t#*.} - ${zcp_t0%.*}${zcp_t0#*.})) -lt ` + ownScopeWait + ` ]; do :; done; fi; ` +
		`unset zcp_t0 zcp_t zcp_cg zcp_; `
}

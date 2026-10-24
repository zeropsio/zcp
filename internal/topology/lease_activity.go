package topology

// LeaseActivity is what zcp observed of a held dev-pool lease: the facts an
// idle label is drawn from (docs/spec-workflows.md §8 DP-21). Times are RFC
// 3339 UTC. A fact zcp did not read is absent — never a zero a reader could
// take for "nothing happened" — and Note says why it is missing.
type LeaseActivity struct {
	// LastChangeAt is the checkout's last change: the newest of the time
	// .git/logs/HEAD was last written (a commit, a checkout, a reset) and the
	// modification time of any modified or untracked file git does not
	// ignore. A deleted tracked file and a rename (which keeps the file's
	// mtime) do not move it.
	LastChangeAt string `json:"lastChangeAt,omitempty"`
	// PreviewLastConnAt is when the lease's preview forward last took a
	// connection, as its supervisor reports it; absent until one does.
	PreviewLastConnAt string `json:"previewLastConnAt,omitempty"`
	// PreviewOpenConns is how many connections the preview forward holds
	// open; nil (absent) when the supervisor did not report it, so a 0 is
	// always an observed zero.
	PreviewOpenConns *int `json:"previewOpenConns,omitempty"`
	// AcquiredAt is when zcp took the lease.
	AcquiredAt string `json:"acquiredAt,omitempty"`
	// ReadAt is when zcp read these facts (its own clock).
	ReadAt string `json:"readAt,omitempty"`
	// Note says what could not be read, and why.
	Note string `json:"note,omitempty"`
}

// LeaseUnsaved is what releasing a held lease would lose, as the release
// guard reads it (docs/spec-workflows.md §8 DP-4): uncommitted and untracked
// files git does not ignore, and commits on no remote branch. Files git
// ignores and stashes are not in it, and a release loses them too. A clean
// checkout reads as zeros; an unread one has no LeaseUnsaved at all.
type LeaseUnsaved struct {
	// Files are the first `git status --porcelain` lines, at most as many as
	// a refused release names.
	Files []string `json:"files,omitempty"`
	// FileCount is how many lines git status answered, Files included.
	FileCount int `json:"fileCount"`
	// UnpushedCommits is how many commits are on no remote branch.
	UnpushedCommits int `json:"unpushedCommits"`
}

// Package devsup is zcp-devsup, the dev-server supervisor a dev service runs as
// its run.start (docs/spec-mate.md §2.8a.2): it holds the named dev servers the
// repo declares in .zcp/dev-servers.json, each as the leader of its own process
// group with its own log, and keeps the start/stop state in $HOME so it
// survives a container restart while a redeploy (a fresh $HOME) starts every
// declared server again. A server that exits on its own stays down and is
// reported as crashed with its exit; one that crashes within its start window
// is started once more first.
//
// The package imports the standard library and golang.org/x/sys only: zcp's
// dev-server tool reads its declaration and state types, and the binary runs
// inside every dev container.
package devsup

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Formats lists the declaration format versions this supervisor runs. A
// declaration in another version is not run; zcp reports the supervisor as
// older than the declaration.
var Formats = []int{1}

// CapabilityEnv names the `ctl env` verb (the lease-wide env overlay).
const CapabilityEnv = "env"

// Capabilities lists the ctl verbs and flags this supervisor has that an
// older one lacks; `ctl version` reports them. An older ctl fails on a verb
// or flag it does not know, so zcp sends one only to a supervisor that lists
// it.
var Capabilities = []string{CapabilityEnv}

// DeclarationPath is where a repo declares its dev servers, relative to the
// repo root.
const DeclarationPath = ".zcp/dev-servers.json"

// Declaration is the repo's .zcp/dev-servers.json: the set of named dev
// servers. It changes only when the set changes; start and stop never write it.
type Declaration struct {
	Version int `json:"version"`
	// PreviewPortBase, when set, has a supervisor whose container has a
	// ZEROPS_StableNumber forward PreviewPortBase+ZEROPS_StableNumber to the
	// preview server's port (PreviewServer): a dev pool's containers each get
	// a public port of their own. Absent: no forward.
	PreviewPortBase int `json:"previewPortBase,omitempty"`
	// SiblingEnv maps an environment variable a part's dev server reads to
	// the pool whose lease of the same branch it reaches. zcp reads it and
	// hands the servers that lease's container hostname through `ctl env`;
	// the supervisor validates it with the rest of the file and never puts it
	// into any environment itself.
	SiblingEnv map[string]string `json:"siblingEnv,omitempty"`
	// Prepare, when set, is the shell command a dev pool's lease runs in the
	// repo root after its checkout and before the supervisor starts or
	// reloads (docs/spec-mate.md §2.8a.1): what a branch's own lockfile needs
	// on top of the dependencies the pool's image baked into it. zcp runs it;
	// the supervisor never does.
	Prepare string   `json:"prepare,omitempty"`
	Servers []Server `json:"servers"`
}

// Server is one named dev server: the shell command, its working directory
// relative to the repo root, the port it listens on (0 when it has none), the
// HTTP path its health probe reads on that port (absent: /), the paths whose
// change restarts it, the dev service that runs it (variant B) and whether it
// starts before anyone asks (absent: it does). Preview marks the server the
// preview port reaches when several have a port (PreviewServer).
type Server struct {
	Name       string `json:"name"`
	Port       int    `json:"port,omitempty"`
	HealthPath string `json:"healthPath,omitempty"`
	Command    string `json:"command"`
	Cwd        string `json:"cwd,omitempty"`
	Watch      *Watch `json:"watch,omitempty"`
	Service    string `json:"service,omitempty"`
	Autostart  *bool  `json:"autostart,omitempty"`
	Preview    bool   `json:"preview,omitempty"`
}

// PreviewServer is the server the preview port reaches: the only one with a
// port, else the one marked preview. ok is false when there is none.
func PreviewServer(servers []Server) (Server, bool) {
	var withPort []Server
	for _, s := range servers {
		if s.Port != 0 {
			withPort = append(withPort, s)
		}
	}
	if len(withPort) == 1 {
		return withPort[0], true
	}
	for _, s := range withPort {
		if s.Preview {
			return s, true
		}
	}
	return Server{}, false
}

// FirstDesired is the desired state of a name the state file does not hold
// yet; from then on only ctl changes it.
func (s Server) FirstDesired() Desired {
	if s.Autostart != nil && !*s.Autostart {
		return DesiredStopped
	}
	return DesiredRunning
}

// Watch names the repo paths whose change restarts a server; .git,
// node_modules and the ignore patterns never do.
type Watch struct {
	Paths  []string `json:"paths"`
	Ignore []string `json:"ignore,omitempty"`
}

// SameRun reports whether two entries run the same thing; a change of
// command, cwd, port or watch restarts the server.
func (s Server) SameRun(o Server) bool {
	if s.Command != o.Command || s.Cwd != o.Cwd || s.Port != o.Port || (s.Watch == nil) != (o.Watch == nil) {
		return false
	}
	return s.Watch == nil || (slices.Equal(s.Watch.Paths, o.Watch.Paths) && slices.Equal(s.Watch.Ignore, o.Watch.Ignore))
}

// ForService keeps the entries the supervisor of the dev service hostname
// runs: those without a service (variant A) and those naming it.
func (d Declaration) ForService(hostname string) []Server {
	out := make([]Server, 0, len(d.Servers))
	for _, s := range d.Servers {
		if s.Service == "" || s.Service == hostname {
			out = append(out, s)
		}
	}
	return out
}

var serverName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// envName is an environment variable name a shell can export.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedEnv are names the container's own value must reach every server
// with: the overlay and siblingEnv never set them, nor any ZEROPS_ name.
var reservedEnv = []string{"hostname", "serviceId", "projectId", "PATH", "HOME"}

// envNameError says why name cannot be set for the servers, nil when it can.
func envNameError(name string) error {
	switch {
	case !envName.MatchString(name):
		return fmt.Errorf("%q is not an environment variable name (%s)", name, envName)
	case strings.HasPrefix(name, "ZEROPS_"), slices.Contains(reservedEnv, name):
		return fmt.Errorf("%q is reserved: the container's own value reaches every server (reserved: ZEROPS_*, %s)", name, strings.Join(reservedEnv, ", "))
	}
	return nil
}

// siblingPool is a pool's service hostname, as siblingEnv names it.
var siblingPool = regexp.MustCompile(`^[a-z0-9]{1,40}$`)

// ErrUnsupportedFormat marks a declaration whose version this supervisor does
// not run.
var ErrUnsupportedFormat = errors.New("unsupported declaration format")

// ParseDeclaration decodes and validates a declaration. Unknown keys are
// ignored within a format version; a version this supervisor does not run is
// an error wrapping ErrUnsupportedFormat.
func ParseDeclaration(b []byte) (Declaration, error) {
	// The version is read first: another format may change the rest.
	var v struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Declaration{}, fmt.Errorf("parse %s: %w", DeclarationPath, err)
	}
	if v.Version <= 0 {
		return Declaration{}, fmt.Errorf("%s: version is missing", DeclarationPath)
	}
	if !slices.Contains(Formats, v.Version) {
		return Declaration{}, fmt.Errorf("%s: format %d is not one this supervisor runs (%v): %w", DeclarationPath, v.Version, Formats, ErrUnsupportedFormat)
	}
	var d Declaration
	if err := json.Unmarshal(b, &d); err != nil {
		return Declaration{}, fmt.Errorf("parse %s: %w", DeclarationPath, err)
	}
	if d.PreviewPortBase < 0 || d.PreviewPortBase > 65535 {
		return Declaration{}, fmt.Errorf("%s: previewPortBase %d is out of range", DeclarationPath, d.PreviewPortBase)
	}
	for _, name := range slices.Sorted(maps.Keys(d.SiblingEnv)) {
		if err := envNameError(name); err != nil {
			return Declaration{}, fmt.Errorf("%s: siblingEnv %w", DeclarationPath, err)
		}
		if pool := d.SiblingEnv[name]; !siblingPool.MatchString(pool) {
			return Declaration{}, fmt.Errorf("%s: siblingEnv %q: %q is not a pool hostname (%s)", DeclarationPath, name, pool, siblingPool)
		}
	}
	names, ports, preview := map[string]bool{}, map[int]string{}, ""
	for i, s := range d.Servers {
		s, err := normalizeServer(s)
		if err != nil {
			return Declaration{}, fmt.Errorf("%s: %w", DeclarationPath, err)
		}
		if names[s.Name] {
			return Declaration{}, fmt.Errorf("%s: %q is declared twice", DeclarationPath, s.Name)
		}
		names[s.Name] = true
		if s.Port != 0 {
			if other, ok := ports[s.Port]; ok {
				return Declaration{}, fmt.Errorf("%s: %q and %q both listen on port %d", DeclarationPath, other, s.Name, s.Port)
			}
			ports[s.Port] = s.Name
		}
		if s.Preview {
			if preview != "" {
				return Declaration{}, fmt.Errorf("%s: %q and %q are both marked preview", DeclarationPath, preview, s.Name)
			}
			preview = s.Name
		}
		d.Servers[i] = s
	}
	return d, nil
}

// LoadDeclaration reads root's declaration; a missing file is an empty
// declaration. Beyond ParseDeclaration it resolves symlinks: a cwd that
// resolves outside root makes the file invalid.
func LoadDeclaration(root string) (Declaration, error) {
	b, err := os.ReadFile(filepath.Join(root, DeclarationPath))
	if errors.Is(err, os.ErrNotExist) {
		return Declaration{Version: Formats[len(Formats)-1], Servers: []Server{}}, nil
	}
	if err != nil {
		return Declaration{}, fmt.Errorf("read %s: %w", DeclarationPath, err)
	}
	d, err := ParseDeclaration(b)
	if err != nil {
		return Declaration{}, err
	}
	for _, s := range d.Servers {
		if err := insideRoot(root, s.Cwd); err != nil {
			return Declaration{}, fmt.Errorf("%s: %q: %w", DeclarationPath, s.Name, err)
		}
	}
	return d, nil
}

// insideRoot checks that rel, with symlinks resolved, stays under root. A
// path that does not exist yet passes; starting the server reports it.
func insideRoot(root, rel string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil //nolint:nilerr // a missing cwd fails visibly when the server starts
	}
	if r, err := filepath.Rel(realRoot, resolved); err != nil || r == ".." || strings.HasPrefix(r, "../") {
		return fmt.Errorf("cwd %s resolves outside the repo", rel)
	}
	return nil
}

// normalizeServer validates one entry and fills the default cwd.
func normalizeServer(s Server) (Server, error) {
	if !serverName.MatchString(s.Name) {
		return Server{}, fmt.Errorf("%q: name must match %s", s.Name, serverName)
	}
	if strings.TrimSpace(s.Command) == "" {
		return Server{}, fmt.Errorf("%q: command is empty", s.Name)
	}
	if s.Port < 0 || s.Port > 65535 {
		return Server{}, fmt.Errorf("%q: port %d is out of range", s.Name, s.Port)
	}
	if s.Preview && s.Port == 0 {
		return Server{}, fmt.Errorf("%q: preview needs a port", s.Name)
	}
	if s.HealthPath != "" && !strings.HasPrefix(s.HealthPath, "/") {
		return Server{}, fmt.Errorf("%q: healthPath %q must start with /", s.Name, s.HealthPath)
	}
	cwd, err := cleanRel(s.Cwd)
	if err != nil {
		return Server{}, fmt.Errorf("%q: cwd: %w", s.Name, err)
	}
	s.Cwd = cwd
	if s.Watch != nil {
		w := Watch{Ignore: s.Watch.Ignore}
		if len(s.Watch.Paths) == 0 {
			return Server{}, fmt.Errorf("%q: watch has no paths", s.Name)
		}
		for _, p := range s.Watch.Paths {
			c, err := cleanRel(p)
			if err != nil {
				return Server{}, fmt.Errorf("%q: watch path %q: %w", s.Name, p, err)
			}
			w.Paths = append(w.Paths, c)
		}
		for _, p := range s.Watch.Ignore {
			if _, err := path.Match(p, ""); err != nil {
				return Server{}, fmt.Errorf("%q: watch ignore %q: %w", s.Name, p, err)
			}
		}
		s.Watch = &w
	}
	return s, nil
}

// cleanRel keeps a repo-relative path inside the repo.
func cleanRel(p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	if strings.HasPrefix(p, "/") {
		return "", errors.New("must be relative to the repo root")
	}
	c := path.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", errors.New("must stay inside the repo")
	}
	return c, nil
}

// ignored reports whether a change at rel (repo-relative, slash-separated)
// must not restart a server watching with w: anything under .git or
// node_modules, or at or under a path an ignore entry matches. An entry is
// matched segment by segment with path.Match, and a `**` segment matches any
// number of directories.
func (w *Watch) ignored(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, seg := range segs {
		if seg == ".git" || seg == "node_modules" {
			return true
		}
	}
	for _, ig := range w.Ignore {
		if matchesLeading(strings.Split(path.Clean(ig), "/"), segs) {
			return true
		}
	}
	return false
}

// matchesLeading reports whether the pattern segments match the leading
// segments of a path: the path is what the pattern names or lies under it.
func matchesLeading(pat, segs []string) bool {
	for ; len(pat) > 0; pat, segs = pat[1:], segs[1:] {
		if pat[0] == "**" {
			for i := range len(segs) + 1 {
				if matchesLeading(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
	}
	return true
}

// covers reports whether rel lies under one of the watched paths.
func (w *Watch) covers(rel string) bool {
	for _, p := range w.Paths {
		if p == "." || rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

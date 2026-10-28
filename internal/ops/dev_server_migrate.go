package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/devsup"
	"gopkg.in/yaml.v3"
)

// A dev service whose dev setup predates the supervisor moves onto it on its
// first zerops_dev_server start: the dev setup gains the two lines that install
// and run zcp-devsup (docs/spec-mate.md §2.8a.2), the repo gains the
// declaration of the server being started, and the dev service is redeployed
// once from inside — uncommitted work stays. Nothing is left on the old path.

// DevsupStart is the dev setup's run.start under the supervisor.
const DevsupStart = "zcp-devsup run"

// DevsupStartFor is the dev setup's run.start for a supervisor whose repository
// root is root: DevsupStart for "" (variant A, /var/www), else `zcp-devsup run
// --root <root>` — a variant-B dev service's code volume, where the shared
// declaration lives (docs/spec-mate.md §2.8a.2).
func DevsupStartFor(root string) string {
	if root == "" {
		return DevsupStart
	}
	return DevsupStart + " --root " + shellWord(root)
}

// plainWordRe is a shell word that needs no quoting.
var plainWordRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// shellWord is s as one shell word: as it is when it needs no quoting.
func shellWord(s string) string {
	if plainWordRe.MatchString(s) {
		return s
	}
	return shellQuote(s)
}

// DevsupAsset is the release asset zcp-devsup ships as.
const DevsupAsset = "zcp-devsup-linux-amd64"

// DevsupLatestURL is the zcp-devsup asset of whichever zcp release is the
// newest when it is fetched: the download a pool setup's install line names
// when zcp cannot name the release itself (the release list unreadable, or a
// static page like the pools-and-box theme). The install line follows the
// redirect (curl -L, wget by default).
const DevsupLatestURL = "https://github.com/zeropsio/zcp/releases/latest/download/" + DevsupAsset

// devsupURLRe bounds a download URL to what the prepareCommands line carries
// inside single quotes.
var devsupURLRe = regexp.MustCompile(`^https?://[A-Za-z0-9._~:/?#\[\]@!$&()*+,;=%-]+/` + DevsupAsset + `$`)

// DevsupURL is where a dev setup's prepareCommands fetch zcp-devsup
// (docs/spec-mate.md §2.8a.2): override (ZCP_DEVSUP_URL, a dev build's own
// upload) when set, else latest — the asset of the newest published zcp
// release, "" when that release does not ship one. Every zcp-devsup release
// reads format 1, the only one zcp writes, and a release never drops a format.
func DevsupURL(latest, override string) (string, error) {
	url := latest
	if override != "" {
		url = override
	}
	switch {
	case url == "":
		return "", errors.New("no published zcp release ships " + DevsupAsset + " yet, so zcp writes no download line; set ZCP_DEVSUP_URL on the zcp service to a build of it")
	case !devsupURLRe.MatchString(url):
		return "", fmt.Errorf("%q is not an http(s) URL ending in /%s", url, DevsupAsset)
	}
	return url, nil
}

// DevsupPrepareCommand is the run.prepareCommands line that installs
// zcp-devsup into the runtime image (never $HOME: a redeploy loses it); wget
// stands in where the base has no curl (Alpine).
func DevsupPrepareCommand(url string) string {
	return "sudo sh -c 'u=" + url + "; f=/usr/local/bin/zcp-devsup; " +
		`if command -v curl >/dev/null; then curl -fsSL "$u" -o "$f"; else wget -qO "$f" "$u"; fi && chmod 0755 "$f"'`
}

// AdoptSupervisorYAML puts the dev setup's run under the supervisor: the
// install line of url in run.prepareCommands — a zcp-devsup line the setup
// already carries stays as written, whichever release it names, since a newer
// one out since is no reason to change the person's file — and run.start
// DevsupStartFor(root) — root is the repository root the supervisor runs on,
// "" for /var/www — spliced into data so nothing else the user wrote changes.
// setup names the dev setup; empty picks the only setup, else the one named
// after hostname, else the one named "dev". It returns the setup it changed;
// changed is false when both lines are there.
func AdoptSupervisorYAML(data []byte, setup, hostname, url, root string) (out []byte, used string, changed bool, err error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, "", false, fmt.Errorf("zerops.yaml: %w", err)
	}
	entry, setup, err := devSetupEntry(&doc, setup, hostname)
	if err != nil {
		return nil, "", false, err
	}
	run := mappingValue(entry, "run")
	if run != nil && run.Kind != yaml.MappingNode {
		return nil, "", false, fmt.Errorf("zerops.yaml setup %q: run is not a mapping", setup)
	}
	want := supervisorLines{prepare: DevsupPrepareCommand(url), start: DevsupStartFor(root)}
	if line := devsupInstallLine(run); line != "" {
		want.prepare = line
	}
	src := newYAMLSource(data, &doc)
	splices, err := supervisorSplices(src, entry, run, setup, want)
	if err != nil || len(splices) == 0 {
		return data, setup, false, err
	}
	out = src.apply(splices)
	if err := spliceVerified(data, out, setup, want); err != nil {
		return nil, "", false, fmt.Errorf("zerops.yaml setup %q: %w; add to its run by hand: prepareCommands item %q and start: %s", setup, err, want.prepare, want.start)
	}
	return out, setup, true, nil
}

// devsupInstallLine is the zcp-devsup install line in run's prepareCommands,
// "" when it carries none.
func devsupInstallLine(run *yaml.Node) string {
	prepare := mappingValue(run, "prepareCommands")
	if prepare == nil || prepare.Kind != yaml.SequenceNode {
		return ""
	}
	for _, item := range prepare.Content {
		if item.Kind == yaml.ScalarNode && strings.Contains(item.Value, "/"+DevsupAsset) {
			return item.Value
		}
	}
	return ""
}

// devSetupName is the setup a dev service deploys when none is named after it.
const devSetupName = "dev"

// devSetupEntry finds the dev setup's mapping and the setup's name.
func devSetupEntry(doc *yaml.Node, setup, hostname string) (*yaml.Node, string, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, "", errors.New("zerops.yaml is empty")
	}
	zerops := mappingValue(doc.Content[0], "zerops")
	if zerops == nil || zerops.Kind != yaml.SequenceNode {
		return nil, "", errors.New("zerops.yaml has no zerops: list")
	}
	entries := map[string]*yaml.Node{}
	var names []string
	for _, entry := range zerops.Content {
		if name := mappingValue(entry, "setup"); name != nil {
			entries[name.Value] = entry
			names = append(names, name.Value)
		}
	}
	if setup == "" {
		switch {
		case len(names) == 1:
			setup = names[0]
		case entries[hostname] != nil:
			setup = hostname
		case entries[devSetupName] != nil:
			setup = devSetupName
		default:
			return nil, "", fmt.Errorf("zerops.yaml has several setups (%s) and none is named %s or dev — zcp cannot tell the dev setup", strings.Join(names, ", "), hostname)
		}
	}
	entry := entries[setup]
	if entry == nil {
		return nil, "", fmt.Errorf("zerops.yaml has no setup %q (setups: %s)", setup, strings.Join(names, ", "))
	}
	return entry, setup, nil
}

// mappingValue is the value of key in mapping m, nil when absent.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// readZeropsYAMLCmd prints the zerops.yaml (or zerops.yml) in dir after a
// line naming the file.
func readZeropsYAMLCmd(dir string) string {
	q := shellQuote(dir)
	return `for f in zerops.yaml zerops.yml; do if [ -f ` + q + `/"$f" ]; then echo "@@zcp-file $f"; cat ` + q + `/"$f"; exit 0; fi; done; echo "@@zcp-file -"`
}

// ReadRepoConfigFile reads the repo config file in hostname's dir —
// zerops.yaml, else zerops.yml — and names the one it read; file is "" when
// dir holds neither.
func ReadRepoConfigFile(ctx context.Context, ssh SSHDeployer, hostname, dir string) (file, content string, err error) {
	out, err := ssh.ExecSSH(ctx, hostname, readZeropsYAMLCmd(dir))
	if err != nil {
		return "", "", fmt.Errorf("read zerops.yaml (or zerops.yml) on %s: %w", hostname, err)
	}
	head, body, _ := strings.Cut(string(out), "\n")
	file, ok := strings.CutPrefix(strings.TrimSpace(head), "@@zcp-file ")
	if !ok || file == "-" {
		return "", "", nil
	}
	return file, body, nil
}

// AdoptSupervisorInDevSetup rewrites the zerops.yaml in hostname's dir — the
// folder its deploy runs from, "" for /var/www — so its dev setup runs the
// supervisor on the repository root given as root (AdoptSupervisorYAML), and returns
// the file's name, the setup and the download its zcp-devsup line installs —
// url, or the one a line the setup already carried names.
func AdoptSupervisorInDevSetup(ctx context.Context, ssh SSHDeployer, hostname, setup, url, dir, root string) (file, used, download string, changed bool, err error) {
	download = url
	file, used, changed, err = rewriteDevSetup(ctx, ssh, hostname, dir, func(data []byte) ([]byte, string, bool, error) {
		out, used, changed, err := AdoptSupervisorYAML(data, setup, hostname, url, root)
		if err == nil {
			download = devsupDownloadIn(out, used, hostname, url)
		}
		return out, used, changed, err
	})
	return file, used, download, changed, err
}

// devsupDownloadIn is the download the zcp-devsup line of setup in data
// installs, fallback when it is unreadable.
func devsupDownloadIn(data []byte, setup, hostname, fallback string) string {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil {
		return fallback
	}
	entry, _, err := devSetupEntry(&doc, setup, hostname)
	if err != nil {
		return fallback
	}
	if line := devsupInstallLine(mappingValue(entry, "run")); line != "" {
		return pinnedDownload(line)
	}
	return fallback
}

// RepinSupervisorYAML points the zcp-devsup install line the dev setup already
// carries at url, changing nothing else; from is the download the line named
// before, "" when the setup carries no such line or it names url already. A
// dev setup without the line stays as written: the first dev-server start
// moves it onto the supervisor. setup is picked as in AdoptSupervisorYAML.
func RepinSupervisorYAML(data []byte, setup, hostname, url string) (out []byte, used, from string, err error) {
	if !bytes.Contains(data, []byte("/"+DevsupAsset)) {
		return data, setup, "", nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, "", "", fmt.Errorf("zerops.yaml: %w", err)
	}
	entry, setup, err := devSetupEntry(&doc, setup, hostname)
	if err != nil {
		return nil, "", "", err
	}
	prepare := mappingValue(mappingValue(entry, "run"), "prepareCommands")
	if prepare == nil || prepare.Kind != yaml.SequenceNode {
		return data, setup, "", nil
	}
	line := DevsupPrepareCommand(url)
	for _, item := range prepare.Content {
		if item.Kind == yaml.ScalarNode && strings.Contains(item.Value, "/"+DevsupAsset) && item.Value != line {
			from = pinnedDownload(item.Value)
			break
		}
	}
	if from == "" {
		return data, setup, "", nil
	}
	src := newYAMLSource(data, &doc)
	splices, err := prepareSplice(src, prepare, setup, line)
	if err != nil {
		return nil, "", "", err
	}
	out = src.apply(splices)
	if err := spliceVerified(data, out, setup, supervisorLines{prepare: line}); err != nil {
		return nil, "", "", fmt.Errorf("zerops.yaml setup %q: %w; replace its zcp-devsup line in run.prepareCommands by hand with: %s", setup, err, line)
	}
	return out, setup, from, nil
}

// pinnedDownload is the URL a zcp-devsup install line downloads, the whole
// line when it is not one zcp wrote.
func pinnedDownload(line string) string {
	_, rest, ok := strings.Cut(line, "u=")
	url, _, end := strings.Cut(rest, ";")
	if !ok || !end || !strings.HasSuffix(url, "/"+DevsupAsset) {
		return line
	}
	return url
}

// RepinSupervisorInDevSetup rewrites the zerops.yaml in hostname's dir (as
// AdoptSupervisorInDevSetup) so its dev setup's zcp-devsup line installs url
// (RepinSupervisorYAML); from is "" when nothing changed.
func RepinSupervisorInDevSetup(ctx context.Context, ssh SSHDeployer, hostname, setup, url, dir string) (file, used, from string, err error) {
	file, used, _, err = rewriteDevSetup(ctx, ssh, hostname, dir, func(data []byte) ([]byte, string, bool, error) {
		out, used, pinned, err := RepinSupervisorYAML(data, setup, hostname, url)
		from = pinned
		return out, used, pinned != "", err
	})
	if err != nil {
		return file, used, "", err
	}
	return file, used, from, nil
}

// rewriteDevSetup reads the zerops.yaml (or zerops.yml) in hostname's dir, ""
// for /var/www, and writes back what edit makes of it when edit changed it.
func rewriteDevSetup(ctx context.Context, ssh SSHDeployer, hostname, dir string, edit func([]byte) ([]byte, string, bool, error)) (file, used string, changed bool, err error) {
	if dir == "" {
		dir = DevRepoRoot
	}
	readCtx, cancel := context.WithTimeout(ctx, containerIdentityTimeout)
	defer cancel()
	file, body, err := ReadRepoConfigFile(readCtx, ssh, hostname, dir)
	if err != nil {
		return "", "", false, err
	}
	if file == "" {
		return "", "", false, fmt.Errorf("%s has no zerops.yaml (or zerops.yml) in %s", hostname, dir)
	}
	updated, used, changed, err := edit([]byte(body))
	if err != nil {
		// The parse and splice messages name the config file generically; the
		// file is known here, so the agent is told its real name and never
		// has reason to rename it.
		return file, used, false, configFileError{err: err, file: file}
	}
	if !changed {
		return file, used, false, nil
	}
	target := strings.TrimSuffix(dir, "/") + "/" + file
	cmd := fmt.Sprintf(`printf '%%s' %s > %s && mv -f %s %s`,
		shellQuote(string(updated)), shellQuote(target+".zcp-tmp"), shellQuote(target+".zcp-tmp"), shellQuote(target))
	writeCtx, cancelWrite := context.WithTimeout(ctx, containerIdentityTimeout)
	defer cancelWrite()
	if _, err := ssh.ExecSSH(writeCtx, hostname, cmd); err != nil {
		return file, used, false, fmt.Errorf("write %s on %s: %w", file, hostname, err)
	}
	return file, used, true, nil
}

// crewDirtyCmd prints a line per crew copy (a git worktree in .crew/<name>)
// whose uncommitted or untracked changes a redeploy would lose; a copy git
// cannot read counts as dirty.
const crewDirtyCmd = `cd '` + DevRepoRoot + `/.crew' 2>/dev/null || exit 0; for d in */; do d=${d%/}; [ -e "$d/.git" ] || continue; out=$(GIT_OPTIONAL_LOCKS=0 git -C "$d" status --porcelain 2>/dev/null) || out=unreadable; [ -z "$out" ] || echo "@@zcp-crew-dirty $d"; done`

// DirtyCrewCopies names hostname's crew copies (.crew/<name>) that hold
// uncommitted or untracked work.
func DirtyCrewCopies(ctx context.Context, ssh SSHDeployer, hostname string) ([]string, error) {
	readCtx, cancel := context.WithTimeout(ctx, containerIdentityTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(readCtx, hostname, crewDirtyCmd)
	if err != nil {
		return nil, fmt.Errorf("read the crew copies on %s: %w", hostname, err)
	}
	var dirty []string
	for line := range strings.Lines(string(out)) {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "@@zcp-crew-dirty "); ok {
			dirty = append(dirty, ".crew/"+name)
		}
	}
	return dirty, nil
}

// DeclareDevServerForStart writes the declaration entry a start of p asks for
// on a dev service without the supervisor yet, and returns the server's name.
func DeclareDevServerForStart(ctx context.Context, ssh SSHDeployer, p DevServerParams) (string, error) {
	ds, err := ReadDevServers(ctx, ssh, p.Hostname, p.Root)
	var noSup *NoSupervisorError
	if err != nil && !errors.As(err, &noSup) {
		return "", err
	}
	if ds.DeclarationErr != nil {
		return "", declarationProblem(ds)
	}
	name, err := resolveServerName(p, ds, true)
	if err != nil {
		return "", err
	}
	declared, isDeclared := ds.Server(name)
	want, err := wantedServer(p, name, declared, isDeclared)
	if err != nil {
		return "", err
	}
	if decl, changed := DeclareDevServer(ds.Declared, want); changed {
		if err := WriteDevDeclaration(ctx, ssh, p.Hostname, p.Root, decl); err != nil {
			return "", err
		}
	}
	return name, nil
}

// DevServersAfterDeploy reads hostname's dev servers right after a deploy
// replaced its container: the supervisor starts every declared server by
// itself, so a dev service with exactly one declared server reports it as
// its start would (health probe on its port). *NoSupervisorError when its
// dev setup does not run the supervisor.
func DevServersAfterDeploy(ctx context.Context, ssh SSHDeployer, hostname, root string) (*DevServers, *DevServerResult, error) {
	ds, err := ReadDevServers(ctx, ssh, hostname, root)
	if err != nil {
		return nil, nil, err
	}
	servers := ds.Servers()
	if len(servers) != 1 {
		return ds, nil, nil
	}
	server := servers[0]
	result := newResult(devServerActionStart, hostname, server.Name)
	st := ds.State.Servers[server.Name]
	result.LogFile, result.PID = st.LogPath, st.Pid
	switch {
	case st.Status == devsup.StatusCrashed:
		applyCrash(ctx, ssh, result, server.Name, st)
	case st.Status == devsup.StatusStopped || st.Status == "":
		// Declared with autostart false, or not run by this supervisor.
		result.Reason = reasonStopped
		result.Message = fmt.Sprintf("Dev server %s is declared but not running; start it with zerops_dev_server action=start hostname=%s.", serverLabel(hostname, server.Name), hostname)
	case server.Port == 0:
		result.Message = fmt.Sprintf("Dev server %s is %s under the supervisor.", serverLabel(hostname, server.Name), st.Status)
	default:
		if result, err = probeStarted(ctx, ssh, ds, result, DevServerParams{Hostname: hostname}, server.Name, server.Port, ds.ReadAt); err != nil {
			return nil, nil, err
		}
	}
	result.settle(ds)
	return ds, result, nil
}

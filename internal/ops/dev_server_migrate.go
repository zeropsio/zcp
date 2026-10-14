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

// DevsupAsset is the release asset zcp-devsup ships as.
const DevsupAsset = "zcp-devsup-linux-amd64"

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
// zcp-devsup into the runtime image (never $HOME: a redeploy loses it).
func DevsupPrepareCommand(url string) string {
	return "sudo sh -c 'curl -fsSL " + url + " -o /usr/local/bin/zcp-devsup && chmod 0755 /usr/local/bin/zcp-devsup'"
}

// AdoptSupervisorYAML puts the dev setup's run under the supervisor: the
// install line in run.prepareCommands (replacing an older zcp-devsup line) and
// run.start `zcp-devsup run`. setup names the dev setup; empty picks the only
// setup, else the one named after hostname, else the one named "dev". It
// returns the setup it changed; changed is false when both lines are there.
func AdoptSupervisorYAML(data []byte, setup, hostname, url string) (out []byte, used string, changed bool, err error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, "", false, fmt.Errorf("zerops.yaml: %w", err)
	}
	run, setup, err := setupRun(&doc, setup, hostname)
	if err != nil {
		return nil, "", false, err
	}
	out, changed, err = adoptSupervisorRun(&doc, run, setup, url)
	if err != nil || !changed {
		return data, setup, false, err
	}
	return out, setup, true, nil
}

// adoptSupervisorRun edits run in doc and re-encodes doc when it changed.
func adoptSupervisorRun(doc, run *yaml.Node, setup, url string) ([]byte, bool, error) {
	line := DevsupPrepareCommand(url)
	changed := false
	prepare := mappingValue(run, "prepareCommands")
	switch {
	case prepare == nil:
		prepare = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setMapping(run, "prepareCommands", prepare)
		fallthrough
	case prepare.Kind == yaml.SequenceNode:
		found := false
		for _, item := range prepare.Content {
			if item.Kind == yaml.ScalarNode && strings.Contains(item.Value, "/"+DevsupAsset) {
				found = true
				if item.Value != line {
					item.Value, item.Style, changed = line, 0, true
				}
			}
		}
		if !found {
			prepare.Content = append(prepare.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: line})
			changed = true
		}
	default:
		return nil, false, fmt.Errorf("zerops.yaml setup %q: run.prepareCommands is not a list", setup)
	}
	start := mappingValue(run, "start")
	if start == nil || start.Kind != yaml.ScalarNode || strings.TrimSpace(start.Value) != DevsupStart {
		setMapping(run, "start", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: DevsupStart})
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, false, fmt.Errorf("zerops.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, false, fmt.Errorf("zerops.yaml: %w", err)
	}
	return out.Bytes(), true, nil
}

// setupRun finds the dev setup's run mapping (creating an empty one when the
// setup has none) and the setup's name.
func setupRun(doc *yaml.Node, setup, hostname string) (*yaml.Node, string, error) {
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
		case entries["dev"] != nil:
			setup = "dev"
		default:
			return nil, "", fmt.Errorf("zerops.yaml has several setups (%s) and none is named %s or dev — zcp cannot tell the dev setup", strings.Join(names, ", "), hostname)
		}
	}
	entry := entries[setup]
	if entry == nil {
		return nil, "", fmt.Errorf("zerops.yaml has no setup %q (setups: %s)", setup, strings.Join(names, ", "))
	}
	run := mappingValue(entry, "run")
	if run == nil {
		run = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMapping(entry, "run", run)
	}
	if run.Kind != yaml.MappingNode {
		return nil, "", fmt.Errorf("zerops.yaml setup %q: run is not a mapping", setup)
	}
	return run, setup, nil
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

// setMapping sets key in mapping m to v, appending the key when absent.
func setMapping(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

// readZeropsYAMLCmd prints the dev repo's zerops.yaml (or zerops.yml) after a
// line naming the file.
const readZeropsYAMLCmd = `for f in zerops.yaml zerops.yml; do if [ -f '` + DevRepoRoot + `'/"$f" ]; then echo "@@zcp-file $f"; cat '` + DevRepoRoot + `'/"$f"; exit 0; fi; done; echo "@@zcp-file -"`

// AdoptSupervisorInDevSetup rewrites hostname's zerops.yaml so its dev setup
// runs the supervisor (AdoptSupervisorYAML) and returns the file's name and
// the setup.
func AdoptSupervisorInDevSetup(ctx context.Context, ssh SSHDeployer, hostname, setup, url string) (file, used string, changed bool, err error) {
	readCtx, cancel := context.WithTimeout(ctx, containerIdentityTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(readCtx, hostname, readZeropsYAMLCmd)
	if err != nil {
		return "", "", false, fmt.Errorf("read zerops.yaml on %s: %w", hostname, err)
	}
	head, body, _ := strings.Cut(string(out), "\n")
	file, ok := strings.CutPrefix(strings.TrimSpace(head), "@@zcp-file ")
	if !ok || file == "-" {
		return "", "", false, fmt.Errorf("%s has no zerops.yaml in %s", hostname, DevRepoRoot)
	}
	updated, used, changed, err := AdoptSupervisorYAML([]byte(body), setup, hostname, url)
	if err != nil || !changed {
		return file, used, false, err
	}
	target := DevRepoRoot + "/" + file
	cmd := fmt.Sprintf(`printf '%%s' %s > %s && mv -f %s %s`,
		shellQuote(string(updated)), shellQuote(target+".zcp-tmp"), shellQuote(target+".zcp-tmp"), shellQuote(target))
	writeCtx, cancelWrite := context.WithTimeout(ctx, containerIdentityTimeout)
	defer cancelWrite()
	if _, err := ssh.ExecSSH(writeCtx, hostname, cmd); err != nil {
		return file, used, false, fmt.Errorf("write %s on %s: %w", file, hostname, err)
	}
	return file, used, true, nil
}

// DeclareDevServerForStart writes the declaration entry a start of p asks for
// on a dev service without the supervisor yet, and returns the server's name.
func DeclareDevServerForStart(ctx context.Context, ssh SSHDeployer, p DevServerParams) (string, error) {
	ds, err := ReadDevServers(ctx, ssh, p.Hostname)
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
		if err := WriteDevDeclaration(ctx, ssh, p.Hostname, decl); err != nil {
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
func DevServersAfterDeploy(ctx context.Context, ssh SSHDeployer, hostname string) (*DevServers, *DevServerResult, error) {
	ds, err := ReadDevServers(ctx, ssh, hostname)
	if err != nil {
		return nil, nil, err
	}
	if len(ds.Declared.Servers) != 1 {
		return ds, nil, nil
	}
	server := ds.Declared.Servers[0]
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

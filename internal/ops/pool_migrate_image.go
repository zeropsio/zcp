package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// The image step of pool-migrate (docs/spec-workflows.md §13 PM-11) deploys a
// part's pool setup to its pool from a commit, never from the dev service's
// working tree: a deploy of a pool bakes what it ships into the image of every
// container the pool creates after it. Before the step names that deploy it
// checks the pool setup in the commit — read with read-only git on the dev
// service, parsed by the zerops.yaml and declaration parsers every other reader
// uses — and it is done once the pool runs an app version newer than the
// commit's last change of its config file.

// The image step's checks. A missing check holds the deploy: the step names
// none until the commit passes it. A dependency check is a finding, named
// beside the deploy and never holding it.
const (
	PoolMigrateImageCommit       = "commit"
	PoolMigrateImageConfig       = "config-file"
	PoolMigrateImageSetup        = "pool-setup"
	PoolMigrateImageStart        = "start"
	PoolMigrateImageInstall      = "install"
	PoolMigrateImagePreviewBase  = "preview-port-base"
	PoolMigrateImagePreviewPorts = "preview-ports"
	PoolMigrateImageService      = "service"
	PoolMigrateImageDepsLockfile = "deps-lockfile"
	PoolMigrateImageDepsBaked    = "deps-baked"
)

// poolMigrateHighestPort is the highest port a zerops.yaml run.ports entry
// may declare (zerops-docs zerops-yaml/specification.mdx, ports: 10–65435).
const poolMigrateHighestPort = 65435

// PoolMigrateImage is what the image step reads of the commit its deploy
// ships and of the app version the pool runs.
type PoolMigrateImage struct {
	// Commit is the commit the checks read and the deploy ships: the dev
	// service's checkout's HEAD, in full; "" when it has none.
	Commit string `json:"commit,omitempty"`
	// Branch is the branch HEAD is on, "" when detached.
	Branch string `json:"branch,omitempty"`
	// File is the config file the commit carries: zerops.yaml, else
	// zerops.yml — the one a deploy of the commit reads.
	File string `json:"file,omitempty"`
	// Setup is the pool setup: the setup named after the pool, the one a
	// deploy to the pool builds.
	Setup string `json:"setup"`
	// PreviewPortBase is the commit's .zcp/dev-servers.json previewPortBase,
	// 0 when it sets none.
	PreviewPortBase int `json:"previewPortBase,omitempty"`
	// MaxContainers is the pool's maxContainers: how many preview ports the
	// setup declares.
	MaxContainers int `json:"maxContainers"`
	// Missing names each check the commit fails; while it names any, the
	// step names no deploy.
	Missing []PoolMigrateImageCheck `json:"missing,omitempty"`
	// Findings name where the setup departs from the dependency rule; they
	// never hold the deploy.
	Findings []PoolMigrateImageCheck `json:"findings,omitempty"`
	// Uncommitted names the pool setup's files the dev checkout changed
	// since the commit, as far as the census lists them: the deploy ships
	// the commit without those changes.
	Uncommitted []string `json:"uncommitted,omitempty"`
	// Deployed is the app version the pool runs, nil when it runs none.
	Deployed *PoolMigrateImageDeployed `json:"deployed,omitempty"`
	// Done is true when the commit passes every check and the pool runs an
	// app version created at or after the commit's last change of File.
	Done bool `json:"done"`
}

// PoolMigrateImageCheck is one check the commit fails, or one finding, with
// what to change.
type PoolMigrateImageCheck struct {
	Check   string `json:"check"`
	Message string `json:"message"`
}

// PoolMigrateImageDeployed is the app version the pool runs: when it was
// created, when the commit last changed its config file, and whether it is
// at least that new. The platform's app-version reads name no commit, so the
// time is the evidence that the pool runs this setup, never a proof.
type PoolMigrateImageDeployed struct {
	Created           string `json:"created"`
	ConfigCommittedAt string `json:"configCommittedAt,omitempty"`
	Current           bool   `json:"current"`
}

// PoolMigrateImageInput is what the image step reads.
type PoolMigrateImageInput struct {
	// Dev is the dev service whose repository holds the commit, read over
	// SSH; Root is that repository's root there (/var/www).
	Dev  string
	Root string
	// Head is the census's main checkout: its HEAD is the commit, and the
	// paths it names say what is not committed yet.
	Head PoolMigrateTree
	// Pool is the part's pool as the direct service list reads it: its name
	// names the setup, its scale the preview ports, its active app version
	// what it runs.
	Pool platform.ServiceStack
	// DevSetup is the dev service's own setup, whose zcp-devsup install line
	// the pool setup takes; "" falls back to the setup named after Dev,
	// then "dev".
	DevSetup string
	// FallbackMaxContainers stands in for a pool that reads no maxContainers.
	FallbackMaxContainers int
	// PartPools are the pools of every part, Pool's among them, when the
	// repository serves several parts — each moving onto a pool of its own —
	// and nil for a single part. With them the declaration is checked to name
	// each part's pool as its servers' service.
	PartPools []string
}

// poolMigrateCommitRead is what the read found at the commit.
type poolMigrateCommitRead struct {
	// noCommit: the repository does not hold the commit.
	noCommit bool
	// configFile and config are the config file the commit carries, ""
	// for none.
	configFile string
	config     string
	// declaration is the commit's .zcp/dev-servers.json, hasDeclaration
	// false when it carries none.
	declaration    string
	hasDeclaration bool
	// configChangedAt is the committer date (ISO 8601) of the commit's last
	// change of configFile.
	configChangedAt string
}

// The read's framing (poolMigrateImageScript).
const (
	poolMigrateImageHeader = "zcp-image 1"
	poolMigrateImageEnd    = "zcp-image-end"
	poolMigrateImageError  = "zcp-image-error "
)

// poolMigrateImageScript reads, at the commit $c, the config file a deploy of
// it reads — zerops.yaml, else zerops.yml — and the dev-server declaration,
// each byte for byte after a line naming its size and path, then the
// committer date of the commit's last change of the config file. Every git
// command in it reads objects only. A commit the repository does not hold
// prints N; a step that fails prints a zcp-image-error line and the read
// stops without its end line, so a read cut short is never a read.
//
//	F <size> <path>   followed by exactly <size> bytes and a newline
//	C <date>          git log --format=%cI of the config file
//	N                 the repository does not hold the commit
const poolMigrateImageScript = `export GIT_OPTIONAL_LOCKS=0 LC_ALL=C
set -f
echo '` + poolMigrateImageHeader + `'
if ! git cat-file -e "$c^{commit}" 2>/dev/null; then echo N; echo '` + poolMigrateImageEnd + `'; exit 0; fi
cf=
for f in ` + zeropsYmlPrimary + ` ` + zeropsYmlFallback + `; do
  if [ "$(git cat-file -t "$c:$f" 2>/dev/null)" = blob ]; then cf=$f; break; fi
done
for f in $cf ` + devsup.DeclarationPath + `; do
  [ "$(git cat-file -t "$c:$f" 2>/dev/null)" = blob ] || continue
  s=$(git cat-file -s "$c:$f") || { echo "` + poolMigrateImageError + `size $f"; exit 0; }
  printf 'F %s %s\n' "$s" "$f"
  git cat-file blob "$c:$f" || { echo; echo "` + poolMigrateImageError + `read $f"; exit 0; }
  echo
done
if [ -n "$cf" ]; then
  t=$(git log -1 --no-show-signature --format=%cI "$c" -- "$cf") || { echo '` + poolMigrateImageError + `log'; exit 0; }
  printf 'C %s\n' "$t"
fi
echo '` + poolMigrateImageEnd + `'`

// PoolMigrateImageCommand is the one SSH command that reads commit's pool
// setup files in the repository rooted at root.
func PoolMigrateImageCommand(root, commit string) string {
	return "cd " + shellQuote(root) + " || exit 3\nc=" + shellQuote(commit) + "\n" + poolMigrateImageScript
}

// ReadPoolMigrateImage reads the pool setup in the dev service's HEAD commit,
// in one SSH round trip of read-only git, and checks it and the app version
// the pool runs (checkPoolMigrateImage). A checkout with no commit is checked
// without SSH. An SSH failure keeps its *platform.SSHExecError (git's stderr
// rides its Output); a read the script could not finish is an error, never an
// empty commit.
func ReadPoolMigrateImage(ctx context.Context, ssh SSHDeployer, in PoolMigrateImageInput) (*PoolMigrateImage, error) {
	if !poolMigrateCommitNamed(in.Head.Head) {
		return checkPoolMigrateImage(in, poolMigrateCommitRead{}), nil
	}
	short := poolMigrateShort(in.Head.Head)
	out, err := ssh.ExecSSH(ctx, in.Dev, PoolMigrateImageCommand(in.Root, in.Head.Head))
	if err != nil {
		return nil, fmt.Errorf("read the pool setup at %s on %s: %w", short, in.Dev, err)
	}
	read, err := parsePoolMigrateImageRead(string(out))
	if err != nil {
		return nil, fmt.Errorf("read the pool setup at %s on %s: %w", short, in.Dev, err)
	}
	return checkPoolMigrateImage(in, read), nil
}

// parsePoolMigrateImageRead reads the script's output: each file by its
// size, so content that holds the framing's own lines stays content.
func parsePoolMigrateImageRead(out string) (poolMigrateCommitRead, error) {
	var r poolMigrateCommitRead
	var noise []string
	header := false
	rest := out
	for rest != "" {
		line, after, _ := strings.Cut(rest, "\n")
		rest = after
		if !header {
			if line == poolMigrateImageHeader {
				header = true
			} else if strings.TrimSpace(line) != "" {
				noise = append(noise, line)
			}
			continue
		}
		switch {
		case line == poolMigrateImageEnd:
			return r, nil
		case line == "N":
			r.noCommit = true
		case strings.HasPrefix(line, "F "):
			sizeText, path, _ := strings.Cut(strings.TrimPrefix(line, "F "), " ")
			size, err := strconv.Atoi(sizeText)
			if err != nil || size < 0 || path == "" {
				return r, fmt.Errorf("the read of the commit names a file oddly: %q", line)
			}
			if len(rest) < size+1 || rest[size] != '\n' {
				return r, fmt.Errorf("the read of %s at the commit came back cut short", path)
			}
			body := rest[:size]
			rest = rest[size+1:]
			if path == devsup.DeclarationPath {
				r.declaration, r.hasDeclaration = body, true
			} else {
				r.configFile, r.config = path, body
			}
		case strings.HasPrefix(line, "C "):
			r.configChangedAt = strings.TrimSpace(strings.TrimPrefix(line, "C "))
		case strings.HasPrefix(line, poolMigrateImageError):
			return r, poolMigrateNoiseError("could not read "+strings.TrimPrefix(line, poolMigrateImageError)+" at the commit", noise)
		default:
			if strings.TrimSpace(line) != "" {
				noise = append(noise, line)
			}
		}
	}
	if !header {
		return r, poolMigrateNoiseError("the dev service answered no read of the commit", noise)
	}
	return r, poolMigrateNoiseError("the read of the commit came back incomplete", noise)
}

// poolMigrateNoiseError is msg with what else the session printed.
func poolMigrateNoiseError(msg string, noise []string) error {
	if len(noise) == 0 {
		return errors.New(msg)
	}
	return fmt.Errorf("%s: %s", msg, strings.Join(noise, "; "))
}

// poolMigrateCommitNamed reports whether head names a commit: a checkout on
// an unborn branch names none (git prints the all-zero object name).
func poolMigrateCommitNamed(head string) bool {
	return strings.Trim(head, "0") != ""
}

// poolMigrateShort is how much of a commit a message names: what
// deploy-from-commit's own messages name.
func poolMigrateShort(sha string) string {
	return sha[:min(10, len(sha))]
}

// checkPoolMigrateImage checks the pool setup the commit carries and the app
// version the pool runs.
func checkPoolMigrateImage(in PoolMigrateImageInput, read poolMigrateCommitRead) *PoolMigrateImage {
	img := &PoolMigrateImage{
		Commit:        in.Head.Head,
		Branch:        in.Head.Branch,
		Setup:         in.Pool.Name,
		MaxContainers: poolMigrateMaxContainers(in.Pool, in.FallbackMaxContainers),
	}
	miss := func(check, msg string) {
		img.Missing = append(img.Missing, PoolMigrateImageCheck{Check: check, Message: msg})
	}
	short := poolMigrateShort(in.Head.Head)
	switch {
	case !poolMigrateCommitNamed(in.Head.Head):
		img.Commit = ""
		miss(PoolMigrateImageCommit, fmt.Sprintf("%s's checkout has no commit yet, so there is nothing to deploy to the pool: commit the pool setup, then call again.", in.Dev))
		return img
	case read.noCommit:
		miss(PoolMigrateImageCommit, fmt.Sprintf("Commit %s, the checkout's HEAD when the census read it, is not in %s's repository now: call again.", short, in.Dev))
		return img
	}
	img.File = read.configFile
	img.Uncommitted = poolMigrateUncommitted(in.Head, read.configFile)
	img.Deployed = poolMigrateImageDeployed(in.Pool, read.configChangedAt)

	var entry *ZeropsYmlEntry
	var doc *ZeropsYmlDoc
	switch parsed, err := ParseZeropsYmlContent([]byte(read.config), read.configFile); {
	case read.configFile == "":
		miss(PoolMigrateImageConfig, fmt.Sprintf("Commit %s carries no %s (or %s): add the pool's setup %q to the repository's %s and commit it.", short, zeropsYmlPrimary, zeropsYmlFallback, img.Setup, zeropsYmlPrimary))
	case err != nil:
		miss(PoolMigrateImageConfig, fmt.Sprintf("%s at %s does not parse: %v.", read.configFile, short, err))
	default:
		doc = parsed
		if entry = doc.FindEntry(img.Setup); entry == nil {
			miss(PoolMigrateImageSetup, fmt.Sprintf("%s at %s has no setup %q (setups: %s): add the pool's setup under the pool's name — a deploy to the pool builds the setup named after it.", read.configFile, short, img.Setup, strings.Join(doc.SetupNames(), ", ")))
		}
	}
	if entry != nil {
		if start := strings.TrimSpace(entry.Run.Start); start != DevsupStart {
			ran := "no run.start"
			if start != "" {
				ran = "run.start `" + start + "`"
			}
			miss(PoolMigrateImageStart, fmt.Sprintf("Setup %q has %s, not `%s`: a pool container runs its dev servers under the supervisor, so its run.start is `%s`.", img.Setup, ran, DevsupStart, DevsupStart))
		}
		if poolMigrateDevsupLine(yamlStrings(entry.Run.PrepareCommands)) == "" {
			miss(PoolMigrateImageInstall, poolMigrateInstallMessage(img.Setup, doc, in))
		}
	}
	base, baseOK := poolMigratePreviewBase(read, short, img.MaxContainers, miss)
	img.PreviewPortBase = base
	if entry != nil && baseOK {
		if msg := poolMigratePreviewPortsMessage(entry, img.Setup, base, img.MaxContainers); msg != "" {
			miss(PoolMigrateImagePreviewPorts, msg)
		}
	}
	if msg := poolMigrateServiceMessage(read, img.Setup, in.PartPools, short); msg != "" {
		miss(PoolMigrateImageService, msg)
	}
	if entry != nil {
		img.Findings = poolMigrateDependencyFindings(entry, img.Setup, read)
	}
	img.Done = len(img.Missing) == 0 && img.Deployed != nil && img.Deployed.Current
	return img
}

// poolMigrateMaxContainers is the pool's maxContainers, capped at what a pool
// holds; fallback when the pool reads none.
func poolMigrateMaxContainers(pool platform.ServiceStack, fallback int) int {
	if a := effectiveAutoscaling(&pool); a != nil && a.HorizontalMaxCount > 0 {
		return min(int(a.HorizontalMaxCount), poolContainerCeiling)
	}
	return fallback
}

// poolMigrateImageDeployed is the app version the pool runs, nil for none or
// for the placeholder a startWithoutCode import leaves: current when it was
// created at or after the commit's last change of the config file.
func poolMigrateImageDeployed(pool platform.ServiceStack, configChangedAt string) *PoolMigrateImageDeployed {
	if !pool.HasDeployedCode() {
		return nil
	}
	d := &PoolMigrateImageDeployed{Created: pool.ActiveAppVersion.Created, ConfigCommittedAt: configChangedAt}
	created, createdErr := time.Parse(time.RFC3339Nano, d.Created)
	changed, changedErr := time.Parse(time.RFC3339, configChangedAt)
	d.Current = createdErr == nil && changedErr == nil && !created.IsZero() && !created.Before(changed)
	return d
}

// poolMigrateDevsupLine is the item of prepareCommands that installs
// zcp-devsup — the download line AdoptSupervisorYAML recognizes — "" for none.
func poolMigrateDevsupLine(prepareCommands []string) string {
	for _, c := range prepareCommands {
		if strings.Contains(c, "/"+DevsupAsset) {
			return c
		}
	}
	return ""
}

// poolMigrateInstallMessage says how the pool setup installs zcp-devsup: with
// the line the dev setup installs it with, when it carries one.
func poolMigrateInstallMessage(setup string, doc *ZeropsYmlDoc, in PoolMigrateImageInput) string {
	msg := fmt.Sprintf("Setup %q's run.prepareCommands install no zcp-devsup, and a pool's image carries the supervisor: ", setup)
	for _, name := range []string{in.DevSetup, in.Dev, devSetupName} {
		e := doc.FindEntry(name)
		if name == "" || e == nil {
			continue
		}
		if line := poolMigrateDevsupLine(yamlStrings(e.Run.PrepareCommands)); line != "" {
			return msg + fmt.Sprintf("add the line the dev setup %q installs it with — %s", name, line)
		}
	}
	return msg + fmt.Sprintf("add the zcp-devsup install line a dev setup gets when its first dev server starts under the supervisor (zerops_dev_server action=start hostname=%q puts it in the dev setup).", in.Dev)
}

// poolMigratePreviewBase reads previewPortBase from the commit's declaration,
// naming what is missing; ok is false when there is none to check the ports
// against.
func poolMigratePreviewBase(read poolMigrateCommitRead, short string, maxContainers int, miss func(check, msg string)) (base int, ok bool) {
	const how = "a lease answers its preview on previewPortBase + its container's stable number (ZEROPS_StableNumber), 1 … maxContainers"
	if !read.hasDeclaration {
		miss(PoolMigrateImagePreviewBase, fmt.Sprintf("Commit %s carries no %s, so no previewPortBase is set: declare the part's dev servers there with `\"previewPortBase\": <port>` (%s) and commit it.", short, devsup.DeclarationPath, how))
		return 0, false
	}
	decl, err := devsup.ParseDeclaration([]byte(read.declaration))
	switch {
	case err != nil:
		miss(PoolMigrateImagePreviewBase, fmt.Sprintf("%s at %s does not parse: %v.", devsup.DeclarationPath, short, err))
		return 0, false
	case decl.PreviewPortBase == 0:
		miss(PoolMigrateImagePreviewBase, fmt.Sprintf("%s at %s sets no previewPortBase: add `\"previewPortBase\": <port>` (%s) and commit it.", devsup.DeclarationPath, short, how))
		return 0, false
	case decl.PreviewPortBase+maxContainers > poolMigrateHighestPort:
		miss(PoolMigrateImagePreviewBase, fmt.Sprintf("previewPortBase %d in %s at %s puts the last preview port at %d, past %d, the highest port a container declares: lower it.", decl.PreviewPortBase, devsup.DeclarationPath, short, decl.PreviewPortBase+maxContainers, poolMigrateHighestPort))
		return decl.PreviewPortBase, false
	}
	return decl.PreviewPortBase, true
}

// poolMigratePreviewPortsMessage names the preview ports the setup's
// run.ports lacks or declares without httpSupport, "" when every one is
// declared with it.
func poolMigratePreviewPortsMessage(entry *ZeropsYmlEntry, setup string, base, maxContainers int) string {
	withHTTP := map[int]bool{}
	for _, p := range entry.Run.Ports {
		withHTTP[p.Port] = withHTTP[p.Port] || p.HTTPSupport
	}
	var lacks, plain []string
	for port := base + 1; port <= base+maxContainers; port++ {
		has, declared := withHTTP[port]
		switch {
		case !declared:
			lacks = append(lacks, strconv.Itoa(port))
		case !has:
			plain = append(plain, strconv.Itoa(port))
		}
	}
	if len(lacks) == 0 && len(plain) == 0 {
		return ""
	}
	var gaps []string
	if len(lacks) > 0 {
		gaps = append(gaps, "lacks "+strings.Join(lacks, ", "))
	}
	if len(plain) > 0 {
		gaps = append(gaps, "declares "+strings.Join(plain, ", ")+" without httpSupport: true")
	}
	return fmt.Sprintf("Setup %q's run.ports %s. A lease answers its preview on previewPortBase + its container's stable number (ZEROPS_StableNumber), 1 … %d (the pool's maxContainers), and a per-port subdomain routes only to a port declared with httpSupport: true, so ports %d to %d each need one.",
		setup, strings.Join(gaps, " and "), maxContainers, base+1, base+maxContainers)
}

// poolMigrateServiceMessage says how the commit's declaration fails a
// repository of several parts, "" when it does not or the repository is one
// part (parts nil). A pool container's supervisor runs the entries naming its
// pool and those naming none (devsup.Declaration.ForService): the part's
// servers must name pool, an entry naming no service would run in every
// part's leases, and one naming a service no part's pool is would run in
// none. A declaration missing or not parsing is the preview-port-base check's.
func poolMigrateServiceMessage(read poolMigrateCommitRead, pool string, parts []string, short string) string {
	if len(parts) == 0 || !read.hasDeclaration {
		return ""
	}
	decl, err := devsup.ParseDeclaration([]byte(read.declaration))
	if err != nil {
		return ""
	}
	var own bool
	var unnamed, stray []string
	for _, s := range decl.Servers {
		switch {
		case s.Service == pool:
			own = true
		case s.Service == "":
			unnamed = append(unnamed, s.Name)
		case !slices.Contains(parts, s.Service):
			stray = append(stray, fmt.Sprintf("%s (service %s)", s.Name, s.Service))
		}
	}
	var gaps []string
	if !own {
		gaps = append(gaps, fmt.Sprintf("no server names %q as its service, so a lease of the part runs none of its dev servers: give each of them `\"service\": \"%s\"`", pool, pool))
	}
	if len(unnamed) > 0 {
		gaps = append(gaps, fmt.Sprintf("%s name no service, so every part's leases run them: give each the pool of its part", strings.Join(unnamed, ", ")))
	}
	if len(stray) > 0 {
		gaps = append(gaps, fmt.Sprintf("%s name a service no part's pool is (%s), so no lease runs them", strings.Join(stray, ", "), strings.Join(parts, ", ")))
	}
	if len(gaps) == 0 {
		return ""
	}
	return fmt.Sprintf("In %s at %s, %s. Each part moves onto a pool of its own, and a pool's container runs the servers naming its pool and those naming none.",
		devsup.DeclarationPath, short, strings.Join(gaps, "; "))
}

// poolMigrateDependencyFindings names where the setup departs from the
// dependency rule: the dependencies baked into /var/www by run.prepareCommands,
// from every file the install reads, which build.addToRunPrepare carries.
func poolMigrateDependencyFindings(entry *ZeropsYmlEntry, setup string, read poolMigrateCommitRead) []PoolMigrateImageCheck {
	var out []PoolMigrateImageCheck
	if len(yamlStrings(entry.Build.AddToRunPrepare)) == 0 {
		out = append(out, PoolMigrateImageCheck{Check: PoolMigrateImageDepsLockfile, Message: fmt.Sprintf(
			"Setup %q lists nothing in build.addToRunPrepare: list there every file the dependency install reads — the manifest and its lockfile, and in a workspace monorepo every member's manifest and the workspace definition — so run.prepareCommands can install from them and the pool's image is rebuilt when one of them changes.", setup)})
	}
	baked := slices.ContainsFunc(yamlStrings(entry.Run.PrepareCommands), func(c string) bool {
		return !strings.Contains(c, "/"+DevsupAsset) && namesPathWithin(c, defaultWorkingDir)
	})
	if baked {
		return out
	}
	msg := fmt.Sprintf("No run.prepareCommands of setup %q writes into %s, so a leased container starts with no dependencies installed: install them into %s there, from the files build.addToRunPrepare lists — what run.prepareCommands writes survives every reset of a container, and the checkout keeps it.",
		setup, defaultWorkingDir, defaultWorkingDir)
	if entry.HasDeployFiles() {
		msg += fmt.Sprintf(" What build.deployFiles ships into %s — dependencies a build installs included — is wiped by every reset, so a lease never sees it.", defaultWorkingDir)
	}
	if read.hasDeclaration {
		if decl, err := devsup.ParseDeclaration([]byte(read.declaration)); err == nil && decl.Prepare != "" {
			msg += fmt.Sprintf(" The declaration's prepare (`%s`) installs them after the checkout instead, on every acquire.", decl.Prepare)
		}
	}
	return append(out, PoolMigrateImageCheck{Check: PoolMigrateImageDepsBaked, Message: msg})
}

// poolMigrateUncommitted names the pool setup's files — the config file and
// the declaration — that the census lists as changed in the checkout since
// its HEAD: staged, modified, conflicted or untracked (an untracked
// directory holding one counts).
func poolMigrateUncommitted(head PoolMigrateTree, configFile string) []string {
	files := make([]string, 0, 3)
	if configFile != "" {
		files = append(files, configFile)
	} else {
		files = append(files, zeropsYmlPrimary, zeropsYmlFallback)
	}
	files = append(files, devsup.DeclarationPath)
	var changed []string
	for _, list := range []*PoolMigratePaths{head.Staged, head.Modified, head.Conflicted, head.Untracked} {
		if list != nil {
			changed = append(changed, list.Paths...)
		}
	}
	var out []string
	for _, f := range files {
		if slices.ContainsFunc(changed, func(p string) bool {
			return p == f || (strings.HasSuffix(p, "/") && strings.HasPrefix(f, p))
		}) {
			out = append(out, f)
		}
	}
	return out
}

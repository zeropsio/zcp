package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// The one pool template's repository half (docs/spec-mate.md §2.8b B-10):
// what a pooled part needs on the repository's main — a pool setup named
// after its pool, a stage setup named after the part, and the dev-server
// declaration's previewPortBase and static-part servers. SeedFiles writes it on HQ's empty first
// commit, onto a change SeedOnto admits; CheckPoolSetups says what a main holding code lacks of it,
// through pool-migrate's own predicates, so a setup pool-migrate's image step
// accepts is accepted here. The pool's import half is bundle.PoolStanza: the
// AI Agent tier in bundle writes it and bundle cannot import ops, while this
// half reads through ParseZeropsYmlContent and the zcp-devsup facts ops owns,
// so the template spans the two packages by the import direction.

// SeedPreviewPortBase is the previewPortBase the seed declares: the value
// zcp's pool guidance names.
const SeedPreviewPortBase = 4000

// seedIdleStart is a stage setup's run.start in the seed: the idle start
// zcp teaches for a runtime with no application yet.
const seedIdleStart = "zsc noop --silent"

// PoolPart is one pooled part as the template reads it: the part (its stage
// setup's name), its pool and stage hostnames, the part's runtime type and
// the pool's maxContainers (bundle.DefaultPoolMaxContainers for 0).
type PoolPart struct {
	Part          string
	Pool          string
	Stage         string
	Type          string
	MaxContainers int
}

// maxContainers is the pool's maxContainers the preview ports follow.
func (p PoolPart) maxContainers() int {
	if p.MaxContainers > 0 {
		return p.MaxContainers
	}
	return bundle.DefaultPoolMaxContainers
}

// seedDoc is the seed's zerops.yaml.
type seedDoc struct {
	Zerops []seedSetup `yaml:"zerops"`
}

type seedSetup struct {
	Setup string    `yaml:"setup"`
	Build seedBuild `yaml:"build"`
	Run   seedRun   `yaml:"run"`
}

type seedBuild struct {
	Base        string `yaml:"base"`
	DeployFiles string `yaml:"deployFiles,omitempty"`
}

type seedRun struct {
	Base            string     `yaml:"base"`
	Ports           []seedPort `yaml:"ports,omitempty"`
	PrepareCommands []string   `yaml:"prepareCommands,omitempty"`
	Start           string     `yaml:"start,omitempty"`
}

type seedPort struct {
	Port        int  `yaml:"port"`
	HTTPSupport bool `yaml:"httpSupport"`
}

// SeedBaseError names a base the catalog cannot supply for a part's seed.
type SeedBaseError struct {
	Part  string
	Type  string
	Field string
	Base  string
}

func (e *SeedBaseError) Error() string {
	return fmt.Sprintf("part %q of type %q: %s base %q is not listed by Zerops", e.Part, e.Type, e.Field, e.Base)
}

type seedPartBases struct {
	build, poolRun, stageRun string
}

// seedBases maps the part's type to the bases of its pool and stage setups.
func seedBases(typ string, catalog *schema.Schemas) (seedPartBases, error) {
	bases := seedPartBases{build: typ, poolRun: typ, stageRun: typ}
	switch topology.RuntimeClassFor(typ) {
	case topology.RuntimeStatic:
		base, ok := catalog.NewestBuildBase("nodejs")
		if !ok {
			return seedPartBases{}, &SeedBaseError{Type: typ, Field: "build.base"}
		}
		if os := topology.OSPrefix(typ); os != "" {
			base = os + "/" + base
		}
		bases.build = base
		bases.poolRun = base
	case topology.RuntimeImplicitWeb:
		_, version, found := strings.Cut(typ, "@")
		version, _, _ = strings.Cut(version, "+")
		phpBase := ""
		if found && version != "" {
			phpBase = "php@" + version
			if os := topology.OSPrefix(typ); os != "" {
				phpBase = os + "/" + phpBase
			}
		}
		bases.build = phpBase
	case topology.RuntimeDynamic, topology.RuntimeManaged, topology.RuntimePool, topology.RuntimeSandbox, topology.RuntimeUnknown:
		// Other classes use the type as supplied.
	}
	if !catalog.HasBuildBase(bases.build) {
		return seedPartBases{}, &SeedBaseError{Type: typ, Field: "build.base", Base: bases.build}
	}
	for _, base := range []string{bases.poolRun, bases.stageRun} {
		if !catalog.HasRunBase(base) {
			return seedPartBases{}, &SeedBaseError{Type: typ, Field: "run.base", Base: base}
		}
	}
	return bases, nil
}

// SeedFiles composes the pool and stage setups and the dev-server declaration.
func SeedFiles(parts []PoolPart, catalog *schema.Schemas) (map[string]string, error) {
	doc := seedDoc{Zerops: make([]seedSetup, 0, 2*len(parts))}
	servers := []devsup.Server{}
	for _, p := range parts {
		bases, err := seedBases(p.Type, catalog)
		if err != nil {
			var baseErr *SeedBaseError
			if errors.As(err, &baseErr) {
				baseErr.Part = p.Part
			}
			return nil, fmt.Errorf("compose seed: %w", err)
		}
		ports := make([]seedPort, 0, p.maxContainers())
		for port := SeedPreviewPortBase + 1; port <= SeedPreviewPortBase+p.maxContainers(); port++ {
			ports = append(ports, seedPort{Port: port, HTTPSupport: true})
		}
		doc.Zerops = append(doc.Zerops, seedSetup{
			Setup: p.Pool,
			Build: seedBuild{Base: bases.build},
			Run: seedRun{
				Base:            bases.poolRun,
				Ports:           ports,
				PrepareCommands: []string{DevsupPrepareCommand(DevsupLatestURL)},
				Start:           DevsupStart,
			},
		})
		stage := seedSetup{Setup: p.Part, Build: seedBuild{Base: bases.build, DeployFiles: "./"}, Run: seedRun{Base: bases.stageRun}}
		if topology.RuntimeClassFor(p.Type) == topology.RuntimeDynamic {
			stage.Run.Start = seedIdleStart
		}
		if topology.RuntimeClassFor(p.Type) == topology.RuntimeStatic {
			// Each declaration's ports must be distinct, even across pools.
			port := SeedPreviewPortBase - 1 - len(servers)
			servers = append(servers, devsup.Server{
				Name: p.Pool, Service: p.Pool, Port: port, Preview: true,
				Command: "npx --yes node-http-server@9.0.2 --port " + strconv.Itoa(port),
			})
		}
		doc.Zerops = append(doc.Zerops, stage)
	}
	var yamlBuf strings.Builder
	enc := yaml.NewEncoder(&yamlBuf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode the seed's %s: %w", zeropsYmlPrimary, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode the seed's %s: %w", zeropsYmlPrimary, err)
	}
	decl, err := json.MarshalIndent(devsup.Declaration{Version: 1, PreviewPortBase: SeedPreviewPortBase, Servers: servers}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the seed's %s: %w", devsup.DeclarationPath, err)
	}
	return map[string]string{
		zeropsYmlPrimary:       yamlBuf.String(),
		devsup.DeclarationPath: string(decl) + "\n",
	}, nil
}

// SeedTitle is the seed change's title and its commit's message head.
const SeedTitle = "Set up the pooled parts"

// SeedMain is what a repository's main is to the seed.
type SeedMain int

const (
	// SeedMainEmpty is HQ's own empty first commit: the one main HQ's Core
	// lands a first change onto, so the seed goes there.
	SeedMainEmpty SeedMain = iota
	// SeedMainWritten holds a file: it is written nothing, its setups are
	// checked.
	SeedMainWritten
	// SeedMainUnborn is a repository with no main (a person made it empty):
	// HQ never lands a first change there, so only a person writes it.
	SeedMainUnborn
)

// SeedMainOf is main as HQ answers it — found false when the repository has
// no main, paths the files its head holds — to the seed.
func SeedMainOf(found bool, paths []string) SeedMain {
	switch {
	case !found:
		return SeedMainUnborn
	case len(paths) > 0:
		return SeedMainWritten
	}
	return SeedMainEmpty
}

// SeedChange is the change HQ answered the seed's open with and its head.
type SeedChange struct {
	Created   bool     // HQ opened it for this ask, not handed back an open one
	Title     string   // its title
	HeadFound bool     // its branch has a head
	HeadTree  string   // the head's tree
	HeadPaths []string // the files the head holds
}

// SeedWrite is what the seed writes onto the change HQ answered.
type SeedWrite int

const (
	// SeedWriteRoot is a commit of the seed with no parent: the change's
	// branch has no head.
	SeedWriteRoot SeedWrite = iota
	// SeedWriteOnHead is a commit of the seed on the head, which holds the
	// seed's files with other content (a seed of an older template).
	SeedWriteOnHead
	// SeedWriteNothing: the head is the seed already, from a run that stopped.
	SeedWriteNothing
	// SeedWriteRefused: the change is not the seed's, or its head holds other
	// files; nothing is written onto it.
	SeedWriteRefused
)

// SeedOnto is what the seed, tree seedTree holding files, writes onto the
// change c: only onto a change opened for the seed — one HQ opened for this
// ask or an open one titled SeedTitle — and over a head only when that head
// holds exactly the seed's files. Any other change HQ hands back (the Mate's
// default change of code, a recipe change) is refused untouched.
func SeedOnto(c SeedChange, seedTree string, files map[string]string) SeedWrite {
	switch {
	case !c.Created && c.Title != SeedTitle:
		return SeedWriteRefused
	case !c.HeadFound:
		return SeedWriteRoot
	case c.HeadTree == seedTree:
		return SeedWriteNothing
	case len(c.HeadPaths) != len(files):
		return SeedWriteRefused
	}
	for _, path := range c.HeadPaths {
		if _, ok := files[path]; !ok {
			return SeedWriteRefused
		}
	}
	return SeedWriteOnHead
}

// SeedOrphaned is whether the seed change HQ did not land is left without a
// use: main was written (mainWritten) and not by the seed's landing
// (seedOnMain false: the seed's commit is not in main's history). Closed then,
// never left open; a seed that landed with its answer lost is not closed.
func SeedOrphaned(mainWritten, seedOnMain bool) bool {
	return mainWritten && !seedOnMain
}

// SeedLeftOpen is whether a change of the Mate in the repository, by its
// title and state as HQ lists it, is a seed change an earlier run left open:
// a plan sent again onto a main another wrote reads it against main
// (SeedOrphaned), never leaving it open for good.
func SeedLeftOpen(title, state string) bool {
	return state == hq.ChangeOpen && title == SeedTitle
}

// SeedCloseDone is whether HQ's answer err to closing an orphaned seed change
// leaves the seed settled: closed now, or no longer open (409
// change_not_open), merged beneath a change that landed on top of it or
// closed meanwhile, which fails no plan.
func SeedCloseDone(err error) bool {
	var refused *hq.RefusedError
	return err == nil || errors.As(err, &refused) && refused.Reason == "change_not_open"
}

// CheckPoolSetups is what a main whose zerops.yaml content is zeropsYaml and
// whose dev-server declaration content is declaration ("" for a file it does
// not hold) lacks for parts, one line per gap naming the setup or the key:
// each part's pool setup and stage setup, the pool setup's `zcp-devsup run`,
// its zcp-devsup install line and its preview ports with httpSupport, and the
// declaration's previewPortBase. Empty: main holds them all.
func CheckPoolSetups(zeropsYaml, declaration string, parts []PoolPart) []string {
	var lacks []string
	var doc *ZeropsYmlDoc
	switch parsed, err := ParseZeropsYmlContent([]byte(zeropsYaml), zeropsYmlPrimary); {
	case strings.TrimSpace(zeropsYaml) == "":
		lacks = append(lacks, fmt.Sprintf("main holds no %s: add the setups %s to it.", zeropsYmlPrimary, strings.Join(poolPartSetups(parts), ", ")))
	case err != nil:
		lacks = append(lacks, fmt.Sprintf("%s does not parse: %v.", zeropsYmlPrimary, err))
	default:
		doc = parsed
	}

	base, baseOK := 0, false
	const how = "a lease answers its preview on previewPortBase + its container's stable number (ZEROPS_StableNumber), 1 … maxContainers"
	if strings.TrimSpace(declaration) == "" {
		lacks = append(lacks, fmt.Sprintf("main holds no %s, so no previewPortBase is set: declare `\"previewPortBase\": <port>` there (%s).", devsup.DeclarationPath, how))
	} else {
		decl, err := devsup.ParseDeclaration([]byte(declaration))
		highest := 0
		for _, p := range parts {
			highest = max(highest, p.maxContainers())
		}
		switch {
		case err != nil:
			lacks = append(lacks, fmt.Sprintf("%s does not parse: %v.", devsup.DeclarationPath, err))
		case poolPreviewBaseUnset(decl.PreviewPortBase):
			lacks = append(lacks, fmt.Sprintf("%s sets no previewPortBase: add `\"previewPortBase\": <port>` (%s).", devsup.DeclarationPath, how))
		case poolPreviewBaseTooHigh(decl.PreviewPortBase, highest):
			lacks = append(lacks, fmt.Sprintf("previewPortBase %d in %s puts the last preview port at %d, past %d, the highest port a container declares: lower it.", decl.PreviewPortBase, devsup.DeclarationPath, decl.PreviewPortBase+highest, poolMigrateHighestPort))
		default:
			base, baseOK = decl.PreviewPortBase, true
		}
	}
	if doc == nil {
		return lacks
	}

	for _, p := range parts {
		pool := doc.FindEntry(p.Pool)
		if pool == nil {
			lacks = append(lacks, fmt.Sprintf("%s has no pool setup %q: a deploy to the pool %s builds the setup named after it.", zeropsYmlPrimary, p.Pool, p.Pool))
		} else {
			if msg := poolSetupStartMessage(pool, p.Pool); msg != "" {
				lacks = append(lacks, msg)
			}
			if poolMigrateDevsupLine(yamlStrings(pool.Run.PrepareCommands)) == "" {
				lacks = append(lacks, poolSetupInstallMessage(p.Pool, ""))
			}
			if baseOK {
				if msg := poolMigratePreviewPortsMessage(pool, p.Pool, base, p.maxContainers()); msg != "" {
					lacks = append(lacks, msg)
				}
			}
		}
		if doc.FindEntry(p.Part) == nil {
			lacks = append(lacks, fmt.Sprintf("%s has no stage setup %q: the stage %s builds the part by the setup named after it.", zeropsYmlPrimary, p.Part, p.Stage))
		}
	}
	return lacks
}

// poolPartSetups are the setups parts need, each pool's then its part's.
func poolPartSetups(parts []PoolPart) []string {
	out := make([]string, 0, 2*len(parts))
	for _, p := range parts {
		out = append(out, strconv.Quote(p.Pool), strconv.Quote(p.Part))
	}
	return out
}

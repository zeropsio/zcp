package ops

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/bundle"
)

// The one pool template's repository half: what a pooled part needs on the
// repository's main — a pool setup named after its pool, a stage setup named
// after the part, and the dev-server declaration's previewPortBase.
// CheckPoolSetups says what a main lacks of it, through pool-migrate's own
// predicates, so a setup pool-migrate's image step accepts is accepted here;
// the stand-up runs it before a commit deploy (docs/spec-mate.md §2.8b) and
// the AI Agent tier's composer before it writes a dev pair as pools
// (§2.8a.5). The pool's import half is bundle.PoolStanza: the AI Agent tier
// in bundle writes it and bundle cannot import ops, while this half reads
// through ParseZeropsYmlContent and the zcp-devsup facts ops owns, so the
// template spans the two packages by the import direction.

// PoolPart is one pooled part as the template reads it: the part (its stage
// setup's name), its pool and stage hostnames, and the pool's maxContainers
// (bundle.DefaultPoolMaxContainers for 0).
type PoolPart struct {
	Part          string
	Pool          string
	Stage         string
	MaxContainers int
}

// maxContainers is the pool's maxContainers the preview ports follow.
func (p PoolPart) maxContainers() int {
	if p.MaxContainers > 0 {
		return p.MaxContainers
	}
	return bundle.DefaultPoolMaxContainers
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

package bundle

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The project as it really is (owner's decision, 2026-10-27): every part of a
// monorepo is its own dev pool and its own stage, nothing shares a container,
// one repository in HQ. The AI Agent tier a second Mate is born from gives it
// that shape — a part's dev pool written as zcp imports one — and a tier zcp
// composed carries a mark that lets zcp tell, later, a tier it wrote and
// nobody changed from one a person or another Mate wrote, so it can follow
// the project when its shape changes without ever writing over a person's
// edit (spec-mate §2.8a.5, D30).

// defaultPoolMaxContainers is the most containers a part's dev pool is
// written with when its live scale was not read: the pool migration's own
// stanza (docs/spec-workflows.md §13.1).
const defaultPoolMaxContainers = 3

// groupPoolEntry writes a part's dev pool on the AI Agent tier as zcp imports
// one (pool-migrate's provision stanza; knowledge theme pools-and-box, "A
// part's dev pool"): its hostname and type, no container until a lease asks
// for one (a container the platform creates for the minimum cannot be reset,
// DP-15), and its live maximum and vertical scale; the tier writes its own
// service variables beside them (groupPoolVault), the sshIsolation that
// admits zcp among them. No build and no setup: a
// pool's image is deployed from a lease of it, never by an import, and a
// runtime built from HQ would read as a dev half to clone into. No
// credential either: a pool of a repository in HQ gets the Mate's own from
// zcp at its first acquire (ZCP_HQ_GIT_TOKEN, DP-16), and its GIT_TOKEN stays
// the person's, so the tier names neither. The comment says which setup its
// image is.
func groupPoolEntry(r GroupRuntime, priority int) (map[string]any, string) {
	entry := map[string]any{
		"hostname":      r.DevHostname,
		"type":          r.ServiceType,
		"priority":      priority,
		"minContainers": 0,
	}
	maxContainers := defaultPoolMaxContainers
	if r.Scaling != nil && r.Scaling.MaxContainers > 0 {
		maxContainers = r.Scaling.MaxContainers
	}
	entry["maxContainers"] = maxContainers
	if r.Scaling != nil {
		vertical := *r.Scaling
		vertical.MinContainers, vertical.MaxContainers = 0, 0
		shape := map[string]any{}
		projectScaling(shape, &vertical)
		if v, ok := shape["verticalAutoscaling"]; ok {
			entry["verticalAutoscaling"] = v
		}
	}
	comment := fmt.Sprintf("A dev pool: one container leased per branch of the repository its stage builds from. Its image is that repository's setup %q, deployed from a lease once the Mate runs; this import builds nothing.", r.SetupName)
	return entry, strings.Join(wrapWords(comment, commentWidth-4), "\n")
}

// poolSSHIsolationKey is a pool's SSH admission: which service its
// containers answer SSH from.
const poolSSHIsolationKey = "sshIsolation"

// groupPoolVault is a pool's service variables as the AI Agent tier writes
// them (groupVaultFields, through write), its sshIsolation as written: an
// admission rule naming the zcp service, never a secret, which a generated
// value would turn into one that admits nobody.
func groupPoolVault(envs []ProjectEnvVar, source string, write valueWriter) []yamlField {
	rest := make([]ProjectEnvVar, 0, len(envs))
	var admission *yamlField
	for _, env := range envs {
		if env.Key == poolSSHIsolationKey {
			admission = &yamlField{key: env.Key, value: newVaultValue(env.Key, env.Value, false)}
			continue
		}
		rest = append(rest, env)
	}
	fields := groupVaultFields(rest, source, write)
	if admission != nil {
		fields = append(fields, *admission)
		sort.SliceStable(fields, func(i, j int) bool { return fields[i].key < fields[j].key })
	}
	return fields
}

// devOnlyTools are the utilities a Mate runs for its own development and a
// stage or a production never does: mail catchers and database admin UIs, by
// their canonical names. The medusa group's production came up with its
// Mate's mailpit once (D30).
var devOnlyTools = map[string]bool{
	"mailpit": true, "mailhog": true, "mailcatcher": true, "maildev": true, "smtp4dev": true,
	"adminer": true, "adminerevo": true, "phpmyadmin": true, "pgadmin": true, "pgweb": true,
	"dbgate": true, "rediscommander": true, "redisinsight": true,
}

// devOnlyUtility reports a utility that is a Mate's own dev tooling: its
// hostname, or the name of the public repository it is built from (with a
// recipe's `recipe-` prefix dropped), names one of devOnlyTools.
func devOnlyUtility(u GroupUtility) bool {
	names := []string{u.Hostname}
	if parsed, err := url.Parse(strings.TrimSpace(u.BuildFromGit)); err == nil {
		names = append(names, strings.TrimSuffix(path.Base(strings.TrimSuffix(parsed.Path, "/")), ".git"))
	}
	for _, name := range names {
		name = strings.TrimPrefix(strings.ToLower(name), "recipe-")
		name = strings.NewReplacer("-", "", "_", "", ".", "").Replace(name)
		if devOnlyTools[name] {
			return true
		}
	}
	return false
}

// tierStampLine is zcp's mark on a tier it composed: the Mate project it was
// composed from and the digest of the file with the mark taken out.
var tierStampLine = regexp.MustCompile(`(?m)^# zcp-recipe: (\S+) sha256=([0-9a-f]{64})\n`)

// StampTier puts zcp's mark on a tier body for the Mate project mateID: one
// comment line at the end of the header — the line the header's last
// paragraph announces — naming the Mate and the sha256 of the whole file
// without that line. A mark the body already carries is replaced, never
// added beside.
func StampTier(body, mateID string) string {
	body = tierStampLine.ReplaceAllString(body, "")
	lines := strings.SplitAfter(body, "\n")
	at := 0
	for at < len(lines) && strings.HasPrefix(lines[at], "#") {
		at++
	}
	mark := fmt.Sprintf("# zcp-recipe: %s sha256=%s\n", mateID, sha256Hex(body))
	return strings.Join(lines[:at], "") + mark + strings.Join(lines[at:], "")
}

// TierStamp reads zcp's mark off a tier body: the Mate project it names,
// whether the file is still exactly as zcp wrote it (intact: the digest of
// the file without the mark is the mark's, and it carries no second mark),
// and whether it carries one at all. Any edit — a value, a comment, a blank
// line — leaves it stamped and not intact.
func TierStamp(body string) (mateID string, intact, stamped bool) {
	loc := tierStampLine.FindStringSubmatchIndex(body)
	if loc == nil {
		return "", false, false
	}
	mateID, digest := body[loc[2]:loc[3]], body[loc[4]:loc[5]]
	rest := body[:loc[0]] + body[loc[1]:]
	return mateID, !tierStampLine.MatchString(rest) && sha256Hex(rest) == digest, true
}

// TierShape is what a tier creates, one line per service, sorted: a runtime
// built from a repository by its part — the repository, the setup and its
// type — and every other service by its hostname and type. Two tiers differ
// in shape exactly when a service is added or removed, a part is retyped or
// builds another setup, or a pool takes a dev service's place: a pool
// migration, a part added or removed, a type changed. A scale, a variable, a
// comment — and a part's hostname, which a person may have chosen and an
// environment made from the tier runs under — are no shape. nil for a body
// that does not parse.
func TierShape(body string) []string {
	var doc struct {
		Services []struct {
			Hostname     string `yaml:"hostname"`
			Type         string `yaml:"type"`
			ZeropsSetup  string `yaml:"zeropsSetup"`
			BuildFromGit string `yaml:"buildFromGit"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil
	}
	shape := make([]string, 0, len(doc.Services))
	for _, svc := range doc.Services {
		typ, setup := strings.TrimSpace(svc.Type), strings.TrimSpace(svc.ZeropsSetup)
		repo, private := publicRepoURL(svc.BuildFromGit)
		if strings.TrimSpace(svc.BuildFromGit) != "" && !private && setup != "" {
			shape = append(shape, "part "+repo+" "+setup+" "+typ)
			continue
		}
		shape = append(shape, "service "+strings.TrimSpace(svc.Hostname)+" "+typ)
	}
	sort.Strings(shape)
	return shape
}

// GroupRecipeBlocker is a part whose stage setup nothing records and no
// zerops.yaml tells without a guess: every tier that needs it is withheld,
// and a Mate's stand-up is not done while one is (spec-mate §10.10).
type GroupRecipeBlocker struct {
	// Runtime is the pair's dev half.
	Runtime string
	// Stage is the stage whose setup is missing, "" for a pair with none.
	Stage string
	// DevSetup is the setup the dev half builds — what a set-default-setup
	// names as its setup beside the stage's.
	DevSetup string
	// Why says what is missing.
	Why string
	// Declared are the setups the pair's zerops.yaml declares beside the dev
	// one, sorted — what the stage's setup is chosen from; none when no
	// zerops.yaml was read.
	Declared []string
}

// GroupRecipeBlockers names every part whose stage setup BuildGroupRecipe
// would not resolve (groupPartSetups), in dev-hostname order: the part's
// stage, the dev setup, why, and the setups to choose from.
func GroupRecipeBlockers(inputs GroupRecipeInputs) []GroupRecipeBlocker {
	runtimes := append([]GroupRuntime(nil), inputs.Runtimes...)
	sort.SliceStable(runtimes, func(i, j int) bool { return runtimes[i].DevHostname < runtimes[j].DevHostname })
	var blockers []GroupRecipeBlocker
	for _, r := range runtimes {
		if _, why := groupPartSetups(r); why != "" {
			b := GroupRecipeBlocker{Runtime: r.DevHostname, DevSetup: r.SetupName, Why: why}
			for i, stage := range r.Stages {
				if strings.TrimSpace(stage.SetupName) == "" || len(r.Stages) == 1 {
					b.Stage = r.Stages[i].Hostname
					break
				}
			}
			if declared, err := setupNamesInZeropsYAML(r.ZeropsYAMLBody); err == nil && strings.TrimSpace(r.ZeropsYAMLBody) != "" {
				for _, name := range dedupeStrings(declared) {
					if name != r.SetupName {
						b.Declared = append(b.Declared, name)
					}
				}
				sort.Strings(b.Declared)
			}
			blockers = append(blockers, b)
		}
	}
	return blockers
}

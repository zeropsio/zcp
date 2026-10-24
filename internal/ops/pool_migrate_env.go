package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// The env step of workflow="pool-migrate" (docs/spec-workflows.md §13 PM-4)
// gives a part's pool the dev service's own service env — the user-set layer,
// never the keys the dev's zerops.yaml owns (they travel with the pool setup)
// nor project env (it reaches every service already). A plain value is copied
// as the API reads it, references included. A secret is read where the API
// cannot show it to every token: the dev container's own environment, over
// SSH, in a fresh session (a service env value reaches a fresh session there
// in clear, no restart). zcp writes it to the pool with its own key, sensitive,
// through the hand-rolled userData writer. The value stays inside zcp's
// process: no result, error, log, state file or session command carries it;
// zcp compares secrets by fingerprint only. Nothing restarts. GIT_TOKEN is the
// pair's, not the part's: a variant-B part runs on a dev service of its own,
// but the pair is one repository whose one GIT_TOKEN sits on the service it
// pushes from (GitTokenFrom), so every part's pool is offered that one.

// PoolMigrateEnvInput is what the env step reads.
type PoolMigrateEnvInput struct {
	Dev  platform.ServiceStack
	Pool platform.ServiceStack
	// Stages are the pair's stages, whose key sets the result compares with
	// the pool's.
	Stages []platform.ServiceStack
	// InMate: zcp runs inside a Mate, where the pool's GIT_TOKEN is the Mate
	// credential an acquire puts there, never the dev's.
	InMate bool
	// CopyGitToken: the person said yes to the pool getting the pair's
	// GIT_TOKEN (gitTokenSource's). Outside a Mate only.
	CopyGitToken bool
	// GitTokenFrom is the service holding the pair's GIT_TOKEN — the dev half,
	// which the pair pushes from — when it is not Dev: a variant-B part runs
	// on a dev service of its own, whose env the pool takes, but the pair is
	// one repository with one GIT_TOKEN. Zero means Dev.
	GitTokenFrom platform.ServiceStack
	// Origin is the dev service's origin, which the pool's leases default to;
	// "" when zcp cannot name it.
	Origin   string
	StateDir string
}

// gitTokenElsewhere reports a pair whose GIT_TOKEN sits on another service
// than the part's dev service.
func (in PoolMigrateEnvInput) gitTokenElsewhere() bool {
	return in.GitTokenFrom.ID != "" && in.GitTokenFrom.ID != in.Dev.ID
}

// gitTokenSource is the service whose GIT_TOKEN the pool is offered.
func (in PoolMigrateEnvInput) gitTokenSource() platform.ServiceStack {
	if in.gitTokenElsewhere() {
		return in.GitTokenFrom
	}
	return in.Dev
}

// PoolMigrateEnv is the env step's state: read (ReadPoolMigrateEnv), or after
// the step ran (ApplyPoolMigrateEnv). It never carries a value.
type PoolMigrateEnv struct {
	// Done: the pool holds every key the step copies — or a key zcp could not
	// read is asked of the person (Requests) — and its default repository is
	// the dev's origin. GitToken never holds it: that is the person's call.
	Done bool `json:"done"`
	// ToWrite are the keys the step would write (the read side).
	ToWrite []PoolMigrateEnvKey `json:"toWrite,omitempty"`
	// Written are the keys the step wrote.
	Written []PoolMigrateEnvKey `json:"written,omitempty"`
	// Unchanged are the keys the pool already holds as the dev does: a plain
	// value equal, a secret equal by fingerprint.
	Unchanged []string `json:"unchanged,omitempty"`
	// Present are secrets the pool holds that were not compared: the read side
	// reads no secret, and the step leaves one it could not read on the dev.
	Present []string `json:"present,omitempty"`
	// Requests are the secrets the pool lacks that zcp could not read on the
	// dev: each is asked of the person, never guessed and never written empty.
	Requests []PoolMigrateEnvRequest `json:"requests,omitempty"`
	// Failed are the keys a write did not take; calling the step again retries.
	Failed      []PoolMigrateEnvSkip   `json:"failed,omitempty"`
	NeverCopied []PoolMigrateEnvSkip   `json:"neverCopied,omitempty"`
	GitToken    PoolMigrateGitToken    `json:"gitToken"`
	DefaultRepo PoolMigrateDefaultRepo `json:"defaultRepo"`
	// StageDiff names, per stage, the keys whose set differs from the pool's.
	// Values are the dev's; aligning a stage is the person's call.
	StageDiff []PoolMigrateStageDiff `json:"stageDiff,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
}

// PoolMigrateEnvKey is one key the step writes, or would.
type PoolMigrateEnvKey struct {
	Key       string `json:"key"`
	Sensitive bool   `json:"sensitive,omitempty"`
	// Change is add (the pool lacks it) or update (the pool holds another
	// value or flag).
	Change string `json:"change"`
}

// The changes a write makes.
const (
	PoolEnvChangeAdd    = "add"
	PoolEnvChangeUpdate = "update"
)

// PoolMigrateEnvSkip is a key with the reason it was not written.
type PoolMigrateEnvSkip struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Why a dev key never reaches the pool.
const (
	// PoolEnvReasonPoolCredential: API_TOKEN and ADMIN_TOKEN are the pool's
	// own orchestrator tokens.
	PoolEnvReasonPoolCredential = "pool-credential"
	// PoolEnvReasonPoolSSHIsolation: sshIsolation is the pool's own, set when
	// it was imported.
	PoolEnvReasonPoolSSHIsolation = "pool-ssh-isolation"
	// PoolEnvReasonZcpOwned: a credential or setting zcp or Mate keeps for
	// itself.
	PoolEnvReasonZcpOwned = "zcp-owned"
	// PoolEnvReasonPlatform: a name the platform keeps for itself.
	PoolEnvReasonPlatform = "platform"
	// PoolEnvReasonPoolZeropsYaml: the pool's deployed zerops.yaml owns the
	// key, so no service env can sit under it.
	PoolEnvReasonPoolZeropsYaml = "pool-zerops-yaml"
)

// PoolMigrateEnvRequest is a secret asked of the person: the key, and the
// words zerops_env action=request takes.
type PoolMigrateEnvRequest struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Why   string `json:"why"`
}

// PoolMigrateGitToken is what the step did, or would do, about the pool's
// GIT_TOKEN.
type PoolMigrateGitToken struct {
	State   string `json:"state"`
	Message string `json:"message"`
}

// The GIT_TOKEN states.
const (
	PoolGitTokenMateCredential = "mate-credential"
	PoolGitTokenDevHasNone     = "dev-has-none"
	PoolGitTokenAsk            = "ask"
	PoolGitTokenOnPool         = "on-pool"
	PoolGitTokenCopied         = "copied"
	PoolGitTokenUnchanged      = "unchanged"
	PoolGitTokenUnreadable     = "unreadable"
	PoolGitTokenFailed         = "failed"
)

// PoolMigrateDefaultRepo is the repository the pool's leases clone when an
// acquire names none: the dev's origin.
type PoolMigrateDefaultRepo struct {
	Repo  string `json:"repo,omitempty"`
	State string `json:"state"`
	// Previous is the repository the record named before the step replaced it.
	Previous string `json:"previous,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// The default-repository states.
const (
	PoolDefaultRepoSeeded      = "seeded"
	PoolDefaultRepoUnchanged   = "unchanged"
	PoolDefaultRepoPending     = "pending"
	PoolDefaultRepoUnavailable = "unavailable"
	PoolDefaultRepoFailed      = "failed"
)

// PoolMigrateStageDiff is one stage's key set against the pool's.
type PoolMigrateStageDiff struct {
	Stage     string   `json:"stage"`
	StageOnly []string `json:"stageOnly,omitempty"`
	PoolOnly  []string `json:"poolOnly,omitempty"`
}

// poolSSHIsolationKey is the pool's own SSH admission, set by its import.
const poolSSHIsolationKey = "sshIsolation"

// poolEnvKeyRe is a key the platform stores: letters, digits and `_`.
var poolEnvKeyRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// zcpOwnedEnvKeys are the credentials zcp keeps that the env step never
// copies; GIT_TOKEN has its own rule.
var zcpOwnedEnvKeys = map[string]bool{
	"ZCP_API_KEY":     true,
	LaunchTokenEnvKey: true,
	GiteaTokenEnvKey:  true,
}

// poolEnvNeverCopied names why key never reaches the pool, "" when it may.
func poolEnvNeverCopied(key string) string {
	switch {
	case poolCredentialKeys[key]:
		return PoolEnvReasonPoolCredential
	case key == poolSSHIsolationKey:
		return PoolEnvReasonPoolSSHIsolation
	case key == GitTokenEnvKey:
		return ""
	case zcpOwnedEnvKeys[key] || isVaultHidden(key):
		return PoolEnvReasonZcpOwned
	case !poolEnvKeyRe.MatchString(key) || strings.HasPrefix(strings.ToUpper(key), platformKeyPrefix) || platformRuntimeKeys[key]:
		return PoolEnvReasonPlatform
	}
	return ""
}

// poolEnvSides is what both sides of the step read: the dev's user layer, the
// pool's rows by kind, each stage's user keys, zcp's record of the pool and
// the pair's GIT_TOKEN row where it sits on another service.
type poolEnvSides struct {
	dev []platform.ServiceEnvVar
	// gitSource is the pair's GIT_TOKEN row when it sits on another service
	// than the part's dev service (gitTokenElsewhere), nil without one.
	gitSource  *platform.ServiceEnvVar
	poolUser   map[string]platform.ServiceEnvVar
	poolYaml   map[string]bool
	poolSystem map[string]bool
	stages     []poolEnvStage
	record     poolEnvRecord
}

type poolEnvStage struct {
	name string
	keys map[string]bool
}

func readPoolEnvSides(ctx context.Context, client platform.Client, in PoolMigrateEnvInput) (*poolEnvSides, error) {
	dev, err := FetchServiceUserEnvs(ctx, client, in.Dev.ID)
	if err != nil {
		return nil, fmt.Errorf("read %s's env: %w", in.Dev.Name, err)
	}
	pool, err := fetchServiceEnvLayers(ctx, client, in.Pool.ID)
	if err != nil {
		return nil, fmt.Errorf("read %s's env: %w", in.Pool.Name, err)
	}
	sides := &poolEnvSides{
		dev:        dev,
		poolUser:   map[string]platform.ServiceEnvVar{},
		poolYaml:   map[string]bool{},
		poolSystem: map[string]bool{},
	}
	for key := range pool.yamlOwned {
		sides.poolYaml[key] = true
	}
	for _, row := range pool.user() {
		sides.poolUser[row.Key] = row
	}
	for _, row := range pool.all {
		if row.IsSystem() {
			sides.poolSystem[row.Key] = true
		}
	}
	for _, stage := range in.Stages {
		rows, err := FetchServiceUserEnvs(ctx, client, stage.ID)
		if err != nil {
			return nil, fmt.Errorf("read %s's env: %w", stage.Name, err)
		}
		keys := make(map[string]bool, len(rows))
		for _, row := range rows {
			keys[row.Key] = true
		}
		sides.stages = append(sides.stages, poolEnvStage{name: stage.Name, keys: keys})
	}
	if in.gitTokenElsewhere() {
		rows, err := FetchServiceUserEnvs(ctx, client, in.GitTokenFrom.ID)
		if err != nil {
			return nil, fmt.Errorf("read %s's env: %w", in.GitTokenFrom.Name, err)
		}
		for i := range rows {
			if rows[i].Key == GitTokenEnvKey {
				row := rows[i]
				sides.gitSource = &row
			}
		}
	}
	if rec, found := readPoolEnvRecord(in.StateDir, in.Pool.Name); found && rec.PoolServiceID == in.Pool.ID {
		sides.record = rec
	}
	return sides, nil
}

// poolEnvSecret reports a row the platform holds sensitive, or one whose value
// it withheld from zcp's token (it masks only sensitive values): on the dev,
// a value only the container shows; on the pool, one held as a secret.
func poolEnvSecret(row platform.ServiceEnvVar) bool { return row.Sensitive || row.Masked }

// poolEnvPlan is the per-key decision both sides share, before any secret is
// read.
type poolEnvPlan struct {
	plain   []platform.ServiceEnvVar // copyable plain dev rows
	secrets []platform.ServiceEnvVar // copyable secret dev rows
	gitDev  *platform.ServiceEnvVar  // the pair's GIT_TOKEN, nil without one
	never   []PoolMigrateEnvSkip
	warns   []string
}

func planPoolEnv(sides *poolEnvSides, in PoolMigrateEnvInput) poolEnvPlan {
	var plan poolEnvPlan
	if in.gitTokenElsewhere() {
		// The part's own dev service holds no GIT_TOKEN of the pair's: one it
		// holds is neither offered nor copied.
		plan.gitDev = sides.gitSource
	}
	for i := range sides.dev {
		row := sides.dev[i]
		if row.Key == GitTokenEnvKey {
			if !in.gitTokenElsewhere() {
				plan.gitDev = &row
			}
			continue
		}
		reason := poolEnvNeverCopied(row.Key)
		switch {
		case reason != "":
		case sides.poolYaml[row.Key]:
			reason = PoolEnvReasonPoolZeropsYaml
		case sides.poolSystem[row.Key]:
			reason = PoolEnvReasonPlatform
		}
		if reason != "" {
			plan.never = append(plan.never, PoolMigrateEnvSkip{Key: row.Key, Reason: reason})
			continue
		}
		if poolEnvSecret(row) {
			plan.secrets = append(plan.secrets, row)
			continue
		}
		plan.plain = append(plan.plain, row)
		if strings.Contains(row.Content, "${"+in.Dev.Name+"_") {
			plan.warns = append(plan.warns, fmt.Sprintf("%s's value references ${%s_…}: on the pool it still reads %s's value, and retiring %s waits until nothing references it.", row.Key, in.Dev.Name, in.Dev.Name, in.Dev.Name))
		}
	}
	return plan
}

// plainState is a plain key's state on the pool: add, update, or "" when the
// pool holds the dev's value and flag.
func plainState(row platform.ServiceEnvVar, pool map[string]platform.ServiceEnvVar) string {
	have, ok := pool[row.Key]
	switch {
	case !ok:
		return PoolEnvChangeAdd
	case have.Sensitive || have.Masked || have.Content != row.Content:
		return PoolEnvChangeUpdate
	}
	return ""
}

// ReadPoolMigrateEnv is the env step's state as the read side derives it: API
// reads and zcp's own records only. It reads no secret — a secret the pool
// holds sensitive counts by presence, compared when the step runs — and
// writes nothing.
func ReadPoolMigrateEnv(ctx context.Context, client platform.Client, in PoolMigrateEnvInput) (*PoolMigrateEnv, error) {
	sides, err := readPoolEnvSides(ctx, client, in)
	if err != nil {
		return nil, err
	}
	plan := planPoolEnv(sides, in)
	out := &PoolMigrateEnv{NeverCopied: plan.never, Warnings: plan.warns}
	held := poolUserKeys(sides.poolUser)
	for _, row := range plan.plain {
		switch change := plainState(row, sides.poolUser); change {
		case "":
			out.Unchanged = append(out.Unchanged, row.Key)
		default:
			out.ToWrite = append(out.ToWrite, PoolMigrateEnvKey{Key: row.Key, Change: change})
			held[row.Key] = true
		}
	}
	requested := sides.record.requested()
	for _, row := range plan.secrets {
		have, ok := sides.poolUser[row.Key]
		switch {
		case ok && poolEnvSecret(have):
			out.Present = append(out.Present, row.Key)
		case ok:
			out.ToWrite = append(out.ToWrite, PoolMigrateEnvKey{Key: row.Key, Sensitive: true, Change: PoolEnvChangeUpdate})
		case requested[row.Key]:
			out.Requests = append(out.Requests, poolEnvRequest(row.Key, in))
		default:
			out.ToWrite = append(out.ToWrite, PoolMigrateEnvKey{Key: row.Key, Sensitive: true, Change: PoolEnvChangeAdd})
			held[row.Key] = true
		}
	}
	out.GitToken = readGitTokenState(plan.gitDev, sides, in)
	out.DefaultRepo = poolDefaultRepoState(in.StateDir, in.Pool.Name, in.Origin)
	out.StageDiff = poolStageDiff(sides.stages, held)
	out.Done = len(out.ToWrite) == 0 && out.DefaultRepo.State != PoolDefaultRepoPending
	return out, nil
}

// readGitTokenState is the GIT_TOKEN rule on the read side, where no secret is
// read and nothing is written. The pool's GIT_TOKEN that zcp copied from the
// dev is the dev's (unchanged), never offered again; one zcp did not write is
// the pool's own (on-pool).
func readGitTokenState(dev *platform.ServiceEnvVar, sides *poolEnvSides, in PoolMigrateEnvInput) PoolMigrateGitToken {
	if in.InMate {
		return gitTokenState(PoolGitTokenMateCredential, in)
	}
	if dev == nil {
		return gitTokenState(PoolGitTokenDevHasNone, in)
	}
	if _, ok := sides.poolUser[GitTokenEnvKey]; ok {
		if _, copied := sides.gitTokenZcpCopied(in.Pool.ID); copied {
			return gitTokenState(PoolGitTokenUnchanged, in)
		}
		return gitTokenState(PoolGitTokenOnPool, in)
	}
	return gitTokenState(PoolGitTokenAsk, in)
}

// gitTokenZcpCopied reports the pool's GIT_TOKEN as the one zcp copied from the
// dev, with the fingerprint zcp's record of this pool service keeps of it:
// the pool holds it as a secret, and its value — when the API shows it —
// fingerprints as the record's; a masked one is taken from the record. A
// GIT_TOKEN the person put there since reads apart whenever it is shown.
// Nothing is read on the dev.
func (sides *poolEnvSides) gitTokenZcpCopied(poolID string) (string, bool) {
	fp, kept := sides.record.Fingerprints[GitTokenEnvKey]
	row, onPool := sides.poolUser[GitTokenEnvKey]
	if !kept || !onPool || !poolEnvSecret(row) {
		return "", false
	}
	if !row.Masked && poolEnvFingerprint(poolID, GitTokenEnvKey, row.Content) != fp {
		return "", false
	}
	return fp, true
}

// gitTokenState words a GIT_TOKEN state for the agent, naming the service
// that holds the pair's GIT_TOKEN.
func gitTokenState(state string, in PoolMigrateEnvInput) PoolMigrateGitToken {
	pool, dev := in.Pool.Name, in.gitTokenSource().Name
	var msg string
	switch state {
	case PoolGitTokenMateCredential:
		msg = fmt.Sprintf("In a Mate the pool's GIT_TOKEN is this Mate's own HQ credential, which zcp puts on %s when a lease of an HQ repository takes a container; %s's is never copied.", pool, dev)
	case PoolGitTokenDevHasNone:
		msg = fmt.Sprintf("%s holds no GIT_TOKEN, so none is copied. A lease of a private repository needs one on %s: the person sets it there in the Zerops dashboard, sensitive — never ask for its value in the chat.", dev, pool)
	case PoolGitTokenAsk:
		msg = fmt.Sprintf("Ask the person whether %s may get %s's GIT_TOKEN: every container a lease of %s takes reads it, a wider reach than %s's. On yes, call again with copyGitToken=true and zcp copies it itself. On no, the person sets %s's own in the Zerops dashboard, sensitive. Never ask for its value in the chat.", pool, dev, pool, dev, pool)
	case PoolGitTokenOnPool:
		msg = fmt.Sprintf("%s already holds a GIT_TOKEN, left as it is. To give it %s's instead, call again with copyGitToken=true once the person said yes.", pool, dev)
	case PoolGitTokenCopied:
		msg = fmt.Sprintf("zcp copied %s's GIT_TOKEN to %s, sensitive; its value never left zcp.", dev, pool)
	case PoolGitTokenUnchanged:
		msg = fmt.Sprintf("%s already holds %s's GIT_TOKEN.", pool, dev)
	case PoolGitTokenUnreadable:
		msg = fmt.Sprintf("zcp could not read %s's GIT_TOKEN on %s, so it copied nothing. The person sets %s's own in the Zerops dashboard, sensitive — never ask for its value in the chat.", dev, dev, pool)
	case PoolGitTokenFailed:
		msg = fmt.Sprintf("zcp did not copy %s's GIT_TOKEN to %s this time (warnings say why); call again with copyGitToken=true.", dev, pool)
	}
	return PoolMigrateGitToken{State: state, Message: msg}
}

// poolEnvRequest words the ask for one secret.
func poolEnvRequest(key string, in PoolMigrateEnvInput) PoolMigrateEnvRequest {
	label := LabelOfKey(key)
	if utf8.RuneCountInString(label) > requestLabelMax {
		label = string([]rune(label)[:requestLabelMax])
	}
	return PoolMigrateEnvRequest{
		Key:   key,
		Label: label,
		Why:   fmt.Sprintf("for %s's part on its dev pool %s; zcp could not read it on %s", in.Dev.Name, in.Pool.Name, in.Dev.Name),
	}
}

func poolUserKeys(pool map[string]platform.ServiceEnvVar) map[string]bool {
	keys := make(map[string]bool, len(pool))
	for key := range pool {
		keys[key] = true
	}
	return keys
}

// poolStageDiff compares each stage's user keys with the pool's, both without
// the names the step never copies and GIT_TOKEN.
func poolStageDiff(stages []poolEnvStage, pool map[string]bool) []PoolMigrateStageDiff {
	compared := func(key string) bool { return key != GitTokenEnvKey && poolEnvNeverCopied(key) == "" }
	out := make([]PoolMigrateStageDiff, 0, len(stages))
	for _, stage := range stages {
		diff := PoolMigrateStageDiff{Stage: stage.name}
		for key := range stage.keys {
			if compared(key) && !pool[key] {
				diff.StageOnly = append(diff.StageOnly, key)
			}
		}
		for key := range pool {
			if compared(key) && !stage.keys[key] {
				diff.PoolOnly = append(diff.PoolOnly, key)
			}
		}
		sort.Strings(diff.StageOnly)
		sort.Strings(diff.PoolOnly)
		out = append(out, diff)
	}
	return out
}

// poolSecretDecision is what the step does with one secret once it read the
// dev's container.
type poolSecretDecision int

const (
	// poolSecretWrite: the pool lacks it, or holds another value or flag.
	poolSecretWrite poolSecretDecision = iota + 1
	// poolSecretEqual: the pool holds the dev's, by fingerprint.
	poolSecretEqual
	// poolSecretPresent: unreadable on the dev; the pool's own is left.
	poolSecretPresent
	// poolSecretAsk: unreadable on the dev and absent on the pool.
	poolSecretAsk
	// poolSecretNotRead: the read itself did not answer.
	poolSecretNotRead
)

// poolEnvWrite is one write the step makes; value never leaves this process.
type poolEnvWrite struct {
	key       string
	value     string
	sensitive bool
	change    string
	existing  *platform.ServiceEnvVar
}

// ApplyPoolMigrateEnv runs the env step: it reads both sides, reads the dev's
// secrets in its container (one SSH round trip, none when the dev holds no
// secret to copy), writes only the keys whose value or flag differs, waits for
// the writes to settle, seeds the pool's default repository and keeps its
// record of the secrets it wrote — fingerprints, never values. Nothing
// restarts: a fresh session in a pool container reads the new values, and a
// process already running keeps its boot env.
func ApplyPoolMigrateEnv(ctx context.Context, client platform.Client, ssh SSHDeployer, in PoolMigrateEnvInput) (*PoolMigrateEnv, error) {
	sides, err := readPoolEnvSides(ctx, client, in)
	if err != nil {
		return nil, err
	}
	plan := planPoolEnv(sides, in)
	step := &poolEnvStep{
		in:    in,
		sides: sides,
		out:   &PoolMigrateEnv{NeverCopied: plan.never, Warnings: plan.warns},
		rec:   poolEnvRecord{PoolServiceID: in.Pool.ID, Fingerprints: map[string]string{}},
	}
	step.planPlain(plan.plain)
	step.readSecrets(ctx, ssh, plan)
	step.planSecrets(plan.secrets)
	step.planGitToken(plan.gitDev)
	held := step.write(ctx, client)
	clear(step.values)
	step.keepRecord()

	out := step.out
	out.DefaultRepo = SeedPoolDefaultRepo(in.StateDir, in.Pool.Name, in.Origin)
	out.StageDiff = poolStageDiff(sides.stages, held)
	out.Done = len(out.Failed) == 0 &&
		out.DefaultRepo.State != PoolDefaultRepoPending && out.DefaultRepo.State != PoolDefaultRepoFailed
	return out, nil
}

// poolEnvStep is one run of the env step: what it read, what it decided and
// the record it keeps. values holds the dev's secrets for the length of the
// run only.
type poolEnvStep struct {
	in      PoolMigrateEnvInput
	sides   *poolEnvSides
	out     *PoolMigrateEnv
	rec     poolEnvRecord
	values  map[string]string
	readErr error
	// gitReadErr is why the pair's GIT_TOKEN could not be read on its own
	// service, when that is not the part's dev service (gitTokenElsewhere).
	gitReadErr error
	writes     []poolEnvWrite
}

// planPlain decides each plain key from the API read alone.
func (s *poolEnvStep) planPlain(rows []platform.ServiceEnvVar) {
	for _, row := range rows {
		change := plainState(row, s.sides.poolUser)
		if change == "" {
			s.out.Unchanged = append(s.out.Unchanged, row.Key)
			continue
		}
		s.writes = append(s.writes, poolEnvWrite{key: row.Key, value: row.Content, change: change, existing: poolRow(s.sides.poolUser, row.Key)})
	}
}

// readSecrets reads every secret to copy — and GIT_TOKEN after the person's
// yes outside a Mate — in one fresh session on the dev; the pair's GIT_TOKEN
// on another service is read in a fresh session there, and a read of it that
// does not answer fails the GIT_TOKEN alone.
func (s *poolEnvStep) readSecrets(ctx context.Context, ssh SSHDeployer, plan poolEnvPlan) {
	keys := make([]string, 0, len(plan.secrets)+1)
	for _, row := range plan.secrets {
		keys = append(keys, row.Key)
	}
	gitToken := s.copiesGitToken(plan.gitDev)
	if gitToken && !s.in.gitTokenElsewhere() {
		keys = append(keys, GitTokenEnvKey)
	}
	if len(keys) > 0 {
		s.values, s.readErr = readDevEnvValues(ctx, ssh, s.in.Dev.Name, keys)
		if s.readErr != nil {
			s.out.Warnings = append(s.out.Warnings, s.readErr.Error()+"; its secrets were not copied — call the step again.")
		}
	}
	if !gitToken || !s.in.gitTokenElsewhere() {
		return
	}
	values, err := readDevEnvValues(ctx, ssh, s.in.GitTokenFrom.Name, []string{GitTokenEnvKey})
	if err != nil {
		s.gitReadErr = err
		s.out.Warnings = append(s.out.Warnings, err.Error()+"; its GIT_TOKEN was not copied — call the step again with copyGitToken=true.")
		return
	}
	if value, ok := values[GitTokenEnvKey]; ok {
		if s.values == nil {
			s.values = map[string]string{}
		}
		s.values[GitTokenEnvKey] = value
	}
	clear(values)
}

// readErrFor is why key could not be read, nil when it was: the pair's
// GIT_TOKEN on another service has a read of its own.
func (s *poolEnvStep) readErrFor(key string) error {
	if key == GitTokenEnvKey && s.in.gitTokenElsewhere() {
		return s.gitReadErr
	}
	return s.readErr
}

func (s *poolEnvStep) copiesGitToken(dev *platform.ServiceEnvVar) bool {
	return !s.in.InMate && s.in.CopyGitToken && dev != nil
}

// decideSecret compares one secret by fingerprint: the pool's value when the
// API shows it, else zcp's record of what it wrote on that pool service.
func (s *poolEnvStep) decideSecret(key string) (*poolEnvWrite, poolSecretDecision) {
	have, onPool := s.sides.poolUser[key]
	value, read := s.values[key]
	switch {
	case s.readErrFor(key) != nil:
		// The record keeps what it knew of a key this run could not read.
		if fp, kept := s.sides.record.Fingerprints[key]; kept {
			s.rec.Fingerprints[key] = fp
		}
		return nil, poolSecretNotRead
	case !read && onPool:
		if fp, kept := s.sides.record.Fingerprints[key]; kept {
			s.rec.Fingerprints[key] = fp
		}
		return nil, poolSecretPresent
	case !read:
		return nil, poolSecretAsk
	}
	fp := poolEnvFingerprint(s.in.Pool.ID, key, value)
	equal := onPool && poolEnvSecret(have) &&
		((!have.Masked && poolEnvFingerprint(s.in.Pool.ID, key, have.Content) == fp) ||
			(have.Masked && s.sides.record.Fingerprints[key] == fp))
	if equal {
		s.rec.Fingerprints[key] = fp
		return nil, poolSecretEqual
	}
	change := PoolEnvChangeAdd
	if onPool {
		change = PoolEnvChangeUpdate
	}
	return &poolEnvWrite{key: key, value: value, sensitive: true, change: change, existing: poolRow(s.sides.poolUser, key)}, poolSecretWrite
}

func (s *poolEnvStep) planSecrets(rows []platform.ServiceEnvVar) {
	for _, row := range rows {
		w, decision := s.decideSecret(row.Key)
		switch decision {
		case poolSecretWrite:
			s.writes = append(s.writes, *w)
		case poolSecretEqual:
			s.out.Unchanged = append(s.out.Unchanged, row.Key)
		case poolSecretPresent:
			s.out.Present = append(s.out.Present, row.Key)
		case poolSecretAsk:
			s.out.Requests = append(s.out.Requests, poolEnvRequest(row.Key, s.in))
		case poolSecretNotRead:
			s.out.Failed = append(s.out.Failed, PoolMigrateEnvSkip{Key: row.Key, Reason: "not read on " + s.in.Dev.Name})
		}
	}
}

// planGitToken applies the GIT_TOKEN rule: in a Mate never; outside one only
// after the person's yes; otherwise as the read side words it, the record
// keeping the fingerprint of a GIT_TOKEN zcp copied earlier.
func (s *poolEnvStep) planGitToken(dev *platform.ServiceEnvVar) {
	if !s.copiesGitToken(dev) {
		s.out.GitToken = readGitTokenState(dev, s.sides, s.in)
		if fp, copied := s.sides.gitTokenZcpCopied(s.in.Pool.ID); copied {
			s.rec.Fingerprints[GitTokenEnvKey] = fp
		}
		return
	}
	w, decision := s.decideSecret(GitTokenEnvKey)
	switch decision {
	case poolSecretWrite:
		s.writes = append(s.writes, *w)
	case poolSecretEqual:
		s.out.GitToken = gitTokenState(PoolGitTokenUnchanged, s.in)
	case poolSecretNotRead:
		s.out.GitToken = gitTokenState(PoolGitTokenFailed, s.in)
	case poolSecretPresent, poolSecretAsk:
		s.out.GitToken = gitTokenState(PoolGitTokenUnreadable, s.in)
	}
}

// write makes every planned write, waits for them to settle, and answers the
// keys the pool holds afterwards.
func (s *poolEnvStep) write(ctx context.Context, client platform.Client) map[string]bool {
	held := poolUserKeys(s.sides.poolUser)
	var procIDs []string
	for _, w := range s.writes {
		procs, reason := writePoolEnv(ctx, client, s.in.Pool.ID, w)
		procIDs = append(procIDs, procs...)
		switch {
		case reason != "":
			if w.existing != nil {
				delete(held, w.key)
			}
			if w.key == GitTokenEnvKey {
				s.out.GitToken = gitTokenState(PoolGitTokenFailed, s.in)
				continue
			}
			s.out.Failed = append(s.out.Failed, PoolMigrateEnvSkip{Key: w.key, Reason: reason})
			continue
		case w.sensitive:
			s.rec.Fingerprints[w.key] = poolEnvFingerprint(s.in.Pool.ID, w.key, w.value)
		}
		held[w.key] = true
		if w.key == GitTokenEnvKey {
			s.out.GitToken = gitTokenState(PoolGitTokenCopied, s.in)
			continue
		}
		s.out.Written = append(s.out.Written, PoolMigrateEnvKey{Key: w.key, Sensitive: w.sensitive, Change: w.change})
	}
	if warn := settlePoolEnvWrites(ctx, client, procIDs); warn != "" {
		s.out.Warnings = append(s.out.Warnings, warn)
	}
	return held
}

// keepRecord saves the fingerprints and asks of this run. A read that did not
// answer leaves the record as it was: it says nothing about the secrets this
// run could not see.
func (s *poolEnvStep) keepRecord() {
	if s.readErr != nil {
		return
	}
	for _, r := range s.out.Requests {
		s.rec.Requested = append(s.rec.Requested, r.Key)
	}
	if err := savePoolEnvRecord(s.in.StateDir, s.in.Pool.Name, s.rec); err != nil {
		s.out.Warnings = append(s.out.Warnings, fmt.Sprintf("zcp could not keep its record of the secrets it wrote on %s (%v); the next call rewrites them.", s.in.Pool.Name, err))
	}
}

func poolRow(pool map[string]platform.ServiceEnvVar, key string) *platform.ServiceEnvVar {
	if row, ok := pool[key]; ok {
		return &row
	}
	return nil
}

// writePoolEnv writes one key on the pool: the row it replaces is removed
// first (an update without the sensitive flag would turn a secret plain, so a
// replace is a fresh create), then the hand-rolled userData writer creates it
// with its flag. It answers the processes it started and, on failure, a reason
// built from the platform's error codes only — never its message, which could
// echo what was sent.
func writePoolEnv(ctx context.Context, client platform.Client, poolID string, w poolEnvWrite) (procs []string, reason string) {
	if w.existing != nil {
		proc, err := client.DeleteUserData(ctx, w.existing.ID)
		if err != nil {
			return nil, "the pool's previous value could not be removed (" + poolEnvErrorCode(err) + ")"
		}
		if proc != nil && proc.ID != "" {
			procs = append(procs, proc.ID)
		}
	}
	proc, err := client.CreateServiceEnvVar(ctx, poolID, w.key, w.value, w.sensitive)
	if err != nil {
		if w.existing != nil {
			return procs, "the pool's previous value was removed, then the write did not take (" + poolEnvErrorCode(err) + "); call the step again"
		}
		return procs, "the write did not take (" + poolEnvErrorCode(err) + "); call the step again"
	}
	if proc != nil && proc.ID != "" {
		procs = append(procs, proc.ID)
	}
	return procs, ""
}

// poolEnvErrorCode names a failed write by the platform's codes.
func poolEnvErrorCode(err error) string {
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return "platform error"
	}
	if pe.APICode != "" && pe.APICode != pe.Code {
		return pe.Code + " " + pe.APICode
	}
	return pe.Code
}

// settlePoolEnvWrites waits for the writes' processes, so the next call reads
// the pool settled instead of busy; a wait that does not settle is a warning,
// never a failure.
func settlePoolEnvWrites(ctx context.Context, client platform.Client, ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	res, err := WaitProcesses(ctx, client, ids, nil)
	switch {
	case err != nil:
		return "the pool's env writes could not be followed to the end; call the flow again in a moment."
	case res.TimedOut:
		return "the pool's env writes are still settling; call the flow again in a moment."
	}
	for _, p := range res.Processes {
		if p.Status != platform.ProcessStatusFinished {
			return fmt.Sprintf("a write's process on the pool ended %s; call the step again.", p.Status)
		}
	}
	return ""
}

// poolEnvFingerprint names a value without carrying it: SHA-256 over the pool
// service id, the key and the value, so the same value under another key or
// on another pool fingerprints apart.
func poolEnvFingerprint(poolServiceID, key, value string) string {
	h := sha256.New()
	h.Write([]byte(poolServiceID))
	h.Write([]byte{0})
	h.Write([]byte(key))
	h.Write([]byte{0})
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// The dev container's answer to the secret read: a begin marker, then per key
// its name, printenv's output and ok/unset, each NUL-terminated, and an end
// marker. An env value holds no NUL, so the framing cannot be forged by one,
// and printenv always ends a value with one newline.
const (
	poolEnvReadBegin = "zcp-env 1"
	poolEnvReadEnd   = "zcp-env-end"
	poolEnvReadOK    = "ok"
	poolEnvReadUnset = "unset"
)

// poolEnvReadCommand reads keys in the session's environment. Each key is a
// single-quoted word handed to printenv, so no value is expanded, split or
// globbed by the shell; printenv's stderr is discarded and the script always
// exits 0, so a value never lands in an error.
func poolEnvReadCommand(keys []string) string {
	var b strings.Builder
	b.WriteString(`printf '%s\000' ` + shellQuote(poolEnvReadBegin) + "; for k in")
	for _, key := range keys {
		b.WriteString(" " + shellQuote(key))
	}
	b.WriteString(`; do printf '%s\000' "$k"; if printenv "$k" 2>/dev/null; then printf '\000` + poolEnvReadOK +
		`\000'; else printf '\000` + poolEnvReadUnset + `\000'; fi; done; printf '%s\000' ` + shellQuote(poolEnvReadEnd) + "; exit 0")
	return b.String()
}

// PoolEnvReadError is a secret read that did not answer. It carries the
// host and the failure only: the session's output, which may hold a value
// already printed, is dropped, and the SSH error is not wrapped.
type PoolEnvReadError struct {
	Host  string
	Cause string
}

func (e *PoolEnvReadError) Error() string {
	return fmt.Sprintf("zcp could not read %s's env over SSH (%s)", e.Host, e.Cause)
}

// readDevEnvValues reads keys in a fresh SSH session on host: a key it
// answers with a non-empty value is in the map; one unset or empty is not.
func readDevEnvValues(ctx context.Context, ssh SSHDeployer, host string, keys []string) (map[string]string, error) {
	for _, key := range keys {
		if !poolEnvKeyRe.MatchString(key) {
			return nil, &PoolEnvReadError{Host: host, Cause: "a key is not an env name"}
		}
	}
	if ssh == nil {
		return nil, &PoolEnvReadError{Host: host, Cause: "no SSH in this zcp"}
	}
	out, err := ssh.ExecSSH(ctx, host, poolEnvReadCommand(keys))
	if err != nil {
		cause := "ssh failed"
		var sshErr *platform.SSHExecError
		switch {
		case errors.As(err, &sshErr) && sshErr.Err != nil:
			cause = sshErr.Err.Error()
		case errors.Is(err, context.DeadlineExceeded):
			cause = "timed out"
		}
		clear(out)
		return nil, &PoolEnvReadError{Host: host, Cause: cause}
	}
	values, perr := parsePoolEnvRead(out, keys)
	clear(out)
	if perr != "" {
		return nil, &PoolEnvReadError{Host: host, Cause: perr}
	}
	return values, nil
}

// parsePoolEnvRead reads the framed answer; on failure it names what was
// wrong, never what it read.
func parsePoolEnvRead(out []byte, keys []string) (map[string]string, string) {
	_, answer, found := bytes.Cut(out, []byte(poolEnvReadBegin+"\x00"))
	if !found {
		return nil, "no answer"
	}
	fields := bytes.Split(answer, []byte{0})
	values := map[string]string{}
	for j := 0; ; j += 3 {
		if j >= len(fields) {
			return nil, "the answer was cut short"
		}
		if string(fields[j]) == poolEnvReadEnd {
			break
		}
		if j+2 >= len(fields) {
			return nil, "the answer was cut short"
		}
		key, value, status := string(fields[j]), fields[j+1], string(fields[j+2])
		switch status {
		case poolEnvReadUnset:
			continue
		case poolEnvReadOK:
		default:
			return nil, "the answer was malformed"
		}
		if !bytes.HasSuffix(value, []byte("\n")) {
			return nil, "the answer was malformed"
		}
		if v := string(value[:len(value)-1]); v != "" && slices.Contains(keys, key) {
			values[key] = v
		}
	}
	return values, ""
}

// poolEnvRecord is zcp's record of the secrets it put on a pool, by pool
// service (a pool re-created under its hostname holds nothing zcp wrote):
// a fingerprint per key — never a value — and the keys asked of the person.
// The step compares a secret the API shows masked by it; the read side counts
// an asked key as the person's.
type poolEnvRecord struct {
	PoolServiceID string            `json:"poolServiceId"`
	Fingerprints  map[string]string `json:"fingerprints,omitempty"`
	Requested     []string          `json:"requested,omitempty"`
}

func (r poolEnvRecord) requested() map[string]bool {
	out := make(map[string]bool, len(r.Requested))
	for _, key := range r.Requested {
		out[key] = true
	}
	return out
}

func poolEnvRecordPath(stateDir, pool string) string {
	return filepath.Join(stateDir, "migrate", "pool-env", pool+".json")
}

func readPoolEnvRecord(stateDir, pool string) (poolEnvRecord, bool) {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return poolEnvRecord{}, false
	}
	raw, err := os.ReadFile(poolEnvRecordPath(stateDir, pool))
	if err != nil {
		return poolEnvRecord{}, false
	}
	var rec poolEnvRecord
	if json.Unmarshal(raw, &rec) != nil || rec.PoolServiceID == "" {
		return poolEnvRecord{}, false
	}
	return rec, true
}

// savePoolEnvRecord writes rec atomically with mode 0600, or removes the
// record when it holds nothing.
func savePoolEnvRecord(stateDir, pool string, rec poolEnvRecord) error {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return errors.New("no state directory or an unusable pool name")
	}
	path := poolEnvRecordPath(stateDir, pool)
	if len(rec.Fingerprints) == 0 && len(rec.Requested) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	sort.Strings(rec.Requested)
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("pool env record %s: %w", pool, err)
	}
	return nil
}

// poolDefaultRepoState is the pool's default repository against the dev's
// origin, read only.
func poolDefaultRepoState(stateDir, pool, origin string) PoolMigrateDefaultRepo {
	repo, reason := poolDefaultRepoOrigin(origin)
	if reason != "" {
		return PoolMigrateDefaultRepo{State: PoolDefaultRepoUnavailable, Reason: reason}
	}
	if topology.SameRepoURL(readPoolDefaultRepo(stateDir, pool), repo) {
		return PoolMigrateDefaultRepo{Repo: repo, State: PoolDefaultRepoUnchanged}
	}
	return PoolMigrateDefaultRepo{Repo: repo, State: PoolDefaultRepoPending}
}

// SeedPoolDefaultRepo makes the dev's origin the repository the pool's leases
// clone when an acquire names none (zcp's per-pool record, DP-5), writing only
// when the record names another repository or none.
func SeedPoolDefaultRepo(stateDir, pool, origin string) PoolMigrateDefaultRepo {
	repo, reason := poolDefaultRepoOrigin(origin)
	if reason != "" {
		return PoolMigrateDefaultRepo{State: PoolDefaultRepoUnavailable, Reason: reason}
	}
	have := readPoolDefaultRepo(stateDir, pool)
	if topology.SameRepoURL(have, repo) {
		return PoolMigrateDefaultRepo{Repo: repo, State: PoolDefaultRepoUnchanged}
	}
	if err := storePoolDefaultRepo(stateDir, pool, repo); err != nil {
		return PoolMigrateDefaultRepo{Repo: repo, State: PoolDefaultRepoFailed, Reason: err.Error()}
	}
	return PoolMigrateDefaultRepo{Repo: repo, State: PoolDefaultRepoSeeded, Previous: have}
}

// poolDefaultRepoOrigin is origin as a lease takes it — as given, since a
// lease clones the remembered URL verbatim (DP-18) — or why it cannot be.
func poolDefaultRepoOrigin(origin string) (string, string) {
	if strings.TrimSpace(origin) == "" {
		return "", "zcp cannot name the dev service's origin here; name repo on the first acquire"
	}
	repo, err := validateLeaseRepo(origin)
	if err != nil {
		return "", "the dev service's origin is not an https URL a lease clones; name repo on the first acquire"
	}
	return repo, ""
}

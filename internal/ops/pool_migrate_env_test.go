// Tests for: ops/pool_migrate_env.go — the env step of pool-migrate
// (docs/spec-workflows.md §13 PM-4): the dev service's own service env copied
// to its pool, plain values from the API, secrets read in the dev container's
// environment and written by zcp, never through the agent, compared by
// fingerprint only, nothing restarted. The secret read runs the real command
// in a real local shell holding the dev's environment, as ssh hands it to the
// dev container's shell.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// envLocalSSH runs each command in a local shell whose environment is env —
// the dev container's service env as a fresh session sees it — and fails the
// way the platform's ssh does. err, when set, fails every call with output as
// the session's combined output.
type envLocalSSH struct {
	mu     sync.Mutex
	env    map[string]string
	err    error
	output string
	calls  []string
}

func (s *envLocalSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, hostname+": "+command)
	env, failWith, output := s.env, s.err, s.output
	s.mu.Unlock()
	if failWith != nil {
		return []byte(output), &platform.SSHExecError{Hostname: hostname, Output: output, Err: failWith}
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
	}
	return out, nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *envLocalSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *envLocalSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, errors.New("the env step never runs a background command")
}

func (s *envLocalSSH) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

// A value no shell may split, expand or glob, ending in a newline.
const (
	envSecretValue = "s3cr3t 'q' \"dq\" $HOME `id` *.go\\n\nsecond line\n"
	envOtherSecret = "another-secret-value-7f3a"
	envGitToken    = "ghp_devTokenValue0123456789"
)

const (
	envDevID   = "svc-dev"
	envPoolID  = "svc-pool"
	envStageID = "svc-stage"
)

func envDevService() platform.ServiceStack {
	return platform.ServiceStack{ID: envDevID, Name: "appdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}}
}

func envPoolService() platform.ServiceStack {
	return platform.ServiceStack{ID: envPoolID, Name: "apppool", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
}

func envStageService() platform.ServiceStack {
	return platform.ServiceStack{ID: envStageID, Name: "appstage", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}}
}

func envRow(id, key, content string, sensitive bool) platform.ServiceEnvVar {
	return platform.ServiceEnvVar{ID: id, Key: key, Content: content, Type: platform.ServiceEnvUser, Sensitive: sensitive}
}

// envMasked is a sensitive row as a token that cannot read it sees it.
func envMasked(id, key string) platform.ServiceEnvVar {
	return platform.ServiceEnvVar{ID: id, Key: key, Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true}
}

// envPoolOwn is what a freshly imported pool holds: its own tokens, its
// sshIsolation and the platform's rows.
func envPoolOwn() []platform.ServiceEnvVar {
	return []platform.ServiceEnvVar{
		envMasked("p-api", "API_TOKEN"),
		envMasked("p-admin", "ADMIN_TOKEN"),
		envRow("p-ssh", "sshIsolation", "vpn service@zcp", false),
		{ID: "p-host", Key: "hostname", Content: "apppool", Type: platform.ServiceEnvSystem},
	}
}

type envFixture struct {
	dev, pool, stage []platform.ServiceEnvVar
	container        map[string]string // the dev container's session env
	in               PoolMigrateEnvInput
	ssh              *envLocalSSH
	mock             *platform.Mock
}

func newEnvFixture(t *testing.T) *envFixture {
	t.Helper()
	return &envFixture{
		dev: []platform.ServiceEnvVar{
			envRow("d-plain", "APP_URL", "${zeropsSubdomain}/app", false),
			envRow("d-db", "DB_HOST", "${db_hostname}", false),
			envMasked("d-secret", "STRIPE_SECRET"),
			{ID: "d-sys", Key: "serviceId", Content: envDevID, Type: platform.ServiceEnvSystem},
		},
		pool:      envPoolOwn(),
		stage:     []platform.ServiceEnvVar{envRow("s-plain", "APP_URL", "https://stage", false), envMasked("s-secret", "STRIPE_SECRET")},
		container: map[string]string{"STRIPE_SECRET": envSecretValue, "APP_URL": "https://dev", "DB_HOST": "db"},
		in: PoolMigrateEnvInput{
			Dev: envDevService(), Pool: envPoolService(), Stages: []platform.ServiceStack{envStageService()},
			Origin: "https://github.com/example/app.git", StateDir: t.TempDir(),
		},
	}
}

// build seeds the mock from the fixture's rows; it keeps a mock already built,
// so a second call sees what the first wrote.
func (f *envFixture) build() {
	if f.mock == nil {
		f.mock = platform.NewMock().
			WithServices([]platform.ServiceStack{f.in.Dev, f.in.Pool, envStageService()}).
			WithServiceEnv(envDevID, f.dev).
			WithServiceEnv(envPoolID, f.pool).
			WithServiceEnv(envStageID, f.stage).
			WithProcess(&platform.Process{ID: "proc-envset-" + envPoolID, Status: platform.ProcessStatusFinished})
		for _, row := range f.pool {
			f.mock.WithProcess(&platform.Process{ID: "proc-envdel-" + row.ID, Status: platform.ProcessStatusFinished})
		}
	}
	if f.ssh == nil {
		f.ssh = &envLocalSSH{env: f.container}
	}
}

func (f *envFixture) apply(t *testing.T) *PoolMigrateEnv {
	t.Helper()
	f.build()
	out, err := ApplyPoolMigrateEnv(context.Background(), f.mock, f.ssh, f.in)
	if err != nil {
		t.Fatalf("ApplyPoolMigrateEnv: %v", err)
	}
	return out
}

func (f *envFixture) read(t *testing.T) *PoolMigrateEnv {
	t.Helper()
	f.build()
	out, err := ReadPoolMigrateEnv(context.Background(), f.mock, f.in)
	if err != nil {
		t.Fatalf("ReadPoolMigrateEnv: %v", err)
	}
	return out
}

// poolRows is the pool's env as the mock holds it now, by key.
func (f *envFixture) poolRows(t *testing.T) map[string]platform.ServiceEnvVar {
	t.Helper()
	rows, err := f.mock.GetServiceEnv(context.Background(), envPoolID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]platform.ServiceEnvVar{}
	for _, r := range rows {
		if _, dup := out[r.Key]; dup {
			t.Fatalf("the pool holds %s twice", r.Key)
		}
		out[r.Key] = r
	}
	return out
}

func writtenKeys(out *PoolMigrateEnv) []string {
	keys := make([]string, 0, len(out.Written))
	for _, w := range out.Written {
		keys = append(keys, w.Key)
	}
	slices.Sort(keys)
	return keys
}

func TestPoolMigrateEnv_PlainVerbatimSecretFromTheContainer(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	out := f.apply(t)

	if got, want := writtenKeys(out), []string{"APP_URL", "DB_HOST", "STRIPE_SECRET"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("written = %v, want %v", got, want)
	}
	rows := f.poolRows(t)
	// Plain values as the API reads them, references included — never the
	// container's resolved values.
	if r := rows["APP_URL"]; r.Content != "${zeropsSubdomain}/app" || r.Sensitive {
		t.Errorf("APP_URL on the pool = %q sensitive=%v, want the dev's verbatim plain value", r.Content, r.Sensitive)
	}
	if r := rows["DB_HOST"]; r.Content != "${db_hostname}" || r.Sensitive {
		t.Errorf("DB_HOST on the pool = %q sensitive=%v, want the reference verbatim", r.Content, r.Sensitive)
	}
	// The secret as the dev container holds it, byte for byte, sensitive.
	if r := rows["STRIPE_SECRET"]; r.Content != envSecretValue || !r.Sensitive {
		t.Errorf("STRIPE_SECRET on the pool: sensitive=%v, exact=%v; want the container's value, sensitive", r.Sensitive, r.Content == envSecretValue)
	}
	if !out.Done {
		t.Errorf("done = false after a full copy: %+v", out)
	}
	if calls := f.ssh.recorded(); len(calls) != 1 || !strings.HasPrefix(calls[0], "appdev: ") {
		t.Fatalf("ssh calls = %v, want one read on the dev service", calls)
	}
}

func TestPoolMigrateEnv_ReservedAndZcpOwnedKeysNeverWritten(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	reserved := map[string]string{
		"API_TOKEN":        PoolEnvReasonPoolCredential,
		"ADMIN_TOKEN":      PoolEnvReasonPoolCredential,
		"sshIsolation":     PoolEnvReasonPoolSSHIsolation,
		"ZCP_API_KEY":      PoolEnvReasonZcpOwned,
		"ZCP_LAUNCH_TOKEN": PoolEnvReasonZcpOwned,
		"GITEA_TOKEN":      PoolEnvReasonZcpOwned,
		"ZCP_AGENT_FLAG":   PoolEnvReasonZcpOwned,
		"MATE_SETTING":     PoolEnvReasonZcpOwned,
	}
	for key := range reserved {
		f.dev = append(f.dev, envRow("d-"+key, key, "dev-"+key, true))
		f.container[key] = "dev-" + key
	}
	before := map[string]platform.ServiceEnvVar{}
	for _, r := range envPoolOwn() {
		before[r.Key] = r
	}
	out := f.apply(t)

	got := map[string]string{}
	for _, s := range out.NeverCopied {
		got[s.Key] = s.Reason
	}
	for key, reason := range reserved {
		if got[key] != reason {
			t.Errorf("neverCopied[%s] = %q, want %q", key, got[key], reason)
		}
		for _, w := range out.Written {
			if w.Key == key {
				t.Errorf("%s was written", key)
			}
		}
	}
	rows := f.poolRows(t)
	// Same row, never replaced (the mock's rows are marked masked in place by
	// the read, so identity is the row id).
	for _, key := range []string{"API_TOKEN", "ADMIN_TOKEN", "sshIsolation"} {
		if rows[key].ID != before[key].ID || rows[key].Sensitive != before[key].Sensitive {
			t.Errorf("the pool's own %s changed: %+v -> %+v", key, before[key], rows[key])
		}
	}
	if n := f.mock.CallCount("DeleteUserData"); n != 0 {
		t.Errorf("DeleteUserData called %d times on a fresh pool", n)
	}
	for _, key := range []string{"ZCP_API_KEY", "ZCP_LAUNCH_TOKEN", "GITEA_TOKEN", "ZCP_AGENT_FLAG", "MATE_SETTING"} {
		if _, has := rows[key]; has {
			t.Errorf("zcp-owned %s reached the pool", key)
		}
	}
	// None of them is even read on the dev.
	for _, call := range f.ssh.recorded() {
		for key := range reserved {
			if strings.Contains(call, "'"+key+"'") {
				t.Errorf("the dev's %s was read: %s", key, call)
			}
		}
	}
}

func TestPoolMigrateEnv_CheckBeforeMutate_WritesOnlyTheDifference(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.pool = append(f.pool,
		envRow("p-url", "APP_URL", "${zeropsSubdomain}/app", false), // equal
		envRow("p-db", "DB_HOST", "old-host", false),                // differs
		envRow("p-secret", "STRIPE_SECRET", envSecretValue, true),   // equal, readable to this token
	)
	out := f.apply(t)

	if got := writtenKeys(out); !reflect.DeepEqual(got, []string{"DB_HOST"}) {
		t.Fatalf("written = %v, want only DB_HOST", got)
	}
	if w := out.Written[0]; w.Change != PoolEnvChangeUpdate {
		t.Errorf("DB_HOST change = %q, want update", w.Change)
	}
	if got := slices.Sorted(slices.Values(out.Unchanged)); !reflect.DeepEqual(got, []string{"APP_URL", "STRIPE_SECRET"}) {
		t.Errorf("unchanged = %v", got)
	}
	if n := f.mock.CallCount("DeleteUserData"); n != 1 {
		t.Errorf("DeleteUserData called %d times, want once for DB_HOST", n)
	}
	if n := f.mock.CallCount("CreateServiceEnvVar"); n != 1 {
		t.Errorf("CreateServiceEnvVar called %d times, want once", n)
	}

	// A second call finds nothing to write.
	again := f.apply(t)
	if len(again.Written) != 0 || !again.Done {
		t.Fatalf("second call wrote %v (done=%v), want nothing", again.Written, again.Done)
	}
	if n := f.mock.CallCount("CreateServiceEnvVar"); n != 1 {
		t.Errorf("the second call wrote again (%d creates)", n)
	}
}

func TestPoolMigrateEnv_FlagDiffers_SecretRewrittenSensitive(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	// The pool holds the dev's secret value, but plain: every token reads it.
	f.pool = append(f.pool, envRow("p-secret", "STRIPE_SECRET", envSecretValue, false))
	out := f.apply(t)
	if !slices.Contains(writtenKeys(out), "STRIPE_SECRET") {
		t.Fatalf("a secret held plain on the pool was not rewritten: %+v", out)
	}
	if r := f.poolRows(t)["STRIPE_SECRET"]; !r.Sensitive {
		t.Error("STRIPE_SECRET is still plain on the pool")
	}
}

// TestPoolMigrateEnv_MaskedPoolSecret_ComparedByRecord pins how the step
// compares a secret it cannot read back: by zcp's own fingerprint record of
// what it wrote on that pool service.
func TestPoolMigrateEnv_MaskedPoolSecret_ComparedByRecord(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.apply(t) // writes STRIPE_SECRET and records its fingerprint

	// The pool reads the secret back masked from now on (a Mate's key).
	f.mock.WithServiceEnv(envPoolID, maskSensitive(f.poolRows(t)))
	again := f.apply(t)
	if slices.Contains(writtenKeys(again), "STRIPE_SECRET") || !slices.Contains(again.Unchanged, "STRIPE_SECRET") {
		t.Fatalf("a masked secret matching the record was rewritten: written %v unchanged %v", again.Written, again.Unchanged)
	}

	// The dev's secret changes: the fingerprint differs, so it is written.
	f.container["STRIPE_SECRET"] = envOtherSecret
	f.mock.WithServiceEnv(envPoolID, maskSensitive(f.poolRows(t)))
	third := f.apply(t)
	if !slices.Contains(writtenKeys(third), "STRIPE_SECRET") {
		t.Fatalf("a changed dev secret was not written: %+v", third)
	}

	// A record of another pool service (the pool re-created) is no match.
	f.mock.WithServiceEnv(envPoolID, maskSensitive(f.poolRows(t)))
	f.in.Pool.ID = "svc-pool-recreated"
	f.mock.WithServices([]platform.ServiceStack{f.in.Dev, f.in.Pool, envStageService()}).
		WithServiceEnv("svc-pool-recreated", maskSensitive(f.poolRows(t))).
		WithProcess(&platform.Process{ID: "proc-envset-svc-pool-recreated", Status: platform.ProcessStatusFinished})
	for _, r := range f.poolRows(t) {
		f.mock.WithProcess(&platform.Process{ID: "proc-envdel-" + r.ID, Status: platform.ProcessStatusFinished})
	}
	fourth := f.apply(t)
	if !slices.Contains(writtenKeys(fourth), "STRIPE_SECRET") {
		t.Fatalf("a record of another pool service counted as a match: %+v", fourth)
	}
}

func maskSensitive(rows map[string]platform.ServiceEnvVar) []platform.ServiceEnvVar {
	out := make([]platform.ServiceEnvVar, 0, len(rows))
	for _, r := range rows {
		if r.Sensitive {
			r.Content = platform.MaskedEnvValue
		}
		out = append(out, r)
	}
	return out
}

func TestPoolMigrateEnv_MaskedBecomesRequest(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		container string
		unset     bool
	}{
		{name: "unset in the container", unset: true},
		{name: "empty in the container", container: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newEnvFixture(t)
			if tt.unset {
				delete(f.container, "STRIPE_SECRET")
			} else {
				f.container["STRIPE_SECRET"] = tt.container
			}
			out := f.apply(t)

			if len(out.Requests) != 1 || out.Requests[0].Key != "STRIPE_SECRET" {
				t.Fatalf("requests = %+v, want STRIPE_SECRET", out.Requests)
			}
			req := out.Requests[0]
			if req.Label != "Stripe secret" || !strings.Contains(req.Why, "appdev") || len(req.Why) > requestWhyMax {
				t.Errorf("request words = %+v", req)
			}
			if err := validateRequestKey(req.Key); err != nil {
				t.Errorf("zerops_env request would refuse the key: %v", err)
			}
			if err := ValidateRequestWords(req.Label, req.Why); err != nil {
				t.Errorf("zerops_env request would refuse the words: %v", err)
			}
			if _, has := f.poolRows(t)["STRIPE_SECRET"]; has {
				t.Fatal("a secret zcp could not read was written (a guess or an empty value)")
			}
			if !out.Done {
				t.Error("a secret asked of the person must not hold the step")
			}

			// The read side names the ask and counts the key as the person's.
			read := f.read(t)
			if !read.Done || len(read.Requests) != 1 || read.Requests[0].Key != "STRIPE_SECRET" {
				t.Fatalf("read after the ask: done=%v requests=%+v toWrite=%+v", read.Done, read.Requests, read.ToWrite)
			}

			// The person saves it on the pool: no ask is left, nothing rewrites it.
			f.mock.WithServiceEnv(envPoolID, append(maskSensitive(f.poolRows(t)), envMasked("p-person", "STRIPE_SECRET")))
			after := f.read(t)
			if len(after.Requests) != 0 || !slices.Contains(after.Present, "STRIPE_SECRET") {
				t.Fatalf("after the person saved it: requests=%+v present=%v", after.Requests, after.Present)
			}
		})
	}
}

func TestPoolMigrateEnv_ReadFails_NothingGuessedNoValueInError(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	// The session printed the secret before ssh failed: its output holds it.
	f.ssh = &envLocalSSH{err: errors.New("exit status 255"), output: "zcp-env 1\x00STRIPE_SECRET\x00" + envSecretValue}
	out := f.apply(t)

	if len(out.Failed) != 1 || out.Failed[0].Key != "STRIPE_SECRET" {
		t.Fatalf("failed = %+v, want STRIPE_SECRET", out.Failed)
	}
	if out.Done {
		t.Error("a read that did not answer must leave the step to do")
	}
	if len(out.Requests) != 0 {
		t.Errorf("a failed read is not the person's to answer: %+v", out.Requests)
	}
	if _, has := f.poolRows(t)["STRIPE_SECRET"]; has {
		t.Fatal("a secret was written without being read")
	}
	assertNoSecret(t, out, f, envSecretValue)
	if !strings.Contains(strings.Join(out.Warnings, " "), "exit status 255") {
		t.Errorf("warnings %v do not say why the read failed", out.Warnings)
	}
}

func TestPoolMigrateEnv_GitTokenRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		inMate     bool
		consent    bool
		devHas     bool
		poolHas    string // "" none, else its value
		unreadable bool
		wantState  string
		wantOnPool string // the pool's GIT_TOKEN after the step, "" for none
		wantRead   bool
	}{
		{name: "in a Mate never copied, even when told", inMate: true, consent: true, devHas: true, wantState: PoolGitTokenMateCredential},
		{name: "outside a Mate without a yes, asked", devHas: true, wantState: PoolGitTokenAsk},
		{name: "outside a Mate on yes, copied", consent: true, devHas: true, wantState: PoolGitTokenCopied, wantOnPool: envGitToken, wantRead: true},
		{name: "on yes, the pool already holds it", consent: true, devHas: true, poolHas: envGitToken, wantState: PoolGitTokenUnchanged, wantOnPool: envGitToken, wantRead: true},
		{name: "on yes, the pool holds another, replaced", consent: true, devHas: true, poolHas: "old-token", wantState: PoolGitTokenCopied, wantOnPool: envGitToken, wantRead: true},
		{name: "without a yes, the pool's own left alone", devHas: true, poolHas: "pool-own", wantState: PoolGitTokenOnPool, wantOnPool: "pool-own"},
		{name: "the dev holds none", consent: true, wantState: PoolGitTokenDevHasNone},
		{name: "on yes, unreadable on the dev", consent: true, devHas: true, unreadable: true, wantState: PoolGitTokenUnreadable, wantRead: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newEnvFixture(t)
			f.in.InMate, f.in.CopyGitToken = tt.inMate, tt.consent
			if tt.devHas {
				f.dev = append(f.dev, envMasked("d-git", GitTokenEnvKey))
				if !tt.unreadable {
					f.container[GitTokenEnvKey] = envGitToken
				}
			}
			if tt.poolHas != "" {
				f.pool = append(f.pool, envRow("p-git", GitTokenEnvKey, tt.poolHas, true))
			}
			out := f.apply(t)

			if out.GitToken.State != tt.wantState || out.GitToken.Message == "" {
				t.Fatalf("gitToken = %+v, want %s", out.GitToken, tt.wantState)
			}
			row, has := f.poolRows(t)[GitTokenEnvKey]
			switch {
			case tt.wantOnPool == "" && has:
				t.Errorf("the pool holds a GIT_TOKEN it should not")
			case tt.wantOnPool != "" && (!has || row.Content != tt.wantOnPool || !row.Sensitive):
				t.Errorf("the pool's GIT_TOKEN: has=%v exact=%v sensitive=%v", has, row.Content == tt.wantOnPool, row.Sensitive)
			}
			read := false
			for _, call := range f.ssh.recorded() {
				read = read || strings.Contains(call, "'"+GitTokenEnvKey+"'")
			}
			if read != tt.wantRead {
				t.Errorf("GIT_TOKEN read on the dev = %v, want %v", read, tt.wantRead)
			}
			if !out.Done {
				t.Errorf("GIT_TOKEN must never hold the step: %+v", out)
			}
			assertNoSecret(t, out, f, envGitToken)
		})
	}
}

// TestPoolMigrateEnv_GitTokenZcpCopied_NotOfferedAgain: once zcp copied the
// dev's GIT_TOKEN to the pool — its record fingerprints the value it wrote
// on that pool service — the read side, and a later step without
// copyGitToken, answer that the pool holds the dev's: neither asks for the
// person's yes again, and the record keeps the fingerprint. A GIT_TOKEN that
// is not the one zcp wrote is the pool's own (on-pool), and is offered as
// before.
func TestPoolMigrateEnv_GitTokenZcpCopied_NotOfferedAgain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		mask      bool   // the pool's GIT_TOKEN reads masked for zcp's token from now on
		replaced  string // the person put this GIT_TOKEN on the pool after the copy
		wantState string
	}{
		{name: "the pool shows the value zcp wrote", wantState: PoolGitTokenUnchanged},
		{name: "the pool's value is masked", mask: true, wantState: PoolGitTokenUnchanged},
		{name: "the person replaced it since", replaced: "pool-own-token", wantState: PoolGitTokenOnPool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newEnvFixture(t)
			f.dev = append(f.dev, envMasked("d-git", GitTokenEnvKey))
			f.container[GitTokenEnvKey] = envGitToken
			f.in.CopyGitToken = true
			if out := f.apply(t); out.GitToken.State != PoolGitTokenCopied {
				t.Fatalf("gitToken on yes = %+v, want copied", out.GitToken)
			}
			f.in.CopyGitToken = false
			rows := f.poolRows(t)
			if tt.replaced != "" {
				row := rows[GitTokenEnvKey]
				row.Content = tt.replaced
				rows[GitTokenEnvKey] = row
			}
			pool := make([]platform.ServiceEnvVar, 0, len(rows))
			for _, r := range rows {
				pool = append(pool, r)
			}
			if tt.mask {
				pool = maskSensitive(rows)
			}
			f.mock.WithServiceEnv(envPoolID, pool)

			check := func(t *testing.T, side string, got PoolMigrateGitToken) {
				t.Helper()
				if got.State != tt.wantState || got.Message == "" {
					t.Fatalf("%s: gitToken = %+v, want %s", side, got, tt.wantState)
				}
				if offers := strings.Contains(got.Message, "copyGitToken=true"); offers != (tt.wantState == PoolGitTokenOnPool) {
					t.Errorf("%s: message %q offers the copy = %v", side, got.Message, offers)
				}
			}
			check(t, "the read side", f.read(t).GitToken)
			before := len(f.ssh.recorded())
			check(t, "a step without copyGitToken", f.apply(t).GitToken)
			for _, call := range f.ssh.recorded()[before:] {
				if strings.Contains(call, "'"+GitTokenEnvKey+"'") {
					t.Fatalf("GIT_TOKEN was read without the person's yes: %s", call)
				}
			}
			check(t, "the read side after that step", f.read(t).GitToken)
		})
	}
}

func TestPoolMigrateEnv_StageKeyDiff(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.dev = append(f.dev, envRow("d-only", "DEV_ONLY", "x", false), envRow("d-api", "API_TOKEN", "app-token", true))
	f.stage = append(f.stage, envRow("s-only", "STAGE_ONLY", "y", false), envRow("s-ssh", "sshIsolation", "vpn", false), envRow("s-git", GitTokenEnvKey, "t", true))
	f.container["DEV_ONLY"] = "x"
	out := f.apply(t)

	want := []PoolMigrateStageDiff{{Stage: "appstage", StageOnly: []string{"STAGE_ONLY"}, PoolOnly: []string{"DB_HOST", "DEV_ONLY"}}}
	if !reflect.DeepEqual(out.StageDiff, want) {
		t.Fatalf("stageDiff = %+v, want %+v", out.StageDiff, want)
	}
	// Values are the dev's: the stage's APP_URL never reached the pool.
	if r := f.poolRows(t)["APP_URL"]; r.Content != "${zeropsSubdomain}/app" {
		t.Errorf("APP_URL on the pool = %q, want the dev's", r.Content)
	}
	// The read side names the same difference before anything is written.
	g := newEnvFixture(t)
	g.dev, g.stage, g.container = f.dev, f.stage, f.container
	if read := g.read(t); !reflect.DeepEqual(read.StageDiff, want) {
		t.Errorf("read-side stageDiff = %+v, want %+v", read.StageDiff, want)
	}
}

// poolEnvAllowedCalls are the platform calls the env step may make: reads, the
// userData delete and create, and following their processes. Nothing
// restarts, reloads, stops or starts.
var poolEnvAllowedCalls = map[string]bool{
	"GetService":            true,
	"GetAppVersionUserData": true,
	"DeleteUserData":        true,
	"CreateServiceEnvVar":   true,
}

func TestPoolMigrateEnv_NoRestart(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.pool = append(f.pool, envRow("p-db", "DB_HOST", "old", false))
	out := f.apply(t)
	if len(out.Written) == 0 {
		t.Fatal("the fixture must write something")
	}
	for method, n := range f.mock.CallCounts {
		if n > 0 && !poolEnvAllowedCalls[method] {
			t.Errorf("the env step called %s %d times", method, n)
		}
	}
	for _, call := range f.ssh.recorded() {
		if strings.Contains(call, "restart") || strings.Contains(call, "kill") || strings.Contains(call, "zsc") {
			t.Errorf("the env step ran %q on the dev", call)
		}
	}
}

// assertNoSecret fails when secret appears in the step's result, its
// warnings, the commands sent over ssh or any file under the state dir.
func assertNoSecret(t *testing.T, out *PoolMigrateEnv, f *envFixture, secret string) {
	t.Helper()
	needles := []string{secret, strings.TrimRight(secret, "\n")}
	if line, _, ok := strings.Cut(secret, "\n"); ok {
		needles = append(needles, line)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	haystacks := map[string]string{"result": string(b)}
	for i, call := range f.ssh.recorded() {
		haystacks["ssh command "+string(rune('0'+i))] = call
	}
	_ = filepath.WalkDir(f.in.StateDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(path)
			haystacks[path] = string(data)
		}
		return nil
	})
	for where, text := range haystacks {
		for _, needle := range needles {
			if needle != "" && strings.Contains(text, needle) {
				t.Errorf("the secret appears in %s", where)
			}
		}
	}
}

func TestPoolMigrateEnv_SecretNeverInResult(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.in.CopyGitToken = true
	f.dev = append(f.dev, envMasked("d-git", GitTokenEnvKey), envRow("d-other", "OTHER_SECRET", envOtherSecret, true))
	f.container[GitTokenEnvKey] = envGitToken
	f.container["OTHER_SECRET"] = envOtherSecret
	out := f.apply(t)
	if len(out.Written) < 3 {
		t.Fatalf("the fixture must copy the secrets: %+v", out)
	}
	for _, secret := range []string{envSecretValue, envOtherSecret, envGitToken} {
		assertNoSecret(t, out, f, secret)
	}
	// The record keeps fingerprints, never a value, readable by its owner only.
	info, err := os.Stat(poolEnvRecordPath(f.in.StateDir, "apppool"))
	if err != nil {
		t.Fatalf("no record: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("record mode = %v, want 0600", info.Mode().Perm())
	}
	// The read side carries none either.
	read := f.read(t)
	for _, secret := range []string{envSecretValue, envOtherSecret, envGitToken} {
		assertNoSecret(t, read, f, secret)
	}
}

func TestPoolMigrateEnv_WriteFailure_NamedByCodeOnly(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.build()
	// An error whose message echoes what was sent.
	echo := platform.NewPlatformError(platform.ErrAPIError, "invalid content "+envSecretValue, "")
	echo.APICode = "invalidUserInput"
	f.mock.WithError("CreateServiceEnvVar", echo)
	out := f.apply(t)

	if len(out.Failed) != 3 || out.Done {
		t.Fatalf("failed = %+v done=%v, want every key failed and the step not done", out.Failed, out.Done)
	}
	for _, fl := range out.Failed {
		if !strings.Contains(fl.Reason, "invalidUserInput") {
			t.Errorf("failure %+v does not name the platform's code", fl)
		}
	}
	assertNoSecret(t, out, f, envSecretValue)
}

func TestPoolMigrateEnv_PoolZeropsYamlKey_NeverWritten(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.in.Pool.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-pool"}
	f.pool = append(f.pool, envRow("p-yaml", "DB_HOST", "${db_hostname}", false))
	f.build()
	f.mock.WithServices([]platform.ServiceStack{f.in.Dev, f.in.Pool, envStageService()}).
		WithAppVersionUserData("av-pool", []platform.ServiceEnvVar{{Key: "DB_HOST", Content: "${db_hostname}"}})
	out := f.apply(t)

	if slices.Contains(writtenKeys(out), "DB_HOST") {
		t.Fatal("a key the pool's zerops.yaml owns was written")
	}
	if !slices.Contains(out.NeverCopied, PoolMigrateEnvSkip{Key: "DB_HOST", Reason: PoolEnvReasonPoolZeropsYaml}) {
		t.Errorf("neverCopied = %+v, want DB_HOST owned by the pool's zerops.yaml", out.NeverCopied)
	}
}

func TestPoolMigrateEnv_DevZeropsYamlKey_NotCopied(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.in.Dev.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-dev"}
	f.build()
	f.mock.WithServices([]platform.ServiceStack{f.in.Dev, f.in.Pool, envStageService()}).
		WithAppVersionUserData("av-dev", []platform.ServiceEnvVar{{Key: "APP_URL", Content: "${zeropsSubdomain}/app"}})
	out := f.apply(t)
	if _, has := f.poolRows(t)["APP_URL"]; has || slices.Contains(writtenKeys(out), "APP_URL") {
		t.Fatal("a key the dev's zerops.yaml owns was copied as service env")
	}
}

func TestReadPoolMigrateEnv_ReadsNoSecretWritesNothing(t *testing.T) {
	t.Parallel()
	f := newEnvFixture(t)
	f.pool = append(f.pool, envRow("p-url", "APP_URL", "${zeropsSubdomain}/app", false), envMasked("p-secret", "STRIPE_SECRET"))
	before := snapshotEnvDir(t, f.in.StateDir)
	out := f.read(t)

	if got := []PoolMigrateEnvKey{{Key: "DB_HOST", Change: PoolEnvChangeAdd}}; !reflect.DeepEqual(out.ToWrite, got) {
		t.Errorf("toWrite = %+v, want %+v", out.ToWrite, got)
	}
	if !reflect.DeepEqual(out.Present, []string{"STRIPE_SECRET"}) || !reflect.DeepEqual(out.Unchanged, []string{"APP_URL"}) {
		t.Errorf("present = %v unchanged = %v", out.Present, out.Unchanged)
	}
	if out.Done || out.DefaultRepo.State != PoolDefaultRepoPending {
		t.Errorf("done = %v defaultRepo = %+v, want pending", out.Done, out.DefaultRepo)
	}
	if out.GitToken.State != PoolGitTokenDevHasNone {
		t.Errorf("gitToken = %+v", out.GitToken)
	}
	for _, m := range []string{"CreateServiceEnvVar", "DeleteUserData"} {
		if n := f.mock.CallCount(m); n != 0 {
			t.Errorf("the read side called %s %d times", m, n)
		}
	}
	if after := snapshotEnvDir(t, f.in.StateDir); !reflect.DeepEqual(before, after) {
		t.Errorf("the read side wrote under the state dir: %v -> %v", before, after)
	}
}

func snapshotEnvDir(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		data, _ := os.ReadFile(path)
		out[path] = string(data)
		return nil
	})
	return out
}

// TestPoolMigrateEnvRead_RealShell_ValuesExact runs the read command in a real
// shell: every value arrives byte for byte — spaces, quotes, `$`, backticks,
// globs and newlines unexpanded and unsplit — an unset key is absent, and
// nothing reaches stderr.
func TestPoolMigrateEnvRead_RealShell_ValuesExact(t *testing.T) {
	t.Parallel()
	env := map[string]string{
		"A_SECRET": envSecretValue,
		"B_SPACES": "  leading and trailing  ",
		"C_EQUALS": "k=v;x=y|z&w",
		"D_EMPTY":  "",
	}
	keys := []string{"A_SECRET", "B_SPACES", "C_EQUALS", "D_EMPTY", "E_UNSET"}
	script := poolEnvReadCommand(keys)
	cmd := exec.CommandContext(t.Context(), "sh", "-c", script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the read command failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("the read command wrote to stderr (%d bytes)", stderr.Len())
	}
	values, perr := parsePoolEnvRead([]byte("ssh banner noise\n"+stdout.String()), keys)
	if perr != "" {
		t.Fatalf("parse: %s", perr)
	}
	want := map[string]string{"A_SECRET": envSecretValue, "B_SPACES": env["B_SPACES"], "C_EQUALS": env["C_EQUALS"]}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("values differ from the environment (keys %v, want %v)", slices.Sorted(mapsKeys(values)), slices.Sorted(mapsKeys(want)))
	}
	for _, key := range keys {
		if !strings.Contains(poolEnvReadCommand(keys), "'"+key+"'") {
			t.Errorf("key %s is not a single-quoted word in the command", key)
		}
	}
}

func mapsKeys(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func TestParsePoolEnvRead_CutShortOrMalformed_NoValue(t *testing.T) {
	t.Parallel()
	keys := []string{"K"}
	for name, out := range map[string]string{
		"no begin":         "K\x00" + envOtherSecret + "\n\x00ok\x00",
		"no end":           "zcp-env 1\x00K\x00" + envOtherSecret + "\n\x00ok\x00",
		"cut in a record":  "zcp-env 1\x00K\x00" + envOtherSecret,
		"unknown status":   "zcp-env 1\x00K\x00" + envOtherSecret + "\n\x00maybe\x00zcp-env-end\x00",
		"no final newline": "zcp-env 1\x00K\x00" + envOtherSecret + "\x00ok\x00zcp-env-end\x00",
	} {
		values, perr := parsePoolEnvRead([]byte(out), keys)
		if perr == "" || values != nil {
			t.Errorf("%s: parsed %d values, want a failure", name, len(values))
		}
		if strings.Contains(perr, envOtherSecret) {
			t.Errorf("%s: the failure carries the value", name)
		}
	}
	// A key zcp did not ask for is dropped.
	values, perr := parsePoolEnvRead([]byte("zcp-env 1\x00X\x00v\n\x00ok\x00zcp-env-end\x00"), keys)
	if perr != "" || len(values) != 0 {
		t.Errorf("an unasked key was kept: %v %q", values, perr)
	}
}

func TestSeedPoolDefaultRepo_CheckBeforeWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const origin = "https://github.com/example/app.git"
	if got := poolDefaultRepoState(dir, "apppool", origin); got.State != PoolDefaultRepoPending || got.Repo != origin {
		t.Fatalf("state before = %+v", got)
	}
	if got := SeedPoolDefaultRepo(dir, "apppool", origin); got.State != PoolDefaultRepoSeeded || got.Previous != "" {
		t.Fatalf("first seed = %+v", got)
	}
	if got := readPoolDefaultRepo(dir, "apppool"); got != origin {
		t.Fatalf("the lease's record names %q, want the origin as given (DP-18)", got)
	}
	if got := SeedPoolDefaultRepo(dir, "apppool", origin+"/"); got.State != PoolDefaultRepoUnchanged {
		t.Fatalf("second seed = %+v, want unchanged", got)
	}
	if err := storePoolDefaultRepo(dir, "apppool", "https://github.com/example/other"); err != nil {
		t.Fatal(err)
	}
	if got := SeedPoolDefaultRepo(dir, "apppool", origin); got.State != PoolDefaultRepoSeeded || got.Previous != "https://github.com/example/other" {
		t.Fatalf("seed over another repository = %+v", got)
	}
	for _, bad := range []string{"", "git@github.com:example/app.git", "https://user:pw@github.com/x/y"} {
		if got := SeedPoolDefaultRepo(dir, "apppool", bad); got.State != PoolDefaultRepoUnavailable || got.Reason == "" {
			t.Errorf("origin %q: %+v, want unavailable with a reason", bad, got)
		}
	}
}

// hostEnvSSH is envLocalSSH per host: each host's fresh session reads its own
// environment, and a host it does not know fails.
type hostEnvSSH struct {
	mu    sync.Mutex
	hosts map[string]*envLocalSSH
	calls []string
}

func (s *hostEnvSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, hostname+": "+command)
	host := s.hosts[hostname]
	s.mu.Unlock()
	if host == nil {
		return nil, &platform.SSHExecError{Hostname: hostname, Err: errors.New("no such host")}
	}
	return host.ExecSSH(ctx, hostname, command)
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *hostEnvSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *hostEnvSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, errors.New("the env step never runs a background command")
}

func (s *hostEnvSSH) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

// TestPoolMigrateEnv_GitTokenFromThePairsPushSource: a variant-B part runs on
// a dev service of its own, whose env its pool takes, but the pair is one
// repository whose one GIT_TOKEN sits on the dev half the pair pushes from
// (GitTokenFrom). The pool is offered that one and, on the person's yes, it
// is read in that service's container — the part's own dev service is never
// asked for a GIT_TOKEN, and one it holds is not the pair's — while every
// other key still comes from the part's dev service. A read of the push
// source that does not answer fails the GIT_TOKEN alone.
func TestPoolMigrateEnv_GitTokenFromThePairsPushSource(t *testing.T) {
	t.Parallel()
	webdev := platform.ServiceStack{ID: "svc-webdev", Name: "webdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}}
	tests := []struct {
		name        string
		sourceHas   bool
		partHas     bool
		consent     bool
		sourceFails bool
		wantState   string
		wantNamed   string
		wantOnPool  string
		wantRead    bool
	}{
		{name: "asked for the push source's", sourceHas: true, wantState: PoolGitTokenAsk, wantNamed: "webdev's GIT_TOKEN"},
		{name: "copied from the push source on yes", sourceHas: true, consent: true, wantState: PoolGitTokenCopied, wantNamed: "webdev's GIT_TOKEN", wantOnPool: envGitToken, wantRead: true},
		{name: "the part's own dev service's is not the pair's", partHas: true, consent: true, wantState: PoolGitTokenDevHasNone, wantNamed: "webdev holds no GIT_TOKEN"},
		{name: "the push source unreadable", sourceHas: true, consent: true, sourceFails: true, wantState: PoolGitTokenFailed, wantNamed: "webdev's GIT_TOKEN", wantRead: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newEnvFixture(t)
			f.in.GitTokenFrom, f.in.CopyGitToken = webdev, tt.consent
			if tt.partHas {
				f.dev = append(f.dev, envMasked("d-git", GitTokenEnvKey))
				f.container[GitTokenEnvKey] = "part-own-token"
			}
			var sourceRows []platform.ServiceEnvVar
			if tt.sourceHas {
				sourceRows = append(sourceRows, envMasked("w-git", GitTokenEnvKey))
			}
			f.build()
			f.mock.WithServices([]platform.ServiceStack{f.in.Dev, f.in.Pool, envStageService(), webdev}).
				WithServiceEnv(webdev.ID, sourceRows)
			source := &envLocalSSH{env: map[string]string{GitTokenEnvKey: envGitToken}}
			if tt.sourceFails {
				source.err = errors.New("exit status 255")
			}
			ssh := &hostEnvSSH{hosts: map[string]*envLocalSSH{"appdev": {env: f.container}, "webdev": source}}

			out, err := ApplyPoolMigrateEnv(context.Background(), f.mock, ssh, f.in)
			if err != nil {
				t.Fatalf("ApplyPoolMigrateEnv: %v", err)
			}
			if out.GitToken.State != tt.wantState || !strings.Contains(out.GitToken.Message, tt.wantNamed) {
				t.Fatalf("gitToken = %+v, want %s naming %q", out.GitToken, tt.wantState, tt.wantNamed)
			}
			rows := f.poolRows(t)
			row, has := rows[GitTokenEnvKey]
			switch {
			case tt.wantOnPool == "" && has:
				t.Errorf("the pool holds a GIT_TOKEN it should not")
			case tt.wantOnPool != "" && (!has || row.Content != tt.wantOnPool || !row.Sensitive):
				t.Errorf("the pool's GIT_TOKEN: has=%v exact=%v sensitive=%v", has, row.Content == tt.wantOnPool, row.Sensitive)
			}
			if r := rows["STRIPE_SECRET"]; r.Content != envSecretValue || !r.Sensitive {
				t.Errorf("the part's own secret was not copied from appdev: sensitive=%v exact=%v", r.Sensitive, r.Content == envSecretValue)
			}
			read := false
			for _, call := range ssh.recorded() {
				if !strings.Contains(call, "'"+GitTokenEnvKey+"'") {
					continue
				}
				read = true
				if !strings.HasPrefix(call, "webdev: ") {
					t.Errorf("GIT_TOKEN was read as %q, want it read on webdev only", call)
				}
			}
			if read != tt.wantRead {
				t.Errorf("GIT_TOKEN read = %v, want %v", read, tt.wantRead)
			}
			if rec, ok := readPoolEnvRecord(f.in.StateDir, f.in.Pool.Name); !ok || rec.Fingerprints["STRIPE_SECRET"] == "" {
				t.Errorf("the record of the part's secret = %+v, want it kept", rec)
			}
			assertNoSecret(t, out, f, envGitToken)
			assertNoSecret(t, out, f, "part-own-token")

			if tt.wantState == PoolGitTokenCopied {
				if got := f.read(t).GitToken; got.State != PoolGitTokenUnchanged || !strings.Contains(got.Message, "webdev's GIT_TOKEN") {
					t.Errorf("read side after the copy = %+v, want unchanged naming webdev's", got)
				}
			}
		})
	}
}

// TestPoolMigrateEnv_DevReadFails_GitTokenCopiedFromElsewhereIsRecorded: the
// pair's GIT_TOKEN lives on another service, which is read on its own. When
// the dev's secret read fails but that read works, the GIT_TOKEN this run
// copied is still recorded as zcp's copy — keeping what the record knew of
// the dev's unread secrets — so the read side calls it zcp's copy (never the
// pool's own) and a later run does not write it again.
func TestPoolMigrateEnv_DevReadFails_GitTokenCopiedFromElsewhereIsRecorded(t *testing.T) {
	t.Parallel()
	webdev := platform.ServiceStack{ID: "svc-webdev", Name: "webdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}}
	f := newEnvFixture(t)
	f.in.GitTokenFrom, f.in.CopyGitToken = webdev, true
	if err := savePoolEnvRecord(f.in.StateDir, f.in.Pool.Name, poolEnvRecord{
		PoolServiceID: envPoolID, Fingerprints: map[string]string{"STRIPE_SECRET": "fp-of-an-earlier-copy"},
	}); err != nil {
		t.Fatal(err)
	}
	f.build()
	f.mock.WithServices([]platform.ServiceStack{f.in.Dev, f.in.Pool, envStageService(), webdev}).
		WithServiceEnv(webdev.ID, []platform.ServiceEnvVar{envMasked("w-git", GitTokenEnvKey)})
	dev := &envLocalSSH{env: f.container, err: errors.New("exit status 255")}
	ssh := &hostEnvSSH{hosts: map[string]*envLocalSSH{"appdev": dev, "webdev": {env: map[string]string{GitTokenEnvKey: envGitToken}}}}

	out, err := ApplyPoolMigrateEnv(context.Background(), f.mock, ssh, f.in)
	if err != nil {
		t.Fatalf("ApplyPoolMigrateEnv: %v", err)
	}
	if out.GitToken.State != PoolGitTokenCopied {
		t.Fatalf("gitToken = %+v, want copied from webdev", out.GitToken)
	}
	rec, ok := readPoolEnvRecord(f.in.StateDir, f.in.Pool.Name)
	if !ok || rec.Fingerprints[GitTokenEnvKey] == "" {
		t.Fatalf("record = %+v, want the GIT_TOKEN this run copied fingerprinted", rec)
	}
	if rec.Fingerprints["STRIPE_SECRET"] != "fp-of-an-earlier-copy" {
		t.Errorf("record = %+v, want what it knew of the unread STRIPE_SECRET kept", rec)
	}
	if got := f.read(t).GitToken; got.State != PoolGitTokenUnchanged {
		t.Errorf("read side = %+v, want unchanged: the pool's GIT_TOKEN is zcp's copy, not the pool's own", got)
	}
}

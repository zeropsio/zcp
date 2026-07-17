# Data Console Testing Architecture

> **Scope**: the governing map for every Data Console verification surface —
> which tier proves which behavior, the rule that decides where a new check
> lands, and the contracts that make coverage MECHANICAL (derived from runtime
> owners) instead of opportunistic. Child of `spec-testing-architecture.md`
> (the repo-wide tier rule); peer of `spec-dataconsole.md` (whose §7 excellence
> contracts are the acceptance criteria this architecture proves).

---

## 1. The five tiers

| Tier | Where | Runs | Proves |
|---|---|---|---|
| T1 offline Go | provider/server/seed/boundary `*_test.go` (untagged) | CI, every PR | shape: taxonomy, error mapping, value fidelity, write-token authority, island boundary |
| T2 offline JS/DOM | `webui/spa/*.test.js`, `webui/domtest/*.dom.test.js`, `extension/studiojs/*.test.js` (Go exec(node) harnesses) | CI, every PR (§7) | SPA + broker logic, DOM state canon |
| T3 hermetic server | `console/server/*_test.go` (fake providers/hosts) | CI, every PR | route matrix, envelope, caller-bound writes, presentation |
| T4 live conformance | `console/provider/conformance/` (`//go:build e2e`) | `make dc-live[-full|-remote]` (§6) | real-engine truth: the engine × proof matrix (§4) |
| T5 assembled UI | `internal/dataconsole/uitest/` (puppeteer) | the UI-validation program's lanes | webview → broker → server → engine as ONE system |

Placement rule (inherits `spec-testing-architecture.md` §2): the FIRST tier
that can prove the behavior is its home. Anything provable offline never lands
in T4/T5; engine truth never claims proof from a fake.

## 2. ServiceProfile — the canonical enumerable owner

`provider.ServiceProfiles` is THE single owner of the service-type universe:
`ServiceProfile{BaseType, Family, Support, ProvenBy}`. `Classify` and
`SupportFor` are DERIVATIONS of this registry, never parallel switches. A type
the registry does not list falls to `FamilyUnknown`/`SupportNotYet` and is
surfaced honestly, never mis-rendered.

- **ProvenBy equivalence**: a profile may declare `ProvenBy: "<baseType>"` — an
  explicit, reviewed statement that live proof is carried by an equivalent
  engine (v1: `mysql` → `mariadb`, owner decision DD-B). The coverage lint (§4)
  VERIFIES the reference resolves to a registered profile with the same family,
  support, and applicable proof set. Equivalence never satisfies type/version
  identity (§8) and never satisfies a proof declared dialect-specific.

## 3. Production-shaped construction

One island-local factory (`console/provider/factory`) builds providers from
typed connection descriptors for BOTH production (`cmd/zcp/studio_console.go`)
and the T4 conformance harness. The harness constructs with the REAL posture —
it has no private read-only switch, so full-tier engines are live-proven under
armed writes. Provider constructors remain the ultimate owners of intrinsic
posture (e.g. `tabular.New` forces `NoEdit` + `readonly=1` for clickhouse);
the factory is composition/reuse, never a second policy site.

**Server action-policy guard**: every mutating route enforces the route's
`ActionID` against the target service's action policy (`ServiceActions`) —
checked after target resolution (body-addressed routes included), before
provider dispatch. A disabled affordance in the SPA is presentation; THIS is
the enforcement. Two distinct refusals: an action PRESENT but disabled
(view-only tier, read-only posture) refuses with the uniform `ErrReadOnly`
(no oracle on which capability condition failed, `spec-dataconsole.md` §5.1);
an action ABSENT from the family's list entirely (upload on a tabular
service) refuses `ErrUnsupported` (422) — the per-service action set is
public knowledge via `GET /api/services`, so this leaks nothing, while a
read-only answer would send an authorized armed caller into a futile re-arm
loop. Pinned by a route-matrix test across ALL mutating routes, not a
single-route sample.

## 4. The proof matrix — declared ⇒ proven

Two proof levels, both derived, both enforced by an OFFLINE lint
(`conformance/coverage_test.go`, untagged):

1. **Action proofs**: enabled `ServiceActions` map onto semantic `ProofID`
   clusters at the smallest independent backend failure mode (several UI
   actions may share one live write-roundtrip proof; their distinct routes stay
   T3).
2. **Engine-contract proofs**: facts actions cannot express — task-confirmed
   writes (meilisearch), vector metadata (qdrant), value fidelity (§7.3 of
   `spec-dataconsole.md`), error parity across sibling engines, insert key
   echo, pagination, read-only query enforcement.

The lint derives the REQUIRED proof set from the §2 registry × §3 actions and
fails on any declared-but-unproven capability. Exceptions exist only as
explicit registry state (`ProvenBy`), never as silence.

Repo-context lints (the AST test-name scan, the version-matrix YAML shape
check) SKIP with a logged reason exactly when the package source tree is
absent — i.e. in a shipped-compiled-binary run (`dc-live-remote`), where the
engine × proof matrix is the lane's claim and the sources are legitimately
not on disk. The guard keys on the source TREE, never the individual input
file, so a missing input inside a present repo still fails loudly
(`conformance/sourcectx_test.go`).

**Support-tier proof shape**: full-tier engines prove positive writes under
armed posture; view-only engines carry an offline policy/constructor proof
PLUS one live writes-armed mutation-refusal cell each; not-yet/unknown types
are proven offline only (honest zero-affordance rendering).

## 5. Typed manifest and profiles

`DC_LIVE_MANIFEST` is a typed manifest — entries carry `{hostname, baseType,
version?}` — replacing bare hostnames, so a manifest slot cannot silently
point at the wrong engine or double-count a family.

- **full profile**: derives the exact engine × ProofID matrix from manifest ×
  §4 and asserts it in Go. A missing or failed REQUIRED cell fails the run.
  The release manifest is one-of-each: all standing engines of every
  full+view-only type (11 on `zcp-eval-clean`).
- **partial profile** (dev default): only configured cells derive; absence,
  unreachability, or setup failure may skip with a logged reason. An EXECUTED
  semantic assertion failure stays red in every profile.
- `TestMain` preserves any nonzero `m.Run()`; the matrix gate only ADDS
  failure (missing/failed required cells) — it never masks one.

**Ledger**: every case records `{caseID, hostname, baseType, family, proofID,
outcome, phase, reason, duration, declaredVersion, observedVersion,
revision, runID}` via wrapped cleanup (a `t.Fatalf` path still records
`fail`). The Go assertions are the gate; the JSON serialization of the same
ledger is the evidence artifact (gitignored).

**Cross-process serialization**: live runs take a project-scoped lock (bounded
acquisition, stale-lock recovery, runID recorded) so a dev run and a release
run cannot interleave fixtures. The stable fixture namespace stays, for crash
recovery, always behind the lock.

## 6. Live-lane operations

- `make dc-live` / `dc-live-full` — from a VPN-connected machine
  (`zcli vpn up`), config via `DC_LIVE_CONFIG`. VPN carries sockets, NOT
  credentials — config generation fetches creds via the REST env API.
- `make dc-live-remote` — the canonical release run, executed ON the zcp
  container (in-project network + REST creds; no VPN dependency):
  `CGO_ENABLED=0 GOOS=linux` `go test -c -tags e2e` binary + the config
  helper from the SAME revision, shipped over SSH (verified host keys) to a
  private remote temp dir (umask 077, 0600 files); invocation via `-test.*`
  flags; credentials never on stdout/argv/logs; the summary/ledger is ALWAYS
  retrieved (nonzero exit included); signal/exit cleanup removes the remote
  dir.
- **Release checklist contract**: a release requires a full-profile ledger
  artifact whose `revision` matches the release commit and whose age is
  within the checklist's window. Until a self-hosted runner exists, the
  checklist (not CI) is the enforcement point.

## 7. The JS/DOM CI lane

CI runs T2 as a named step: pinned Node (`setup-node`), `npm ci` in `webui/`,
then the Go exec harnesses. `ZCP_JS_REQUIRED=1` (set in CI) turns a
missing-node/jsdom skip into a FAILURE in all three harnesses
(`spa_jstest_test.go`, `spa_domtest_test.go`, `studio_jstest_test.go`);
locally, absence still skips. The non-empty glob guards stay: a harness that
finds zero test files fails.

## 8. Version policy

- **Standing testbed**: one pinned version per type; the ledger records
  declared (platform REST) and observed (protocol handshake) versions;
  drift is warn-only — the platform upgrades server-side.
- **Throwaway compatibility runs**: `e2e/testdata/dataconsole/
  version-matrix.import.yaml` stands up the multi-version types' alternate
  versions in a disposable project (one alternate per multi-version type;
  live-verified 2026-07-18: postgresql@17, elasticsearch@8.16,
  meilisearch@1.10, qdrant@1.10, nats@2.10 — kafka's second version was
  retired from the live catalog, making it de-facto single-version; the
  api-tier test `internal/schema/dataconsole_version_matrix_test.go` pins the
  YAML against the LIVE catalog so this list can never silently rot); the run
  HARD-asserts platform-declared type/version == requested before any proof
  executes — a substituted version fails the run, it never silently "proves"
  the wrong engine. Cadence: release-gated, plus a monthly provisioning
  rehearsal.

## 9. Relationship to other specs

- `spec-testing-architecture.md` §2 — tier semantics (T4 is the e2e-tagged
  data-plane suite named there).
- `spec-dataconsole.md` §7 — the excellence contracts T4 proves; §5 the write
  posture §3's server guard enforces.
- The UI-validation program owns T5's method (three oracles, two lanes).

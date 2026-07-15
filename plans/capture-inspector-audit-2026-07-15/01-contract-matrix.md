# Capture Inspector audit — contract matrix

Status legend: PASS, FAIL, PARTIAL, NOT RUN.

| Contract | Authority | Evidence | Status | Finding |
|---|---|---|---|---|
| Raw canonical files are append-only and inspection is read-only | spec §2.1, §8.2 | four-corpus before/after SHA-256 and metadata inventories | PASS | — |
| Provider/MCP observer does not alter disabled protocol behavior | spec §2.2 | exact 184-byte base/head MCP transcript; proxy and partial-I/O tests | PASS | — |
| Capture loss and failures cannot be silent | spec §2.3 | late-drain and incomplete-stream RED probes | FAIL | `LIFE-002`, `INT-002`, `CORR-002` |
| Completeness is distinct from hash validity | spec §2.4 | terminal-only provider/MCP and open lifecycle-scope probes | FAIL | `INT-002`, `INT-003` |
| Every derived claim has evidence coordinates and equality basis | spec §2.5, §8.1 | synthetic and real-corpus metric audit; graph correlation probe | FAIL | `CORR-001`, `CORR-003`, `EVID-001` |
| Credential headers are structurally absent | spec §2.6 | 3,648 provider records; zero credential-header occurrences | PASS | — |
| Missing client identity remains unattributed | spec §2.7, §5 | focused inspector tests and source review | PASS | — |
| `on/off/status` is transactional and reconciles daemon/configuration | spec §3 | real sandbox happy path plus fault probe | FAIL | `LIFE-001` |
| Late capture failure downgrades terminal status | spec §3, §7 | deterministic drain-time fault injection | FAIL | `LIFE-002` |
| Manifest identity agrees with canonical record identity | spec §7–8 | copied-record and symlink-parent probes | FAIL | `INT-001`, `INT-004` |
| Provider/MCP streams prove required start and end boundaries | spec §7–8 | terminal-only stream probes | FAIL | `INT-002` |
| Lifecycle hierarchy is complete or explicitly incomplete | spec §5.1, §8 | unclosed eval-run probe | FAIL | `INT-003` |
| Tool propagation `exact` means byte equality | handoff §7.4; spec §8.2 | non-text MCP result probe | FAIL | `CORR-001` |
| Incomplete SSE does not become a completed tool claim | spec §2.3, §8 | incomplete content-block probe | FAIL | `CORR-002` |
| Weak/ambiguous identity is not upgraded to proven causality | spec §8.1–8.2 | mismatched-argument and graph-basis probes | FAIL | `CORR-003` |
| Projection IDs are deterministic and unique from canonical coordinates | spec §8.1 | all four real captures contain duplicate MCP call IDs | FAIL | `PROJ-001` |
| Root discovery fails loudly on duplicate IDs | spec §8.1 | root duplicate passes; pinned/root duplicate does not | PARTIAL | `WEB-002` |
| Finalized tampering is visible and detail remains blocked | spec §8 | detail re-hash tests pass; cached integrity badge can remain valid | FAIL | `WEB-001` |
| Paths are traversal- and symlink-safe | isolation I6 | normal traversal tests pass; parent/manifest symlink probes fail | FAIL | `INT-004`, `WEB-003` |
| Browser is loopback-only, capability-gated, reveal-gated, CSP-safe | spec §8.2 | web suite, taint probe, tagged browser, history probe | PARTIAL | `SEC-001` |
| One-time launch token leaves current URL/history | isolation I6 | Playwright history probe | PASS | — |
| Pre-reveal APIs omit prompt/result/thinking/tool body taint | spec §8.1 | five-class synthetic taint probe | PASS | — |
| Real Cards/Flow/Split acceptance for all four captures | handoff acceptance table | tagged Playwright against real corpus | PASS | — |
| Facade is compiler-private, cold, and CLI-composed only | architecture spec; isolation I1–I4 | AST guards, mutations, cold runtime control | PASS | — |
| Normal MCP does not reach Inspector | isolation I4 | dependency closure and exact transcript | PASS | — |
| Capture-disabled eval behavior is unchanged | non-interference gate | base/head HOME differential and filesystem probe | FAIL | `ISO-001`, `ISO-002` |
| Native tests, race, vet, JS syntax, fast lint | repo gates | retained logs | PASS | — |
| Full repository lint | CI workflow | base 0 issues; head 74 issues | FAIL | `CI-002` |
| Linux, Darwin, and Windows builds | CI workflow | Linux/Darwin pass; Windows head fails while base passes | FAIL | `CI-001` |
| Live credential/provider operation | operator policy | credentials absent; intentionally not attempted | NOT RUN | accepted audit limitation, not ship evidence |
| Large 250 MB/1 GB and decompression-bomb bounds | audit plan | no destructive/high-memory run | NOT RUN | remains unmeasured; final verdict is already NO-GO |

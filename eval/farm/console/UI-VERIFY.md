# Farm console UI verification

This runbook verifies the real Go handlers and templates against deterministic,
in-memory farm data. The fixture has no production route, platform credential,
observer process, Worker, paid model call, or external service access.

## Start the fixture

From the repository root, run:

```sh
ZCP_FARM_UI_FIXTURE=1 go test ./internal/eval/farm/console \
  -run '^TestUIFixtureServer$' -count=1 -timeout=0 -v
```

The test first checks every manifest entry through the actual handler. It then
opens seven loopback listeners on dynamically allocated ports and prints a JSON
route manifest. Open the `available` variant's `/login` URL and enter the
`local-only test token` printed immediately above the manifest. The session
cookie is host-scoped, so that login applies to the other `localhost` ports.
Stop the test process to close every listener and release the held fake jobs.

The fixture clock is fixed at `2026-09-11T12:00:00Z`. Its store is thread-safe.
Three fake assessments remain running behind a channel, a fourth remains
queued, and one completed attempt records a deterministic pre-store failure.
The login-delay seam records calls behind a mutex and never pauses a UI pass.

## State inventory

The JSON manifest is the exact machine-readable inventory for that invocation;
its URLs include the allocated ports. These are the stable state groups and
paths behind it.

| Surface | Meaningful fixture states and paths |
|---|---|
| Login | initial `/login`; rejected `/login?error=1&next=/r/ui-current-deploy`; safe return path on the same deep link; expired-session redirect is asserted by the test |
| Shell | assessment on with four queued/running jobs on `/?notice=queued&n=4`; `observer-off` `/`; `observer-unavailable` `/r/ui-states-not-assessed`; queued, busy and not-finished notices |
| Overview | latest plus previous `/`; Evaluation, Empty and All history through `kind`; unknown cost sorted ascending; no match with `kind=empty&since=1h`; no evaluation in `empty-source` `/`; completed 1/1 coverage with no problems in `clean-source` `/` |
| Problems | all statuses `/problems?status=all&since=90d`; one route for each of `new`, `first-seen`, `recurring`, `still-emitted`, `gone`, and `unconfirmed`; unavailable newest-build coverage; 24-hour scope with recurring status derived from older full history; expanded members; combined filter/sort; no match; unassessed-only in `unassessed-source`; failed/unparsed evidence warning; no source in `empty-source` |
| Findings | verified and unverified quotes, their literal quote text, and run/finding/step/fix links `/findings?since=90d`; one combined cause/severity/surface/scenario/batch/build/window/sort route; no match; no source in `empty-source` |
| Batch | all five precedence group headings asserted together on `/b/ui-states`; no previous `/b/ui-previous`; filtered runs; no, partial (`2/9`, including absent and failed attempts) and full assessment coverage; zero and unknown cost; held whole-batch action; no eligible assessment `/b/ui-empty` |
| Run verdict | passed `ui-states-clean`; failed `ui-states-failed`; blocked `ui-states-blocked`; not started `ui-states-not-started`; running `ui-live-running-a`; stalled `ui-states-stalled` |
| Run assessment | absent, successful clean with explicit agreeing checks and no Disputed marker, an independent disputed state, inconclusive with goal not reached, error, unparsed raw output, queued, running and pre-store failure; failed-current with older-success fallback; explicit historical assessment |
| Run evidence | partial record `ui-partial-record`; long transcript and preserved JSON lexemes `ui-current-deploy`; cited and error step filters; zero and unknown cost; disputed check; missing record on the never-started run |
| Terms | definitions and stable section navigation `/terms`; follow links from status help and return with browser history |
| HTML errors | invalid filter 400; missing route/batch/run/assessment 404; Overview store failure 500; Problems, Findings, Batch and Run store failures 502, each on the `store-error` listener |

The automated test also proves the rejected login retains its safe `next`,
renders an associated inline error without echoing the token, sets no cookie,
and that an expired signed session returns to login. Every manifest GET asserts
its HTTP status, a state-specific public phrase, and the CSP header.

## Browser matrix

Record each cell as `PASS`, `FAIL: <defect>`, `BLOCKED: <reason>`, or `NOT RUN`.
Do not turn a fixture result into a deployed-console claim.

| Pass | Width/theme/zoom | Required observations | Result |
|---|---|---|---|
| Route families | 1440 px, light, 100% | Login, Overview, Problems, Findings, Batch, Run, Terms and HTML error variants; one H1; value before record; no unexplained blanks | NOT RUN |
| Narrow | 400 px, light, 100% | Navigation, filter controls, cards, tables, queue details, error actions and long identifiers remain usable without document-wide overflow | NOT RUN |
| Dense | 768 px, light, 100% | `/problems?status=all&since=90d`, `/b/ui-states`, and the long run keep readable hierarchy and aligned numeric data | NOT RUN |
| Dark | 1440 px and 400 px, dark | Representative Overview, Problems, Batch, Run, Login and error pages retain contrast, focus and written status meaning | NOT RUN |
| Zoom | 1440 px at 200% | No clipped controls or hidden content; wide table/code regions scroll locally and remain keyboard focusable | NOT RUN |
| Keyboard | 1440 px, light | Skip link, sidebar, filters, native disclosures, table/code scroll regions, action controls and sign-out have visible focus and logical order | NOT RUN |
| Long/disclosure | 400 px and 1440 px | `ui-current-deploy` long steps wrap; empty thinking stays absent; Findings/Problems/Record disclosures work without JavaScript | NOT RUN |
| Deep link | 1440 px, light | Problem → representative finding → cited step → finding return; filtered cited-step link clears only the blocking step filter; historical assessment returns to current | NOT RUN |

For the rejected-login view, open the manifest's `login-rejected` URL. Also
submit any wrong value once and confirm the redirect reaches the same state,
the token field is empty, its error is announced, and the deep return path is
retained. Do not paste a real credential into the fixture.

For native disclosure and keyboard checks, use Tab/Shift+Tab and Enter/Space.
For local overflow checks, focus each labelled wide table or `<pre>` region and
scroll it horizontally; the page itself must not acquire horizontal overflow.
At 200% zoom, verify the effective content width as well as text size.

## Result ledger

| Layer | Command or observation | Result |
|---|---|---|
| Actual-handler state matrix | `go test ./internal/eval/farm/console -run '^TestUIFixtureServer$' -short -count=1 -v` | PASS (2026-09-12) |
| Console package | `go test ./internal/eval/farm/console -count=1` | PASS (2026-09-12) |
| Race | `go test -race ./internal/eval/farm/console -run '^TestUIFixtureServer$' -short -count=1` | PASS (2026-09-12) |
| Full lint | `/tmp/zcp-tools/golangci-lint run ./...` | PASS, 0 issues (2026-09-12) |
| Browser fixture matrix | Matrix above, using the emitted dynamic URLs | NOT RUN |
| Deployed revision | Repeat critical auth/routes/warm reads against the named internal console | NOT RUN |

Keep failures and blockers as their own rows. A later successful rerun may add
a new dated row; it must not erase the earlier evidence.

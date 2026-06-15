package recipe

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

// validators_worker_subscription.go — Run-22 R2-WK-1 + R2-WK-2 closure.
//
// Showcase-tier worker codebases ship at minContainers≥2 from tier 4
// onwards. A NATS subscription without `{ queue: '<group>' }` fans
// out every message to every replica → double-indexing, double-LPUSH,
// broken ordering. A shutdown handler that calls `unsubscribe()`
// instead of `await sub.drain()` drops in-flight events on every
// rolling deploy.
//
// `briefs/feature/worker_subscription_shape.md` teaches the source-code
// contract at feature (where worker source is authored) and
// `briefs/codebase-content/worker_kb_supplements.md` teaches the KB
// content shape that explains the same trap to porter readers. This
// gate is the engine-side teeth that catches violations even when the
// agent skips both atoms — the split landed in run-22 followup F-5
// after evidence that the previous combined `showcase_tier_supplements.md`
// arrived one phase too late (codebase-content, after worker source was
// already authored).
//
// Regex-based source scan (per FIX_SPEC R2-WK-* "Start with a regex
// implementation that catches the common shapes"). Upgrade to AST
// parsing only if the regex misses real-world cases.

// workerSubscriptionScanExts — TS/JS file extensions worth scanning
// for the worker patterns. Worker recipes ship NestJS / plain Node;
// keep the set tight so file walks stay fast.
var workerSubscriptionScanExts = map[string]bool{
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".mjs": true,
	".cjs": true,
}

// workerSubscriptionSkipDirs — vendored / build-output trees that
// would otherwise pollute the scan with library code.
var workerSubscriptionSkipDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	"build":        true,
	".next":        true,
	".svelte-kit":  true,
	".git":         true,
}

// natsSubscribeRE matches a NATS subscribe call (`<expr>.subscribe(`).
// The full subscribe-call substring is captured up to the matching
// close paren; the queue-option check inspects whether `queue:` (or
// `'queue'` / `"queue"`) appears inside the captured argument list.
//
// Naming pattern follows the published `nats.js` API; we look for
// `.subscribe(` rather than only `nc.subscribe(` so plain object-
// destructured subscribers (e.g. `const { subscribe } = nc; ...
// subscribe(SUBJECT)`) and class-field subscribers (`this.sub =
// this.nc.subscribe(...)`) all match.
var natsSubscribeRE = regexp.MustCompile(`\.subscribe\s*\(`)

// queueOptionRE matches the queue option inside a subscribe call
// argument list. Tolerates minor formatting variation: `queue:`,
// `"queue":`, `'queue':`, with surrounding whitespace.
var queueOptionRE = regexp.MustCompile(`(?:^|[\s,{])(?:["']?queue["']?\s*:)`)

// onModuleDestroyRE marks the start of an `onModuleDestroy(...)`
// method body (NestJS lifecycle hook). Matches the canonical
// `async onModuleDestroy()`, `onModuleDestroy()`, plus `OnModuleDestroy`
// case variants the framework allows.
var onModuleDestroyRE = regexp.MustCompile(`(?i)\b(?:async\s+)?onModuleDestroy\s*\(`)

// sigtermHandlerRE marks the start of a SIGTERM handler block.
// Matches `process.on('SIGTERM', ...)` / `process.on("SIGTERM", ...)`.
var sigtermHandlerRE = regexp.MustCompile(`process\.on\s*\(\s*['"]SIGTERM['"]`)

// unsubscribeCallRE matches `<expr>.unsubscribe(` — the wrong shutdown
// call. Detected only inside an onModuleDestroy / SIGTERM-handler
// block.
var unsubscribeCallRE = regexp.MustCompile(`\.unsubscribe\s*\(`)

// drainCallRE matches `<expr>.drain(` — the right shutdown call. A
// drain inside the same block as an unsubscribe call accepts the
// pattern (caller is intentionally unsubscribing-then-draining-other-
// resources); only naked unsubscribe-without-drain refuses.
var drainCallRE = regexp.MustCompile(`\.drain\s*\(`)

// scanWorkerSubscriptionsAt walks `root` and returns one Violation
// per worker-pattern violation found. Two violation codes:
//
//   - worker-subscribe-missing-queue-option — `nc.subscribe(SUBJECT)`
//     without a `{ queue: ... }` option.
//   - worker-shutdown-uses-unsubscribe — `unsubscribe()` inside an
//     onModuleDestroy / SIGTERM handler block without a corresponding
//     `drain()` call in the same block.
//
// Missing root → empty result, no error (caller gates on stat). The
// scan is best-effort — unreadable files / bad encoding skip silently
// rather than failing the whole walk.
func scanWorkerSubscriptionsAt(root string) ([]Violation, error) {
	var vs []Violation
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr
		}
		if d.IsDir() {
			if workerSubscriptionSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !workerSubscriptionScanExts[ext] {
			return nil
		}
		body, rerr := readFileCapped(path, 512*1024)
		if rerr != nil {
			return nil //nolint:nilerr
		}
		vs = append(vs, scanWorkerSubscriptionFile(path, body)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return vs, nil
}

// scanWorkerSubscriptionFile applies both pattern checks to a single
// file body, returning one violation per hit.
func scanWorkerSubscriptionFile(path, body string) []Violation {
	subVs := scanNATSSubscribeQueueOption(path, body)
	shutdownVs := scanShutdownHandlerUnsubscribe(path, body)
	out := make([]Violation, 0, len(subVs)+len(shutdownVs))
	out = append(out, subVs...)
	out = append(out, shutdownVs...)
	return out
}

// scanNATSSubscribeQueueOption finds every `.subscribe(...)` call and
// checks the captured argument list for the queue option. A subscribe
// with no queue option in any reachable position is a violation.
func scanNATSSubscribeQueueOption(path, body string) []Violation {
	var vs []Violation
	for _, idx := range natsSubscribeRE.FindAllStringIndex(body, -1) {
		// idx[1] is one past the opening `(` of the subscribe call.
		args, ok := captureBalancedParens(body, idx[1]-1)
		if !ok {
			// Unmatched paren — likely truncation or odd source. Don't
			// spuriously flag; the AST upgrade path catches this case.
			continue
		}
		if queueOptionRE.MatchString(args) {
			continue
		}
		// Skip benign `subscribe` shapes that don't refer to NATS:
		// rxjs `.subscribe(callback)` (Observable), EventEmitter
		// `.subscribe(handler)`. The strongest signal is the file
		// also referencing nats-typed identifiers — be conservative
		// and only flag when the file imports from a nats-shaped
		// module OR uses `NatsConnection` / `Subscription` types.
		if !fileLooksLikeNATS(body) {
			continue
		}
		line := lineNumber(body, idx[0])
		vs = append(vs, Violation{
			Code:     "worker-subscribe-missing-queue-option",
			Path:     fmt.Sprintf("%s:%d", path, line),
			Message:  "NATS `.subscribe(...)` call missing `{ queue: '<group>' }` option — at tier 4-5 (`minContainers: 2`) every replica receives every message → duplicated work. Pass a queue group: `nc.subscribe(SUBJECT, { queue: 'workers' })`.",
			Severity: SeverityBlocking,
		})
	}
	return vs
}

// scanShutdownHandlerUnsubscribe walks each onModuleDestroy / SIGTERM
// handler block and checks for `unsubscribe()` calls. Reports a
// violation when the block contains an unsubscribe but no drain call
// — the caller intended a graceful shutdown but used the wrong API.
func scanShutdownHandlerUnsubscribe(path, body string) []Violation {
	var vs []Violation
	starts := append([]int{}, findAll(onModuleDestroyRE, body)...)
	starts = append(starts, findAll(sigtermHandlerRE, body)...)
	for _, start := range starts {
		// Locate the opening `{` after the matched signature.
		openBrace := strings.Index(body[start:], "{")
		if openBrace < 0 {
			continue
		}
		blockStart := start + openBrace
		block, ok := captureBalancedBraces(body, blockStart)
		if !ok {
			continue
		}
		if !unsubscribeCallRE.MatchString(block) {
			continue
		}
		// drain() in the same block accepts the pattern — caller is
		// intentionally combining unsubscribe (other resources) with
		// drain (NATS sub). Strict refusal is naked-unsubscribe-only.
		if drainCallRE.MatchString(block) {
			// Block contains both: still flag the unsubscribe so the
			// agent removes it — drain alone is the contract. But
			// downgrade to notice (caller likely already drains and
			// just needs to remove the unsubscribe call).
			line := lineNumber(body, start)
			vs = append(vs, Violation{
				Code:     "worker-shutdown-uses-unsubscribe",
				Path:     fmt.Sprintf("%s:%d", path, line),
				Message:  "shutdown handler calls `unsubscribe()` alongside `drain()` — drop the unsubscribe call; `drain()` already stops receiving and finishes the iterator. `unsubscribe()` abandons in-flight messages on rolling deploy.",
				Severity: SeverityNotice,
			})
			continue
		}
		line := lineNumber(body, start)
		vs = append(vs, Violation{
			Code:     "worker-shutdown-uses-unsubscribe",
			Path:     fmt.Sprintf("%s:%d", path, line),
			Message:  "shutdown handler calls `unsubscribe()` instead of `await sub.drain()` — `unsubscribe()` drops in-flight messages on rolling deploys (tier 4-5). Use `await this.sub.drain(); await this.nc.drain();` so the worker finishes pending work before exiting.",
			Severity: SeverityBlocking,
		})
	}
	return vs
}

// fileLooksLikeNATS returns true when the file body shows a strong
// signal that the `subscribe` calls are NATS-typed. Conservative
// guardrail to avoid flagging rxjs / EventEmitter `.subscribe(...)`.
func fileLooksLikeNATS(body string) bool {
	// Direct import / require from nats library.
	if strings.Contains(body, "from 'nats'") || strings.Contains(body, `from "nats"`) {
		return true
	}
	if strings.Contains(body, "require('nats')") || strings.Contains(body, `require("nats")`) {
		return true
	}
	// Type identifiers from the nats library.
	if strings.Contains(body, "NatsConnection") {
		return true
	}
	// Subject naming convention (fact-attestation reaches here too).
	if strings.Contains(body, "nc.subscribe") || strings.Contains(body, "nats.subscribe") {
		return true
	}
	return false
}

// captureBalancedParens returns the substring between the `(` at
// `openIdx` and its matching `)`, plus a bool indicating whether a
// matching paren was found. Tolerates nested parens / parens inside
// strings (string awareness is light — single + double quote tracked).
func captureBalancedParens(s string, openIdx int) (string, bool) {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '(' {
		return "", false
	}
	depth := 0
	inSingle := false
	inDouble := false
	inBacktick := false
	start := openIdx + 1
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		// Skip escaped chars inside strings.
		if (inSingle || inDouble || inBacktick) && c == '\\' && i+1 < len(s) {
			i++
			continue
		}
		switch c {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case '(':
			if !inSingle && !inDouble && !inBacktick {
				depth++
			}
		case ')':
			if !inSingle && !inDouble && !inBacktick {
				depth--
				if depth == 0 {
					return s[start:i], true
				}
			}
		}
	}
	return "", false
}

// captureBalancedBraces returns the substring between the `{` at
// `openIdx` and its matching `}`, plus a bool indicating whether a
// matching brace was found. Same string-awareness as
// captureBalancedParens.
func captureBalancedBraces(s string, openIdx int) (string, bool) {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '{' {
		return "", false
	}
	depth := 0
	inSingle := false
	inDouble := false
	inBacktick := false
	start := openIdx + 1
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		if (inSingle || inDouble || inBacktick) && c == '\\' && i+1 < len(s) {
			i++
			continue
		}
		switch c {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case '{':
			if !inSingle && !inDouble && !inBacktick {
				depth++
			}
		case '}':
			if !inSingle && !inDouble && !inBacktick {
				depth--
				if depth == 0 {
					return s[start:i], true
				}
			}
		}
	}
	return "", false
}

// findAll returns the start byte offsets of every match of `re`
// against `body`.
func findAll(re *regexp.Regexp, body string) []int {
	matches := re.FindAllStringIndex(body, -1)
	out := make([]int, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[0])
	}
	return out
}

// lineNumber returns the 1-based line number that contains byte offset
// `pos` in `body`.
func lineNumber(body string, pos int) int {
	if pos < 0 {
		return 1
	}
	if pos > len(body) {
		pos = len(body)
	}
	return strings.Count(body[:pos], "\n") + 1
}

// gateWorkerSubscription runs the worker-subscription scan against
// every showcase-tier worker codebase in the plan. Skips:
//
//   - Plans that aren't showcase tier (the multi-replica failure
//     modes are vacuous on tiers that don't set minContainers≥2).
//   - Codebases that aren't workers (`!cb.IsWorker`).
//   - Worker codebases that share a codebase with another role
//     (cb.SharesCodebaseWith != ""): the broker subscription, if
//     any, lives in the shared service and the scaffold-time
//     scaffolding for it lives in the parent role's tree.
//   - Codebases without a SourceRoot, or with an unreachable
//     SourceRoot (pre-scaffold / chain-parent codebases).
func gateWorkerSubscription(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	if ctx.Plan.Tier != tierShowcase {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if !cb.IsWorker {
			continue
		}
		if cb.SharesCodebaseWith != "" {
			continue
		}
		if cb.SourceRoot == "" {
			continue
		}
		vs, err := scanWorkerSubscriptionsAt(cb.SourceRoot)
		if err != nil {
			out = append(out, Violation{
				Code:    "worker-subscription-scan-failed",
				Path:    cb.SourceRoot,
				Message: err.Error(),
			})
			continue
		}
		out = append(out, vs...)
	}
	return out
}

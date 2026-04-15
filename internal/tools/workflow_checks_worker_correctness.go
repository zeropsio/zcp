// Tests for: internal/tools/workflow_checks_worker_correctness.go —
// production-correctness rules for worker codebases.
package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkWorkerProductionCorrectness enforces that worker README.md
// (or CLAUDE.md, whichever the build chose) carries gotchas covering
// the two production-correctness concerns that bite every scaled
// worker deployment on Zerops:
//
//  1. Queue-group semantics under horizontal scaling — when
//     minContainers > 1, a NATS (or Kafka, or any broker) consumer
//     that doesn't specify a queue group processes every message N
//     times, one per replica. v16's nestjs-showcase shipped without
//     this gotcha in the workerdev README; only the feature
//     subagent's code review noticed "queue: 'worker'" was missing
//     from the StatusPanel, and even then the README never mentioned
//     why that option was needed. A reader deploying a fresh worker
//     to Zerops with minContainers:2 will run every background job
//     twice and never know why their database is full of duplicates.
//
//  2. Graceful shutdown on SIGTERM — Zerops sends SIGTERM to the
//     container during rolling deploys. A consumer that doesn't
//     drain in-flight messages before exit loses work. The drain
//     pattern (nc.drain() on SIGTERM, wait for completion, exit)
//     is fundamental to long-running broker consumers and applies
//     to every worker on Zerops regardless of framework.
//
// The rule: for every target with IsWorker=true, at least one
// gotcha stem in the worker README's knowledge-base fragment must
// normalize-match the queue-group topic, and at least one must
// normalize-match the graceful-shutdown topic. The match uses the
// same token-set intersection as the predecessor-floor check, so
// lightly-reworded phrasings ("NATS queue group mandatory for HA"
// vs "Without a queue group, workers double-process under scale")
// still collide.
//
// Shared-codebase workers (SharesCodebaseWith != "") skip this check
// — their operational knowledge lives in the host target's README.
// The caller filters these out before calling.
func checkWorkerProductionCorrectness(hostname string, readmeContent string, target workflow.RecipeTarget) []workflow.StepCheck {
	if !target.IsWorker {
		return nil
	}
	if target.SharesCodebaseWith != "" {
		return nil
	}
	kb := extractFragmentContent(readmeContent, "knowledge-base")
	if kb == "" {
		// Fragment-missing error surfaces via checkReadmeFragments —
		// don't duplicate it from here.
		return nil
	}
	stems := workflow.ExtractGotchaStems(kb)
	if len(stems) == 0 {
		return nil
	}

	bodies := workerGotchaBodies(kb)
	haveQueueGroup := false
	haveShutdown := false
	for i, stem := range stems {
		body := ""
		if i < len(bodies) {
			body = bodies[i]
		}
		combined := strings.ToLower(stem + " " + body)
		if matchesQueueGroupTopic(combined) {
			haveQueueGroup = true
		}
		if matchesShutdownTopic(combined) {
			haveShutdown = true
		}
	}

	var checks []workflow.StepCheck
	if !haveQueueGroup {
		checks = append(checks, workflow.StepCheck{
			Name:   hostname + "_worker_queue_group_gotcha",
			Status: statusFail,
			Detail: fmt.Sprintf(
				"worker %q README has no gotcha covering queue-group semantics under horizontal scaling. Under Zerops `minContainers > 1`, a broker consumer that doesn't set a queue group (NATS `queue: 'workers'`, Kafka consumer group, etc.) processes every message ONCE PER REPLICA — so a 2-container worker runs every job twice. This is a production-correctness bug that only manifests after the first scale-out, and users cannot discover it from the scaffold alone. Add a gotcha describing the trap (stem naming the broker + 'queue group' or 'consumer group' + 'minContainers' / 'horizontal' / 'double-process') with the exact client-library option that sets it. The `queue: 'workers'` option in NestJS's createMicroservice, the `GroupID` in sarama / confluent-kafka, etc.",
				hostname,
			),
		})
	}
	if !haveShutdown {
		checks = append(checks, workflow.StepCheck{
			Name:   hostname + "_worker_shutdown_gotcha",
			Status: statusFail,
			Detail: fmt.Sprintf(
				"worker %q README has no gotcha covering graceful shutdown on SIGTERM. Zerops sends SIGTERM to running containers during rolling deploys; a broker consumer that exits on SIGTERM without draining in-flight messages acks the batch, crashes, and loses the work. This is a production-correctness bug that only manifests during deploys. Add a gotcha covering the trap (stem naming 'SIGTERM' or 'graceful shutdown' or 'in-flight' or 'drain') with the concrete call sequence: catch SIGTERM, call `nc.drain()` / equivalent, await, then `process.exit(0)`. For NestJS microservice workers this is `app.close()` plus the underlying transport's drain.",
				hostname,
			),
		})
	}
	if len(checks) == 0 {
		return []workflow.StepCheck{{
			Name:   hostname + "_worker_production_correctness",
			Status: statusPass,
		}}
	}
	return checks
}

// checkWorkerDrainCodeBlock enforces that a separate-codebase worker
// README carries a fenced code block showing the SIGTERM → drain → exit
// call sequence. v7's worker README had this as IG #3 with a full
// typescript diff; v18's worker README shipped the drain topic only as
// prose inside a gotcha — correct content but no copy-paste reference.
//
// The existing `worker_shutdown_gotcha` check verifies the topic is
// mentioned anywhere in the knowledge-base fragment. This complementary
// check verifies the topic has concrete reference code: a fenced block
// somewhere in the worker README (either the integration-guide or the
// knowledge-base fragment) must contain a drain call AND a process/app
// exit call, proving the reader has a copy-pasteable implementation.
//
// Detection is intentionally loose on language — drain sequences show
// up as typescript in Node workers, go in Go workers, python in Celery
// workers. The signal is: "a fenced block mentions draining and
// exiting in the same block".
func checkWorkerDrainCodeBlock(hostname string, readmeContent string, target workflow.RecipeTarget) []workflow.StepCheck {
	if !target.IsWorker {
		return nil
	}
	if target.SharesCodebaseWith != "" {
		return nil
	}
	// Collect all fenced-block bodies from the whole README (both
	// IG and knowledge-base fragments live inside it).
	blocks := extractFencedBlockBodies(readmeContent)
	for _, b := range blocks {
		lower := strings.ToLower(b)
		if !containsDrainCall(lower) {
			continue
		}
		if !containsExitCall(lower) {
			continue
		}
		return []workflow.StepCheck{{
			Name:   hostname + "_drain_code_block",
			Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_drain_code_block",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"worker %q README has a drain-topic gotcha but no fenced code block showing the actual SIGTERM → drain → exit call sequence. Add an integration-guide step (e.g. \"### N. Drain on SIGTERM\") with a copy-pasteable code block: process.on('SIGTERM', stop); stop() { await nc.drain(); await dataSource.destroy(); process.exit(0); } — the exact call sequence the reader needs. The existing shutdown gotcha tells them to drain; this check ensures they have reference code to lift.",
			hostname,
		),
	}}
}

// drainCallTokens identify a drain or close call inside a fenced
// block — "drain(" covers nc.drain()/q.drain()/consumer.drain(),
// "app.close" covers NestJS microservice shutdown, "graceful" catches
// Go-style graceful shutdown helpers.
var drainCallTokens = []string{
	"drain(",
	".drain ",
	"app.close(",
	"server.close(",
	"graceful shutdown",
	"gracefulshutdown",
}

// exitCallTokens identify a process/app exit call inside the same
// fenced block — proves the block shows the call sequence, not just
// the drain.
var exitCallTokens = []string{
	"process.exit",
	"os.exit",
	"exit(0)",
	"exit(1)",
	"return nil", // go-style: `if err := drain(); err != nil { ... } return nil`
}

func containsDrainCall(lowerBlock string) bool {
	for _, token := range drainCallTokens {
		if strings.Contains(lowerBlock, token) {
			return true
		}
	}
	return false
}

func containsExitCall(lowerBlock string) bool {
	for _, token := range exitCallTokens {
		if strings.Contains(lowerBlock, token) {
			return true
		}
	}
	return false
}

// extractFencedBlockBodies returns the body text of every fenced code
// block in the content. Used by the drain-code-block check to walk
// every block looking for a call sequence.
func extractFencedBlockBodies(content string) []string {
	var out []string
	rest := content
	for {
		start := strings.Index(rest, "```")
		if start < 0 {
			return out
		}
		// Skip past the opening fence line (lang tag included).
		lineEnd := strings.Index(rest[start:], "\n")
		if lineEnd < 0 {
			return out
		}
		bodyStart := start + lineEnd + 1
		end := strings.Index(rest[bodyStart:], "```")
		if end < 0 {
			return out
		}
		out = append(out, rest[bodyStart:bodyStart+end])
		rest = rest[bodyStart+end+3:]
	}
}

// workerGotchaBodies extracts the body text for each gotcha bullet
// in the knowledge-base fragment. Mirrors the stem extraction shape
// used by the predecessor-floor check so the two walk the same
// content in the same order.
func workerGotchaBodies(kb string) []string {
	entries := workflow.ExtractGotchaEntries(kb)
	bodies := make([]string, 0, len(entries))
	for _, e := range entries {
		bodies = append(bodies, e.Body)
	}
	return bodies
}

// queueGroupTopicTokens are the substrings whose presence in a
// normalized gotcha signals coverage of queue-group semantics.
// Match is "any of these tokens in the combined stem+body", which
// tolerates the many phrasings the topic shows up in ("queue group",
// "consumer group", "GroupID", "double-process", "exactly once",
// "minContainers").
var queueGroupTopicTokens = []string{
	"queue group",
	"queuegroup",
	"queue: '",
	"queue: \"",
	"consumer group",
	"consumergroup",
	"groupid",
	"group id",
	"double-process",
	"double process",
	"processed twice",
	"duplicate processing",
	"exactly once",
	"at-most-once",
	"at most once",
	"fan out",
	"fan-out",
	"replicated subscription",
	"every replica",
	"per replica",
	"load-balance messages",
	"load balance messages",
	"mincontainers",
}

func matchesQueueGroupTopic(combined string) bool {
	for _, token := range queueGroupTopicTokens {
		if strings.Contains(combined, token) {
			return true
		}
	}
	return false
}

// shutdownTopicTokens are the substrings whose presence in a
// normalized gotcha signals coverage of graceful shutdown on SIGTERM.
var shutdownTopicTokens = []string{
	"sigterm",
	"graceful shutdown",
	"graceful-shutdown",
	"in-flight",
	"in flight",
	"nc.drain",
	".drain(",
	"drain the subscription",
	"drain subscription",
	"drain in-flight",
	"stop signal",
	"shutdown hook",
	"lose messages",
	"losing messages",
	"lose the work",
	"rolling deploy",
	"rolling restart",
}

func matchesShutdownTopic(combined string) bool {
	for _, token := range shutdownTopicTokens {
		if strings.Contains(combined, token) {
			return true
		}
	}
	return false
}

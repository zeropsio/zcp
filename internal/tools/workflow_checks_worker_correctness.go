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

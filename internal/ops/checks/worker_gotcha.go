package checks

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

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

// shutdownTopicTokens are the substrings whose presence in a normalized
// gotcha signals coverage of graceful shutdown on SIGTERM.
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

// matchesQueueGroupTopic returns true when `combined` (stem+body,
// lowercased) contains any queue-group topic token.
func matchesQueueGroupTopic(combined string) bool {
	for _, token := range queueGroupTopicTokens {
		if strings.Contains(combined, token) {
			return true
		}
	}
	return false
}

// matchesShutdownTopic mirrors matchesQueueGroupTopic for shutdown
// topic coverage.
func matchesShutdownTopic(combined string) bool {
	for _, token := range shutdownTopicTokens {
		if strings.Contains(combined, token) {
			return true
		}
	}
	return false
}

// workerGotchaCombinedLines walks the knowledge-base fragment and
// returns each `<stem> <body>` (lowercased) concatenation — one per
// gotcha bullet. Returns nil when the fragment is absent OR when no
// bullets are present; both are upstream concerns handled by fragment
// + kb-presence checks.
func workerGotchaCombinedLines(readmeContent string) []string {
	kb := extractFragmentContent(readmeContent, "knowledge-base")
	if kb == "" {
		return nil
	}
	entries := workflow.ExtractGotchaEntries(kb)
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, strings.ToLower(e.Stem+" "+e.Body))
	}
	return out
}

// CheckWorkerQueueGroupGotcha fails when a separate-codebase worker's
// README has no gotcha covering queue-group semantics under
// `minContainers > 1`. Whenever a worker runs more than one replica —
// for throughput scaling OR for HA / rolling-deploy availability — a
// broker consumer that doesn't set a queue group processes every message
// once per replica; a 2-container worker runs every job twice.
//
// Returns nil (no row) when the target is not a worker, is a
// shared-codebase worker (operational knowledge lives in the host
// target's README), or when the knowledge-base fragment is absent /
// has no bullets — those are upstream surfaces.
func CheckWorkerQueueGroupGotcha(_ context.Context, hostname, readmeContent string, target workflow.RecipeTarget) []workflow.StepCheck {
	if !target.IsWorker || target.SharesCodebaseWith != "" {
		return nil
	}
	lines := workerGotchaCombinedLines(readmeContent)
	if lines == nil {
		return nil
	}
	if slices.ContainsFunc(lines, matchesQueueGroupTopic) {
		return []workflow.StepCheck{{
			Name:   hostname + "_worker_queue_group_gotcha",
			Status: StatusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_worker_queue_group_gotcha",
		Status: StatusFail,
		Detail: fmt.Sprintf(
			"worker %q README has no gotcha covering queue-group semantics under `minContainers > 1`. Whenever a worker runs more than one replica — whether the replicas exist for throughput scaling or for HA / rolling-deploy availability — a broker consumer that doesn't set a queue group (NATS `queue: 'workers'`, Kafka consumer group, etc.) processes every message ONCE PER REPLICA, so a 2-container worker runs every job twice. This is a production-correctness bug that only manifests after the first scale-out, and users cannot discover it from the scaffold alone. Add a gotcha describing the trap (stem naming the broker + 'queue group' or 'consumer group' + 'minContainers' / 'per replica' / 'double-process') with the exact client-library option that sets it. The `queue: 'workers'` option in NestJS's createMicroservice, the `GroupID` in sarama / confluent-kafka, etc.",
			hostname,
		),
	}}
}

// CheckWorkerShutdownGotcha fails when a separate-codebase worker's
// README has no gotcha covering graceful shutdown on SIGTERM. Zerops
// sends SIGTERM to running containers during rolling deploys; a broker
// consumer that exits on SIGTERM without draining in-flight messages
// acks the batch, crashes, and loses the work.
//
// Returns nil when the target is not a worker or shares codebase; or
// when the knowledge-base fragment is absent / empty (upstream
// surfaces).
func CheckWorkerShutdownGotcha(_ context.Context, hostname, readmeContent string, target workflow.RecipeTarget) []workflow.StepCheck {
	if !target.IsWorker || target.SharesCodebaseWith != "" {
		return nil
	}
	lines := workerGotchaCombinedLines(readmeContent)
	if lines == nil {
		return nil
	}
	if slices.ContainsFunc(lines, matchesShutdownTopic) {
		return []workflow.StepCheck{{
			Name:   hostname + "_worker_shutdown_gotcha",
			Status: StatusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_worker_shutdown_gotcha",
		Status: StatusFail,
		Detail: fmt.Sprintf(
			"worker %q README has no gotcha covering graceful shutdown on SIGTERM. Zerops sends SIGTERM to running containers during rolling deploys; a broker consumer that exits on SIGTERM without draining in-flight messages acks the batch, crashes, and loses the work. This is a production-correctness bug that only manifests during deploys. Add a gotcha covering the trap (stem naming 'SIGTERM' or 'graceful shutdown' or 'in-flight' or 'drain') with the concrete call sequence: catch SIGTERM, call `nc.drain()` / equivalent, await, then `process.exit(0)`. For NestJS microservice workers this is `app.close()` plus the underlying transport's drain.",
			hostname,
		),
	}}
}

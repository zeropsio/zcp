package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// Run-22 R2-WK-1 + R2-WK-2 — worker subscription gate tests.

// TestWorkerSubscriptionGate_RefusesNakedSubscribe — `nc.subscribe(...)`
// without a `{ queue: ... }` option is the run-22 dogfood bug:
// `this.nc.subscribe(ITEMS_EVENT_SUBJECT)` at tier 4-5 fans out every
// message to every replica → double-indexing.
func TestWorkerSubscriptionGate_RefusesNakedSubscribe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/worker.service.ts", `
import { connect, type NatsConnection, type Subscription } from 'nats';

export class WorkerService {
  private nc!: NatsConnection;
  private sub!: Subscription;

  async onModuleInit() {
    this.nc = await connect({ servers: 'broker:4222' });
    this.sub = this.nc.subscribe('items.events');
  }
}
`)
	vs, err := scanWorkerSubscriptionsAt(dir)
	if err != nil {
		t.Fatalf("scanWorkerSubscriptionsAt: %v", err)
	}
	if !containsCode(vs, "worker-subscribe-missing-queue-option") {
		t.Errorf("expected worker-subscribe-missing-queue-option, got %+v", vs)
	}
}

// TestWorkerSubscriptionGate_AcceptsCorrectSubscribe — same shape with
// a queue option passes silently.
func TestWorkerSubscriptionGate_AcceptsCorrectSubscribe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/worker.service.ts", `
import { connect, type NatsConnection, type Subscription } from 'nats';

export class WorkerService {
  private nc!: NatsConnection;
  private sub!: Subscription;

  async onModuleInit() {
    this.nc = await connect({ servers: 'broker:4222' });
    this.sub = this.nc.subscribe('items.events', { queue: 'workers' });
  }

  async onModuleDestroy() {
    await this.sub.drain();
    await this.nc.drain();
  }
}
`)
	vs, err := scanWorkerSubscriptionsAt(dir)
	if err != nil {
		t.Fatalf("scanWorkerSubscriptionsAt: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("correct shape should not flag, got %+v", vs)
	}
}

// TestWorkerSubscriptionGate_RefusesUnsubscribeShutdown — `unsubscribe()`
// inside `onModuleDestroy` (without a `drain()` call) drops in-flight
// events on rolling deploys.
func TestWorkerSubscriptionGate_RefusesUnsubscribeShutdown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/worker.service.ts", `
import { connect, type NatsConnection, type Subscription } from 'nats';

export class WorkerService {
  private nc!: NatsConnection;
  private sub!: Subscription;

  async onModuleInit() {
    this.nc = await connect({ servers: 'broker:4222' });
    this.sub = this.nc.subscribe('items.events', { queue: 'workers' });
  }

  async onModuleDestroy() {
    try {
      await this.sub?.unsubscribe();
    } catch {}
  }
}
`)
	vs, err := scanWorkerSubscriptionsAt(dir)
	if err != nil {
		t.Fatalf("scanWorkerSubscriptionsAt: %v", err)
	}
	if !containsCode(vs, "worker-shutdown-uses-unsubscribe") {
		t.Errorf("expected worker-shutdown-uses-unsubscribe, got %+v", vs)
	}
}

// TestWorkerSubscriptionGate_RefusesUnsubscribeInSigtermHandler — same
// pattern with a plain `process.on('SIGTERM', ...)` shape (non-NestJS
// worker codebases — plain Node).
func TestWorkerSubscriptionGate_RefusesUnsubscribeInSigtermHandler(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/worker.ts", `
import { connect } from 'nats';

async function main() {
  const nc = await connect({ servers: 'broker:4222' });
  const sub = nc.subscribe('items.events', { queue: 'workers' });

  process.on('SIGTERM', async () => {
    await sub.unsubscribe();
    await nc.close();
  });
}
main();
`)
	vs, err := scanWorkerSubscriptionsAt(dir)
	if err != nil {
		t.Fatalf("scanWorkerSubscriptionsAt: %v", err)
	}
	if !containsCode(vs, "worker-shutdown-uses-unsubscribe") {
		t.Errorf("expected worker-shutdown-uses-unsubscribe in SIGTERM block, got %+v", vs)
	}
}

// TestWorkerSubscriptionGate_AcceptsCorrectShape — full canonical shape:
// queue option on subscribe, drain on shutdown.
func TestWorkerSubscriptionGate_AcceptsCorrectShape(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/worker.service.ts", `
import {
  Injectable,
  OnModuleDestroy,
  OnModuleInit,
} from '@nestjs/common';
import { connect, type NatsConnection, type Subscription } from 'nats';

@Injectable()
export class WorkerService implements OnModuleInit, OnModuleDestroy {
  private nc!: NatsConnection;
  private sub!: Subscription;

  async onModuleInit() {
    this.nc = await connect({ servers: 'broker:4222' });
    this.sub = this.nc.subscribe('items.events', { queue: 'workers' });
  }

  async onModuleDestroy() {
    await this.sub.drain();
    await this.nc.drain();
  }
}
`)
	vs, err := scanWorkerSubscriptionsAt(dir)
	if err != nil {
		t.Fatalf("scanWorkerSubscriptionsAt: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("canonical shape should not flag, got %+v", vs)
	}
}

// TestWorkerSubscriptionGate_IgnoresRxJSSubscribe — observable
// `.subscribe(callback)` calls in non-NATS contexts must not trigger.
func TestWorkerSubscriptionGate_IgnoresRxJSSubscribe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/handler.ts", `
import { Subject } from 'rxjs';

const events$ = new Subject<string>();
events$.subscribe((msg) => {
  console.log('msg', msg);
});
`)
	vs, err := scanWorkerSubscriptionsAt(dir)
	if err != nil {
		t.Fatalf("scanWorkerSubscriptionsAt: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("rxjs subscribe should not flag, got %+v", vs)
	}
}

// TestWorkerSubscriptionGate_IgnoresNodeModules — vendored library
// code is skipped.
func TestWorkerSubscriptionGate_IgnoresNodeModules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "node_modules/some-lib/index.ts", `
import { connect } from 'nats';
const nc = await connect({});
nc.subscribe('foo');
`)
	vs, err := scanWorkerSubscriptionsAt(dir)
	if err != nil {
		t.Fatalf("scanWorkerSubscriptionsAt: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("node_modules should be skipped, got %+v", vs)
	}
}

// TestGateWorkerSubscription_FiresOnlyForShowcaseWorker — the gate
// only runs on showcase-tier worker codebases. Non-showcase tiers
// + non-worker codebases skip silently.
func TestGateWorkerSubscription_FiresOnlyForShowcaseWorker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "worker")
	writeSourceFile(t, root, "src/items-indexer.service.ts", `
import { connect, type NatsConnection, type Subscription } from 'nats';
const nc: NatsConnection = await connect({});
const sub: Subscription = nc.subscribe('items.events');
`)

	// Showcase + worker → flagged.
	vs := gateWorkerSubscription(GateContext{
		Plan: &Plan{
			Tier: tierShowcase,
			Codebases: []Codebase{
				{Hostname: "worker", IsWorker: true, SourceRoot: root},
			},
		},
	})
	if !containsCode(vs, "worker-subscribe-missing-queue-option") {
		t.Errorf("showcase + worker should flag, got %+v", vs)
	}

	// Non-showcase tier → skipped.
	vs = gateWorkerSubscription(GateContext{
		Plan: &Plan{
			Tier: "minimal",
			Codebases: []Codebase{
				{Hostname: "worker", IsWorker: true, SourceRoot: root},
			},
		},
	})
	if len(vs) != 0 {
		t.Errorf("non-showcase tier should skip, got %+v", vs)
	}

	// Showcase + non-worker → skipped.
	vs = gateWorkerSubscription(GateContext{
		Plan: &Plan{
			Tier: tierShowcase,
			Codebases: []Codebase{
				{Hostname: "api", IsWorker: false, SourceRoot: root},
			},
		},
	})
	if len(vs) != 0 {
		t.Errorf("non-worker codebase should skip, got %+v", vs)
	}

	// Worker that shares a codebase with api → skipped.
	vs = gateWorkerSubscription(GateContext{
		Plan: &Plan{
			Tier: tierShowcase,
			Codebases: []Codebase{
				{Hostname: "worker", IsWorker: true, SharesCodebaseWith: "api", SourceRoot: root},
			},
		},
	})
	if len(vs) != 0 {
		t.Errorf("worker sharing a codebase should skip, got %+v", vs)
	}
}

// TestGateWorkerSubscription_FlagsRun22ShapeExactly — uses the run-22
// `items-indexer.service.ts:81` + `:91` shape verbatim. Pinning this
// via the gate prevents recurrence of the exact dogfood bug.
func TestGateWorkerSubscription_FlagsRun22ShapeExactly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/items-indexer/items-indexer.service.ts", `
import {
  Injectable,
  Logger,
  OnModuleDestroy,
  OnModuleInit,
} from '@nestjs/common';
import {
  connect,
  type NatsConnection,
  StringCodec,
  type Subscription,
} from 'nats';

const ITEMS_EVENT_SUBJECT = 'items.events';

@Injectable()
export class ItemsIndexerService implements OnModuleInit, OnModuleDestroy {
  private nc!: NatsConnection;
  private sub!: Subscription;

  async onModuleInit() {
    this.nc = await connect({ servers: 'broker:4222' });
    this.sub = this.nc.subscribe(ITEMS_EVENT_SUBJECT);
  }

  async onModuleDestroy() {
    try {
      await this.sub?.unsubscribe();
    } catch {}
    try {
      await this.nc?.drain();
    } catch {}
  }
}
`)
	vs, err := scanWorkerSubscriptionsAt(dir)
	if err != nil {
		t.Fatalf("scanWorkerSubscriptionsAt: %v", err)
	}
	if !containsCode(vs, "worker-subscribe-missing-queue-option") {
		t.Error("run-22 shape: missing-queue-option not flagged")
	}
	// nc.drain() is in the SAME block as sub.unsubscribe() — the gate
	// downgrades this case to notice (caller already drains nc; the
	// fix is dropping the unsubscribe call). Notice still surfaces in
	// the violation list.
	if !containsCode(vs, "worker-shutdown-uses-unsubscribe") {
		t.Error("run-22 shape: shutdown-uses-unsubscribe not flagged")
	}
	for _, msg := range messagesForCode(vs, "worker-shutdown-uses-unsubscribe") {
		// Either the strict refusal phrasing OR the alongside-drain
		// downgrade message is acceptable — both teach the same fix.
		if !strings.Contains(msg, "drain") {
			t.Errorf("shutdown-uses-unsubscribe message must reference drain: %q", msg)
		}
	}
}

// messagesForCode returns the Message field of every violation with
// the given Code. Test helper.
func messagesForCode(vs []Violation, code string) []string {
	var out []string
	for _, v := range vs {
		if v.Code == code {
			out = append(out, v.Message)
		}
	}
	return out
}

# platform-principles / graceful-shutdown

Runtime services (both api and worker) handle SIGTERM by draining in-flight work and exiting with code 0. The Zerops runtime sends SIGTERM on rolling deploys, scaling-down events, and restarts; a service that crashes or hangs on SIGTERM produces visible tail latency and dropped in-flight requests for users.

## What "drain" means

- **api** — stop accepting new connections; let in-flight HTTP handlers complete; close database pools and other long-lived resources; exit 0.
- **worker** — stop pulling new messages from the queue; let in-progress handlers complete; ACK each message only after successful processing (not before); exit 0 once the queue reads return empty and no handler is still running.

For Nest apps specifically: call `app.enableShutdownHooks()` during bootstrap. This wires the module lifecycle (`OnModuleDestroy`, `beforeApplicationShutdown`, `OnApplicationShutdown`) to SIGTERM so every provider that implements a lifecycle hook actually runs it.

```typescript
async function bootstrap() {
  const app = await NestFactory.create(AppModule);
  app.enableShutdownHooks();
  await app.listen(3000);
}
```

For bare Express / Fastify / other runtimes: register a SIGTERM handler that calls the server's own graceful-shutdown routine, then exits.

```typescript
process.on('SIGTERM', async () => {
  await server.close();
  process.exit(0);
});
```

## ACK timing for workers

The message is ACKed after the handler completes successfully, not on receipt. If the handler crashes, the message returns to the queue for another worker to pick up; if SIGTERM arrives while the handler is running, the handler finishes, the message is ACKed, and only then does the worker exit. ACK-on-receipt turns every SIGTERM into lost work.

## Pre-attest before returning

Grep your source tree for the signal handlers and the Nest bootstrap call (framework-appropriate — adjust `/src` to match your project layout):

```
ssh {host} "grep -rnE 'SIGTERM|enableShutdownHooks' /var/www/src 2>/dev/null | grep -q ."
```

Non-zero means the code does not register graceful shutdown. Add the handler (or the Nest bootstrap call) and re-run.

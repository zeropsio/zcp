// Počasí worker: no port. Every minute it "downloads" data (computes it, no network)
// and logs one line, so a dev server without a listener has visible output.
const everyMs = Number(process.env.WORKER_INTERVAL_MS ?? 60_000);

function tick() {
  const tempC = (new Date().getMinutes() % 25) - 3;
  console.log(`worker: fetched Praha ${tempC} °C at ${new Date().toISOString()}`);
}

tick();
setInterval(tick, everyMs);

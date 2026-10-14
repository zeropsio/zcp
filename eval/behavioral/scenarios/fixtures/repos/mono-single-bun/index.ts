// Počasí: one Bun server, no dependencies, no network — the forecast is computed.
const port = Number(process.env.PORT ?? 3000);

const days = ["Po", "Út", "St", "Čt", "Pá", "So", "Ne"];

function forecast(city: string) {
  const seed = [...city].reduce((sum, ch) => sum + ch.charCodeAt(0), 0);
  return days.map((day, i) => ({ day, tempC: ((seed + i * 7) % 25) - 3 }));
}

Bun.serve({
  port,
  fetch(req) {
    const url = new URL(req.url);
    if (url.pathname === "/health") return new Response("ok");
    const city = url.searchParams.get("city") ?? "Praha";
    if (url.pathname === "/api/forecast") return Response.json({ city, days: forecast(city) });
    const rows = forecast(city).map((d) => `<li>${d.day}: ${d.tempC} °C</li>`).join("");
    return new Response(`<!doctype html><meta charset="utf-8"><title>Počasí</title><h1>Počasí — ${city}</h1><ul>${rows}</ul>`, {
      headers: { "content-type": "text/html; charset=utf-8" },
    });
  },
});

console.log(`počasí listening on :${port}`);

// Počasí web: renders the api's forecast. API_URL comes from a project variable
// built over ${zeropsSubdomainHost}, never from ${apistage_zeropsSubdomain} (MODEL S2 g).
const port = Number(process.env.PORT ?? 3000);
const apiUrl = process.env.API_URL ?? "http://localhost:8080";

Bun.serve({
  port,
  async fetch(req) {
    const url = new URL(req.url);
    if (url.pathname === "/health") return new Response("ok");
    const city = url.searchParams.get("city") ?? "Praha";
    let rows = "<li>api unreachable</li>";
    try {
      const res = await fetch(`${apiUrl}/forecast?city=${encodeURIComponent(city)}`);
      const body = (await res.json()) as { days: { day: string; tempC: number }[] };
      rows = body.days.map((d) => `<li>${d.day}: ${d.tempC} °C</li>`).join("");
    } catch {}
    return new Response(`<!doctype html><meta charset="utf-8"><title>Počasí</title><h1>Počasí — ${city}</h1><ul>${rows}</ul>`, {
      headers: { "content-type": "text/html; charset=utf-8" },
    });
  },
});

console.log(`web listening on :${port}, api ${apiUrl}`);

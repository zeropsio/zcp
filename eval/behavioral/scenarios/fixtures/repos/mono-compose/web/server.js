// Počasí web: a page that asks the api for the forecast.
import { createServer } from "node:http";

const port = Number(process.env.PORT ?? 3000);
const apiUrl = process.env.API_URL ?? "http://127.0.0.1:8080";

createServer(async (req, res) => {
  const url = new URL(req.url, "http://localhost");
  if (url.pathname === "/health") return res.end("ok");
  try {
    const forecast = await (await fetch(`${apiUrl}/forecast`)).json();
    const rows = forecast.days.map((d) => `<li>${d.day}: ${d.tempC} °C</li>`).join("");
    res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    res.end(`<!doctype html><meta charset="utf-8"><title>Počasí</title><h1>Počasí — ${forecast.city}</h1><ul>${rows}</ul>`);
  } catch {
    res.writeHead(502).end("api unreachable");
  }
}).listen(port, () => console.log(`web listening on :${port}`));

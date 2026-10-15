// Počasí api: the forecast as JSON. DATABASE_URL names the database the
// compose file runs next to it; this tiny api only reports whether it is set.
import { createServer } from "node:http";

const port = Number(process.env.PORT ?? 8080);
const days = ["Po", "Út", "St", "Čt", "Pá", "So", "Ne"];
const forecast = (city) => {
  const seed = [...city].reduce((sum, ch) => sum + ch.charCodeAt(0), 0);
  return days.map((day, i) => ({ day, tempC: ((seed + i * 7) % 25) - 3 }));
};

createServer((req, res) => {
  const url = new URL(req.url, "http://localhost");
  if (url.pathname === "/health") return res.end(process.env.DATABASE_URL ? "ok db-configured" : "ok");
  if (url.pathname === "/forecast") {
    res.writeHead(200, { "content-type": "application/json" });
    return res.end(JSON.stringify({ city: "Praha", days: forecast("Praha") }));
  }
  res.writeHead(404).end();
}).listen(port, () => console.log(`api listening on :${port}`));

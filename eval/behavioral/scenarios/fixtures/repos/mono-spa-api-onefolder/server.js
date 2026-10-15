// One server for the whole app: /api/forecast as JSON, everything else from dist/.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, join, normalize } from "node:path";

const port = Number(process.env.PORT ?? 3000);
const days = ["Po", "Út", "St", "Čt", "Pá", "So", "Ne"];
const types = { ".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css" };
const forecast = (city) => {
  const seed = [...city].reduce((sum, ch) => sum + ch.charCodeAt(0), 0);
  return days.map((day, i) => ({ day, tempC: ((seed + i * 7) % 25) - 3 }));
};

createServer(async (req, res) => {
  const url = new URL(req.url, "http://localhost");
  if (url.pathname === "/health") return res.end("ok");
  if (url.pathname === "/api/forecast") {
    res.writeHead(200, { "content-type": "application/json" });
    return res.end(JSON.stringify({ city: "Praha", days: forecast("Praha") }));
  }
  const rel = normalize(url.pathname === "/" ? "/index.html" : url.pathname).replace(/^(\.\.[/\\])+/, "");
  try {
    const body = await readFile(join("dist", rel));
    res.writeHead(200, { "content-type": types[extname(rel)] ?? "application/octet-stream" });
    res.end(body);
  } catch {
    res.writeHead(404).end();
  }
}).listen(port, () => console.log(`pocasi listening on :${port}`));

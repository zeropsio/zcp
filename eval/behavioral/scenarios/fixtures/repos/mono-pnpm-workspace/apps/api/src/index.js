// Počasí api: the shared forecast as JSON.
import { createServer } from "node:http";
import { forecast } from "@mono/shared";

const port = Number(process.env.PORT ?? 8080);

createServer((req, res) => {
  const url = new URL(req.url, "http://localhost");
  if (url.pathname === "/health") return res.end("ok");
  if (url.pathname === "/forecast") {
    const city = url.searchParams.get("city") ?? "Praha";
    res.writeHead(200, { "content-type": "application/json", "access-control-allow-origin": "*" });
    return res.end(JSON.stringify({ city, days: forecast(city) }));
  }
  res.writeHead(404).end();
}).listen(port, () => console.log(`api listening on :${port}`));

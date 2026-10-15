// The Mate-shaped server: /health and /api/hello on a fixed port from env.
import { createServer } from "node:http";

const port = Number(process.env.DEV_SERVER_PORT ?? 13773);
if (port === 3773) throw new Error("3773 belongs to the installed Mate unit");

createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://localhost");
  if (url.pathname === "/health") return void res.end("ok");
  if (url.pathname === "/api/hello") {
    res.writeHead(200, { "content-type": "application/json" });
    return void res.end(JSON.stringify({ hello: "server", home: process.env.DEV_HOME ?? null }));
  }
  res.writeHead(404).end();
}).listen(port, () => console.log(`server listening on :${port}`));

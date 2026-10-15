// The production server: serves src/ on 8080 on every interface.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";

createServer(async (_req, res) => {
  res.writeHead(200, { "content-type": "text/html" });
  res.end(await readFile(new URL("../src/index.html", import.meta.url), "utf8"));
}).listen(8080, "0.0.0.0");

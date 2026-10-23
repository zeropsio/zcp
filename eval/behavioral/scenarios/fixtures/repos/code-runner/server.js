// Code runner. POST /run takes {"code": "<python source>"}, runs it in a
// clean container leased from the `sandbox` Swarm pool and answers
// {"output", "exitCode"}. The container is released with a reset, so nothing
// one submission leaves behind reaches the next. User code never runs in this
// service's own container.
"use strict";

const http = require("node:http");

const PORT = Number(process.env.PORT) || 3000;
const SANDBOX_URL = process.env.SANDBOX_URL || "http://orch.sandbox.zerops";
const SANDBOX_TOKEN = process.env.SANDBOX_TOKEN || "";

async function orchestrator(path, { lease, body } = {}) {
  const headers = { authorization: `Bearer ${SANDBOX_TOKEN}` };
  if (lease) headers["x-swarm-lease"] = lease;
  if (body !== undefined) headers["content-type"] = "application/json";
  const res = await fetch(SANDBOX_URL + path, {
    method: "POST",
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(120000),
  });
  if (!res.ok) {
    throw new Error(`sandbox ${path.split("?")[0]} answered ${res.status}`);
  }
  return res;
}

async function runInSandbox(code) {
  const acquired = await orchestrator("/container/acquire", {
    body: { start: true, clean: true, leaseTtlSeconds: 300 },
  });
  const container = await acquired.json();
  const base = `/container/${encodeURIComponent(container.id)}`;
  try {
    const res = await orchestrator(`${base}/exec`, {
      lease: container.lease,
      body: { command: ["python3", "-c", code], timeoutSeconds: 10 },
    });
    let output = "";
    let exitCode = null;
    for (const line of (await res.text()).split("\n")) {
      if (!line.trim()) continue;
      const frame = JSON.parse(line);
      if (frame.type === "stdout") output += Buffer.from(frame.data, "base64").toString("utf8");
      if (frame.type === "exit") exitCode = frame.exitCode;
      if (frame.type === "error") throw new Error(`sandbox: ${frame.error}`);
    }
    return { output, exitCode };
  } finally {
    await orchestrator(`${base}/release?reset=true`, { lease: container.lease }).catch(() => {});
  }
}

function send(res, status, type, body) {
  res.writeHead(status, { "content-type": type });
  res.end(body);
}

const server = http.createServer((req, res) => {
  if (req.method === "GET" && req.url === "/") {
    send(res, 200, "text/plain", "code-runner\n");
    return;
  }
  if (req.method === "POST" && req.url === "/run") {
    let raw = "";
    req.on("data", (chunk) => {
      raw += chunk;
    });
    req.on("end", async () => {
      let code;
      try {
        code = JSON.parse(raw).code;
      } catch {
        send(res, 400, "application/json", JSON.stringify({ error: "the body must be JSON" }));
        return;
      }
      if (typeof code !== "string" || code === "") {
        send(res, 400, "application/json", JSON.stringify({ error: "code is required" }));
        return;
      }
      try {
        send(res, 200, "application/json", JSON.stringify(await runInSandbox(code)));
      } catch (err) {
        send(res, 502, "application/json", JSON.stringify({ error: String(err.message || err) }));
      }
    });
    return;
  }
  send(res, 404, "text/plain", "not found\n");
});

server.listen(PORT, () => console.log(`code-runner listening on ${PORT}`));

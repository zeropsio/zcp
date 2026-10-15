// Runs the server and the web together on ports from the environment. Like
// z3's runner it keeps its own home under .dev-home and never binds 3773,
// the port of the installed Mate unit on the same container.
import { spawn } from "node:child_process";

const serverPort = Number(process.env.DEV_SERVER_PORT ?? 13773);
const webPort = Number(process.env.DEV_WEB_PORT ?? 5733);
if (serverPort === 3773) throw new Error("3773 belongs to the installed Mate unit");

const env = { ...process.env, DEV_SERVER_PORT: String(serverPort), DEV_WEB_PORT: String(webPort), DEV_HOME: ".dev-home" };
for (const [cwd, cmd] of [["apps/server", "dev"], ["apps/web", "dev"]] as const) {
  spawn("npm", ["run", cmd], { cwd, env, stdio: "inherit" });
}

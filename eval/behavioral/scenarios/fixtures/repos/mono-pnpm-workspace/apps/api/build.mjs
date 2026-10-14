// The api's build: copy src to dist (plain ESM, nothing to compile).
import { cpSync, rmSync } from "node:fs";

rmSync(new URL("./dist", import.meta.url), { recursive: true, force: true });
cpSync(new URL("./src", import.meta.url), new URL("./dist", import.meta.url), { recursive: true });

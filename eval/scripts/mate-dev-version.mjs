// Select the packed dev version locally, before any build or container effect.
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

try {
  const repo = resolve(process.argv[2]);
  const git = (args) => {
    const result = spawnSync("git", args, { cwd: repo, encoding: "utf8" });
    return { code: result.status ?? 1, output: result.stdout ?? "" };
  };
  const checkedGit = (args, failure) => {
    const result = git(args);
    if (result.code !== 0) throw new Error(failure);
    return result.output.trim();
  };
  const sha = checkedGit(["rev-parse", "--short", "HEAD"], "cannot read Mate HEAD");
  let base = process.env.MATE_DEV_BASE_VERSION;
  if (base === undefined) {
    base = JSON.parse(readFileSync(resolve(repo, "apps/server/package.json"), "utf8")).version;
    console.error("Using the source package version; a dev build below zcp's runtime floor will be replaced. Set MATE_DEV_BASE_VERSION to an official source-ancestor release if needed.");
  } else {
    if (base.match(/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u)?.[0] !== base ||
        !base.split(".").every((part) => Number.isSafeInteger(Number(part)))) {
      throw new Error("MATE_DEV_BASE_VERSION must be canonical MAJOR.MINOR.PATCH");
    }
    const ref = `refs/tags/v${base}`;
    const official = checkedGit(
      ["ls-remote", "--tags", "git@github.com:zeropsio/mate.git", ref, `${ref}^{}`],
      "cannot verify the official release tag; fetch it from zeropsio/mate and ensure GitHub SSH access",
    ).split("\n").map((line) => line.split(/\s+/u));
    const release = official.find(([, name]) => name === `${ref}^{}`)?.[0] ??
      official.find(([, name]) => name === ref)?.[0];
    const local = checkedGit(["rev-parse", "--verify", `${ref}^{commit}`], "fetch the official release tag before selecting a dev base");
    if (!release || release !== local) throw new Error("local tag does not match the official release tag");
    const pkg = JSON.parse(checkedGit(["show", `${ref}:apps/server/package.json`], "cannot read release package version"));
    if (pkg.version !== base) throw new Error("official release package does not match the requested dev base");

    // Mate owns detached version-bump validation; do not infer source from a parent alone.
    const shared = resolve(repo, "packages/shared");
    const require = createRequire(resolve(shared, "package.json"));
    const Effect = await import(pathToFileURL(require.resolve("effect/Effect")).href);
    const { readReleaseSource } = await import(pathToFileURL(resolve(shared, "src/releaseSource.ts")).href);
    const source = await Effect.runPromise(readReleaseSource(ref, (args) => Effect.sync(() => git(args))));
    if (!source) throw new Error("official release has no validated source");
    if (git(["merge-base", "--is-ancestor", source, "HEAD"]).code !== 0) {
      throw new Error("release source is not an ancestor of Mate HEAD; choose a supported base");
    }
  }
  console.log(`${base}-dev.${sha}`);
} catch (error) {
  console.error(`Mate dev version: ${error.message}`);
  process.exitCode = 1;
}

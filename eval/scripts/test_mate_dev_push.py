"""Dev-push contract tests: stop at packing; never build or contact a container."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ENTRY = Path(__file__).with_name("mate-dev-push.sh").resolve()


class MateDevPushTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "mate"
        self.repo.mkdir()
        self.env = {k: v for k, v in os.environ.items() if not k.startswith(("GIT_", "MATE_", "EVAL_"))}
        self.env.update(GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_AUTHOR_NAME="Fixture", GIT_AUTHOR_EMAIL="fixture@example.test",
                        GIT_COMMITTER_NAME="Fixture", GIT_COMMITTER_EMAIL="fixture@example.test")
        self.git("init", "-q")
        server = self.repo / "apps/server"
        (server / "scripts").mkdir(parents=True)
        (server / "package.json").write_text(json.dumps({"version": "0.15.101"}))
        (server / "scripts/cli.ts").write_text(
            'import {writeFileSync} from "node:fs";\n'
            'if (process.argv[2] === "pack") {writeFileSync(process.env.PACK_ARGS, JSON.stringify(process.argv.slice(3))); process.exit(7);}\n')
        # Mate owns release-source validation. Stub its Effect boundary here;
        # these tests own official-tag binding, ancestry and the pack consumer.
        shared = self.repo / "packages/shared"
        (shared / "src").mkdir(parents=True)
        (shared / "package.json").write_text('{"type":"module"}')
        (shared / "src/releaseSource.ts").write_text(
            'export const readReleaseSource = (ref, git) => async () => {const r = await git(["rev-parse", ref + "^"])(); return r.code === 0 ? r.output.trim() : undefined;};\n')
        effect = shared / "node_modules/effect"
        effect.mkdir(parents=True)
        (effect / "package.json").write_text('{"exports":{"./Effect":"./Effect.cjs"}}')
        (effect / "Effect.cjs").write_text('exports.sync = f => async () => f(); exports.runPromise = e => e();')
        self.git("add", ".")
        self.git("commit", "-qm", "source")
        self.source = self.git("rev-parse", "HEAD")
        (server / "package.json").write_text(json.dumps({"version": "0.16.39"}))
        self.git("commit", "-qam", "chore(release): mate 0.16.39")
        self.release = self.git("rev-parse", "HEAD")
        self.git("tag", "v0.16.39")
        self.git("checkout", "-q", "--detach", self.source)
        self.real_git = shutil.which("git")
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        (bin_dir / "vp").write_text("#!/bin/sh\nexit 0\n")
        (bin_dir / "vp").chmod(0o755)
        (bin_dir / "git").write_text(
            '#!/usr/bin/env python3\nimport os,sys\n'
            'if "ls-remote" in sys.argv:\n'
            ' assert sys.argv[1:] == ["ls-remote", "--tags", "git@github.com:zeropsio/mate.git", "refs/tags/v0.16.39", "refs/tags/v0.16.39^{}"]\n'
            ' print(os.environ["OFFICIAL_TAG"] + "\\trefs/tags/v0.16.39")\n'
            'else: os.execv(os.environ["REAL_GIT"], [os.environ["REAL_GIT"], *sys.argv[1:]])\n')
        (bin_dir / "git").chmod(0o755)
        self.args_file = self.root / "pack-args.json"
        self.env.update(PATH=str(bin_dir) + os.pathsep + os.environ["PATH"],
                        MATE_REPO=str(self.repo), MATE_DEV_BASE_VERSION="0.16.39",
                        MATE_SKIP_WEB="1", MATE_NODE_PTY_PREBUILD=str(self.root),
                        PACK_ARGS=str(self.args_file), REAL_GIT=self.real_git,
                        OFFICIAL_TAG=self.release)

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.repo), *args], env=self.env,
                                       text=True, stderr=subprocess.DEVNULL).strip()

    def run_push(self):
        return subprocess.run(["bash", str(ENTRY), "mate"], env=self.env,
                              capture_output=True, text=True)

    def test_verified_release_source_base_reaches_the_packed_artifact(self):
        result = self.run_push()
        self.assertEqual(result.returncode, 7, result.stderr)
        args = json.loads(self.args_file.read_text())
        actual = args[args.index("--app-version") + 1]
        self.assertEqual(actual, "0.16.39-dev." + self.git("rev-parse", "--short", "HEAD"),
                         "dev artifact must name the verified release base and actual source SHA")

    def test_default_keeps_the_source_package_base_and_explains_the_runtime_floor(self):
        del self.env["MATE_DEV_BASE_VERSION"]
        result = self.run_push()
        self.assertEqual(result.returncode, 7, result.stderr)
        args = json.loads(self.args_file.read_text())
        self.assertEqual(args[args.index("--app-version") + 1],
                         "0.15.101-dev." + self.git("rev-parse", "--short", "HEAD"))
        self.assertIn("below zcp's runtime floor will be replaced", result.stderr)

    def test_invalid_or_unsupported_base_stops_before_pack_or_remote_delivery(self):
        for base in ["v0.16.39", "00.16.39", "0.16.39-dev.x", "0.16.39+build", "0.16.39\n"]:
            with self.subTest(base=base):
                self.env["MATE_DEV_BASE_VERSION"] = base
                result = self.run_push()
                self.assertNotEqual(result.returncode, 7, "invalid base reached packing")
                self.assertFalse(self.args_file.exists())
                self.assertIn("canonical MAJOR.MINOR.PATCH", result.stderr)
        result = subprocess.run(["bash", str(ENTRY), "all"], env=self.env,
                                capture_output=True, text=True)
        self.assertIn("canonical MAJOR.MINOR.PATCH", result.stderr)
        self.assertNotIn("### zcp binary", result.stdout)

    def test_unofficial_or_nonancestor_release_cannot_raise_the_dev_base(self):
        self.env["OFFICIAL_TAG"] = "0" * 40
        result = self.run_push()
        self.assertIn("official release tag", result.stderr)
        self.assertFalse(self.args_file.exists())
        self.env["OFFICIAL_TAG"] = self.release
        self.git("checkout", "-q", "--orphan", "unrelated")
        self.git("commit", "-qm", "unrelated source")
        result = self.run_push()
        self.assertIn("release source is not an ancestor", result.stderr)
        self.assertFalse(self.args_file.exists())


if __name__ == "__main__":
    unittest.main()

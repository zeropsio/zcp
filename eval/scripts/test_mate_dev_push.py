"""Delivery contract tests: mock external effects; never build or contact a container."""

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


class BuildDeployTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.entry = self.root / "eval/scripts/build-deploy.sh"
        self.entry.parent.mkdir(parents=True)
        shutil.copy2(ENTRY.with_name("build-deploy.sh"), self.entry)
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        self.commands_file = self.root / "commands.jsonl"
        mock = (
            '#!/usr/bin/env python3\n'
            'import json, os, sys\n'
            'from pathlib import Path\n'
            'name = Path(sys.argv[0]).name\n'
            'with open(os.environ["DELIVERY_COMMANDS"], "a") as log:\n'
            ' log.write(json.dumps([name, *sys.argv[1:]]) + "\\n")\n'
            'if name == "make":\n'
            ' target = Path("builds/zcp-linux-amd64")\n'
            ' target.parent.mkdir(parents=True, exist_ok=True)\n'
            ' target.write_bytes(b"fixture binary")\n'
            'elif name == "shasum": print("a" * 64 + "  " + sys.argv[-1])\n'
            'elif name == "ssh" and sys.argv[-1].startswith("sha256sum "):\n'
            ' print(os.environ["DELIVERY_REMOTE_HASH"] + "  /usr/local/bin/zcp")\n'
        )
        for name in ("make", "scp", "ssh", "shasum"):
            command = bin_dir / name
            command.write_text(mock)
            command.chmod(0o755)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("EVAL_")}
        self.env.update(PATH=str(bin_dir) + os.pathsep + os.environ["PATH"],
                        EVAL_REMOTE_HOST="fixture.invalid", EVAL_SSH_OPTS="-F /dev/null",
                        DELIVERY_COMMANDS=str(self.commands_file), DELIVERY_REMOTE_HASH="a" * 64)

    def run_deploy(self):
        result = subprocess.run(["bash", str(self.entry)], cwd=self.root, env=self.env,
                                capture_output=True, text=True)
        commands = [json.loads(line) for line in self.commands_file.read_text().splitlines()]
        return result, commands

    def test_verified_install_does_not_terminate_unowned_processes(self):
        result, commands = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        binary = str(self.root / "builds/zcp-linux-amd64")
        self.assertEqual(commands[0], ["make", "linux-amd"])
        self.assertEqual(commands[1][:4], ["scp", "-F", "/dev/null", binary])
        self.assertRegex(commands[1][4], r"^fixture\.invalid:/tmp/zcp\.deploy\.\d+$")
        remote_tmp = commands[1][4].split(":", 1)[1]
        remote = [command[-1] for command in commands if command[0] == "ssh"]
        self.assertTrue(all(command[1:4] == ["-F", "/dev/null", "fixture.invalid"]
                            for command in commands if command[0] == "ssh"))
        self.assertEqual(remote[0],
                         f"sudo install -m 0755 '{remote_tmp}' '/usr/local/bin/zcp' && rm -f '{remote_tmp}'")
        self.assertIn('LOCAL_BIN="$HOME/.local/bin/zcp"', remote[1])
        self.assertIn("ln -s '/usr/local/bin/zcp' \"$LOCAL_BIN\"", remote[1])
        self.assertIn(["shasum", "-a", "256", binary], commands)
        self.assertEqual(remote[2], "sha256sum /usr/local/bin/zcp")
        self.assertIn("Deploy OK (hash match:", result.stdout)
        self.assertNotRegex("\n".join(remote), r"\b(?:pkill|killall|kill)\b",
                            "installer must never request process termination")
        self.assertEqual(len(remote), 3)

    def test_checksum_mismatch_still_refuses_delivery_success(self):
        self.env["DELIVERY_REMOTE_HASH"] = "b" * 64
        result, commands = self.run_deploy()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("WARNING: Hash mismatch!", result.stdout)
        self.assertNotIn("Deploy OK", result.stdout)
        self.assertEqual(commands[-1],
                         ["ssh", "-F", "/dev/null", "fixture.invalid", "sha256sum /usr/local/bin/zcp"])


if __name__ == "__main__":
    unittest.main()

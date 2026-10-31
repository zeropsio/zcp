"""Contract tests for the public changed-check entry point (no Go toolchain needed)."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ENTRY = Path(__file__).with_name("check-changed.sh")
MODULE = "example.test/zcp"
PACKAGES = ["cmd/zcp", "internal/content", "internal/leaf", "internal/reader",
            "internal/testreader", "internal/unrelated"]


class CheckChangedTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        # Git exports repository-local variables to hooks. A fixture must never
        # inherit GIT_DIR/GIT_INDEX_FILE (or the hook's -c configuration).
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        subprocess.run(["git", "init", "-q", str(self.root)], env=self.env, check=True)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        graph = []
        for name in PACKAGES:
            package = {"ImportPath": MODULE + "/" + name,
                       "Dir": str(self.root / name)}
            if name == "internal/reader":
                package["Imports"] = [MODULE + "/internal/leaf"]
            if name == "internal/testreader":
                package["XTestImports"] = [MODULE + "/internal/reader"]
            if name == "cmd/zcp":
                package["TestImports"] = [MODULE + "/internal/testreader"]
            if name == "internal/leaf":
                package["EmbedFiles"] = ["assets/page.html"]
            graph.append(package)
        self.graph = self.root / "graph.json"
        self.graph.write_text("\n".join(json.dumps(p) for p in graph))
        go = self.bin / "go"
        go.write_text("#!/bin/sh\ncat \"$GRAPH\"\n")
        go.chmod(0o755)
        self.env.update(PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        GRAPH=str(self.graph))

    def run_plan(self, files):
        result = subprocess.run(["bash", str(ENTRY), "--plan", "--files", *files],
                                cwd=self.root, env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_changed_file_selects_owners_and_reverse_test_imports(self):
        cases = [
            ("Go source", ["internal/leaf/value.go"],
             ["cmd/zcp", "internal/content", "internal/leaf", "internal/reader", "internal/testreader"]),
            ("external test import", ["internal/reader/reader_test.go"],
             ["cmd/zcp", "internal/content", "internal/reader", "internal/testreader"]),
            ("embedded resource", ["internal/leaf/assets/page.html"],
             ["cmd/zcp", "internal/content", "internal/leaf", "internal/reader", "internal/testreader"]),
            ("new embedded resource", ["internal/leaf/assets/new.html"],
             ["cmd/zcp", "internal/content", "internal/leaf", "internal/reader", "internal/testreader"]),
            ("fixture", ["internal/unrelated/testdata/input.json"],
             ["internal/content", "internal/unrelated"]),
            ("Markdown reader", ["docs/spec-workflows.md"], ["internal/content"]),
            ("runtime reader", ["eval/behavioral/scenarios/example.json"],
             ["cmd/zcp", "internal/content"]),
            ("module", ["go.mod"], PACKAGES),
            ("global build config", ["Makefile"], PACKAGES),
            ("unknown owner", ["new-assets/input.bin"], PACKAGES),
            ("empty diff", [], []),
        ]
        for title, files, expected in cases:
            with self.subTest(title=title):
                plan = self.run_plan(files)
                self.assertEqual(plan["packages"], ["./" + p for p in expected])
                if title == "unknown owner":
                    self.assertEqual(plan["unresolved"], files)

    def test_deleted_package_widens_instead_of_losing_consumers(self):
        plan = self.run_plan(["internal/deleted/owner.go"])
        self.assertEqual(plan["packages"], ["./" + p for p in PACKAGES])
        self.assertEqual(plan["unresolved"], ["internal/deleted/owner.go"])

    def test_deleted_nested_package_does_not_become_its_parent(self):
        plan = self.run_plan(["internal/leaf/deleted/owner.go"])
        self.assertEqual(plan["packages"], ["./" + p for p in PACKAGES])
        self.assertEqual(plan["unresolved"], ["internal/leaf/deleted/owner.go"])

    def test_staged_deletion_is_selected(self):
        source = self.root / "internal/unrelated/old.go"
        source.parent.mkdir(parents=True)
        source.write_text("package unrelated\n")
        subprocess.run(["git", "add", "."], cwd=self.root, env=self.env, check=True)
        subprocess.run(["git", "-c", "user.name=Test", "-c", "user.email=test@example.test",
                        "commit", "-qm", "fixture"],
                       cwd=self.root, env=self.env, check=True)
        source.unlink()
        subprocess.run(["git", "add", "-u"], cwd=self.root, env=self.env, check=True)
        result = subprocess.run(["bash", str(ENTRY), "--plan"], cwd=self.root,
                                env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["packages"],
                         ["./internal/content", "./internal/unrelated"])

    def test_source_reader_checks_do_not_propagate_unchanged_exports(self):
        with self.graph.open("a") as graph:
            for name in ("internal/topology", "internal/topologyconsumer", "internal/eval",
                         "internal/dataconsole", "internal/captureinspector"):
                package = {"ImportPath": MODULE + "/" + name, "Dir": str(self.root / name)}
                if name == "internal/topologyconsumer":
                    package["Imports"] = [MODULE + "/internal/topology"]
                graph.write("\n" + json.dumps(package))
        plan = self.run_plan(["internal/unrelated/value.go"])
        self.assertEqual(plan["packages"], ["./" + p for p in (
            "internal/captureinspector", "internal/content", "internal/dataconsole",
            "internal/eval", "internal/topology", "internal/unrelated")])

    def test_incomplete_graph_widens_selection(self):
        packages = [json.loads(line) for line in self.graph.read_text().splitlines()]
        packages[0]["DepsErrors"] = [{"Err": "missing imported package"}]
        self.graph.write_text("\n".join(json.dumps(p) for p in packages))
        plan = self.run_plan(["docs/example.md"])
        self.assertEqual(plan["packages"], ["./" + p for p in PACKAGES])
        self.assertEqual(plan["wide"], ["incomplete Go package graph"])

    def test_cross_package_filesystem_readers_are_selected(self):
        cases = [
            ("internal/content/templates/claude_test.md",
             {"internal/content", "internal/skillpacks", "internal/tools"}),
            ("internal/content/atoms/example.md", {"internal/content", "internal/authoring/recipe"}),
            ("internal/knowledge/recipes/example.md",
             {"internal/knowledge", "internal/authoring/recipe", "internal/workflow"}),
            ("docs/spec-workflows.md", {"internal/eval"}),
            ("docs/zcprecipator3/system.md", {"internal/eval", "internal/authoring/recipe"}),
            ("internal/tools/testdata/mate-contract/dev-servers.declaration.json",
             {"internal/tools", "internal/devsup"}),
            ("internal/dataconsole/console/webui/dist/index.html",
             {"internal/dataconsole/console/webui", "internal/dataconsole/console",
              "internal/dataconsole/extension"}),
            ("internal/dataconsole/extension/templates/vscode-studio/lib/consoleRoutes.js",
             {"internal/dataconsole/extension", "internal/dataconsole/console/server"}),
            ("e2e/testdata/dataconsole/version-matrix.import.yaml",
             {"e2e", "internal/schema", "internal/dataconsole/console/provider/conformance"}),
            ("internal/captureinspector/browsertest/smoke.cjs",
             {"internal/captureinspector", "internal/captureinspector/internal/web"}),
        ]
        names = {"internal/content", "internal/unrelated"}.union(*(names for _, names in cases))
        self.graph.write_text("\n".join(json.dumps({
            "ImportPath": MODULE + "/" + name, "Dir": str(self.root / name)}) for name in names))
        for file, readers in cases:
            with self.subTest(file=file):
                self.assertEqual(self.run_plan([file])["packages"],
                                 ["./" + p for p in sorted(readers | {"internal/content"})])

    def test_precommit_runs_selected_short_tests_and_lint(self):
        scripts = self.root / "scripts"
        scripts.mkdir()
        for name in ("check-changed.sh", "check_changed.py"):
            shutil.copy2(ENTRY.with_name(name), scripts / name)
        (self.root / "go.mod").write_text("module " + MODULE + "\n")
        source = self.root / "internal/unrelated/new.go"
        source.parent.mkdir(parents=True)
        source.write_text("package unrelated\n")
        subprocess.run(["git", "add", "internal/unrelated/new.go"], cwd=self.root,
                       env=self.env, check=True)
        (self.bin / "go").write_text(
            '#!/bin/sh\nif [ "$1" = list ]; then cat "$GRAPH"; '
            'else if [ -n "${GIT_DIR:-}${GIT_INDEX_FILE:-}" ]; then '
            'echo "Git hook environment leaked into checks" >&2; exit 19; fi; '
            'echo "go ${GOTOOLCHAIN:-unset} $*" >> "$CALLS"; exit "${TEST_EXIT:-0}"; fi\n')
        lint = self.bin / "golangci-lint"
        lint.write_text('#!/bin/sh\necho "lint ${GOTOOLCHAIN:-unset} $*" >> "$CALLS"\n')
        lint.chmod(0o755)
        calls = self.root / "calls"
        env = dict(self.env, CALLS=str(calls), GIT_DIR=str(self.root / ".git"),
                   GIT_INDEX_FILE=str(self.root / ".git/index"), GOTOOLCHAIN="go1.27.3")
        result = subprocess.run(["bash", str(ENTRY.parent.parent / ".githooks/pre-commit")],
                                cwd=self.root, env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls.read_text().splitlines(), [
            "go go1.27.3 test -short -json ./internal/content ./internal/unrelated",
            "lint go1.25.12 run ./internal/content ./internal/unrelated --timeout=120s"])
        calls.unlink()
        result = subprocess.run(["bash", str(ENTRY.parent.parent / ".githooks/pre-commit")],
                                cwd=self.root, env=dict(env, TEST_EXIT="7"),
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls.read_text().splitlines(), [
            "go go1.27.3 test -short -json ./internal/content ./internal/unrelated"])

    def test_prepush_does_not_repeat_checks(self):
        result = subprocess.run(["bash", str(ENTRY.parent.parent / ".githooks/pre-push")],
                                cwd=self.root, env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")

    def test_corpus_guard_reports_only_unsynced_packages_without_hiding_failures(self):
        message = ("knowledge corpus not synced — run `zcp sync pull` before testing "
                   "(a fresh clone/worktree embeds only the mailpit recipe; CI pulls the corpus first).")
        guarded = MODULE + "/internal/authoring/recipe"
        other = MODULE + "/internal/content"

        def event(package, action, output=None, test=None):
            value = {"Package": package, "Action": action}
            if output is not None:
                value["Output"] = output
            if test is not None:
                value["Test"] = test
            return value

        def missing(package=guarded):
            return [event(package, "start"), event(package, "output", message + "\n"),
                    event(package, "output", "FAIL\t" + package + "\t0.001s\n"),
                    event(package, "fail")]

        cases = [
            ("corpus present", [event(guarded, "start"), event(guarded, "pass")], 0, True, []),
            ("corpus missing", missing(), 1, True, [guarded]),
            ("knowledge corpus missing", missing(MODULE + "/internal/knowledge"), 1, True,
             [MODULE + "/internal/knowledge"]),
            ("other failure", [event(guarded, "output", "assertion failed\n", "TestRecipe"),
                               event(guarded, "fail", test="TestRecipe"),
                               event(guarded, "fail")], 1, False, []),
            ("missing and other failure", missing() + [event(other, "fail")], 1, False, [guarded]),
            ("guard text in failed test", [event(guarded, "run", test="TestRecipe")]
             + missing(), 1, False, []),
            ("guard text from other package", missing(other), 1, False, []),
            ("guard and build error", missing() + [event(other, "output", "build failed\n")],
             1, False, [guarded]),
            ("guard and extra error", [event(guarded, "output", "unexpected error\n")]
             + missing(), 1, False, []),
            ("guard and raw error", missing() + ["go: unexpected tool error"], 1, False, [guarded]),
            ("guard and tool failure", missing(), 2, False, [guarded]),
        ]
        lint = self.bin / "golangci-lint"
        lint.write_text('#!/bin/sh\necho "lint $*" >> "$CALLS"\nexit "${LINT_EXIT:-0}"\n')
        lint.chmod(0o755)
        (self.bin / "go").write_text(
            '#!/bin/sh\nif [ "$1" = list ]; then cat "$GRAPH"; else '
            'echo "go $*" >> "$CALLS"; cat "$EVENTS"; exit "$TEST_EXIT"; fi\n')
        with self.graph.open("a") as graph:
            for package in (guarded, MODULE + "/internal/knowledge"):
                name = package.removeprefix(MODULE + "/")
                graph.write("\n" + json.dumps({"ImportPath": package, "Dir": str(self.root / name)}))
        calls = self.root / "calls"
        events_file = self.root / "events"
        for title, events, test_exit, success, skipped in cases:
            with self.subTest(title=title):
                calls.unlink(missing_ok=True)
                events_file.write_text("\n".join(
                    json.dumps(value) if isinstance(value, dict) else value for value in events) + "\n")
                result = subprocess.run(
                    ["bash", str(ENTRY), "--files", "internal/content/atoms/example.md",
                     "internal/knowledge/documents.go"], cwd=self.root,
                    env=dict(self.env, CALLS=str(calls), EVENTS=str(events_file), TEST_EXIT=str(test_exit)),
                    capture_output=True, text=True)
                self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
                self.assertEqual(calls.read_text().splitlines()[0],
                                 "go test -short -json ./internal/authoring/recipe "
                                 "./internal/content ./internal/knowledge")
                for package in (guarded, MODULE + "/internal/knowledge", other):
                    notice = (package + ": not run locally: knowledge corpus not synced — "
                              "run make sync; CI runs it")
                    self.assertEqual(notice in result.stdout, package in skipped, result.stdout)
                self.assertEqual(any(line.startswith("lint ") for line in calls.read_text().splitlines()),
                                 success, result.stdout + result.stderr)

        # Even the accepted environment failure must not bypass native lint.
        events_file.write_text("\n".join(json.dumps(value) for value in missing()) + "\n")
        result = subprocess.run(
            ["bash", str(ENTRY), "--files", "internal/content/atoms/example.md"], cwd=self.root,
            env=dict(self.env, CALLS=str(calls), EVENTS=str(events_file), TEST_EXIT="1", LINT_EXIT="9"),
            capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()

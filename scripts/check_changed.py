"""Select local Go checks from imports, embedded inputs and filesystem readers.

Go supplies production and test import edges. Filesystem test readers are leaves:
editing a file they inspect changes their assertions, not their compiled exports.
Keeping those edges distinct avoids turning every edit into a topology-wide build.
Unknown ownership and global configuration deliberately select every package.
"""

import argparse
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys


# These cross-package reads are invisible to go list. Package-local resources
# (including new/deleted embeds and testdata) belong to their nearest package.
# Entries name test consumers, so they do not propagate through production imports.
TEST_READERS = (
    ("internal/content/templates/", ("internal/skillpacks", "internal/tools")),  # catalog / description drift
    ("internal/content/atoms/", ("internal/authoring/recipe",)),  # briefs_version_pin_lint_test.go
    ("docs/", ("internal/eval",)),  # verification_vocab_test.go spec anchors
    ("docs/zcprecipator3/system.md", ("internal/authoring/recipe",)),  # verdict table
    ("internal/knowledge/", ("internal/authoring/recipe",)),  # duplicated_teaching_lint_test.go
    ("internal/knowledge/recipes/", ("internal/workflow",)),  # recipe_override_shape_test.go
    ("internal/dataconsole/console/webui/dist/",
     ("internal/dataconsole/console", "internal/dataconsole/extension")),  # contract / studio_spa
    ("internal/dataconsole/extension/templates/vscode-studio/lib/consoleRoutes.js",
     ("internal/dataconsole/console/server",)),  # consoleroutes_test.go
    ("internal/tools/testdata/mate-contract/", ("internal/devsup",)),  # declaration_test.go
    ("internal/authoring/recipe/testdata/goldens/", ("internal/ops",)),  # dev_server_migrate_test.go
    ("internal/captureinspector/browsertest/",
     ("internal/captureinspector/internal/web",)),  # browser_smoke_test.go
    ("internal/eval/farm/observer/testdata/", ("internal/eval/farm/console",)),  # worker_test.go
    ("e2e/testdata/dataconsole/",
     ("internal/schema", "internal/dataconsole/console/provider/conformance")),  # version matrix
    ("eval/", ("internal/content", "internal/eval", "cmd/zcp")),  # scenario / gate-set readers
)
GLOBAL_FILES = {"go.mod", "go.sum", "go.work", "go.work.sum", "Makefile", "Dockerfile"}
SELECTOR_FILES = {"scripts/check-changed.sh", "scripts/check_changed.py",
                  "scripts/test_check_changed.py"}

# Exact TestMain diagnostic from knowledge.UnsyncedCorpusMessage. Match the
# guard's verdict, not a second filesystem count or a test's incidental text.
UNSYNCED_CORPUS_MESSAGE = (
    "knowledge corpus not synced — run `zcp sync pull` before testing "
    "(a fresh clone/worktree embeds only the mailpit recipe; CI pulls the corpus first).")
CORPUS_GUARDED_PACKAGES = ("./internal/knowledge", "./internal/authoring/recipe")


def output(*args):
    return subprocess.check_output(args, text=True)


def package_graph():
    # -e retains the inventory on a broken package; the plan then widens, and
    # go test/lint report the real error instead of silently omitting its owner.
    raw = output("go", "list", "-e", "-json", "./...")
    decoder = json.JSONDecoder()
    packages = []
    while raw.strip():
        package, end = decoder.raw_decode(raw.lstrip())
        packages.append(package)
        raw = raw.lstrip()[end:]
    if not packages:
        raise RuntimeError("go list returned no packages")
    return packages


def select(root, files, packages):
    by_dir = {Path(p["Dir"]).relative_to(root).as_posix(): p for p in packages}
    by_import = {p["ImportPath"]: directory for directory, p in by_dir.items()}
    reverse = {directory: set() for directory in by_dir}
    embeds = {}
    for directory, package in by_dir.items():
        for field in ("Imports", "TestImports", "XTestImports"):
            for imported in package.get(field, []):
                if imported in by_import:
                    reverse[by_import[imported]].add(directory)
        for field in ("EmbedFiles", "TestEmbedFiles", "XTestEmbedFiles"):
            for resource in package.get(field, []):
                embeds.setdefault(directory + "/" + resource, set()).add(directory)

    owners, readers, unresolved, wide = set(), set(), [], []
    if any(p.get("Error") or p.get("DepsErrors") for p in packages):
        wide.append("incomplete Go package graph")
    for file in files:
        path = PurePosixPath(file)
        if (file in GLOBAL_FILES or path.name.startswith(".golangci.")
                or file.startswith((".githooks/", ".github/", "tools/"))
                or path.suffix == ".mk"):
            wide.append(file)
        found = False
        # repo_drift_test.go scans tracked text, not just Go or Markdown.
        if "internal/content" in by_dir:
            readers.add("internal/content")
        if file.endswith(".go"):
            readers.update(("internal/eval", "internal/captureinspector"))  # architecture_test.go
            if file.startswith(("internal/", "cmd/")):
                readers.update(("internal/topology", "internal/dataconsole"))  # boundary scans
            if file.startswith("internal/tools/"):
                readers.add("internal/platform")  # project_admin_imports_test.go
            if file.startswith(("internal/tools/", "internal/server/", "internal/workflow/",
                                "internal/ops/", "internal/content/")):
                readers.add("internal/workflow")  # closed_at / atom_body_access lint
            if file.startswith("internal/") and file.endswith("_test.go"):
                readers.add("internal/authoring/recipe")  # verdict table test-title scan
            if file.startswith(("internal/ops/", "internal/workflow/",
                                "internal/topology/", "internal/envclass/")):
                readers.add("internal/tools")  # evalisms_lint_test.go
            if file in {"internal/tools/" + name + ".go" for name in (
                    "dev_server", "dev_server_migrate", "verify", "deploy_ssh",
                    "deploy_batch", "delete", "subdomain")} | {
                        "internal/platform/units.go", "cmd/zcp/main.go"}:
                readers.add("internal/ops")  # dev_server_test.go source assertions
        for prefix, consumers in TEST_READERS:
            if file == prefix or (prefix.endswith("/") and file.startswith(prefix)):
                readers.update(consumers)
                found = True
        if file in embeds:
            owners.update(embeds[file])
            found = True
        # Go ownership is directory-exact: a deleted nested package must not
        # become its surviving parent. Resources may live below their owner.
        parents = [path.parent] if file.endswith(".go") else path.parents
        if file.endswith(".go"):
            found = False
        for parent in parents:
            if parent.as_posix() in by_dir:
                owners.add(parent.as_posix())
                found = True
                break
        if file.endswith(".md") or file in SELECTOR_FILES:
            found = True
        if not found and file not in wide:
            unresolved.append(file)

    selected = set(owners)
    pending = list(owners)
    while pending:
        for consumer in reverse[pending.pop()] - selected:
            selected.add(consumer)
            pending.append(consumer)
    selected.update(readers & by_dir.keys())
    if wide or unresolved:
        selected = set(by_dir)
    return {"files": sorted(files), "packages": ["./" + p for p in sorted(selected)],
            "unresolved": sorted(unresolved), "wide": sorted(wide)}


def run_tests(selected, check_env):
    # JSON attributes interleaved output to its package and distinguishes a
    # TestMain guard from a named test that happens to print the same message.
    command = ["go", "test", "-short", "-json", *selected]
    states = {}
    unattributed_output = False
    with subprocess.Popen(command, env=check_env, stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT, text=True) as process:
        for line in process.stdout:
            try:
                event = json.loads(line)
            except ValueError:
                event = None
            if not isinstance(event, dict) or not event.get("Package"):
                print(line, end="", flush=True)
                unattributed_output = True
                continue
            package = event["Package"]
            state = states.setdefault(package, {"output": [], "tested": False, "result": None})
            state["tested"] |= "Test" in event
            if event.get("Action") == "output":
                text = event.get("Output", "")
                state["output"].append(text)
                print(text, end="", flush=True)
            if "Test" not in event and event.get("Action") in ("pass", "fail", "skip"):
                state["result"] = event["Action"]
        returncode = process.wait()

    skipped = []
    other_failure = unattributed_output
    for package, state in states.items():
        if state["result"] in ("pass", "skip"):
            continue
        guarded = any(path in selected and package.endswith("/" + path[2:])
                      for path in CORPUS_GUARDED_PACKAGES)
        exact_guard = re.fullmatch(
            re.escape(UNSYNCED_CORPUS_MESSAGE + "\nFAIL\t" + package + "\t") + r"[0-9.]+s\n",
            "".join(state["output"]))
        if guarded and state["result"] == "fail" and not state["tested"] and exact_guard:
            skipped.append(package)
            print(f"{package}: not run locally: knowledge corpus not synced — "
                  "run make sync; CI runs it", flush=True)
        else:
            other_failure = True
    if returncode and not (returncode == 1 and skipped and not other_failure):
        raise subprocess.CalledProcessError(returncode, command)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--plan", action="store_true", help="print the JSON plan without running checks")
    parser.add_argument("--base", help="compare a Git base with the working tree (default: staged diff)")
    parser.add_argument("--files", nargs="*", help="explicit repository-relative changed paths")
    args = parser.parse_args()
    root = Path(output("git", "rev-parse", "--show-toplevel").strip()).resolve()
    os.chdir(root)
    if args.files is not None:
        files = args.files
    else:
        revision = [args.base] if args.base else ["--cached"]
        files = output("git", "diff", *revision, "--name-only", "--no-renames", "-z").split("\0")
        files = [file for file in files if file]
    plan = select(root, files, package_graph()) if files else {
        "files": [], "packages": [], "unresolved": [], "wide": []}
    if args.plan:
        print(json.dumps(plan, indent=2))
        return
    for reason in plan["unresolved"]:
        print(f"Unresolved ownership: {reason}; selecting all packages", flush=True)
    for reason in plan["wide"]:
        print(f"Global input: {reason}; selecting all packages", flush=True)
    selected = plan["packages"]
    print("Changed-check packages: " + (" ".join(selected) or "none"), flush=True)
    # Honor the hook's index when selecting, then release its repository binding.
    # Tests create their own repositories; inheriting GIT_DIR/GIT_INDEX_FILE
    # would make their git commands mutate the caller's checkout instead.
    git_local = set(output("git", "rev-parse", "--local-env-vars").splitlines())
    check_env = {key: value for key, value in os.environ.items() if key not in git_local}
    if any(file in SELECTOR_FILES or file.startswith(".githooks/") for file in files):
        subprocess.run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "scripts",
                        "-p", "test_check_changed.py"], env=check_env, check=True)
    if not selected:
        return
    lint = str(root / "bin/golangci-lint")
    if not os.access(lint, os.X_OK):
        lint = shutil.which("golangci-lint")
    if not lint:
        raise RuntimeError("golangci-lint not found; run make setup")
    run_tests(selected, check_env)
    subprocess.run([lint, "run", *selected, "--timeout=120s"], env=check_env, check=True)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"check-changed: {error}", file=sys.stderr)
        sys.exit(1)

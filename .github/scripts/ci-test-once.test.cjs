const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

// These Go harnesses delegate to the actual JS files. Repeating their packages
// after ./... repeats every delegated file, even with a different -run filter.
test('CI runs delegated JS files once with dependencies required', () => {
  const workflow = fs.readFileSync(path.join(__dirname, '../workflows/ci.yml'), 'utf8');
  const runs = workflow.split(/\n      - /).filter(step => /go test\b/.test(step.replace(/^\s*#.*$/gm, '')));
  const liveGitCommand = "go test -race -tags livegit ./internal/ops -run '^(TestHQGit_AnHQThatNeverAcceptsEndsWithinTheBound|TestHQGit_ATransferIsBoundedByItsStallNotItsLength|TestReadOriginTip_StallingOrigin_EndsWithinItsBound)$'";
  const liveGit = runs.filter(step => step.includes(liveGitCommand));
  assert.equal(liveGit.length, 1, 'the separate live Git invocation runs only its transport witnesses');
  const delegated = runs.filter(step => !liveGit.includes(step));
  assert.equal(delegated.length, 1, 'one Go test invocation owns the delegated JS files');
  assert.match(delegated[0], /ZCP_JS_REQUIRED: ['"]?1['"]?/, 'the first invocation requires node and jsdom');
  assert.match(delegated[0], /go test -race \.\/\.\.\. -count=1/);
  assert.ok(workflow.indexOf('npm ci --prefix internal/dataconsole/console/webui') < workflow.indexOf('go test -race'), 'dependencies precede tests');
});

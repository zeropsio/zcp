const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

// These Go harnesses delegate to the actual JS files. Repeating their packages
// after ./... repeats every delegated file, even with a different -run filter.
test('CI runs delegated JS files once with dependencies required', () => {
  const workflow = fs.readFileSync(path.join(__dirname, '../workflows/ci.yml'), 'utf8');
  const runs = workflow.split(/\n      - /).filter(step => /go test\b/.test(step.replace(/^\s*#.*$/gm, '')));
  assert.equal(runs.length, 1, 'one Go test invocation owns the delegated JS files');
  assert.match(runs[0], /ZCP_JS_REQUIRED: ['"]?1['"]?/, 'the first invocation requires node and jsdom');
  assert.match(runs[0], /go test -race \.\/\.\.\. -count=1/);
  assert.ok(workflow.indexOf('npm ci --prefix internal/dataconsole/console/webui') < workflow.indexOf('go test -race'), 'dependencies precede tests');
});

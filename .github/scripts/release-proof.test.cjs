const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const selector = require('./select-corpus.cjs');

// Execute the release's real github-script consumer with finite API responses.
const workflow = readFileSync(join(__dirname, '../workflows/knowledge.yml'), 'utf8');
const script = workflow.split('          script: |\n')[1].split('\n      - name:')[0]
  .split('\n').map(line => line.slice(12)).join('\n');
const consume = new (Object.getPrototypeOf(async function () {}).constructor)(
  'github', 'context', 'core', 'process', 'require', script);
const sha = 'a'.repeat(40);
const digest = 'b'.repeat(64);
const run = { id: 12, run_attempt: 2, head_sha: sha, head_branch: 'main',
  head_repository: { full_name: 'zeropsio/zcp' }, event: 'push',
  status: 'completed', conclusion: 'success' };
const artifact = { name: `knowledge-v1-${sha}-12-2-${digest}`, expired: false,
  workflow_run: { id: 12, head_sha: sha } };

async function release({ runs = [run], updates = [], artifacts = [artifact], lint = 'success', apiError } = {}) {
  const outputs = {};
  const waits = [];
  const reads = [];
  const actions = { listWorkflowRuns: 'runs', listWorkflowRunArtifacts: 'artifacts',
    listJobsForWorkflowRunAttempt: 'jobs', getWorkflowRun: async params => {
      assert.equal(params.run_id, 12);
      assert.ok(updates.length, 'waiting must finish from the supplied verdicts');
      const data = updates.shift();
      runs = runs.map(item => item.id === data.id ? data : item);
      reads.push(data.status);
      return { data };
    } };
  const github = { rest: { actions }, paginate: async (method, params) => {
    if (apiError) throw new Error(apiError);
    if (method === 'runs') {
      assert.equal(params.workflow_id, 'ci.yml');
      assert.equal(params.head_sha, sha);
      return runs.filter(item => params.status !== 'success' || item.conclusion === 'success');
    }
    assert.equal(params.run_id, 12);
    if (method === 'jobs') {
      assert.equal(params.attempt_number, 2);
      return [{ name: 'test', conclusion: 'success' }, { name: 'lint', conclusion: lint }];
    }
    return artifacts;
  } };
  const wait = async ms => { assert.ok(ms > 0); waits.push(ms); };
  await consume(github, { repo: { owner: 'zeropsio', repo: 'zcp' } },
    { setOutput: (key, value) => { outputs[key] = value; }, warning() {}, info() {} },
    { env: { SOURCE_SHA: sha } }, () => ({ ...selector,
      requireCorpus: (...args) => selector.requireCorpus(...args, wait) }));
  return { outputs, waits, reads };
}

test('Release reuses finished green CI without waiting or starting tests', async () => {
  assert.deepEqual(await release(), { outputs: {
    'run-id': 12, artifact: artifact.name, digest,
  }, waits: [], reads: [] });
});

for (const status of ['in_progress', 'queued', 'waiting', 'pending', 'requested']) {
  test(`Release waits for ${status} CI and reuses its green verdict`, async () => {
    const pending = { ...run, status, conclusion: null };
    const result = await release({ runs: [pending], updates: [pending, run] });
    assert.equal(result.outputs.artifact, artifact.name);
    assert.equal(result.outputs.digest, digest);
    assert.equal(result.waits.length, 2, 'release waits instead of falling back to its own tests');
    assert.deepEqual(result.reads, [status, 'completed']);
  });
}

for (const conclusion of ['failure', 'cancelled', 'timed_out', 'skipped', 'neutral', 'action_required']) {
  test(`Release blocks a ${conclusion} CI verdict`, async () => {
    await assert.rejects(release({ runs: [{ ...run, status: 'in_progress', conclusion: null }],
      updates: [{ ...run, conclusion }] }), /CI.*proof|CI.*verdict/);
  });
}

for (const [scenario, options] of Object.entries({
  'no exact-SHA CI': { runs: [] },
  'failed completed CI': { runs: [{ ...run, conclusion: 'failure' }] },
  'different commit': { runs: [{ ...run, head_sha: 'c'.repeat(40) }] },
  'PR CI': { runs: [{ ...run, event: 'pull_request' }] },
  'foreign repository CI': { runs: [{ ...run, head_repository: { full_name: 'fork/zcp' } }] },
  'ordinary worktree CI': { runs: [{ ...run, head_branch: 'wt/other' }] },
  'missing corpus': { artifacts: [] },
  'expired corpus': { artifacts: [{ ...artifact, expired: true }] },
  'skipped lint': { lint: 'skipped' },
  'unavailable API': { apiError: 'API unavailable' },
})) {
  test(`Release blocks ${scenario} without starting fresh validation`, async () => {
    await assert.rejects(release(options));
  });
}

test('Release corpus download and digest verification fail closed', () => {
  for (const name of ['Download proven corpus', 'Verify proven corpus']) {
    const step = workflow.split(`      - name: ${name}\n`)[1].split('\n      - ')[0];
    assert.doesNotMatch(step, /continue-on-error:/, `${name} must block release on failure`);
  }
  const releaseWorkflow = readFileSync(join(__dirname, '../workflows/release.yml'), 'utf8');
  assert.match(releaseWorkflow, /validation:\s+uses: \.\/\.github\/workflows\/ci.yml\s+with:\s+reuse-ci: true/);
  assert.match(releaseWorkflow, /needs: validation/);
  const ci = readFileSync(join(__dirname, '../workflows/ci.yml'), 'utf8');
  for (const job of ['test', 'lint']) {
    const consumer = ci.split(`\n  ${job}:\n`)[1].split('\n  lint:')[0];
    assert.match(consumer, /if: needs\.knowledge\.outputs\.reused != 'true'/,
      `proven corpus skips release ${job}`);
  }
  assert.match(workflow, /reused: \$\{\{ steps\.verify\.outcome == 'success' \}\}/);
});

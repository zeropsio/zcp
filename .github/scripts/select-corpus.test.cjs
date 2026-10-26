const { test } = require('node:test');
const assert = require('node:assert/strict');
const { selectCorpus } = require('./select-corpus.cjs');

const sha = 'a'.repeat(40);
const digest = 'b'.repeat(64);
const repo = { owner: 'zeropsio', repo: 'zcp' };
const run = { id: 12, run_attempt: 2, head_sha: sha, head_branch: 'wt/keys',
  head_repository: { full_name: 'zeropsio/zcp' }, event: 'push',
  status: 'completed', conclusion: 'success' };
const artifact = { name: `knowledge-v1-${sha}-12-2-${digest}`, expired: false,
  workflow_run: { id: 12, head_sha: sha } };
const jobs = ['test', 'lint'].map(name => ({ name, conclusion: 'success' }));

async function select(runs = [run], artifacts = [artifact], coverage = jobs) {
  const actions = { listWorkflowRuns: 'runs', listWorkflowRunArtifacts: 'artifacts',
    listJobsForWorkflowRunAttempt: 'jobs' };
  const github = { rest: { actions }, paginate: async (method, params) => {
    if (method === 'runs') {
      assert.equal(params.workflow_id, 'ci.yml');
      assert.equal(params.head_sha, sha);
      assert.equal(params.status, 'success');
      return runs;
    }
    assert.equal(params.run_id, 12);
    if (method === 'jobs') {
      assert.equal(params.attempt_number, 2);
      return coverage;
    }
    return artifacts;
  } };
  return selectCorpus(github, repo, sha);
}

test('Exact successful integration SHA with full coverage returns the immutable corpus', async () => {
  assert.deepEqual(await select(), { runID: 12, artifactName: artifact.name, digest });
});

test('Main and manual dispatch can provide full CI proof', async () => {
  assert.ok(await select([{ ...run, head_branch: 'main', event: 'workflow_dispatch' }]));
});

for (const [scenario, patch] of Object.entries({
  'different SHA': { head_sha: 'c'.repeat(40) },
  'ordinary worktree': { head_branch: 'wt/zcp-ci' },
  'pull request': { event: 'pull_request' },
  'foreign repository': { head_repository: { full_name: 'fork/zcp' } },
  'unfinished run': { status: 'in_progress' },
  'failed run': { conclusion: 'failure' },
})) {
  test(`${scenario} cannot supply release proof`, async () => {
    assert.equal(await select([{ ...run, ...patch }]), null);
  });
}

for (const [scenario, patch] of Object.entries({
  'expired artifact': { expired: true },
  'previous attempt': { name: artifact.name.replace('-12-2-', '-12-1-') },
  'invalid digest': { name: artifact.name.slice(0, -1) },
  'wrong artifact SHA': { workflow_run: { id: 12, head_sha: 'c'.repeat(40) } },
})) {
  test(`${scenario} requires fresh validation`, async () => {
    assert.equal(await select([run], [{ ...artifact, ...patch }]), null);
  });
}

test('Missing corpus requires fresh validation', async () => {
  assert.equal(await select([run], []), null);
});

test('Skipped lint is not full CI proof', async () => {
  assert.equal(await select([run], [artifact], [jobs[0], { name: 'lint', conclusion: 'skipped' }]), null);
});

test('A newer ineligible run does not hide an eligible run', async () => {
  assert.ok(await select([{ ...run, id: 13, head_branch: 'wt/other' }, run]));
});

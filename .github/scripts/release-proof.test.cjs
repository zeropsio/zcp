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

const tree = 'd'.repeat(40);
const merged = 'e'.repeat(40);
const head = 'f'.repeat(40);
const prRun = { id: 30, run_attempt: 1, head_sha: head, head_branch: 'fix/card',
  head_repository: { full_name: 'zeropsio/zcp' }, event: 'pull_request',
  status: 'completed', conclusion: 'success' };
const prArtifact = { name: `knowledge-v1-${merged}-30-1-${digest}`, expired: false,
  workflow_run: { id: 30, head_sha: head } };
const tested = [{ name: 'test', conclusion: 'success' }, { name: 'lint', conclusion: 'success' }];

// mode 'commit' is the release (reuse-ci); mode 'tree' is a push to main (reuse-tree).
async function consumeProof({ mode = 'commit', runs = [run], updates = [], artifacts = [artifact],
  lint = 'success', jobs, pulls = [{ head: { sha: head } }], trees = { [merged]: tree }, apiError } = {}) {
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
  const git = { getCommit: async params => {
    if (!trees[params.commit_sha]) throw Object.assign(new Error('Not Found'), { status: 404 });
    return { data: { sha: params.commit_sha, tree: { sha: trees[params.commit_sha] } } };
  } };
  const repos = { listPullRequestsAssociatedWithCommit: 'pulls' };
  const github = { rest: { actions, git, repos }, paginate: async (method, params) => {
    if (apiError) throw new Error(apiError);
    if (method === 'pulls') {
      assert.equal(params.commit_sha, sha);
      return pulls;
    }
    if (method === 'runs') {
      assert.equal(params.workflow_id, 'ci.yml');
      if (mode === 'tree') assert.equal(params.event, 'pull_request');
      return runs.filter(item => item.head_sha === params.head_sha &&
        (params.status !== 'success' || item.conclusion === 'success'));
    }
    const owner = runs.find(item => item.id === params.run_id);
    assert.ok(owner, 'artifacts and jobs are read only from a listed run');
    if (method === 'jobs') {
      assert.equal(params.attempt_number, owner.run_attempt);
      return jobs ?? [tested[0], { name: 'lint', conclusion: lint }];
    }
    return artifacts;
  } };
  const wait = async ms => { assert.ok(ms > 0); waits.push(ms); };
  await consume(github, { repo: { owner: 'zeropsio', repo: 'zcp' } },
    { setOutput: (key, value) => { outputs[key] = value; }, warning() {}, info() {} },
    { env: { SOURCE_SHA: sha, SOURCE_TREE: tree, REUSE_MODE: mode } }, () => ({ ...selector,
      requireCorpus: (...args) => selector.requireCorpus(...args, wait) }));
  return { outputs, waits, reads };
}

const release = options => consumeProof(options);
const mainPush = options => consumeProof({ mode: 'tree', runs: [prRun], artifacts: [prArtifact], ...options });

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

const reusedMain = [
  { name: 'knowledge / prepare', conclusion: 'success',
    steps: [{ name: 'Verify proven corpus', conclusion: 'success' }] },
  { name: 'test', conclusion: 'skipped' }, { name: 'lint', conclusion: 'skipped' },
];

test('A main push whose tree passed CI on its pull request reuses that verdict', async () => {
  assert.deepEqual(await mainPush(), { outputs: {
    'run-id': 30, artifact: prArtifact.name, digest,
  }, waits: [], reads: [] });
});

for (const [scenario, options] of Object.entries({
  'red pull request CI': { runs: [{ ...prRun, conclusion: 'failure' }] },
  'unfinished pull request CI': { runs: [{ ...prRun, status: 'in_progress', conclusion: null }] },
  'no pull request': { pulls: [] },
  'a pull request CI of another tree': { trees: { [merged]: 'c'.repeat(40) } },
  'an unknown tested commit': { trees: {} },
  'a forked pull request CI': { runs: [{ ...prRun, head_repository: { full_name: 'fork/zcp' } }] },
  'a pull request CI with skipped lint': { lint: 'skipped' },
  'a pull request CI that itself reused a verdict': { jobs: reusedMain },
  'a pull request CI without its corpus': { artifacts: [] },
  'a pull request CI corpus from another attempt': { artifacts: [{ ...prArtifact, name: prArtifact.name.replace('-30-1-', '-30-2-') }] },
})) {
  test(`A main push with ${scenario} is tested in full`, async () => {
    assert.deepEqual(await mainPush(options), { outputs: {}, waits: [], reads: [] });
  });
}

test('A main push whose verdict lookup fails does not claim a verdict', async () => {
  await assert.rejects(mainPush({ apiError: 'API unavailable' }), /API unavailable/);
});

test('Release accepts its exact commit\'s green main run that reused a pull request verdict', async () => {
  assert.deepEqual(await release({ jobs: reusedMain }), { outputs: {
    'run-id': 12, artifact: artifact.name, digest,
  }, waits: [], reads: [] });
});

test('Release blocks a main run that skipped tests without a verified verdict', async () => {
  const unverified = [{ ...reusedMain[0], steps: [{ name: 'Verify proven corpus', conclusion: 'skipped' }] },
    ...reusedMain.slice(1)];
  await assert.rejects(release({ jobs: unverified }), /No green full CI proof/);
});

test('Only a push to main looks up a same-tree verdict', () => {
  const ci = readFileSync(join(__dirname, '../workflows/ci.yml'), 'utf8');
  assert.match(ci, /reuse-ci: \$\{\{ inputs\.reuse-ci \|\| false \}\}\n\s+reuse-tree: \$\{\{ github\.event_name == 'push' && github\.ref == 'refs\/heads\/main' \}\}/);
  assert.match(workflow, /if: inputs\.reuse-ci \|\| inputs\.reuse-tree\n/);
  assert.match(workflow, /REUSE_MODE: \$\{\{ inputs\.reuse-ci && 'commit' \|\| 'tree' \}\}/);
});

test('Release builds never wait on a Go cache restore', () => {
  const releaseWorkflow = readFileSync(join(__dirname, '../workflows/release.yml'), 'utf8');
  const setups = releaseWorkflow.split('- uses: actions/setup-go@').slice(1);
  assert.ok(setups.length >= 2, 'the build and release jobs set up Go');
  for (const setup of setups) {
    assert.match(setup.split('\n      - ')[0], /\n\s+cache: false\n/, 'setup-go restores no cache in a release');
  }
});

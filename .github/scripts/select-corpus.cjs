// A source SHA alone is not proof: require full CI coverage and the retained
// corpus from that successful run attempt. PR and release wrappers cannot attest.
// knowledge-v1-<checked-out commit>-<run>-<attempt>-<SHA-256>; a pull request
// run checks out its merge commit, so the name records the commit it tested.
const CORPUS = /^knowledge-v1-([a-f0-9]{40})-(\d+)-(\d+)-([a-f0-9]{64})$/;

function isOwnGreenRun(run, repo) {
  return run.head_repository?.full_name === `${repo.owner}/${repo.repo}` &&
    run.status === 'completed' && run.conclusion === 'success';
}

function isFullCIRun(run, repo, sha) {
  return run.head_sha === sha && ['push', 'workflow_dispatch'].includes(run.event) &&
    ['main', 'wt/keys'].includes(run.head_branch) &&
    run.head_repository?.full_name === `${repo.owner}/${repo.repo}`;
}

async function runCorpus(github, repo, run) {
  const artifacts = await github.paginate(github.rest.actions.listWorkflowRunArtifacts, {
    ...repo, run_id: run.id, per_page: 100,
  });
  for (const item of artifacts) {
    const match = CORPUS.exec(item.name);
    if (item.expired || !match || match[2] !== String(run.id) || match[3] !== String(run.run_attempt) ||
      item.workflow_run?.id !== run.id || item.workflow_run?.head_sha !== run.head_sha) continue;
    return { source: match[1], proof: { runID: run.id, artifactName: item.name, digest: match[4] } };
  }
  return null;
}

// 'tested' when test and lint passed in this attempt; 'reused' when its
// preparation verified an earlier green verdict, which is why they were skipped.
async function attemptVerdict(github, repo, run) {
  const jobs = await github.paginate(github.rest.actions.listJobsForWorkflowRunAttempt, {
    ...repo, run_id: run.id, attempt_number: run.run_attempt, per_page: 100,
  });
  if (['test', 'lint'].every(name => jobs.some(job => job.name === name && job.conclusion === 'success'))) return 'tested';
  const prepare = jobs.find(job => job.name === 'knowledge / prepare' && job.conclusion === 'success');
  return prepare?.steps?.some(step => step.name === 'Verify proven corpus' && step.conclusion === 'success') ? 'reused' : null;
}

async function selectCorpus(github, repo, sha) {
  const runs = await github.paginate(github.rest.actions.listWorkflowRuns, {
    ...repo, workflow_id: 'ci.yml', head_sha: sha, status: 'success', per_page: 100,
  });
  for (const run of runs) {
    if (!isFullCIRun(run, repo, sha) || !isOwnGreenRun(run, repo)) continue;
    const corpus = await runCorpus(github, repo, run);
    if (!corpus || corpus.source !== sha || !await attemptVerdict(github, repo, run)) continue;
    return corpus.proof;
  }
  return null;
}

// A gone commit is an unknown tree, never a match.
async function commitTree(github, repo, sha) {
  try {
    return (await github.rest.git.getCommit({ ...repo, commit_sha: sha })).data.tree.sha;
  } catch (error) {
    if (error.status === 404 || error.status === 422) return null;
    throw error;
  }
}

// A push to main reuses the green verdict of a pull request that tested this
// exact tree. Absence is not an error: the caller then tests the tree in full.
async function selectTreeCorpus(github, repo, sha, tree) {
  const pulls = await github.paginate(github.rest.repos.listPullRequestsAssociatedWithCommit, {
    ...repo, commit_sha: sha, per_page: 100,
  });
  for (const pull of pulls) {
    const runs = await github.paginate(github.rest.actions.listWorkflowRuns, {
      ...repo, workflow_id: 'ci.yml', event: 'pull_request', head_sha: pull.head.sha, status: 'success', per_page: 100,
    });
    for (const run of runs) {
      if (run.event !== 'pull_request' || run.head_sha !== pull.head.sha || !isOwnGreenRun(run, repo)) continue;
      const corpus = await runCorpus(github, repo, run);
      if (!corpus || await commitTree(github, repo, corpus.source) !== tree) continue;
      if (await attemptVerdict(github, repo, run) !== 'tested') continue;
      return corpus.proof;
    }
  }
  return null;
}

// Release consumes CI's verdict; absence of proof must never start a second suite.
async function requireCorpus(github, repo, sha, wait = ms => new Promise(resolve => setTimeout(resolve, ms))) {
  const proof = await selectCorpus(github, repo, sha);
  if (proof) return proof;
  const runs = await github.paginate(github.rest.actions.listWorkflowRuns, {
    ...repo, workflow_id: 'ci.yml', head_sha: sha, per_page: 100,
  });
  let run = runs.find(item => isFullCIRun(item, repo, sha) && item.status !== 'completed');
  while (run && run.status !== 'completed') {
    await wait(15000);
    ({ data: run } = await github.rest.actions.getWorkflowRun({ ...repo, run_id: run.id }));
  }
  if (run && run.conclusion !== 'success') throw new Error(`CI verdict for ${sha}: ${run.conclusion}`);
  const completedProof = await selectCorpus(github, repo, sha);
  if (!completedProof) throw new Error(`No green full CI proof for ${sha}`);
  return completedProof;
}

module.exports = { selectCorpus, selectTreeCorpus, requireCorpus };

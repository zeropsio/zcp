// A source SHA alone is not proof: require full CI coverage and the retained
// corpus from that successful run attempt. PR and release wrappers cannot attest.
async function selectCorpus(github, repo, sha) {
  const runs = await github.paginate(github.rest.actions.listWorkflowRuns, {
    ...repo, workflow_id: 'ci.yml', head_sha: sha, status: 'success', per_page: 100,
  });
  for (const run of runs) {
    if (run.head_sha !== sha || run.status !== 'completed' || run.conclusion !== 'success' ||
        !['push', 'workflow_dispatch'].includes(run.event) ||
        !['main', 'wt/keys'].includes(run.head_branch) ||
        run.head_repository?.full_name !== `${repo.owner}/${repo.repo}`) continue;
    const artifacts = await github.paginate(github.rest.actions.listWorkflowRunArtifacts, {
      ...repo, run_id: run.id, per_page: 100,
    });
    const prefix = `knowledge-v1-${sha}-${run.id}-${run.run_attempt}-`;
    const artifact = artifacts.find(item => !item.expired && item.name.startsWith(prefix) &&
      /^[a-f0-9]{64}$/.test(item.name.slice(prefix.length)) &&
      item.workflow_run?.id === run.id && item.workflow_run?.head_sha === sha);
    if (!artifact) continue;
    const jobs = await github.paginate(github.rest.actions.listJobsForWorkflowRunAttempt, {
      ...repo, run_id: run.id, attempt_number: run.run_attempt, per_page: 100,
    });
    if (!['test', 'lint'].every(name => jobs.some(job => job.name === name && job.conclusion === 'success'))) continue;
    return { runID: run.id, artifactName: artifact.name, digest: artifact.name.slice(prefix.length) };
  }
  return null;
}
module.exports = { selectCorpus };

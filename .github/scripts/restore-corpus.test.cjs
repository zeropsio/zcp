const { test } = require('node:test');
const assert = require('node:assert/strict');
const { createHash } = require('node:crypto');
const { mkdtempSync, mkdirSync, readFileSync, writeFileSync, existsSync, rmSync } = require('node:fs');
const { tmpdir } = require('node:os');
const { join } = require('node:path');
const { spawnSync } = require('node:child_process');

// Exercise the shell actually used by all three workflow consumers.
const action = readFileSync(join(__dirname, '../actions/restore-knowledge/action.yml'), 'utf8');
const restore = action.split('      run: |\n')[1].split('\n').map(line => line.slice(8)).join('\n');

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'zcp-corpus-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const source = join(root, 'source');
  const corpus = join(root, 'artifact');
  const workspace = join(root, 'workspace');
  const knowledge = join(workspace, 'internal/knowledge');
  mkdirSync(corpus);
  for (const dir of ['themes', 'bases', 'recipes', 'guides', 'decisions', 'playbooks']) {
    mkdirSync(join(source, dir), { recursive: true });
    mkdirSync(join(knowledge, dir), { recursive: true });
  }
  writeFileSync(join(source, 'recipes/mailpit.md'), 'tracked override\n');
  writeFileSync(join(source, 'recipes/example.import.yml'), 'project: fixture\n');
  writeFileSync(join(source, 'guides/.hidden'), 'embedded by all:guides\n');
  writeFileSync(join(knowledge, 'recipes/stale.md'), 'must disappear\n');
  const archive = join(corpus, 'corpus.tar');
  const packed = spawnSync('tar', ['-cf', archive, '-C', source, '.']);
  assert.equal(packed.status, 0, packed.stderr.toString());
  const digest = createHash('sha256').update(readFileSync(archive)).digest('hex');
  return { source, corpus, workspace, knowledge, digest };
}

function runRestore(f, digest) {
  return spawnSync('bash', ['-c', restore], { encoding: 'utf8', env: {
    ...process.env, GITHUB_WORKSPACE: f.workspace, CORPUS_DIR: f.corpus, CORPUS_DIGEST: digest,
  } });
}

test('Verified corpus replaces stale files and preserves exact embedded bytes', t => {
  const f = fixture(t);
  const result = runRestore(f, f.digest);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(existsSync(join(f.knowledge, 'recipes/stale.md')), false);
  for (const file of ['recipes/mailpit.md', 'recipes/example.import.yml', 'guides/.hidden']) {
    assert.deepEqual(readFileSync(join(f.knowledge, file)), readFileSync(join(f.source, file)));
  }
});

test('Digest mismatch blocks extraction before touching the checkout', t => {
  const f = fixture(t);
  writeFileSync(join(f.corpus, 'corpus.tar'), 'tampered');
  const result = runRestore(f, f.digest);
  assert.notEqual(result.status, 0);
  assert.equal(readFileSync(join(f.knowledge, 'recipes/stale.md'), 'utf8'), 'must disappear\n');
  assert.equal(existsSync(join(f.knowledge, 'recipes/mailpit.md')), false);
});

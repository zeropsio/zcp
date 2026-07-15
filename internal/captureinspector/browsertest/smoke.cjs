'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

let activeBrowser;

function loadPlaywright() {
  const override = process.env.ZCP_PLAYWRIGHT_MODULE;
  return require(override || 'playwright');
}

async function assertFlowUsesPageScroll(page, label) {
  const metrics = await page.locator('.flow-viewport').evaluate(element => ({
    overflowX: getComputedStyle(element).overflowX,
    overflowY: getComputedStyle(element).overflowY,
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
    clientHeight: element.clientHeight,
    scrollHeight: element.scrollHeight,
  }));
  assert.equal(metrics.overflowX, 'visible', `${label}: flow must not own horizontal scrolling`);
  assert.equal(metrics.overflowY, 'visible', `${label}: flow must not own vertical scrolling`);
  assert.ok(metrics.scrollWidth <= metrics.clientWidth + 1, `${label}: flow content must fit its page width`);
  assert.ok(metrics.scrollHeight <= metrics.clientHeight + 1, `${label}: flow content must expand the page`);
}

async function main() {
  const launchURL = process.argv[2];
  if (!launchURL) throw new Error('usage: node smoke.cjs <one-time-launch-url>');
  const output = process.env.ZCP_BROWSER_OUTPUT || path.join(__dirname, 'screenshots');
  fs.mkdirSync(output, {recursive: true});

  const {chromium} = loadPlaywright();
  activeBrowser = await chromium.launch({headless: true});
  const context = await activeBrowser.newContext({viewport: {width: 1440, height: 1000}});
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on('pageerror', error => errors.push(`page: ${error.message}`));
  page.on('console', message => {
    if (message.type() === 'error') errors.push(`console: ${message.text()}`);
  });
  page.on('dialog', dialog => dialog.accept());

  await page.goto(launchURL, {waitUntil: 'networkidle'});
  await page.locator('.flow-node').first().waitFor();
  await assertFlowUsesPageScroll(page, '1440px flow');
  assert.equal(new URL(page.url()).pathname, '/', 'one-time capability must leave the current URL');
  assert.equal(await page.locator('[style]').count(), 0, 'strict CSP must not require inline styles');

  const captureID = await page.locator('#capture-select').inputValue();
  const preReveal = await context.request.get(new URL(`/api/v1/captures/${encodeURIComponent(captureID)}/raw?file=provider.jsonl&seq=1`, page.url()).href);
  const preRevealStatus = preReveal.status();
  assert.equal(preRevealStatus, 403, 'raw plaintext must remain reveal-gated');

  await page.getByRole('button', {name: 'Cards', exact: true}).click();
  await page.locator('.trace-card').first().waitFor();
  await page.getByRole('button', {name: 'Flow map', exact: true}).click();
  await page.locator('.flow-edge-hit').first().focus();
  await page.keyboard.press('Enter');
  await page.getByText('EVIDENCE LINK', {exact: true}).waitFor();
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('.flow-inspector').count(), 0, 'Escape must close the selected flow sidebar');

  await page.getByRole('button', {name: 'Reveal plaintext', exact: true}).click();
  await page.getByRole('button', {name: 'Plaintext enabled', exact: true}).waitFor();

  await page.getByRole('button', {name: 'Flow map', exact: true}).click();
  await page.locator('.flow-node.lane-context').first().click();
  await page.getByRole('button', {name: 'Open formatted model context'}).click();
  await page.getByText('FULL MODEL CONTEXT', {exact: true}).waitFor();
  assert.equal(await page.evaluate(() => window.__zcpXSS), undefined, 'captured markup must never execute');
  assert.equal(await page.locator('img[src="x"]').count(), 0, 'captured markup must remain text');
  assert.equal(await page.locator('.flow-detail-workspace').count(), 1, 'flow detail must use one inspector surface');
  const nestedFlowScrollbars = await page.locator('.flow-detail-workspace pre,.flow-detail-workspace .json-tree').evaluateAll(elements =>
    elements.filter(element => element.scrollHeight > element.clientHeight + 2 && ['auto', 'scroll'].includes(getComputedStyle(element).overflowY)).length);
  assert.equal(nestedFlowScrollbars, 0, 'flow workspace must own vertical scrolling');
  await page.screenshot({path: path.join(output, 'flow-detail-1440.png'), fullPage: false});

  await page.getByRole('button', {name: 'Split', exact: true}).click();
  await page.locator('.flow-inspector.split').waitFor();
  await page.setViewportSize({width: 1024, height: 900});
  await assertFlowUsesPageScroll(page, '1024px split');
  const responsiveSidebar = await page.locator('.flow-inspector').evaluate(element => ({
    position: getComputedStyle(element).position,
    box: element.getBoundingClientRect().toJSON(),
  }));
  assert.equal(responsiveSidebar.position, 'fixed', 'selected flow detail must use a viewport sidebar');
  assert.ok(responsiveSidebar.box.top >= 0 && responsiveSidebar.box.bottom <= 901, 'flow sidebar must be immediately visible after selection');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, '1024px layout must not overflow the page');
  await page.screenshot({path: path.join(output, 'split-1024.png'), fullPage: false});
  await page.getByRole('button', {name: 'Close flow detail'}).click();
  assert.equal(await page.locator('.flow-inspector').count(), 0, 'closing Split detail must restore the unobstructed flow map');

  await page.locator('[data-tab="context"]').click();
  await page.locator('.context-row').first().click();
  await page.locator('.drawer.context-workspace.open').waitFor();
  assert.equal(await page.locator('.drawer.open').count(), 1, 'global evidence detail must not stack drawers');
  await page.setViewportSize({width: 2560, height: 1200});
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1), false, '2560px layout must not overflow the page');
  await page.screenshot({path: path.join(output, 'drawer-2560.png'), fullPage: false});
  await page.getByRole('button', {name: 'Close evidence drawer'}).click();

  await page.locator('[data-tab="metrics"]').click();
  const metricEvidence = page.locator('section.panel [data-evidence]').first();
  await metricEvidence.waitFor();
  assert.match(await metricEvidence.textContent(), /^[1-9][0-9]* refs$/, 'known metrics must expose evidence coordinates');
  await metricEvidence.click();
  await page.getByRole('dialog', {name: 'Evidence detail'}).waitFor();
  await page.getByRole('button', {name: 'Close evidence drawer'}).click();

  const captureIDs = await page.locator('#capture-select option').evaluateAll(options => options.map(option => option.value));
  const acceptance = [];
  for (const id of captureIDs) {
    await page.locator('#capture-select').selectOption(id);
    await page.waitForURL(url => new URL(url).searchParams.get('capture') === id);
    await page.locator('[data-tab="trace"]').click();
    await page.getByRole('button', {name: 'Cards', exact: true}).click();
    await page.locator('.trace-card').first().waitFor();
    const cards = await page.locator('.trace-card').count();
    await page.getByRole('button', {name: 'Flow map', exact: true}).click();
    await page.locator('.flow-node').first().waitFor();
    const nodes = await page.locator('.flow-node').count();
    const edges = await page.locator('.flow-edge-hit').count();
    const errorNodes = await page.locator('.flow-node.status-error').count();
    const differentEdges = await page.locator('.flow-edge-group.status-different').count();
    const rewriteEdges = await page.locator('.flow-edge-group.status-rewritten,.flow-edge-group.status-reset').count();
    const phases = await page.locator('.flow-phase-band').count();
    const lastNode = page.locator('.flow-node').last();
    await lastNode.scrollIntoViewIfNeeded();
    const pageScrollBeforeSelection = await page.evaluate(() => window.scrollY);
    await lastNode.click();
    await page.locator('.flow-node.selected').waitFor();
    const pageScrollAfterSelection = await page.evaluate(() => window.scrollY);
    const selectedSidebar = await page.locator('.flow-inspector').evaluate(element => ({position: getComputedStyle(element).position, top: element.getBoundingClientRect().top, focused: document.activeElement === element}));
    assert.ok(Math.abs(pageScrollAfterSelection - pageScrollBeforeSelection) <= 2, `capture ${id}: node selection must preserve document scroll`);
    assert.equal(selectedSidebar.position, 'fixed', `capture ${id}: selected detail must open in a fixed sidebar`);
    assert.ok(selectedSidebar.top >= 0 && selectedSidebar.top <= 67, `capture ${id}: selected sidebar must be visible immediately`);
    assert.equal(selectedSidebar.focused, true, `capture ${id}: selected sidebar must receive focus without scrolling`);
    await page.getByRole('button', {name: 'Split', exact: true}).click();
    await page.locator('.flow-inspector.split').waitFor();
    assert.ok(cards > 0 && nodes > 0 && edges > 0, `capture ${id} must project Cards and Flow evidence`);
    acceptance.push({id, cards, nodes, edges, errorNodes, differentEdges, rewriteEdges, phases, pageScrollAfterSelection});
  }

  assert.deepEqual(errors, [], 'browser console and page errors');
  await activeBrowser.close();
  activeBrowser = undefined;
  process.stdout.write(JSON.stringify({captureID, acceptance, screenshots: output, errors}, null, 2) + '\n');
}

main().catch(async error => {
  console.error(error);
  if (activeBrowser) await activeBrowser.close().catch(() => {});
  process.exitCode = 1;
});

#!/usr/bin/env node
/* Optional UI smoke: requires Playwright and its Chromium installation. */
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { spawn } = require('node:child_process');
const { randomBytes } = require('node:crypto');
const fs = require('node:fs/promises');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');

async function main() {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'agent-post-ui-'));
  const password = randomBytes(24).toString('hex');
  const port = await new Promise(resolve => {
    const listener = net.createServer();
    listener.listen(0, '127.0.0.1', () => { const port = listener.address().port; listener.close(() => resolve(port)); });
  });
  const base = `http://127.0.0.1:${port}`;
  const binary = process.env.AGENT_POST_BINARY || path.resolve(__dirname, '..', 'bin', process.platform === 'win32' ? 'agent-post.exe' : 'agent-post');
  const server = spawn(binary, ['serve', '--data-dir', path.join(directory, 'data'), '--listen', `127.0.0.1:${port}`], { env: { ...process.env, AGENT_POST_ADMIN_PASSWORD: password }, stdio: ['ignore', 'ignore', 'pipe'] });
  let diagnostic = '';
  server.stderr.on('data', bytes => { diagnostic += bytes.toString(); });
  let spawnError;
  const closed = new Promise(resolve => server.once('close', resolve));
  server.on('error', error => { spawnError = error; diagnostic = error.message; });
  let browser;
  try {
    let ready = false;
    for (let i = 0; i < 100; i++) {
      try { ready = (await fetch(`${base}/healthz`)).ok; if (ready) break; } catch {}
      if (spawnError || server.exitCode !== null) throw new Error(diagnostic);
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert.ok(ready, 'service starts');
    browser = await chromium.launch({ headless: true, ...(process.env.AGENT_POST_CHROMIUM ? { executablePath: process.env.AGENT_POST_CHROMIUM } : {}) });
    const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 }, permissions: ['clipboard-read', 'clipboard-write'] });
    const page = await context.newPage();
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    await page.goto(`${base}/admin/`);
    await page.getByLabel('Administrator password').fill(password);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await page.getByRole('heading', { name: 'Overview', exact: true }).waitFor();
    await page.getByRole('link', { name: 'Hosts', exact: true }).click();
    await page.getByRole('button', { name: 'Register host', exact: true }).click();
    assert.equal(await page.evaluate(() => document.activeElement.id), 'host-name', 'host dialog focuses its first field');
    await page.getByLabel('Host name', { exact: true }).fill('fixture-host');
    await page.getByLabel('Operating system', { exact: true }).fill('Test OS');
    await page.getByLabel('Allowed IP addresses', { exact: true }).fill('127.0.0.1\n::1');
    await page.getByRole('button', { name: 'Save host', exact: true }).click();
    await page.locator('#host-rows .cell-name', { hasText: 'fixture-host' }).waitFor();
    for (const name of ['zeta-host', 'Alpha-host', 'host-10', 'host-9']) {
      const created = await fetch(`${base}/admin/api/hosts`, { method: 'POST', headers: { Authorization: `Bearer ${password}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ name, os: '', ips: ['192.0.2.1'], metadata: {} }) });
      assert.equal(created.status, 200);
    }
    const hostOrder = () => page.locator('#host-rows tr').evaluateAll(rows => rows.map(row => row.cells[0].firstChild.textContent));
    const expectedOrder = ['Alpha-host', 'fixture-host', 'host-9', 'host-10', 'zeta-host'];
    for (let i = 0; i < 3; i++) {
      await page.locator('#refresh').click();
      await page.waitForFunction(() => document.querySelectorAll('#host-rows tr').length === 5);
      assert.deepEqual(await hostOrder(), expectedOrder, 'rows keep a stable, natural name order across refreshes');
    }
    await page.locator('#page-hosts th[data-sort=name] button').click();
    assert.deepEqual(await hostOrder(), [...expectedOrder].reverse(), 'name header toggles descending order');
    assert.equal(await page.locator('#page-hosts th[data-sort=name]').getAttribute('aria-sort'), 'descending');
    await page.locator('#page-hosts th[data-sort=name] button').click();
    await page.getByRole('searchbox', { name: 'Filter hosts' }).fill('HOST-1');
    assert.deepEqual(await hostOrder(), ['host-10'], 'filter matches names case-insensitively');
    assert.equal((await page.locator('#page-hosts .list-count').textContent()).trim(), '1 of 5');
    await page.getByRole('searchbox', { name: 'Filter hosts' }).fill('192.0.2.1');
    assert.equal((await hostOrder()).length, 4, 'filter matches allowed IP addresses');
    await page.getByRole('searchbox', { name: 'Filter hosts' }).fill('');
    await page.getByRole('combobox', { name: 'Status filter' }).first().selectOption('disabled');
    await page.getByText('No results match the filter.', { exact: true }).waitFor();
    await page.getByRole('combobox', { name: 'Status filter' }).first().selectOption('');
    assert.equal((await hostOrder()).length, 5);
    await page.getByRole('link', { name: 'Agents', exact: true }).click();
    await page.getByRole('button', { name: 'Create agent', exact: true }).click();
    await page.getByLabel('Agent name', { exact: true }).fill('fixture-agent');
    await page.getByRole('button', { name: 'Save agent', exact: true }).click();
    await page.getByRole('heading', { name: 'Agent API key', exact: true }).waitFor();
    const oldKey = await page.getByLabel('New API key', { exact: true }).inputValue();
    assert.ok(oldKey.startsWith('apk_'));
    assert.equal(await page.evaluate(() => document.activeElement.id), 'copy-key', 'key dialog focuses the copy action');
    await page.locator('#copy-key').click();
    await page.getByRole('button', { name: 'Copied', exact: true }).waitFor();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), oldKey, 'copy places the issued key on the clipboard');
    await page.getByRole('button', { name: 'I saved the key', exact: true }).click();
    await page.locator('#agent-rows .cell-name', { hasText: 'fixture-agent' }).waitFor();
    const agentMenu = page.getByRole('button', { name: 'Actions for fixture-agent', exact: true });
    await agentMenu.focus();
    await page.keyboard.press('Enter');
    assert.equal(await page.evaluate(() => document.activeElement.textContent), 'Rotate key', 'menu focuses its first visible item');
    await page.keyboard.press('ArrowDown');
    assert.equal(await page.evaluate(() => document.activeElement.textContent), 'Revoke key');
    await page.keyboard.press('Escape');
    assert.equal(await agentMenu.getAttribute('aria-expanded'), 'false');
    assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), 'Actions for fixture-agent', 'Escape returns focus to the menu button');
    await agentMenu.click();
    await page.getByRole('menuitem', { name: 'Rotate key', exact: true }).click();
    assert.equal(await page.locator('#confirm-subject').textContent(), 'fixture-agent', 'confirmation names its target');
    assert.equal(await page.locator('#confirm-action').getAttribute('class'), 'danger', 'destructive confirmations use the danger style');
    assert.equal(await page.evaluate(() => document.activeElement.textContent), 'Cancel', 'confirmation dialogs focus the safe choice');
    await page.locator('#confirm-action').click();
    await page.getByRole('heading', { name: 'Agent API key', exact: true }).waitFor();
    assert.notEqual(await page.getByLabel('New API key', { exact: true }).inputValue(), oldKey);
    assert.equal((await fetch(`${base}/api/v1/me`, { headers: { Authorization: `Bearer ${oldKey}` } })).status, 401);
    await page.getByRole('button', { name: 'I saved the key', exact: true }).click();
    await page.waitForFunction(() => document.querySelector('#issued-key').value === '');
    assert.equal(await page.locator('#issued-key').inputValue(), '');
    await agentMenu.click();
    await page.getByRole('menuitem', { name: 'Disable', exact: true }).click();
    await page.locator('#confirm-action').click();
    await page.locator('#agent-rows .status.off').waitFor();
    await agentMenu.click();
    await page.getByRole('menuitem', { name: 'Enable', exact: true }).click();
    assert.equal(await page.locator('#confirm-action').getAttribute('class'), 'primary', 'enabling is not styled as destructive');
    await page.locator('#confirm-action').click();
    await page.locator('#agent-rows .status:not(.off)').waitFor();
    await page.getByRole('link', { name: 'Limits & retention', exact: true }).click();
    await page.getByLabel('Messages per channel', { exact: true }).fill('12');
    await page.getByRole('button', { name: 'Save limits', exact: true }).click();
    await page.locator('#confirm-action').click();
    await page.getByText('Settings revision 2', { exact: true }).waitFor();
    await page.reload();
    await page.getByLabel('Messages per channel', { exact: true }).waitFor();
    assert.equal(await page.getByLabel('Messages per channel', { exact: true }).inputValue(), '12');
    await page.getByRole('link', { name: 'Overview', exact: true }).click();
    if (process.env.AGENT_POST_SCREENSHOT_DIR) {
      await fs.mkdir(process.env.AGENT_POST_SCREENSHOT_DIR, { recursive: true });
      await page.screenshot({ path: path.join(process.env.AGENT_POST_SCREENSHOT_DIR, 'admin-desktop.png'), fullPage: true });
    }
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'mobile overview has no horizontal page overflow');
    if (process.env.AGENT_POST_SCREENSHOT_DIR) await page.screenshot({ path: path.join(process.env.AGENT_POST_SCREENSHOT_DIR, 'admin-mobile.png'), fullPage: true });
    await page.getByRole('link', { name: 'Agents', exact: true }).click();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'mobile agent cards fit the viewport');
    assert.equal(await page.locator('#page-agents thead').isVisible(), false, 'phones show cards instead of a table header');
    const toggleBox = await agentMenu.boundingBox();
    assert.ok(toggleBox && toggleBox.x + toggleBox.width <= 390 && toggleBox.height >= 40, 'card actions are visible and touch-sized without horizontal scrolling');
    await agentMenu.click();
    const sheet = await page.getByRole('menu', { name: 'Actions for fixture-agent' }).boundingBox();
    assert.ok(sheet.y + sheet.height > 780 && sheet.width > 360, 'row actions open as a bottom sheet');
    await page.getByRole('menuitem', { name: 'Edit', exact: true }).waitFor();
    await page.keyboard.press('Escape');
    assert.equal(await page.getByRole('button', { name: 'Sign out', exact: true }).isVisible(), false, 'mobile account controls stay in the menu');
    await page.getByRole('button', { name: 'More', exact: true }).click();
    assert.equal(await page.locator('#menu-toggle').getAttribute('aria-expanded'), 'true');
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await page.getByLabel('Administrator password').waitFor();
    assert.deepEqual(errors, []);
    console.log(JSON.stringify({ browser: await browser.version(), viewport: [1440, 390], passed: ['login', 'hosts', 'stable row order', 'dialog initial focus', 'agents', 'filter, sort and status filter', 'key copy', 'keyboard row menus', 'key rotation and old-key rejection', 'disable and enable', 'settings', 'session reload', 'mobile cards and action sheet', 'logout'], pageErrors: errors }));
  } finally {
    if (browser) await browser.close();
    if (server.pid && server.exitCode === null) server.kill('SIGTERM');
    let timer;
    await Promise.race([closed, new Promise(resolve => { timer = setTimeout(() => { server.kill('SIGKILL'); resolve(); }, 5000); })]);
    clearTimeout(timer);
    await fs.rm(directory, { recursive: true, force: true });
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });

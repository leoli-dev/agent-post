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
    const page = await browser.newPage({ locale: 'en-US', viewport: { width: 1440, height: 1000 } });
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    await page.goto(`${base}/admin/`);
    await page.getByLabel('Administrator password').fill(password);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await page.getByRole('heading', { name: 'Ready when your agents are.' }).waitFor();
    await page.getByRole('link', { name: 'Hosts', exact: true }).click();
    await page.getByRole('button', { name: 'Register host', exact: true }).click();
    await page.getByLabel('Host name', { exact: true }).fill('fixture-host');
    await page.getByLabel('Operating system', { exact: true }).fill('Test OS');
    await page.getByLabel('Allowed IP addresses', { exact: true }).fill('127.0.0.1\n::1');
    await page.getByRole('button', { name: 'Save host', exact: true }).click();
    await page.getByRole('cell', { name: 'fixture-host', exact: false }).waitFor();
    await page.getByRole('link', { name: 'Agents', exact: true }).click();
    await page.getByRole('button', { name: 'Create agent', exact: true }).click();
    await page.getByLabel('Agent name', { exact: true }).fill('fixture-agent');
    await page.getByRole('button', { name: 'Save agent', exact: true }).click();
    await page.getByRole('heading', { name: 'Agent API key', exact: true }).waitFor();
    const oldKey = await page.getByLabel('New API key', { exact: true }).inputValue();
    assert.ok(oldKey.startsWith('apk_'));
    await page.getByRole('button', { name: 'I saved the key', exact: true }).click();
    await page.getByRole('cell', { name: 'fixture-agent', exact: false }).waitFor();
    await page.getByRole('button', { name: 'Rotate key', exact: true }).click();
    await page.locator('#confirm-action').click();
    await page.getByRole('heading', { name: 'Agent API key', exact: true }).waitFor();
    assert.notEqual(await page.getByLabel('New API key', { exact: true }).inputValue(), oldKey);
    assert.equal((await fetch(`${base}/api/v1/me`, { headers: { Authorization: `Bearer ${oldKey}` } })).status, 401);
    await page.getByRole('button', { name: 'I saved the key', exact: true }).click();
    await page.waitForFunction(() => document.querySelector('#issued-key').value === '');
    assert.equal(await page.locator('#issued-key').inputValue(), '');
    await page.getByRole('button', { name: 'Disable', exact: true }).click();
    await page.locator('#confirm-action').click();
    await page.getByRole('button', { name: 'Enable', exact: true }).waitFor();
    await page.getByRole('button', { name: 'Enable', exact: true }).click();
    await page.locator('#confirm-action').click();
    await page.getByRole('button', { name: 'Disable', exact: true }).waitFor();
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
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'mobile agent table scrolls inside its panel');
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await page.getByLabel('Administrator password').waitFor();
    assert.deepEqual(errors, []);
    console.log(JSON.stringify({ browser: await browser.version(), viewport: [1440, 390], passed: ['login', 'hosts', 'agents', 'key rotation and old-key rejection', 'disable and enable', 'settings', 'session reload', 'mobile containment', 'logout'], pageErrors: errors }));
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

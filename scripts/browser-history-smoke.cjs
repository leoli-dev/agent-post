#!/usr/bin/env node
/* Read-only channel inspection against a real service and Playwright Chromium. */
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { spawn } = require('node:child_process');
const { randomBytes } = require('node:crypto');
const fs = require('node:fs/promises');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');

async function main() {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'agent-post-history-'));
  const password = randomBytes(24).toString('hex');
  const port = await new Promise(resolve => {
    const listener = net.createServer();
    listener.listen(0, '127.0.0.1', () => { const port = listener.address().port; listener.close(() => resolve(port)); });
  });
  const base = `http://127.0.0.1:${port}`;
  const binary = process.env.AGENT_POST_BINARY || path.resolve(__dirname, '..', 'bin', process.platform === 'win32' ? 'agent-post.exe' : 'agent-post');
  const server = spawn(binary, ['serve', '--data-dir', path.join(directory, 'data'), '--listen', `127.0.0.1:${port}`], { env: { ...process.env, AGENT_POST_ADMIN_PASSWORD: password }, stdio: ['ignore', 'ignore', 'pipe'] });
  let diagnostic = '', spawnError;
  const closed = new Promise(resolve => server.once('close', resolve));
  server.stderr.on('data', bytes => { diagnostic += bytes.toString(); });
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
    const json = async (route, credential = password, body, method = body === undefined ? 'GET' : 'POST') => {
      const response = await fetch(`${base}${route}`, {
        method, headers: { Authorization: `Bearer ${credential}`, 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body)
      });
      assert.equal(response.status, 200, `${method} ${route}: ${await response.clone().text()}`);
      return response.json();
    };
    browser = await chromium.launch({ headless: true, ...(process.env.AGENT_POST_CHROMIUM ? { executablePath: process.env.AGENT_POST_CHROMIUM } : {}) });
    const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } });
    const page = await context.newPage();
    const errors = [], requests = [], mutations = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(`${base}/admin/`);
    await page.locator('#password').fill(password);
    await page.locator('#login-form button[type=submit]').click();
    await page.locator('#console').waitFor({ state: 'visible' });
    page.on('request', request => {
      if (request.url().includes('/admin/api/')) requests.push(request.url());
      if (request.url().includes('/admin/api/') && request.method() !== 'GET') mutations.push(`${request.method()} ${new URL(request.url()).pathname}`);
    });
    await page.locator('a[data-page=channels]').click();
    await page.locator('#channels-empty').waitFor({ state: 'visible' });
    assert.equal((await page.locator('#channels-empty').textContent()).trim(), 'No channels yet.');
    assert.equal(await page.locator('#channels-prev').isDisabled(), true);
    assert.equal(await page.locator('#channels-next').isDisabled(), true);

    const host = (await json('/admin/api/hosts', password, { name: 'history-fixture-host', os: 'Synthetic OS', ips: ['127.0.0.1'] })).host;
    const sender = await json('/admin/api/agents', password, { name: '<b>fixture-sender</b>', host_id: host.id });
    const recipient = await json('/admin/api/agents', password, { name: 'fixture-recipient', host_id: host.id });
    const pending = await json('/admin/api/agents', password, { name: 'fixture-pending', host_id: host.id });
    const settings = (await json('/admin/api/state')).settings;
    await json('/admin/api/settings', password, { ...settings, page_size: 2, claim_seconds: 300 }, 'PUT');
    const body = '<img src=x onerror=globalThis.__historyExecuted=true>\nHistory text & unicode 中文 🚀\n' + 'x'.repeat(320);
    const filename = 'report-<svg onload=globalThis.__historyExecuted=true>.txt';
    const attachmentBytes = Buffer.from('Synthetic history attachment\n<script>inert download</script>\n');
    const privateBodies = ['private oldest fixture', 'private middle fixture', body, 'private newest fixture'];
    let privateID;
    for (const message of privateBodies) {
      const sent = await json('/api/v1/messages/direct', sender.api_key, { to_agent_ids: [recipient.agent.id], body: message });
      assert.equal(sent.results[0].status, 'sent');
      privateID = sent.results[0].channel_id;
    }
    const form = new FormData();
    form.append('metadata', JSON.stringify({ to_agent_ids: [recipient.agent.id], body: 'private attachment fixture' }));
    form.append('file', new Blob([attachmentBytes], { type: 'text/plain' }), filename);
    const upload = await fetch(`${base}/api/v1/messages/direct`, { method: 'POST', headers: { Authorization: `Bearer ${sender.api_key}` }, body: form });
    assert.equal(upload.status, 200, 'attachment fixture upload succeeds');
    assert.equal((await upload.json()).results[0].status, 'sent');
    const groupName = 'Review <b>fixture group</b>';
    const groupID = (await json('/api/v1/groups', sender.api_key, { name: groupName, invitee_ids: [recipient.agent.id, pending.agent.id] })).channel_id;
    await json(`/api/v1/groups/${groupID}/messages`, sender.api_key, { body: 'group before joining fixture' });
    await json(`/api/v1/groups/${groupID}/join`, recipient.api_key, {});
    await json(`/api/v1/groups/${groupID}/messages`, recipient.api_key, { body: 'group reply fixture' });
    const emptyName = 'Empty fixture group';
    const emptyID = (await json('/api/v1/groups', sender.api_key, { name: emptyName, invitee_ids: [] })).channel_id;
    const held = await json('/api/v1/inbox/claim', recipient.api_key, { channel_id: privateID, limit: 1 });
    assert.equal(held.messages.length, 1, 'fixture has a live claim');
    const before = await json('/api/v1/inbox', recipient.api_key);

    const channelIDs = () => page.locator('#channel-list button[data-channel-id]').evaluateAll(nodes => nodes.map(node => node.dataset.channelId));
    const waitChannel = async id => {
      await page.waitForFunction(value => {
        const nodes = document.querySelectorAll('#channel-list button[data-channel-id]');
        return nodes.length === 1 && nodes[0].dataset.channelId === value;
      }, id);
    };
    const waitMessages = async expected => {
      await page.waitForFunction(values => JSON.stringify([...document.querySelectorAll('#message-list .message-body')].map(node => node.textContent)) === JSON.stringify(values), expected);
    };
    const selectChannel = async (id, expected) => {
      await page.locator('#channel-query').fill(id);
      await waitChannel(id);
      await page.locator(`#channel-list button[data-channel-id="${id}"]`).click();
      if (expected.length) await waitMessages(expected);
      else await page.locator('#messages-empty').waitFor({ state: 'visible' });
      assert.equal(await page.locator('#history-channel-id code').getAttribute('title'), id, 'selected channel exposes its full ID');
    };
    await page.locator('#refresh').click();
    await page.waitForFunction(() => document.querySelectorAll('#channel-list button[data-channel-id]').length === 2);
    const firstIDs = await channelIDs();
    assert.equal(await page.locator('#channels-prev').isDisabled(), true);
    assert.equal(await page.locator('#channels-next').isDisabled(), false);
    await page.locator('#channels-next').click();
    await page.waitForFunction(() => document.querySelectorAll('#channel-list button[data-channel-id]').length === 1);
    assert.deepEqual([...firstIDs, ...await channelIDs()].sort(), [privateID, groupID, emptyID].sort(), 'channel pages include private, populated group, and empty group channels exactly once');
    assert.equal(await page.locator('#channels-next').isDisabled(), true);
    await page.locator('#channels-prev').click();
    await page.waitForFunction(ids => JSON.stringify([...document.querySelectorAll('#channel-list button[data-channel-id]')].map(node => node.dataset.channelId)) === JSON.stringify(ids), firstIDs);

    await selectChannel(privateID, ['private attachment fixture', 'private newest fixture']);
    const privateTitle = await page.locator('#history-title').textContent();
    assert.ok(privateTitle.includes(sender.agent.name) && privateTitle.includes(recipient.agent.name), 'private history identifies both participants');
    const attachment = await page.locator('.message-attachment').textContent();
    assert.ok(attachment.includes(filename), 'attachment filename is displayed literally');
    assert.ok(attachment.includes('text/plain'), 'attachment media type is visible');
    assert.equal(await page.locator('.message-attachment a').count(), 0, 'attachment inspection presents metadata');
    if (process.env.AGENT_POST_SCREENSHOT_DIR) {
      await fs.mkdir(process.env.AGENT_POST_SCREENSHOT_DIR, { recursive: true });
      await page.screenshot({ path: path.join(process.env.AGENT_POST_SCREENSHOT_DIR, 'admin-history-desktop.png'), fullPage: true });
    }
    await page.locator('#messages-next').click();
    await waitMessages([body, 'private middle fixture']);
    await page.locator('#messages-next').click();
    await waitMessages(['private oldest fixture']);
    assert.equal(await page.locator('#messages-next').isDisabled(), true);
    await page.locator('#messages-prev').click();
    await waitMessages([body, 'private middle fixture']);
    await page.locator('#history-order').selectOption('oldest');
    await waitMessages(['private oldest fixture', 'private middle fixture']);
    assert.equal(await page.locator('#messages-prev').isDisabled(), true, 'changing order resets the page cursor');
    await page.locator('#messages-next').click();
    await waitMessages([body, 'private newest fixture']);
    assert.equal(await page.locator('.message-body img, .message-body svg, .attachment-name svg, #history-title b, #channel-list b').count(), 0, 'message bodies, filenames, and names create no markup');
    assert.equal(await page.evaluate(() => globalThis.__historyExecuted), undefined);

    // New channel controls and generated content must retranslate without refetching.
    for (const locale of ['fr', 'zh-Hans', 'zh-Hant', 'it', 'es', 'ru', 'en']) {
      const requestCount = requests.length;
      await page.locator('[data-i18n-language]:visible').first().selectOption(locale);
      await page.waitForFunction(value => document.documentElement.lang === value, locale);
      assert.equal(await page.locator('#history-title').textContent(), privateTitle, `${locale}: participant names stay verbatim`);
      await waitMessages([body, 'private newest fixture']);
      const nextLabel = await page.evaluate(() => window.AgentPostI18n.t('Next messages'));
      assert.equal((await page.locator('#messages-next').textContent()).trim(), nextLabel, `${locale}: history paging is translated`);
      assert.equal(requests.length, requestCount, `${locale}: language switching does not refetch history`);
      await page.setViewportSize({ width: 390, height: 844 });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `${locale}: history has no page-level horizontal overflow`);
      if (process.env.AGENT_POST_SCREENSHOT_DIR && ['en', 'ru', 'zh-Hant'].includes(locale)) {
        await fs.mkdir(process.env.AGENT_POST_SCREENSHOT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.AGENT_POST_SCREENSHOT_DIR, `admin-history-${locale}-mobile.png`), fullPage: true });
      }
      await page.setViewportSize({ width: 1440, height: 1000 });
    }
    await page.locator('#history-order').selectOption('newest');
    await waitMessages(['private attachment fixture', 'private newest fixture']);
    await page.reload();
    await page.locator('#console').waitFor({ state: 'visible' });
    await page.locator('#page-channels').waitFor({ state: 'visible' });
    await selectChannel(privateID, ['private attachment fixture', 'private newest fixture']);
    assert.equal(await page.locator('#login-view').isVisible(), false, 'history remains accessible through the restored admin session');

    // Hold one real response until another channel has rendered. Its late
    // messages must not be placed under the newly selected channel's title.
    const delayedPattern = `**/admin/api/channels/${privateID}/messages?*`;
    let releaseDelayed, requestHeld;
    const heldRequest = new Promise(resolve => { requestHeld = resolve; });
    const release = new Promise(resolve => { releaseDelayed = resolve; });
    await page.route(delayedPattern, async route => {
      const response = await route.fetch();
      requestHeld();
      await release;
      await route.fulfill({ response });
    }, { times: 1 });
    try {
      const staleResponse = page.waitForResponse(response => new URL(response.url()).pathname === `/admin/api/channels/${privateID}/messages`);
      await page.locator(`#channel-list button[data-channel-id="${privateID}"]`).click();
      await heldRequest;
      assert.equal(await page.locator('#history-status').isVisible(), true, 'a pending history request shows a loading state');
      await selectChannel(emptyID, []);
      releaseDelayed();
      await staleResponse;
      await page.waitForLoadState('networkidle');
      assert.equal(await page.locator('#history-title').textContent(), emptyName, 'late response preserves the newly selected channel title');
      assert.equal(await page.locator('#message-list .message').count(), 0, 'late response cannot replace the new channel history');
      assert.equal(await page.locator('#messages-empty').isVisible(), true);
    } finally {
      releaseDelayed();
      await page.unroute(delayedPattern);
    }

    await page.locator('a[data-page=groups]').click();
    const groupRow = page.locator('#group-rows tr').filter({ has: page.getByText(groupName, { exact: true }) });
    await groupRow.getByRole('button', { name: 'View messages', exact: true }).click();
    await page.locator('#page-channels').waitFor({ state: 'visible' });
    await waitMessages(['group reply fixture', 'group before joining fixture']);
    assert.equal(await page.locator('#history-title').textContent(), groupName, 'Groups shortcut opens the intended channel history');
    await page.locator('#channel-query').fill(privateID);
    await waitChannel(privateID);
    await page.locator('#channel-kind').selectOption('group');
    await page.locator('#channels-empty').waitFor({ state: 'visible' });
    assert.equal((await channelIDs()).length, 0, 'channel type filter excludes a private channel matching the query');
    await selectChannel(groupID, ['group reply fixture', 'group before joining fixture']);
    assert.equal(await page.locator('#history-title').textContent(), groupName, 'group history preserves its name');
    const groupMessages = await page.locator('#message-list').textContent();
    assert.ok(groupMessages.includes(sender.agent.name) && groupMessages.includes(recipient.agent.name), 'group messages identify both senders');
    assert.equal(await page.locator('#history-title b, #message-list b').count(), 0);
    await selectChannel(emptyID, []);
    assert.equal(await page.locator('#history-title').textContent(), emptyName);
    assert.equal((await page.locator('#messages-empty').textContent()).trim(), 'No retained messages in this channel.');
    assert.equal(await page.locator('#messages-prev').isDisabled(), true);
    assert.equal(await page.locator('#messages-next').isDisabled(), true);
    await page.locator('#channel-query').fill('missing-history-fixture');
    await page.locator('#channels-empty').waitFor({ state: 'visible' });
    assert.equal((await channelIDs()).length, 0, 'server-backed search has an empty result state');
    await page.locator('#channel-query').fill('fixture group');
    await page.waitForFunction(() => document.querySelectorAll('#channel-list button[data-channel-id]').length === 2);
    assert.deepEqual((await channelIDs()).sort(), [groupID, emptyID].sort(), 'channel query matches group names');
    assert.deepEqual(errors, []);
    assert.deepEqual(mutations, [], 'inspection only issues GET requests');
    const after = await json('/api/v1/inbox', recipient.api_key);
    for (const field of ['unread', 'claimable', 'leased']) assert.equal(after[field], before[field], `inspection preserves ${field}`);
    const ack = await json('/api/v1/inbox/ack', recipient.api_key, { acks: [{ message_id: held.messages[0].id, claim_token: held.messages[0].claim_token }] });
    assert.equal(ack.results[0].status, 'read', 'inspection preserves the current claim token');
    console.log(JSON.stringify({ browser: await browser.version(), viewport: [1440, 390], passed: ['empty channel and message states', 'private and group history', 'channel type and name/ID filters', 'channel and message cursor paging', 'oldest/newest order', 'literal message, filename and name rendering', 'attachment metadata', 'all seven languages', 'mobile containment', 'admin session reload', 'late response isolation', 'Groups history shortcut', 'read-only requests and preserved unread/claim state'], pageErrors: errors }));
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

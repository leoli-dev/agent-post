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
    const privateBodies = ['private oldest fixture shared-content-fixture', 'private middle fixture', body, 'private newest fixture'];
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
    const secondPrivate = await json('/api/v1/messages/direct', sender.api_key, { to_agent_ids: [pending.agent.id], body: 'second private shared-content-fixture' });
    const secondPrivateID = secondPrivate.results[0].channel_id;
    const thirdPrivate = await json('/api/v1/messages/direct', recipient.api_key, { to_agent_ids: [pending.agent.id], body: 'third private branch fixture' });
    const thirdPrivateID = thirdPrivate.results[0].channel_id;
    const groupName = 'Review <b>fixture group</b>';
    const groupID = (await json('/api/v1/groups', sender.api_key, { name: groupName, invitee_ids: [recipient.agent.id, pending.agent.id] })).channel_id;
    await json(`/api/v1/groups/${groupID}/messages`, sender.api_key, { body: 'group before joining fixture' });
    await json(`/api/v1/groups/${groupID}/join`, recipient.api_key, {});
    await json(`/api/v1/groups/${groupID}/messages`, recipient.api_key, { body: 'group reply fixture' });
    const emptyName = 'Empty fixture group';
    const emptyID = (await json('/api/v1/groups', sender.api_key, { name: emptyName, invitee_ids: [] })).channel_id;
    const otherGroupName = 'Recipient owned fixture group';
    const otherGroupID = (await json('/api/v1/groups', recipient.api_key, { name: otherGroupName, invitee_ids: [sender.agent.id] })).channel_id;
    await json(`/api/v1/groups/${otherGroupID}/join`, sender.api_key, {});
    await json(`/api/v1/groups/${otherGroupID}/messages`, recipient.api_key, { body: 'other group branch fixture' });
    const held = await json('/api/v1/inbox/claim', recipient.api_key, { channel_id: privateID, limit: 1 });
    assert.equal(held.messages.length, 1, 'fixture has a live claim');
    const before = await json('/api/v1/inbox', recipient.api_key);

    const channelIDs = () => page.locator('#channel-list button[data-channel-id]').evaluateAll(nodes => nodes.map(node => node.dataset.channelId));
    const waitChannelIDs = async expected => {
      await page.waitForFunction(values => JSON.stringify([...document.querySelectorAll('#channel-list button[data-channel-id]')].map(node => node.dataset.channelId).sort()) === JSON.stringify([...values].sort()), expected);
    };
    const waitMessages = async expected => {
      await page.waitForFunction(values => JSON.stringify([...document.querySelectorAll('#message-list .message-body')].map(node => node.textContent)) === JSON.stringify(values), expected);
    };
    const updateChannels = async action => {
      const loaded = page.waitForResponse(response => new URL(response.url()).pathname === '/admin/api/channels');
      await action();
      await loaded;
      await page.locator('#channels-status').waitFor({ state: 'hidden' });
    };
    const resetChannels = async kind => {
      const tab = page.locator(`#channel-tab-${kind}`);
      if (await tab.getAttribute('aria-selected') !== 'true') await updateChannels(() => tab.click());
      if (await page.locator('#channel-query').inputValue()) await updateChannels(() => page.locator('#channel-query').fill(''));
      if (await page.locator('#channel-participant').inputValue()) await updateChannels(() => page.locator('#channel-participant').selectOption(''));
      if (kind === 'group' && await page.locator('#channel-owner').inputValue()) await updateChannels(() => page.locator('#channel-owner').selectOption(''));
      while (!await page.locator('#channels-prev').isDisabled()) await updateChannels(() => page.locator('#channels-prev').click());
    };
    const selectChannel = async (kind, id, expected) => {
      await resetChannels(kind);
      for (let remaining = 10; !(await channelIDs()).includes(id) && remaining > 0; remaining--) {
        assert.equal(await page.locator('#channels-next').isDisabled(), false, `intended channel is present in ${kind} tab; shown=${JSON.stringify(await channelIDs())}; wanted=${id}; query=${await page.locator('#channel-query').inputValue()}; error=${await page.locator('#channels-error').textContent()}`);
        await updateChannels(() => page.locator('#channels-next').click());
      }
      await page.locator(`#channel-list button[data-channel-id="${id}"]`).click();
      if (expected.length) await waitMessages(expected);
      else await page.locator('#messages-empty').waitFor({ state: 'visible' });
      assert.equal(await page.locator('#history-channel-id code').getAttribute('title'), id, 'selected channel exposes its full ID');
    };
    const searchChannels = async (query, expected) => {
      const loaded = page.waitForResponse(response => {
        const url = new URL(response.url());
        return url.pathname === '/admin/api/channels' && (url.searchParams.get('q') || '') === query;
      });
      await page.locator('#channel-query').fill(query);
      await loaded;
      await waitChannelIDs(expected);
    };
    const filterChannels = async (selector, value, expected) => {
      const loaded = page.waitForResponse(response => new URL(response.url()).pathname === '/admin/api/channels');
      await page.locator(selector).selectOption(value);
      await loaded;
      await waitChannelIDs(expected);
    };
    assert.equal(await page.locator('#channel-tab-private').getAttribute('role'), 'tab');
    assert.equal(await page.locator('#channel-tab-group').getAttribute('role'), 'tab');
    assert.equal(await page.locator('#channel-tab-private').getAttribute('aria-selected'), 'true', 'channels start in the private tab');
    assert.equal(await page.locator('#channel-owner').isVisible(), false, 'private channels have no owner filter');
    for (const kind of ['private', 'group']) assert.equal(await page.locator(`#channel-tab-${kind} svg`).count(), 1, `${kind} tab has a distinguishing icon`);
    await page.locator('#refresh').click();
    await page.waitForFunction(() => document.querySelectorAll('#channel-list button[data-channel-id]').length === 2);
    const firstIDs = await channelIDs();
    assert.equal(await page.locator('#channels-prev').isDisabled(), true);
    assert.equal(await page.locator('#channels-next').isDisabled(), false);
    await page.locator('#channels-next').click();
    await page.waitForFunction(() => document.querySelectorAll('#channel-list button[data-channel-id]').length === 1);
    assert.deepEqual([...firstIDs, ...await channelIDs()].sort(), [privateID, secondPrivateID, thirdPrivateID].sort(), 'private tab pages contain private channels exactly once');
    assert.equal(await page.locator('#channels-next').isDisabled(), true);
    await page.locator('#channels-prev').click();
    await waitChannelIDs(firstIDs);

    await selectChannel('private', privateID, ['private attachment fixture', 'private newest fixture']);
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
    await waitMessages(['private oldest fixture shared-content-fixture']);
    assert.equal(await page.locator('#messages-next').isDisabled(), true);
    await page.locator('#messages-prev').click();
    await waitMessages([body, 'private middle fixture']);
    await page.locator('#history-order').selectOption('oldest');
    await waitMessages(['private oldest fixture shared-content-fixture', 'private middle fixture']);
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
    await selectChannel('private', privateID, ['private attachment fixture', 'private newest fixture']);
    assert.equal(await page.locator('#login-view').isVisible(), false, 'history remains accessible through the restored admin session');

    // Hold one real response until another channel has rendered. Its late
    // messages must not be placed under the newly selected channel's title.
    const delayedPattern = `**/admin/api/channels/${privateID}/messages?*`;
    let releaseDelayed, requestHeld, routeFinished, delayedRouteError, routeStarted = false;
    const heldRequest = new Promise(resolve => { requestHeld = resolve; });
    const release = new Promise(resolve => { releaseDelayed = resolve; });
    const routeDone = new Promise(resolve => { routeFinished = resolve; });
    await page.route(delayedPattern, async route => {
      routeStarted = true;
      try {
        const response = await route.fetch();
        requestHeld();
        await release;
        await route.fulfill({ response });
      } catch (error) { delayedRouteError = error; }
      finally { routeFinished(); }
    }, { times: 1 });
    try {
      const staleResponse = page.waitForResponse(response => new URL(response.url()).pathname === `/admin/api/channels/${privateID}/messages`);
      await page.locator(`#channel-list button[data-channel-id="${privateID}"]`).click();
      await heldRequest;
      assert.equal(await page.locator('#history-status').isVisible(), true, 'a pending history request shows a loading state');
      await selectChannel('group', emptyID, []);
      releaseDelayed();
      await staleResponse;
      await page.waitForLoadState('networkidle');
      assert.equal(await page.locator('#history-title').textContent(), emptyName, 'late response preserves the newly selected channel title');
      assert.equal(await page.locator('#message-list .message').count(), 0, 'late response cannot replace the new channel history');
      assert.equal(await page.locator('#messages-empty').isVisible(), true);
    } finally {
      releaseDelayed();
      if (routeStarted) await routeDone;
      await page.unroute(delayedPattern);
    }

    assert.equal(delayedRouteError, undefined, 'delayed history response completes');
    await page.locator('a[data-page=groups]').click();
    const groupRow = page.locator('#group-rows tr').filter({ has: page.getByText(groupName, { exact: true }) });
    await groupRow.getByRole('button', { name: 'View messages', exact: true }).click();
    await page.locator('#page-channels').waitFor({ state: 'visible' });
    await waitMessages(['group reply fixture', 'group before joining fixture']);
    assert.equal(await page.locator('#history-title').textContent(), groupName, 'Groups shortcut opens the intended channel history');
    assert.equal(await page.locator('#channel-tab-group').getAttribute('aria-selected'), 'true', 'Groups shortcut selects the group tab');
    assert.equal(await page.locator('#channel-owner').isVisible(), true, 'group channels expose an owner filter');
    await selectChannel('group', groupID, ['group reply fixture', 'group before joining fixture']);
    assert.equal(await page.locator('#history-title').textContent(), groupName, 'group history preserves its name');
    const groupMessages = await page.locator('#message-list').textContent();
    assert.ok(groupMessages.includes(sender.agent.name) && groupMessages.includes(recipient.agent.name), 'group messages identify both senders');
    assert.equal(await page.locator('#history-title b, #message-list b, #channel-participant b, #channel-owner b').count(), 0);
    if (process.env.AGENT_POST_SCREENSHOT_DIR) {
      await page.locator('[data-i18n-language]:visible').first().selectOption('zh-Hans');
      await page.waitForFunction(() => document.documentElement.lang === 'zh-Hans');
      await page.screenshot({ path: path.join(process.env.AGENT_POST_SCREENSHOT_DIR, 'admin-history-groups-zh-Hans-desktop.png'), fullPage: true });
      await page.locator('[data-i18n-language]:visible').first().selectOption('en');
      await page.waitForFunction(() => document.documentElement.lang === 'en');
    }
    await selectChannel('group', emptyID, []);
    assert.equal(await page.locator('#history-title').textContent(), emptyName);
    assert.equal((await page.locator('#messages-empty').textContent()).trim(), 'No retained messages in this channel.');
    assert.equal(await page.locator('#messages-prev').isDisabled(), true);
    assert.equal(await page.locator('#messages-next').isDisabled(), true);

    await resetChannels('group');
    const firstGroupIDs = await channelIDs();
    assert.equal(firstGroupIDs.length, 2);
    await page.locator('#channels-next').click();
    await page.waitForFunction(() => document.querySelectorAll('#channel-list button[data-channel-id]').length === 1);
    assert.deepEqual([...firstGroupIDs, ...await channelIDs()].sort(), [groupID, emptyID, otherGroupID].sort(), 'group tab pages exclude every private channel');
    await filterChannels('#channel-owner', sender.agent.id, [groupID, emptyID]);
    await filterChannels('#channel-participant', recipient.agent.id, [groupID]);
    await filterChannels('#channel-participant', pending.agent.id, []);
    assert.equal(await page.locator('#channels-empty').isVisible(), true, 'pending invitations do not count as group participation');
    await filterChannels('#channel-participant', sender.agent.id, [groupID, emptyID]);
    await filterChannels('#channel-owner', recipient.agent.id, [otherGroupID]);
    await searchChannels('other group branch fixture', [otherGroupID]);
    await page.locator(`#channel-list button[data-channel-id="${otherGroupID}"]`).click();
    await waitMessages(['other group branch fixture']);
    await searchChannels('fixture group', []);
    assert.equal((await channelIDs()).length, 0, 'group search does not match a group name');
    await resetChannels('group');
    await filterChannels('#channel-owner', sender.agent.id, [groupID, emptyID]);
    await filterChannels('#channel-participant', recipient.agent.id, [groupID]);
    await searchChannels('group reply fixture', [groupID]);
    await page.locator(`#channel-list button[data-channel-id="${groupID}"]`).click();
    await waitMessages(['group reply fixture']);
    await searchChannels('', [groupID]);
    await waitMessages(['group reply fixture', 'group before joining fixture']);

    await resetChannels('private');
    await searchChannels('shared-content-fixture', [privateID, secondPrivateID]);
    await filterChannels('#channel-participant', recipient.agent.id, [privateID]);
    await page.locator(`#channel-list button[data-channel-id="${privateID}"]`).click();
    await waitMessages(['private oldest fixture shared-content-fixture']);
    await searchChannels('', [privateID, thirdPrivateID]);
    await waitMessages(['private attachment fixture', 'private newest fixture']);
    await searchChannels(sender.agent.name, []);
    assert.equal((await channelIDs()).length, 0, 'private search does not match a participant name');
    await searchChannels(privateID, []);
    assert.equal((await channelIDs()).length, 0, 'private search does not match channel IDs');
    await searchChannels('History text', [privateID]);
    await page.locator(`#channel-list button[data-channel-id="${privateID}"]`).click();
    await waitMessages([body]);
    await searchChannels('missing-history-fixture', []);
    assert.equal((await channelIDs()).length, 0, 'body search has an empty result state');
    assert.deepEqual(errors, []);
    assert.deepEqual(mutations, [], 'inspection only issues GET requests');
    const after = await json('/api/v1/inbox', recipient.api_key);
    for (const field of ['unread', 'claimable', 'leased']) assert.equal(after[field], before[field], `inspection preserves ${field}`);
    const ack = await json('/api/v1/inbox/ack', recipient.api_key, { acks: [{ message_id: held.messages[0].id, claim_token: held.messages[0].claim_token }] });
    assert.equal(ack.results[0].status, 'read', 'inspection preserves the current claim token');
    console.log(JSON.stringify({ browser: await browser.version(), viewport: [1440, 390], passed: ['empty channel and message states', 'private and group history', 'private/group tabs and icons', 'message body search without name or ID matching', 'participant and group owner filter combinations', 'channel and message cursor paging', 'oldest/newest order', 'literal message, filename and name rendering', 'attachment metadata', 'all seven languages', 'mobile containment', 'admin session reload', 'late response isolation', 'Groups history shortcut', 'read-only requests and preserved unread/claim state'], pageErrors: errors }));
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

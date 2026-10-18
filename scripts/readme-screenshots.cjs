#!/usr/bin/env node
/* README artwork from the real console, an isolated service, and synthetic data.
   Optional development dependencies: Playwright and its Chromium browser. */
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { spawn } = require('node:child_process');
const { randomBytes } = require('node:crypto');
const fs = require('node:fs/promises');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const output = path.join(root, 'docs', 'images');
const locales = [
  {
    id: 'en', suffix: '', lang: 'en-US',
    headline: 'A mailbox<br>for your<br>AI agents.',
    description: 'Self-hosted communication.<br>One identity across sessions.',
    label: 'PRIVATE CONVERSATION',
    features: ['Private messages', 'Invited groups', 'One inbox per agent'],
    flow: ['Poll', 'Claim', 'Read', 'ACK'],
    note: 'Agents communicate. Humans administer.',
    demo: 'Actual console · Synthetic demo data',
    captions: ['Private conversation', 'Release review', 'Documentation desk'],
    private: [
      'Please review the installation draft. Focus on account identity, host IP checks, and the claim / ACK flow.',
      'I will check those sections. Please share the draft as a text attachment.',
      'Here is the draft. Each logical agent keeps one account across its sessions.',
      'Review note: ACK records that a message was read. It does not mean a requested task is complete.'
    ],
    group: [
      'Use this group to review the public setup guide. Keep examples synthetic and save important notes outside the mailbox.',
      'The draft is attached. It covers private messages, invited groups, and one attachment per message.',
      'I will review the claim and retention sections next. Please keep the distinction between reading and task completion explicit.'
    ],
    other: 'Please check the wording of the agent setup guide.',
  },
  {
    id: 'zh-Hans', suffix: '.zh-CN', lang: 'zh-CN',
    headline: '让 AI Agent<br>拥有自己的<br>邮箱。',
    description: '自托管通信服务。<br>一个逻辑身份，跨会话共用。',
    label: '独立私信会话',
    features: ['独立私信', '邀请制群组', '跨会话收件箱'],
    flow: ['轮询', '领取', '阅读', 'ACK'],
    note: 'Agent 负责通信，人类负责管理。',
    demo: '真实界面 · 合成演示数据',
    captions: ['私信会话', '发布文档审阅', '文档讨论'],
    private: [
      '请审阅安装指南草稿，重点检查账号身份、主机 IP 校验，以及消息领取与 ACK 流程。',
      '我会检查这几个部分。请把草稿作为文字附件发来。',
      '草稿已附上。同一逻辑 agent 的不同会话共用一个账号。',
      '审阅备注：ACK 只表示消息已读，不表示消息中请求的任务已经完成。'
    ],
    group: [
      '这个群组用于审阅公开接入指南。示例使用合成数据，重要笔记请另外保存。',
      '草稿已附上，内容涵盖独立私信、邀请制群组，以及每条消息最多一个附件。',
      '接下来我会检查领取和保留规则，请明确区分消息已读与任务完成。'
    ],
    other: '请检查 agent 接入指南的措辞。',
  }
];

function heroHTML(locale, screenshot) {
  const height = Math.max(1020, Math.ceil(screenshot.readUInt32BE(20) * 890 / screenshot.readUInt32BE(16)) + 170);
  return `<!doctype html><html lang="${locale.lang}"><meta charset="utf-8">
  <style>
    * { box-sizing: border-box; } body { margin: 0; width: 1600px; height: ${height}px; padding: 64px; display: grid; grid-template-columns: 520px 892px; gap: 60px; background: #111c2c; color: #f4f7fc; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', sans-serif; -webkit-font-smoothing: antialiased; }
    .story { display: flex; flex-direction: column; align-items: flex-start; }
    .brand { display: flex; align-items: center; gap: 13px; font-size: 25px; font-weight: 650; letter-spacing: -.5px; }
    .mark { width: 36px; height: 36px; background: #497ef2; border-radius: 9px; padding: 7px; }
    .headline { margin: 74px 0 0; font-size: 78px; font-weight: 660; letter-spacing: -3px; line-height: 1.08; }
    .interfaces { color: #afc6ff; font: 600 16px ui-monospace, Menlo, monospace; letter-spacing: 3px; }
    .description { font-size: 26px; line-height: 1.6; color: #c6d2e5; margin: 30px 0 32px; }
    .features { display: flex; gap: 10px; flex-wrap: wrap; }
    .features span { font-size: 16px; padding: 8px 12px; border: 1px solid #3b4b64; border-radius: 5px; color: #d1dcee; }
    .window { align-self: start; border: 1px solid #53647d; border-radius: 12px; overflow: hidden; background: #fff; box-shadow: 0 22px 55px #050d1a66; }
    .window-bar { display: flex; align-items: center; justify-content: space-between; height: 40px; padding: 0 18px; background: #e9edf3; color: #47536a; font-size: 12px; letter-spacing: 1.3px; }
    .window-bar small { font-size: 12px; letter-spacing: 0; }
    .window img { display: block; width: 890px; height: auto; }
    .footer { margin-top: auto; padding-top: 64px; color: #b4c4dd; font-size: 19px; }
    .flow { display: flex; align-items: center; gap: 17px; margin-top: 22px; font: 19px ui-monospace, Menlo, sans-serif; color: #edf3ff; }
    .flow b { color: #7893bf; font-weight: 400; }
  </style><div class="story"><div class="brand"><svg class="mark" viewBox="0 0 24 24"><path d="M3 6h18v13H3zM3 6l9 7 9-7" fill="none" stroke="white" stroke-width="1.8" stroke-linejoin="round"/></svg>Agent Post</div><h1 class="headline">${locale.headline}</h1><p class="description">${locale.description}</p><div class="interfaces">HTTP / CLI / MCP</div><div class="features" style="margin-top:28px">${locale.features.map(text => `<span>${text}</span>`).join('')}</div><div class="footer"><span>${locale.note}</span><div class="flow">${locale.flow.join('<b>→</b>')}</div></div></div>
  <div class="window"><div class="window-bar"><span>${locale.label}</span><small>${locale.demo}</small></div><img src="data:image/png;base64,${screenshot.toString('base64')}" alt=""></div>
  </html>`;
}

async function capture(locale, browser, binary) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'agent-post-readme-'));
  const password = randomBytes(24).toString('hex');
  const port = await new Promise(resolve => {
    const listener = net.createServer();
    listener.listen(0, '127.0.0.1', () => {
      const assigned = listener.address().port;
      listener.close(() => resolve(assigned));
    });
  });
  const base = `http://127.0.0.1:${port}`;
  const server = spawn(binary, ['serve', '--data-dir', path.join(directory, 'data'), '--listen', `127.0.0.1:${port}`], {
    env: { ...process.env, AGENT_POST_ADMIN_PASSWORD: password }, stdio: ['ignore', 'ignore', 'pipe']
  });
  let diagnostic = '', spawnError;
  const closed = new Promise(resolve => server.once('close', resolve));
  server.stderr.on('data', bytes => { diagnostic += bytes.toString(); });
  server.on('error', error => { spawnError = error; });
  let context;
  try {
    context = await browser.newContext({ locale: locale.lang, colorScheme: 'light', viewport: { width: 1470, height: 760 }, deviceScaleFactor: 1 });
    let ready = false;
    for (let attempt = 0; attempt < 100; attempt++) {
      try { ready = (await fetch(`${base}/healthz`)).ok; if (ready) break; } catch {}
      if (spawnError || server.exitCode !== null) throw new Error(spawnError?.message || diagnostic);
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert.ok(ready, 'isolated service is ready');
    const json = async (route, credential = password, body) => {
      const response = await fetch(`${base}${route}`, {
        method: body === undefined ? 'GET' : 'POST',
        headers: { Authorization: `Bearer ${credential}`, 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body)
      });
      assert.equal(response.status, 200, `${route}: ${await response.clone().text()}`);
      return response.json();
    };
    // Loopback is the only execution address; all names and displayed extra IPs are synthetic.
    const hosts = [];
    for (const [name, ip] of [['Atlas lab', '192.0.2.10'], ['Orbit lab', '192.0.2.20'], ['Relay lab', '192.0.2.30']]) {
      hosts.push((await json('/admin/api/hosts', password, { name, os: 'Demo host', ips: ['127.0.0.1', ip] })).host);
    }
    const agents = [];
    for (const [name, host] of [['Builder', 0], ['Reviewer', 1], ['Planner', 2], ['Researcher', 2], ['Writer', 0]]) {
      agents.push(await json('/admin/api/agents', password, { name, host_id: hosts[host].id }));
    }
    const [builder, reviewer, planner, researcher, writer] = agents;
    const direct = async (sender, recipient, body) => {
      const sent = await json('/api/v1/messages/direct', sender.api_key, { to_agent_ids: [recipient.agent.id], body });
      assert.equal(sent.results[0].status, 'sent');
      return sent.results[0].channel_id;
    };
    const upload = async (route, sender, metadata) => {
      const form = new FormData();
      form.append('metadata', JSON.stringify(metadata));
      form.append('file', new Blob(['# Synthetic installation draft\n\nOne logical account across sessions.\nACK means read, not task completion.\n'], { type: 'text/markdown' }), 'installation-draft.md');
      const response = await fetch(`${base}${route}`, { method: 'POST', headers: { Authorization: `Bearer ${sender.api_key}` }, body: form });
      assert.equal(response.status, 200, 'synthetic attachment is accepted');
    };
    const privateID = await direct(builder, reviewer, locale.private[0]);
    await direct(reviewer, builder, locale.private[1]);
    await upload('/api/v1/messages/direct', builder, { to_agent_ids: [reviewer.agent.id], body: locale.private[2] });
    await direct(reviewer, builder, locale.private[3]);
    await direct(planner, builder, locale.other);
    await direct(researcher, writer, locale.other);
    const groupID = (await json('/api/v1/groups', planner.api_key, { name: locale.captions[1], invitee_ids: [builder.agent.id, reviewer.agent.id] })).channel_id;
    for (const agent of [builder, reviewer]) await json(`/api/v1/groups/${groupID}/join`, agent.api_key, {});
    await json(`/api/v1/groups/${groupID}/messages`, planner.api_key, { body: locale.group[0] });
    await upload(`/api/v1/groups/${groupID}/messages`, builder, { body: locale.group[1] });
    await json(`/api/v1/groups/${groupID}/messages`, reviewer.api_key, { body: locale.group[2] });
    const otherID = (await json('/api/v1/groups', writer.api_key, { name: locale.captions[2], invitee_ids: [researcher.agent.id] })).channel_id;
    await json(`/api/v1/groups/${otherID}/join`, researcher.api_key, {});
    await json(`/api/v1/groups/${otherID}/messages`, writer.api_key, { body: locale.other });
    for (const agent of agents) await json('/api/v1/inbox', agent.api_key);

    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(`${base}/admin/`);
    await page.locator('#password').fill(password);
    await page.locator('#login-form button[type=submit]').click();
    await page.locator('#console').waitFor({ state: 'visible' });
    await page.locator('[data-i18n-language]:visible').first().selectOption(locale.id);
    assert.equal(await page.locator('html').getAttribute('lang'), locale.id);
    await page.locator('a[data-page=channels]').click();
    await page.locator(`#channel-list button[data-channel-id="${privateID}"]`).click();
    await page.locator('.message-body').filter({ hasText: locale.private[3] }).waitFor();
    await page.locator('#history-order').selectOption('oldest');
    await page.waitForFunction(first => document.querySelector('.message-body')?.textContent === first, locale.private[0]);
    assert.equal(await page.locator('.message-attachment').count(), 1);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.evaluate(() => document.fonts.ready);
    await page.setViewportSize({ width: 1470, height: 1400 });
    await page.evaluate(() => { window.scrollTo(0, 0); return new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))); });
    const panelBounds = await page.locator('.history-panel').boundingBox();
    assert.ok(panelBounds.y > 56 && panelBounds.y + panelBounds.height < 1400, 'complete history panel fits below the sticky header');
    const privateScreen = await page.screenshot({ clip: panelBounds });
    // Hero uses a complete, unmodified history panel, scaled uniformly in a frame.
    const artwork = await context.newPage();
    await artwork.setContent(heroHTML(locale, privateScreen));
    await artwork.setViewportSize({ width: 1600, height: await artwork.evaluate(() => document.body.offsetHeight) });
    await artwork.evaluate(() => Promise.all([document.fonts.ready, ...[...document.images].map(image => image.decode())]));
    await artwork.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    assert.ok(await artwork.evaluate(() => document.querySelector('.window').getBoundingClientRect().bottom <= innerHeight - 60), 'hero includes the complete panel');
    await artwork.screenshot({ path: path.join(output, `hero${locale.suffix}.png`) });
    await artwork.close();

    await page.locator('#channel-tab-group').click();
    await page.locator(`#channel-list button[data-channel-id="${groupID}"]`).click();
    await page.locator('.message-body').filter({ hasText: locale.group[2] }).waitFor();
    await page.locator('#history-order').selectOption('oldest');
    await page.waitForFunction(first => document.querySelector('.message-body')?.textContent === first, locale.group[0]);
    await page.setViewportSize({ width: 1470, height: 1120 });
    await page.evaluate(() => { window.scrollTo(0, 0); return new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))); });
    await page.screenshot({ path: path.join(output, `console-channels${locale.suffix}.png`) });
    await page.locator('a[data-page=agents]').click();
    assert.equal(await page.locator('#agent-rows tr').count(), agents.length);
    await page.setViewportSize({ width: 1470, height: 630 });
    await page.evaluate(() => { window.scrollTo(0, 0); return new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))); });
    await page.screenshot({ path: path.join(output, `console-agents${locale.suffix}.png`) });
    assert.deepEqual(errors, [], 'console has no JavaScript errors');
    console.log(`${locale.lang}: hero, group history, and identities captured from Chromium`);
  } finally {
    try {
      if (context) await context.close();
    } finally {
      server.kill('SIGTERM');
      try { await closed; }
      finally { await fs.rm(directory, { recursive: true, force: true }); }
    }
  }
}

async function main() {
  await fs.mkdir(output, { recursive: true });
  const binary = process.env.AGENT_POST_BINARY || path.join(root, 'bin', process.platform === 'win32' ? 'agent-post.exe' : 'agent-post');
  const browser = await chromium.launch({ headless: true, ...(process.env.AGENT_POST_CHROMIUM ? { executablePath: process.env.AGENT_POST_CHROMIUM } : {}) });
  try {
    console.log(`Capturing with Chromium ${browser.version()}`);
    for (const locale of locales) await capture(locale, browser, binary);
  } finally { await browser.close(); }
}

main().catch(error => { console.error(error); process.exitCode = 1; });

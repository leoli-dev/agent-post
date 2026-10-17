#!/usr/bin/env node
/* Real Chromium checks for localized admin flows and language-switch state. */
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { spawn } = require('node:child_process');
const { randomBytes } = require('node:crypto');
const fs = require('node:fs/promises');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');

const locales = ['en', 'fr', 'zh-Hans', 'zh-Hant', 'it', 'es', 'ru'];
const loginLabels = { en: 'Sign in', fr: 'Se connecter', 'zh-Hans': '登录', 'zh-Hant': '登入', it: 'Accedi', es: 'Iniciar sesión', ru: 'Войти' };

async function main() {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'agent-post-i18n-'));
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
    browser = await chromium.launch({ headless: true, ...(process.env.AGENT_POST_CHROMIUM ? { executablePath: process.env.AGENT_POST_CHROMIUM } : {}) });
    const context = await browser.newContext({ locale: 'en-US', viewport: { width: 1440, height: 1000 } });
    const page = await context.newPage();
    const errors = [], requests = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => { if (request.url().includes('/admin/api/')) requests.push(request.url()); });
    await page.goto(`${base}/admin/`);
    await page.waitForFunction(() => !!window.AgentPostI18n);
    assert.deepEqual(await page.evaluate(() => window.AgentPostI18n.supportedLocales.map(item => item.value)), locales);
    const pick = async locale => {
      await page.locator('[data-i18n-language]:visible').first().selectOption(locale);
      await page.waitForFunction(value => document.documentElement.lang === value, locale);
      assert.equal(await page.evaluate(() => window.AgentPostI18n.getLocale()), locale);
    };
    const switchInPlace = async locale => {
      const before = requests.length;
      await page.evaluate(value => window.AgentPostI18n.setLocale(value), locale);
      await page.waitForFunction(value => document.documentElement.lang === value, locale);
      assert.equal(requests.length, before, 'language switching never sends an admin request');
    };
    const login = async () => {
      await page.locator('#password').fill(password);
      await page.locator('#login-form button[type=submit]').click();
      await page.locator('#console').waitFor({ state: 'visible' });
    };
    const titleByLocale = {}, loginByLocale = {}, hostLabelByLocale = {}, errorByLocale = {};
    for (const locale of locales) {
      await pick(locale);
      titleByLocale[locale] = await page.title();
      loginByLocale[locale] = await page.locator('#login-form button[type=submit]').textContent();
      assert.equal(loginByLocale[locale].trim(), loginLabels[locale], `${locale}: localized login label`);
      assert.equal(await page.locator('[data-i18n-language]:visible option').count(), 7);
      await page.locator('#password').fill('synthetic-wrong-password');
      await page.locator('#login-form button[type=submit]').click();
      await page.waitForFunction(() => document.querySelector('#login-error').textContent.trim().length > 0);
      errorByLocale[locale] = await page.locator('#login-error').textContent();
      assert.notEqual(errorByLocale[locale].trim(), 'unauthorized', 'admin authentication errors have a localized explanation');
      await login();
      await page.locator('a[data-page=hosts]').click();
      await page.locator('#new-host').click();
      hostLabelByLocale[locale] = await page.locator('label[for=host-name]').textContent();
      const name = `fixture-${locale} <b>plain text</b>`;
      await page.locator('#host-name').fill(name);
      await page.locator('#host-os').fill('Synthetic OS');
      await page.locator('#host-ips').fill('not-an-ip');
      await page.locator('#host-form button[type=submit]').click();
      await page.waitForFunction(() => document.querySelector('#host-form .form-error').textContent.trim().length > 0);
      assert.notEqual((await page.locator('#host-form .form-error').textContent()).trim(), 'invalid_ip');
      await page.locator('#host-ips').fill('127.0.0.1');
      await page.locator('#host-form button[type=submit]').click();
      await page.locator('#host-dialog').waitFor({ state: 'hidden' });
      await page.locator('#host-rows').getByText(name, { exact: true }).waitFor();
      assert.equal(await page.locator('#host-rows b').count(), 0, 'host names remain inert text');
      await page.locator('a[data-page=agents]').click();
      await page.locator('#new-agent').click();
      await page.locator('#agent-name').fill(`fixture-agent-${locale}`);
      const hostId = await page.locator('#agent-host option').filter({ hasText: name }).getAttribute('value');
      await page.locator('#agent-host').selectOption(hostId);
      await page.locator('#agent-form button[type=submit]').click();
      await page.locator('#key-dialog').waitFor({ state: 'visible' });
      const key = await page.locator('#issued-key').inputValue();
      assert.ok(key.startsWith('apk_'));
      const next = locales[(locales.indexOf(locale) + 1) % locales.length];
      await switchInPlace(next);
      assert.equal(await page.locator('#issued-key').inputValue(), key, 'issued key survives a language switch');
      await switchInPlace(locale);
      await page.locator('#key-dialog .dialog-actions .close-dialog').click();
      await page.locator('#key-dialog').waitFor({ state: 'hidden' });
      await page.locator('a[data-page=settings]').click();
      await page.locator('#setting-message_count').fill(String(110 + locales.indexOf(locale)));
      await switchInPlace(next);
      assert.equal(await page.locator('#setting-message_count').inputValue(), String(110 + locales.indexOf(locale)), 'unsaved settings survive a language switch');
      await switchInPlace(locale);
      await page.locator('#settings-form button[type=submit]').click();
      await page.locator('#confirm-dialog').waitFor({ state: 'visible' });
      await switchInPlace(next);
      const saving = page.waitForResponse(response => response.url().endsWith('/admin/api/settings') && response.request().method() === 'PUT');
      await page.locator('#confirm-action').click();
      const saved = await saving;
      assert.equal(saved.status(), 200);
      assert.equal((await saved.json()).settings.message_count, 110 + locales.indexOf(locale), 'pending confirmation callback saves the intended values');
      await page.waitForFunction(value => document.querySelector('#setting-message_count').value === value && !document.querySelector('#confirm-dialog').open, String(110 + locales.indexOf(locale)));
      await switchInPlace(locale);
      await page.reload();
      await page.locator('#console').waitFor({ state: 'visible' });
      assert.equal(await page.locator('html').getAttribute('lang'), locale, 'explicit preference survives reload');
      await page.setViewportSize({ width: 390, height: 844 });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `${locale}: settings have no horizontal page overflow`);
      await page.locator('a[data-page=agents]').click();
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `${locale}: dense tables stay inside their panels`);
      if (process.env.AGENT_POST_SCREENSHOT_DIR) {
        await fs.mkdir(process.env.AGENT_POST_SCREENSHOT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.AGENT_POST_SCREENSHOT_DIR, `admin-i18n-${locale}.png`), fullPage: false });
      }
      await page.setViewportSize({ width: 1440, height: 1000 });
      await page.locator('#logout').click();
      await page.locator('#login-view').waitFor({ state: 'visible' });
      assert.equal(await page.locator('html').getAttribute('lang'), locale, 'language preference survives logout');
    }
    for (const locale of locales.filter(value => value !== 'en')) {
      assert.notEqual(loginByLocale[locale], loginByLocale.en, `${locale}: translated login button`);
      assert.notEqual(titleByLocale[locale], titleByLocale.en, `${locale}: translated document title`);
      assert.notEqual(hostLabelByLocale[locale], hostLabelByLocale.en, `${locale}: translated forms`);
      assert.notEqual(errorByLocale[locale], errorByLocale.en, `${locale}: translated authentication errors`);
    }
    // Switching while a dialog has invalid JSON must preserve the error and input.
    await pick('en'); await login(); await page.locator('a[data-page=hosts]').click(); await page.locator('#new-host').click();
    await page.locator('#host-name').fill('unsaved host'); await page.locator('#host-ips').fill('127.0.0.1'); await page.locator('#host-metadata').fill('{broken');
    await page.locator('#host-form button[type=submit]').click();
    await page.waitForFunction(() => document.querySelector('#host-form .form-error').textContent.trim().length > 0);
    const englishError = await page.locator('#host-form .form-error').textContent();
    await switchInPlace('ru');
    assert.equal(await page.locator('#host-name').inputValue(), 'unsaved host');
    assert.equal(await page.locator('#host-metadata').inputValue(), '{broken');
    assert.ok((await page.locator('#host-form .form-error').textContent()).trim());
    assert.notEqual(await page.locator('#host-form .form-error').textContent(), englishError, 'open validation error retranslates');
    await page.locator('#host-dialog .close-dialog').first().click();
    await context.close();
    const detectionCases = [['fr-CA', 'fr'], ['it-CH', 'it'], ['es-MX', 'es'], ['ru-RU', 'ru'], ['zh-TW', 'zh-Hant'], ['zh-HK', 'zh-Hant'], ['zh-MO', 'zh-Hant'], ['zh-CN', 'zh-Hans'], ['zh-SG', 'zh-Hans'], ['zh-Hant-CN', 'zh-Hant'], ['zh-Hans-HK', 'zh-Hans'], ['de-DE', 'en']];
    for (const [language, expected] of detectionCases) {
      const detected = await browser.newContext({ locale: language }); const testPage = await detected.newPage();
      testPage.on('pageerror', error => errors.push(error.message));
      await testPage.goto(`${base}/admin/`);
      await testPage.waitForFunction(() => !!window.AgentPostI18n);
      assert.equal(await testPage.locator('html').getAttribute('lang'), expected, `browser locale ${language}`);
      await detected.close();
    }
    const blocked = await browser.newContext({ locale: 'fr-CA' });
    await blocked.addInitScript(() => {
      Object.defineProperty(Storage.prototype, 'getItem', { value() { throw new DOMException('synthetic blocked storage', 'SecurityError'); } });
      Object.defineProperty(Storage.prototype, 'setItem', { value() { throw new DOMException('synthetic blocked storage', 'SecurityError'); } });
    });
    const blockedPage = await blocked.newPage(); blockedPage.on('pageerror', error => errors.push(error.message));
    await blockedPage.goto(`${base}/admin/`); await blockedPage.waitForFunction(() => !!window.AgentPostI18n);
    assert.equal(await blockedPage.locator('html').getAttribute('lang'), 'fr');
    await blockedPage.locator('[data-i18n-language]:visible').first().selectOption('zh-Hant');
    assert.equal(await blockedPage.locator('html').getAttribute('lang'), 'zh-Hant', 'switching works without storage');
    await blocked.close();
    assert.deepEqual(errors, []);
    console.log(JSON.stringify({ browser: await browser.version(), locales, localeFlows: ['translated login and auth errors', 'host validation and creation', 'agent and key creation', 'key preservation', 'unsaved settings and confirmation preservation', 'save and reload', 'mobile containment', 'logout persistence'], detectionCases: detectionCases.length, storageBlocked: 'passed', pageErrors: errors }));
  } finally {
    if (browser) await browser.close();
    if (server.pid && server.exitCode === null) server.kill('SIGTERM');
    let timer; await Promise.race([closed, new Promise(resolve => { timer = setTimeout(() => { server.kill('SIGKILL'); resolve(); }, 5000); })]); clearTimeout(timer);
    await fs.rm(directory, { recursive: true, force: true });
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });

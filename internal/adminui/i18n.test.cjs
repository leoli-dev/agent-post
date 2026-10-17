"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const {test} = require("node:test");
const source = fs.readFileSync(path.join(__dirname, "i18n.js"), "utf8");
const catalogs = JSON.parse(source.match(/const catalogs = ([\s\S]*?);\nconst supportedLocales/)[1]);
const locales = ["en", "fr", "zh-Hans", "zh-Hant", "it", "es", "ru"];
function node(dataset = {}, value = "") {
  return {dataset, value, textContent: "", attributes: {}, children: [], events: {},
    setAttribute(name, value) { this.attributes[name] = value; },
    replaceChildren(...children) { this.children = children; },
    addEventListener(name, callback) { this.events[name] = callback; }};
}
function load({languages = ["en"], saved, blocked = false, elements = []} = {}) {
  const writes = [], events = [];
  const doc = {documentElement: {}, title: "", createElement: () => node(),
    querySelectorAll(selector) {
      const attribute = selector.match(/^\[data-([a-z0-9-]+)\]$/)?.[1];
      const key = attribute?.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
      return elements.filter(element => key && key in element.dataset);
    }};
  const context = {document: doc, navigator: {languages}, Intl,
    localStorage: {getItem() { if (blocked) throw new Error("blocked"); return saved; },
      setItem(key, value) { if (blocked) throw new Error("blocked"); writes.push([key, value]); }},
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
    window: {dispatchEvent(event) { events.push(event); }}};
  vm.runInNewContext(source, context, {filename: "i18n.js"});
  return {api: context.window.AgentPostI18n, doc, writes, events};
}
test("all seven catalogs are complete and preserve interpolation parameters", () => {
  assert.deepEqual(Object.keys(catalogs), locales);
  const keys = Object.keys(catalogs.en).sort();
  for (const locale of locales) {
    assert.deepEqual(Object.keys(catalogs[locale]).sort(), keys);
    for (const key of keys) {
      assert.ok(catalogs[locale][key].trim(), `${locale}: ${key}`);
      assert.deepEqual(catalogs[locale][key].match(/\{[a-z_]+\}/gi), catalogs.en[key].match(/\{[a-z_]+\}/gi), `${locale}: ${key}`);
    }
  }
});
test("every static attribute and generated settings label has a catalog entry", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  for (const match of html.matchAll(/data-i18n(?:-placeholder|-aria-label|-title)?="([^"]+)"/g)) {
    const key = match[1].replaceAll("&amp;", "&").replaceAll("&#x27;", "'").replaceAll("&quot;", '"');
    assert.ok(key in catalogs.en, key);
  }
  const app = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
  const labels = vm.runInNewContext(`(${app.match(/const settingLabels = (\{[\s\S]*?\n\});/)[1]})`);
  for (const values of Object.values(labels)) for (const key of values) assert.ok(key in catalogs.en, key);
  assert.ok(html.indexOf('/admin/i18n.js') < html.indexOf('/admin/app.js'));
});
test("browser preferences, script-first Chinese aliases, persistence and unsupported fallback", () => {
  for (const [language, expected] of [["fr-CA", "fr"], ["it-IT", "it"], ["es-MX", "es"], ["ru-RU", "ru"],
    ["zh-TW", "zh-Hant"], ["zh-HK", "zh-Hant"], ["zh-MO", "zh-Hant"], ["zh-CN", "zh-Hans"], ["zh-SG", "zh-Hans"],
    ["zh", "zh-Hans"], ["zh-Hans-HK", "zh-Hans"], ["zh-Hant-CN", "zh-Hant"], ["zh_Hant_TW", "zh-Hant"], ["de-DE", "en"]]) {
    assert.equal(load({languages: [language]}).api.getLocale(), expected, language);
  }
  assert.equal(load({languages: ["de-DE", "fr-CA"]}).api.getLocale(), "fr");
  assert.equal(load({languages: ["fr-CA"], saved: "it"}).api.getLocale(), "it");
  assert.equal(load({languages: ["fr-CA"], saved: "unsupported"}).api.getLocale(), "fr");
  const blocked = load({languages: ["zh-TW"], blocked: true});
  assert.equal(blocked.api.getLocale(), "zh-Hant");
  assert.doesNotThrow(() => blocked.api.setLocale("ru"));
  assert.equal(blocked.api.getLocale(), "ru");
});
test("live locale changes update text and accessible labels without changing field values", () => {
  const title = node({i18n: "Edit host"});
  const placeholder = node({i18nPlaceholder: "e.g. Linux"}, "unsaved host OS");
  const close = node({i18nAriaLabel: "Close"});
  const key = node({}, "synthetic-private-key");
  const picker = node({i18nLanguage: ""});
  const pending = node({i18n: "Files awaiting deletion: {count}."}); pending._i18nParams = {count: 12345};
  const {api, doc, writes, events} = load({elements: [title, placeholder, close, key, picker, pending]});
  const expected = ["Sign in", "Se connecter", "登录", "登入", "Accedi", "Iniciar sesión", "Войти"];
  locales.forEach((locale, index) => {
    api.setLocale(locale);
    assert.equal(api.getLocale(), locale); assert.equal(doc.documentElement.lang, locale);
    assert.equal(api.t("Sign in"), expected[index]); assert.equal(picker.value, locale);
    assert.equal(title.textContent, catalogs[locale]["Edit host"]);
    assert.equal(placeholder.value, "unsaved host OS"); assert.equal(key.value, "synthetic-private-key");
    assert.equal(close.attributes["aria-label"], catalogs[locale].Close);
    assert.equal(doc.title, `Agent Post · ${catalogs[locale].Administration}`);
    assert.ok(pending.textContent.includes(new Intl.NumberFormat(locale).format(12345)));
  });
  assert.equal(events.length, 7);
  assert.deepEqual(writes.map(([key]) => key), Array(7).fill("agent-post.locale"));
  assert.equal(picker.children.length, 7);
  assert.equal(picker.children[3].textContent, "繁體中文");
  assert.equal(api.formatDate("not a date"), "—");
});

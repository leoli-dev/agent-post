"use strict";
const i18n = window.AgentPostI18n;
const t = i18n.t;
const $ = (selector) => document.querySelector(selector);
let csrf = "";
let state = {hosts: [], agents: [], groups: [], settings: {}};
let noticeTimer;
let confirmAction;
const settingLabels = {
  message_count: ["Messages per channel", "Keep the newest messages; older messages are deleted."],
  body_codepoints: ["Body length in Unicode code points", "Combining characters and emoji sequences may use several code points."],
  file_bytes: ["Maximum file size in bytes", "At most one file per message."],
  file_count: ["Files per channel", "File eviction also deletes its carrying message. Zero disables attachments."],
  claim_seconds: ["Claim duration in seconds", "Applies to new claims. ACK records read status."],
  claim_batch: ["Maximum claim batch", "Maximum messages that one active call can claim."],
  page_size: ["Maximum page size", "Bounds history, directory, and other listing responses."],
  direct_recipients: ["Maximum private recipients", "Each recipient gets an independent private copy."],
  group_members: ["Maximum group members", "Includes pending invitations."],
  groups_owned: ["Maximum groups owned per agent", "Bounds the spaces one agent can create."],
  concurrent_uploads: ["Concurrent uploads", "Service-wide admission limit."],
  temporary_upload_bytes: ["Temporary upload budget in bytes", "Must accommodate the maximum file size."]
};
// Server-side bounds from the admin settings handler, mirrored for early feedback.
const settingMax = {message_count: 100000, body_codepoints: 1000000, file_bytes: 1073741824, file_count: 100000, claim_seconds: 86400, claim_batch: 1000, page_size: 1000, direct_recipients: 100, group_members: 1000, groups_owned: 1000, concurrent_uploads: 32, temporary_upload_bytes: 4294967296};
const settingGroups = [
  ["Retention", "Messages and attachments share one lifetime. Lower limits also remove existing content that exceeds them.", ["message_count", "file_count"]],
  ["Message and file size", "", ["body_codepoints", "file_bytes"]],
  ["Claims and paging", "", ["claim_seconds", "claim_batch", "page_size"]],
  ["Membership and capacity", "", ["direct_recipients", "group_members", "groups_owned", "concurrent_uploads", "temporary_upload_bytes"]]
];
// Lowering these deletes stored content (see the server's retention pass).
const destructiveSettings = ["message_count", "body_codepoints", "file_bytes", "file_count"];
// Message descriptors retain their meaning when the language changes. Never
// translate user-supplied names, metadata, IDs or credentials.
function setMessage(node, key, params = {}) {
  node.dataset.i18n = key; node._i18nParams = params;
  node.textContent = t(key, params);
}
function clearMessage(node) {
  delete node.dataset.i18n; delete node._i18nParams; node.textContent = "";
}
class UIError extends Error {
  constructor(key, params = {}) { super(t(key, params)); this.key = key; this.params = params; }
}
function showError(node, error) {
  const key = error instanceof UIError ? error.key : "error.generic";
  const params = error instanceof UIError ? error.params : {};
  setMessage(node, key, params);
  if (error.code) {
    node._supportCode = error.code;
    node.textContent += ` ${t("Support code: {code}", {code: error.code})}`;
  } else delete node._supportCode;
}
function notice(message, failed = false) {
  clearTimeout(noticeTimer);
  const node = $("#notice");
  if (message instanceof Error) showError(node, message);
  else { delete node._supportCode; setMessage(node, message); }
  node.classList.toggle("failure", failed); node.hidden = false;
  noticeTimer = setTimeout(() => { node.hidden = true; }, failed ? 12000 : 5000);
}
function setMenu(open) {
  $("#console").classList.toggle("menu-open", open);
  $("#menu-toggle").setAttribute("aria-expanded", String(open));
}
function setPasswordVisible(show) {
  const toggle = $("#password-toggle"), label = show ? "Hide password" : "Show password";
  $("#password").type = show ? "text" : "password";
  toggle.setAttribute("aria-pressed", String(show));
  toggle.dataset.i18nAriaLabel = label; toggle.dataset.i18nTitle = label;
  toggle.setAttribute("aria-label", t(label)); toggle.title = t(label);
}
let refreshedAt;
function showRefreshed() {
  if (!refreshedAt) return;
  setMessage($("#refreshed"), "Updated {time}", {time: new Intl.DateTimeFormat(i18n.getLocale(), {timeStyle: "medium"}).format(refreshedAt)});
}
function showLogin() {
  csrf = "";
  setMenu(false);
  $("#console").hidden = true;
  $("#login-view").hidden = false;
  $("#password").value = "";
  setPasswordVisible(false);
  state = {hosts: [], agents: [], groups: [], settings: {}};
  document.querySelectorAll("dialog[open]").forEach(d => d.close());
}
async function api(path, method = "GET", body) {
  let response;
  try {
    response = await fetch(`/admin/api/${path}`, {
      method, credentials: "same-origin",
      headers: {"Content-Type": "application/json", "X-CSRF-Token": csrf},
      body: body === undefined ? undefined : JSON.stringify(body)
    });
  } catch { throw new UIError("error.network"); }
  let data;
  try { data = await response.json(); } catch { throw new UIError("error.response"); }
  if (!response.ok) {
    if (response.status === 401 && path !== "login") showLogin();
    const code = typeof data?.error?.code === "string" && /^[a-z0-9_]{1,80}$/.test(data.error.code) ? data.error.code : `http_${response.status}`;
    const known = ["unauthorized", "csrf_required", "invalid_json", "invalid_unicode", "invalid_settings", "invalid_host", "invalid_ip", "invalid_host_metadata", "invalid_agent", "agent_name_exists", "not_found", "forbidden", "internal_error", "authentication_capacity", "request_too_large"];
    const key = code === "unauthorized" && path === "login" ? "error.login" : known.includes(code) ? `error.${code}` : "error.generic";
  resetInspector();
    const error = new UIError(key); error.code = code; throw error;
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) throw new UIError("error.response");
  return data;
}
function el(tag, text, cls) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = String(text);
  if (cls) node.className = cls;
  return node;
}
function cell(row, text) { const node = el("td", text); row.append(node); return node; }
function statusCell(row, enabled) { const node = el("span", undefined, enabled ? "status" : "status off"); setMessage(node, enabled ? "Enabled" : "Disabled"); const td = cell(row); td.className = "cell-status"; td.append(node); }
const pageTitles = {overview: "Overview", hosts: "Hosts", agents: "Agents", groups: "Groups", channels: "Channels", settings: "Limits & retention"};
function route() {
  const names = Object.keys(pageTitles);
  const page = names.includes(location.hash.slice(1)) ? location.hash.slice(1) : "overview";
  // Section IDs differ from the hash so the browser never jumps to them.
  const changed = $(`#page-${page}`).hidden;
  names.forEach(name => { $(`#page-${name}`).hidden = name !== page; });
  if (changed) window.scrollTo(0, 0);
  setMessage($("#page-title"), pageTitles[page]);
  setMenu(false);
  document.querySelectorAll("[data-page]").forEach(a => {
    if (a.dataset.page === page) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  });
}
function ask(title, description, action, {danger = false, subject = "", changes = []} = {}) {
  setMessage($("#confirm-title"), title);
  $("#confirm-subject").textContent = subject; $("#confirm-subject").hidden = !subject;
  $("#confirm-changes").replaceChildren(...changes.map(change => {
    const item = el("li", undefined, change.lowered ? "lowered" : "");
    const name = el("span"); setMessage(name, settingLabels[change.name][0]);
    const values = el("span", undefined, "values"); setMessage(values, "{old} → {new}", {old: change.old, new: change.new});
    item.append(name, values);
    if (change.lowered) { const flag = el("span", undefined, "flag"); setMessage(flag, "Lowered"); item.append(flag); }
    return item;
  }));
  $("#confirm-changes").hidden = !changes.length;
  setMessage($("#confirm-description"), description);
  setMessage($("#confirm-action"), title);
  $("#confirm-action").className = danger ? "danger" : "primary";
  confirmAction = action;
  $("#confirm-dialog").showModal();
  if (page === "channels" && csrf && !channels.loaded && !channels.busy) loadChannels(true);
}
function keyResult(key, agent) {
  $("#issued-key").value = key;
  setMessage($("#issued-agent"), agent ? "Agent ID: {id}" : "The previous key is no longer valid.", agent ? {id: agent.id} : {});
  $("#key-dialog").showModal();
}
// The admin state endpoint has no defined order; sort so rows keep their
// positions across refreshes.
function byName(items) {
  const collator = new Intl.Collator(i18n.getLocale(), {numeric: true, sensitivity: "base"});
  return [...(items || [])].sort((a, b) => collator.compare(a.name || "", b.name || "") || String(a.id).localeCompare(String(b.id)));
}
const SVG = "http://www.w3.org/2000/svg";
function icon(name) {
  const svg = document.createElementNS(SVG, "svg"); svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS(SVG, "use"); use.setAttribute("href", `#${name}`);
  svg.append(use); return svg;
}
// Accessible names that interpolate user data; retranslated on language change.
function setAria(node, key, params = {}) {
  node.dataset.ariaKey = key; node._ariaParams = params;
  node.setAttribute("aria-label", t(key, params));
}
function copyText(text, done) {
  return navigator.clipboard.writeText(text).then(() => notice(done), () => notice("Copy failed.", true));
}
function shortId(id) {
  const value = String(id), cut = value.indexOf("_") + 7;
  return value.length > cut + 8 ? `${value.slice(0, cut)}…${value.slice(-4)}` : value;
}
function idChip(id) {
  const wrap = el("span", undefined, "id");
  const code = el("code", shortId(id)); code.title = id;
  const copy = el("button", undefined, "icon-button copy-id"); copy.type = "button";
  setAria(copy, "Copy ID"); copy.append(icon("i-copy"));
  copy.addEventListener("click", () => copyText(id, "ID copied."));
  wrap.append(code, copy); return wrap;
}
function relativeTime(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  const seconds = (date.getTime() - Date.now()) / 1000, size = Math.abs(seconds);
  const format = new Intl.RelativeTimeFormat(i18n.getLocale(), {numeric: "auto"});
  for (const [unit, length] of [["year", 31536000], ["month", 2592000], ["week", 604800], ["day", 86400], ["hour", 3600], ["minute", 60]]) {
    if (size >= length) return format.format(Math.round(seconds / length), unit);
  }
  return format.format(0, "second");
}
function timeNode(value) {
  const node = el("time", relativeTime(value)); node.dateTime = value;
  node.dataset.relative = value; node.title = i18n.formatDate(value); return node;
}
function label(key) { const node = el("span", undefined, "cell-label"); setMessage(node, key); return node; }

let openMenu;
const phone = window.matchMedia("(max-width: 767px)");
function closeMenu(returnFocus = false) {
  if (!openMenu) return;
  const {menu, toggle} = openMenu; openMenu = undefined;
  menu.hidden = true; toggle.setAttribute("aria-expanded", "false");
  $("#menu-backdrop").hidden = true;
  if (returnFocus) toggle.focus();
}
function showMenu(menu, toggle) {
  closeMenu();
  openMenu = {menu, toggle};
  menu.hidden = false; toggle.setAttribute("aria-expanded", "true");
  menu.style.top = ""; menu.style.left = "";
  if (phone.matches) $("#menu-backdrop").hidden = false;
  else {
    const anchor = toggle.getBoundingClientRect(), size = menu.getBoundingClientRect();
    const below = anchor.bottom + 4 + size.height <= window.innerHeight - 8;
    menu.style.top = `${below ? anchor.bottom + 4 : Math.max(8, anchor.top - 4 - size.height)}px`;
    menu.style.left = `${Math.max(8, Math.min(anchor.right - size.width, window.innerWidth - size.width - 8))}px`;
  }
  [...menu.querySelectorAll("[role=menuitem]")].find(node => node.offsetParent !== null)?.focus();
}
function rowMenu(name, items) {
  const wrap = el("div", undefined, "row-menu");
  const toggle = el("button", undefined, "icon-button row-menu-toggle"); toggle.type = "button";
  toggle.setAttribute("aria-haspopup", "menu"); toggle.setAttribute("aria-expanded", "false");
  setAria(toggle, "Actions for {name}", {name}); toggle.append(icon("i-more"));
  const menu = el("div", undefined, "menu"); menu.hidden = true; menu.setAttribute("role", "menu");
  setAria(menu, "Actions for {name}", {name});
  menu.append(el("p", name, "menu-title"));
  for (const item of items) {
    if (item === "-") { const line = el("div", undefined, "menu-sep"); line.setAttribute("role", "separator"); menu.append(line); continue; }
    const entry = el("button", undefined, ["menuitem", item.danger && "danger", item.phoneOnly && "phone-only"].filter(Boolean).join(" "));
    entry.type = "button"; entry.tabIndex = -1; entry.setAttribute("role", "menuitem"); setMessage(entry, item.label);
    entry.addEventListener("click", () => { closeMenu(true); Promise.resolve().then(item.action).catch(error => notice(error, true)); });
    menu.append(entry);
  }
  toggle.addEventListener("click", event => { event.stopPropagation(); if (openMenu?.menu === menu) closeMenu(); else showMenu(menu, toggle); });
  menu.addEventListener("keydown", event => {
    const entries = [...menu.querySelectorAll("[role=menuitem]")].filter(node => node.offsetParent !== null);
    const index = entries.indexOf(document.activeElement);
    const move = {ArrowDown: index + 1, ArrowUp: index - 1, Home: 0, End: entries.length - 1}[event.key];
    if (move !== undefined) { event.preventDefault(); entries[(move + entries.length) % entries.length]?.focus(); }
    else if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); closeMenu(true); }
    else if (event.key === "Tab") closeMenu();
  });
  wrap.append(toggle, menu); return wrap;
}
function editButton(action) {
  const node = el("button", undefined, "quiet compact edit"); node.type = "button";
  setMessage(node, "Edit"); node.addEventListener("click", action); return node;
}

// List pages: client-side filter and sort over the full admin state.
const lists = {
  hosts: {sort: "name", dir: 1}, agents: {sort: "name", dir: 1}, groups: {sort: "name", dir: 1}
};
function compareBy(key, dir, value) {
  const collator = new Intl.Collator(i18n.getLocale(), {numeric: true, sensitivity: "base"});
  return (a, b) => {
    const x = value(a, key), y = value(b, key);
    const order = typeof x === "number" ? x - y : collator.compare(x, y);
    return order * dir || collator.compare(a.name || "", b.name || "") || String(a.id).localeCompare(String(b.id));
  };
}
function filtered(name, items, text) {
  const list = lists[name], section = $(`#page-${name}`);
  const query = section.querySelector(".filter").value.trim().toLocaleLowerCase();
  const status = section.querySelector(".status-filter")?.value || "";
  const shown = items.filter(item => (!status || (status === "enabled") === !!item.enabled) && (!query || text(item).toLocaleLowerCase().includes(query)));
  section.querySelectorAll("th[data-sort]").forEach(th => {
    if (th.dataset.sort === list.sort) th.setAttribute("aria-sort", list.dir > 0 ? "ascending" : "descending");
    else th.removeAttribute("aria-sort");
  });
  const count = section.querySelector(".list-count");
  if (shown.length !== items.length) setMessage(count, "{shown} of {total}", {shown: shown.length, total: items.length}); else clearMessage(count);
  const empty = section.querySelector(".empty");
  if (items.length && !shown.length) setMessage(empty, "No results match the filter."); else setMessage(empty, empty.dataset.emptyKey);
  empty.hidden = shown.length > 0;
  section.querySelector("table").hidden = shown.length === 0;
  section.querySelector(".toolbar").hidden = items.length === 0;
  return shown;
}
function renderLists() {
  const hosts = state.hosts || [], agents = state.agents || [], groups = state.groups || [];
  const hostName = id => hosts.find(h => h.id === id)?.name || id;
  const agentName = id => agents.find(a => a.id === id)?.name || id;
  const hostValue = (host, key) => key === "status" ? Number(!host.enabled) : key === "os" ? host.os || "" : host.name || "";
  const shownHosts = filtered("hosts", hosts, host => [host.name, host.id, host.os, ...(host.ips || [])].join(" "))
    .sort(compareBy(lists.hosts.sort, lists.hosts.dir, hostValue));
  $("#host-rows").replaceChildren(...shownHosts.map(host => {
    const row = el("tr");
    const name = cell(row); name.className = "cell-name"; name.append(el("span", host.name, "primary"), idChip(host.id));
    const os = cell(row, host.os || undefined); os.className = "cell-meta1"; if (!host.os) setMessage(os, "Unspecified");
    const ips = cell(row); ips.className = "cell-meta2"; (host.ips || []).forEach(ip => ips.append(el("span", ip, "ip")));
    statusCell(row, host.enabled);
    const actions = cell(row); actions.className = "cell-actions";
    const toggle = () => ask(host.enabled ? "Disable host" : "Enable host", host.enabled ? "Agents on this host will no longer authenticate until the host is enabled." : "Agents on this host can authenticate with valid keys and allowed IP addresses.", async () => { await api(`hosts/${host.id}`, "PATCH", {enabled: !host.enabled}); await reload(); }, {danger: host.enabled, subject: host.name});
    actions.append(editButton(() => editHost(host)), rowMenu(host.name, [{label: "Edit", action: () => editHost(host), phoneOnly: true}, {label: host.enabled ? "Disable" : "Enable", action: toggle, danger: host.enabled}]));
    return row;
  }));
  const agentValue = (agent, key) => key === "status" ? Number(!agent.enabled) : key === "host" ? hostName(agent.host_id) : key === "last_access" ? (agent.last_access ? Date.parse(agent.last_access) : 0) : agent.name || "";
  const shownAgents = filtered("agents", agents, agent => [agent.name, agent.id, hostName(agent.host_id)].join(" "))
    .sort(compareBy(lists.agents.sort, lists.agents.dir, agentValue));
  $("#agent-rows").replaceChildren(...shownAgents.map(agent => {
    const row = el("tr");
    const name = cell(row); name.className = "cell-name"; name.append(el("span", agent.name, "primary"), idChip(agent.id));
    const host = cell(row); host.className = "cell-meta1"; host.append(label("Host"), el("span", hostName(agent.host_id)));
    const access = cell(row); access.className = "cell-meta2"; access.append(label("Last access"));
    if (agent.last_access) access.append(timeNode(agent.last_access)); else { const never = el("span", undefined, "muted"); setMessage(never, "Never accessed"); access.append(never); }
    statusCell(row, agent.enabled);
    const actions = cell(row); actions.className = "cell-actions";
    actions.append(editButton(() => editAgent(agent)), rowMenu(agent.name, [
      {label: "Edit", action: () => editAgent(agent), phoneOnly: true},
      {label: "Rotate key", action: () => ask("Rotate key", "The current API key stops working immediately. Update this agent's sessions with the new key.", async () => { const result = await api(`agents/${agent.id}/rotate-key`, "POST", {}); keyResult(result.api_key, agent); }, {danger: true, subject: agent.name})},
      {label: "Revoke key", danger: true, action: () => ask("Revoke key", "The current key stops working immediately. Issue a new key with Rotate key when the agent needs access again.", async () => { await api(`agents/${agent.id}/revoke-key`, "POST", {}); notice("API key revoked."); }, {danger: true, subject: agent.name})},
      "-",
      {label: agent.enabled ? "Disable" : "Enable", danger: agent.enabled, action: () => ask(agent.enabled ? "Disable agent" : "Enable agent", "This changes account access. Retained conversations continue to follow the channel's retention rules.", async () => { await api(`agents/${agent.id}`, "PATCH", {enabled: !agent.enabled}); await reload(); }, {danger: agent.enabled, subject: agent.name})}
    ]));
    return row;
  }));
  const groupValue = (group, key) => key === "owner" ? agentName(group.owner_id) : group.name || "";
  const shownGroups = filtered("groups", groups, group => [group.name, group.id, agentName(group.owner_id)].join(" "))
    .sort(compareBy(lists.groups.sort, lists.groups.dir, groupValue));
  $("#group-rows").replaceChildren(...shownGroups.map(group => {
    const row = el("tr");
    const name = cell(row); name.className = "cell-name"; name.append(el("span", group.name, "primary"));
    const id = cell(row); id.className = "cell-meta1"; id.append(idChip(group.id));
    const owner = cell(row); owner.className = "cell-meta2"; owner.append(label("Owner"), el("span", agentName(group.owner_id)));
    return row;
  }));
}
function render() {
  const hosts = state.hosts || [], agents = state.agents || [], groups = state.groups || [];
  const disabledHosts = hosts.filter(host => !host.enabled), disabledAgents = agents.filter(agent => !agent.enabled);
  setNumber($("#host-count"), hosts.length);
  setNumber($("#agent-count"), agents.length);
  setNumber($("#group-count"), groups.length);
  for (const [node, items, disabled] of [[$("#host-note"), hosts, disabledHosts], [$("#agent-note"), agents, disabledAgents]]) {
    if (!items.length) clearMessage(node);
    else setMessage(node, disabled.length ? "Disabled: {count}" : "All enabled", {count: disabled.length});
    node.classList.toggle("warn", disabled.length > 0);
  }
  $("#setup-panel").hidden = hosts.length > 0 && agents.length > 0;
  $("#setup-host").classList.toggle("done", hosts.length > 0);
  $("#setup-agent").classList.toggle("done", agents.length > 0);
  renderAttention(hosts, agents, disabledHosts);
    const actions = cell(row); actions.className = "cell-actions";
    const view = el("button", undefined, "secondary compact"); view.type = "button"; setMessage(view, "View messages");
    view.addEventListener("click", () => { location.hash = "#channels"; route(); inspectChannel({id: group.id, group_id: group.id, kind: "group"}); });
    actions.append(view);
  const cleanup = state.cleanup || {};
  setMessage($("#cleanup-summary"), cleanup.pending_files ? "Files awaiting deletion: {count}." : "No files awaiting deletion.", {count: cleanup.pending_files || 0});
  const cleanupErrors = {"physical file deletion failed": "cleanup.physical", "staging cleanup failed": "cleanup.staging", "unreferenced file cleanup failed": "cleanup.orphan"};
const channels = {items: [], cursor: "", next: "", back: [], loaded: false, busy: false, request: 0};
const messages = {cursor: "", next: "", back: [], busy: false, request: 0};
let inspectedChannel;
let channelSearchTimer;
function agentName(id) { return state.agents.find(agent => agent.id === id)?.name || id; }
function channelName(channel) {
  return channel.kind === "group" ? state.groups.find(group => group.id === channel.group_id)?.name || channel.group_id : (channel.agent_ids || []).map(agentName).join(" ↔ ") || channel.id;
}
function absoluteTimeNode(value) {
  const node = el("time", i18n.formatDate(value)); node.dateTime = value; node.dataset.absolute = value; return node;
}
function renderChannels() {
  $("#channel-list").replaceChildren(...channels.items.map(channel => {
    const button = el("button", undefined, "channel-choice"); button.type = "button";
    button.dataset.channelId = channel.id; button.setAttribute("aria-pressed", String(channel.id === inspectedChannel?.id));
    setAria(button, "View history for {name}", {name: channelName(channel)});
    const name = el("strong", channelName(channel));
    const meta = el("span", undefined, "channel-meta"), kind = el("span"), count = el("span");
    setMessage(kind, channel.kind === "group" ? "Group" : "Private");
    setMessage(count, "{count} retained messages", {count: channel.message_count});
    meta.append(kind, count);
    const files = el("span", undefined, "help"); setMessage(files, "{count} retained files", {count: channel.file_count});
    button.append(name, meta, files, el("span", channel.id, "channel-id"));
    if (channel.last_message_at) button.append(timeNode(channel.last_message_at));
    button.addEventListener("click", () => inspectChannel(channel));
    return button;
  }));
  $("#channels-prev").disabled = channels.busy || !channels.back.length;
  $("#channels-next").disabled = channels.busy || !channels.next;
  $("#channels-empty").hidden = channels.busy || !channels.loaded || channels.items.length > 0 || !!$("#channels-error").textContent;
  setMessage($("#channels-empty"), $("#channel-query").value.trim() || $("#channel-kind").value ? "No results match the filter." : "No channels yet.");
  if (inspectedChannel) $("#history-title").textContent = channelName(inspectedChannel);
}
async function loadChannels(reset = false) {
  clearTimeout(channelSearchTimer);
  if (reset) { channels.cursor = ""; channels.back = []; }
  const request = ++channels.request;
  channels.busy = true; channels.items = []; channels.next = ""; clearMessage($("#channels-error"));
  setMessage($("#channels-status"), "Loading channels…"); $("#channels-status").hidden = false;
  renderChannels();
  const query = new URLSearchParams({limit: Math.min(20, state.settings.page_size || 20)});
  if (channels.cursor) query.set("cursor", channels.cursor);
  if ($("#channel-kind").value) query.set("kind", $("#channel-kind").value);
  if ($("#channel-query").value.trim()) query.set("q", $("#channel-query").value.trim());
  try {
    const result = await api(`channels?${query}`);
    if (request !== channels.request) return;
    channels.items = result.channels || []; channels.next = result.next_cursor || ""; channels.loaded = true;
  } catch (error) {
    if (request !== channels.request) return;
    showError($("#channels-error"), error);
  } finally {
    if (request === channels.request) { channels.busy = false; $("#channels-status").hidden = true; renderChannels(); }
  }
}
function inspectChannel(channel, focus = true) {
  inspectedChannel = channel;
  $("#history-placeholder").hidden = true; $("#channel-detail").hidden = false;
  $("#history-title").textContent = channelName(channel);
  $("#history-channel-id").replaceChildren(idChip(channel.id));
  renderChannels();
  loadMessages(true);
  if (focus) { $("#history-title").focus({preventScroll: true}); $("#channel-detail").scrollIntoView({block: "nearest"}); }
}
function renderMessages(items) {
  $("#message-list").replaceChildren(...items.map(message => {
    const item = el("article", undefined, "message"); item.dataset.messageId = message.id;
    const head = el("header", undefined, "message-head"); head.append(el("strong", agentName(message.sender_id)), absoluteTimeNode(message.created_at));
    item.append(head, el("p", message.body, "message-body"));
    if (message.attachment) {
      const file = message.attachment, attachment = el("div", undefined, "message-attachment"), title = el("span", undefined, "help");
      setMessage(title, "Attachment");
      const size = el("span", undefined, "help"); setMessage(size, "{size} bytes", {size: file.size});
      attachment.append(title, el("span", file.filename, "attachment-name"), size, el("span", file.media_type, "help"), idChip(file.id));
      item.append(attachment);
    }
    const ids = el("div", undefined, "message-identities"); ids.append(idChip(message.sender_id), idChip(message.id)); item.append(ids);
    return item;
  }));
}
function messagePaging() {
  $("#messages-prev").disabled = messages.busy || !messages.back.length;
  $("#messages-next").disabled = messages.busy || !messages.next;
}
async function loadMessages(reset = false) {
  if (!inspectedChannel) return;
  if (reset) { messages.cursor = ""; messages.back = []; }
  const request = ++messages.request;
  messages.busy = true; messages.next = ""; clearMessage($("#history-error")); renderMessages([]); messagePaging();
  $("#messages-empty").hidden = true; $("#history-status").hidden = false; setMessage($("#history-status"), "Loading messages…");
  const query = new URLSearchParams({limit: Math.min(20, state.settings.page_size || 20), order: $("#history-order").value});
  if (messages.cursor) query.set("cursor", messages.cursor);
  try {
    const result = await api(`channels/${encodeURIComponent(inspectedChannel.id)}/messages?${query}`);
    if (request !== messages.request) return;
    renderMessages(result.messages || []); messages.next = result.next_cursor || "";
    $("#messages-empty").hidden = !!result.messages?.length;
  } catch (error) {
    if (request !== messages.request) return;
    showError($("#history-error"), error);
  } finally {
    if (request === messages.request) { messages.busy = false; $("#history-status").hidden = true; messagePaging(); }
  }
}
function resetInspector() {
  clearTimeout(channelSearchTimer); ++channels.request; ++messages.request;
  Object.assign(channels, {items: [], cursor: "", next: "", back: [], loaded: false, busy: false});
  Object.assign(messages, {cursor: "", next: "", back: [], busy: false}); inspectedChannel = undefined;
  $("#channel-query").value = ""; $("#channel-kind").value = ""; $("#history-order").value = "newest";
  $("#channel-list").replaceChildren(); $("#message-list").replaceChildren();
  $("#history-title").textContent = ""; $("#history-channel-id").replaceChildren();
  $("#channel-detail").hidden = true; $("#history-placeholder").hidden = false;
  $("#channels-status").hidden = true; $("#history-status").hidden = true;
  clearMessage($("#channels-error")); clearMessage($("#history-error"));
}
  if (cleanup.last_error) setMessage($("#cleanup-error"), cleanupErrors[cleanup.last_error] || "cleanup.generic");
  else clearMessage($("#cleanup-error"));
  setNumber($("#overview-revision"), state.settings.revision || 1);
  renderLists();
  renderSettings();
}
function renderAttention(hosts, agents, disabledHosts) {
  const off = new Set(disabledHosts.map(host => host.id));
  const items = [
    ...byName(disabledHosts).map(host => ["Host {name} is disabled.", host.name, "#hosts", "Manage hosts"]),
    ...byName(agents.filter(agent => !agent.enabled)).map(agent => ["Agent {name} is disabled.", agent.name, "#agents", "Manage agents"]),
    ...byName(agents.filter(agent => agent.enabled && off.has(agent.host_id))).map(agent => ["Agent {name} cannot authenticate because its host is disabled.", agent.name, "#hosts", "Manage hosts"]),
    ...byName(agents.filter(agent => agent.enabled && !off.has(agent.host_id) && !agent.last_access)).map(agent => ["Agent {name} has not accessed the service yet.", agent.name, "#agents", "Manage agents"])
  ];
  const limit = 6, list = items.slice(0, limit).map(([key, name, href, link]) => {
    const item = el("li"), text = el("span"); setMessage(text, key, {name});
    const anchor = el("a"); anchor.href = href; setMessage(anchor, link);
    item.append(text, anchor); return item;
  });
  if (items.length > limit) { const more = el("li", undefined, "more"); setMessage(more, "And {count} more.", {count: items.length - limit}); list.push(more); }
  $("#attention-list").replaceChildren(...list);
  $("#attention-list").hidden = !items.length;
  $("#attention-empty").hidden = items.length > 0;
  renderChannels();
}
function formatHint(name, value) {
  if (!Number.isFinite(value) || value <= 0) return "";
  const locale = i18n.getLocale();
  if (name.endsWith("bytes") && value >= 1024) {
    const [size, unit] = value >= 1 << 30 ? [value / (1 << 30), "GiB"] : value >= 1 << 20 ? [value / (1 << 20), "MiB"] : [value / 1024, "KiB"];
    return `= ${new Intl.NumberFormat(locale, {maximumFractionDigits: 1}).format(size)} ${unit}`;
  }
  if (name === "claim_seconds" && value >= 60) {
    const [size, unit] = value >= 3600 ? [value / 3600, "hour"] : [value / 60, "minute"];
    return `= ${new Intl.NumberFormat(locale, {style: "unit", unit, unitDisplay: "long", maximumFractionDigits: 1}).format(size)}`;
  }
  return "";
}
let settingsBaseline = {};
function settingChanges() {
  return [...$("#settings-fields").querySelectorAll("input")]
    .filter(input => input.value !== "" && Number(input.value) !== settingsBaseline[input.name])
    .map(input => ({name: input.name, old: settingsBaseline[input.name], new: Number(input.value), lowered: destructiveSettings.includes(input.name) && Number(input.value) < settingsBaseline[input.name]}));
}
function updateSettingsState() {
  const changes = settingChanges(), changed = new Set(changes.map(change => change.name));
  $("#settings-fields").querySelectorAll("input").forEach(input => {
    const wrap = input.closest(".field"), was = wrap.querySelector(".was");
    wrap.classList.toggle("changed", changed.has(input.name));
    if (changed.has(input.name)) setMessage(was, "Was {value}", {value: settingsBaseline[input.name]}); else clearMessage(was);
    was.hidden = !changed.has(input.name);
    wrap.querySelector(".unit-hint").textContent = formatHint(input.name, Number(input.value));
  });
  setMessage($("#settings-dirty"), changes.length ? "Unsaved changes: {count}" : "No unsaved changes", {count: changes.length});
  $("#settings-form").classList.toggle("dirty", changes.length > 0);
  $("#settings-discard").disabled = !changes.length;
  $("#settings-form").querySelector('[type="submit"]').disabled = !changes.length;
}
function renderSettings() {
  const fields = $("#settings-fields");
  // Keep unsaved edits across refreshes; rebuild only when nothing is pending.
  if (!fields.querySelector("input") || !settingChanges().length) {
    fields.replaceChildren(...settingGroups.map(([legend, description, names]) => {
      const set = el("fieldset", undefined, "panel setting-group");
      const title = el("legend"); setMessage(title, legend); set.append(title);
      if (description) { const note = el("p", undefined, "group-note"); setMessage(note, description); set.append(note); }
      const grid = el("div", undefined, "settings-grid");
      for (const name of names.filter(name => name in state.settings)) {
        const [labelKey, help] = settingLabels[name];
        const wrap = el("div", undefined, "field"), lbl = el("label"); setMessage(lbl, labelKey); lbl.htmlFor = `setting-${name}`;
        const row = el("div", undefined, "input-row");
        const input = el("input"); input.type = "number"; input.inputMode = "numeric"; input.id = `setting-${name}`; input.name = name; input.value = state.settings[name];
        input.min = name === "file_count" ? "0" : "1"; input.max = String(settingMax[name]); input.step = "1"; input.required = true;
        const unit = el("span", undefined, "unit-hint"); unit.setAttribute("aria-hidden", "true");
        row.append(input, unit);
        const hint = el("p", undefined, "help"); setMessage(hint, help); hint.id = `help-${name}`; input.setAttribute("aria-describedby", hint.id);
        const was = el("p", undefined, "was"); was.hidden = true;
        wrap.append(lbl, row, was, hint); grid.append(wrap);
      }
      set.append(grid); return set;
    }));
  }
  settingsBaseline = {...state.settings};
  setMessage($("#settings-revision"), "Settings revision {revision}", {revision: state.settings.revision || 1});
  updateSettingsState();
}
async function reload() {
  state = await api("state"); refreshedAt = new Date(); render(); showRefreshed();
  if (location.hash === "#channels") {
    await loadChannels(true);
    if (inspectedChannel) await loadMessages(true);
  }
}
function editHost(host) {
  const form = $("#host-form"); form.reset();
  form.elements.id.value = host?.id || "";
  form.elements.name.value = host?.name || "";
  form.elements.os.value = host?.os || "";
  form.elements.ips.value = (host?.ips || []).join("\n");
  form.elements.metadata.value = JSON.stringify(host?.metadata || {}, null, 2);
  setMessage($("#host-title"), host ? "Edit host" : "Register host");
  clearMessage(form.querySelector(".form-error"));
  $("#host-dialog").showModal();
}
function editAgent(agent) {
  if (!state.hosts.length) { notice("Register a host before creating an agent.", true); return; }
  const form = $("#agent-form"); form.reset();
  form.elements.id.value = agent?.id || "";
  form.elements.name.value = agent?.name || "";
  $("#agent-host").replaceChildren(...byName(state.hosts).map(host => { const option = el("option", host.name); option.value = host.id; return option; }));
  if (agent) form.elements.host_id.value = agent.host_id;
  setMessage($("#agent-title"), agent ? "Edit agent" : "Create agent");
  clearMessage(form.querySelector(".form-error"));
  $("#agent-dialog").showModal();
}
async function formAction(form, action) {
  const submit = form.querySelector('[type="submit"]'); submit.disabled = true;
  const errorNode = form.querySelector(".form-error"); if (errorNode) clearMessage(errorNode);
  try { await action(); } catch (error) { if (errorNode) showError(errorNode, error); else notice(error, true); }
  finally { submit.disabled = false; }
}
$("#login-form").addEventListener("submit", event => {
  event.preventDefault();
  formAction(event.currentTarget, async () => {
    clearMessage($("#login-error"));
    try {
      const result = await api("login", "POST", {password: $("#password").value}); csrf = result.csrf_token;
      await reload(); $("#password").value = ""; $("#login-view").hidden = true; $("#console").hidden = false; route();
    } catch (error) { showError($("#login-error"), error); }
  });
});
$("#host-form").addEventListener("submit", event => {
  event.preventDefault(); const form = event.currentTarget;
  formAction(form, async () => {
    let metadata;
    try { metadata = JSON.parse(form.elements.metadata.value || "{}"); } catch { throw new UIError("Metadata must be a JSON object with string values."); }
    if (!metadata || Array.isArray(metadata) || typeof metadata !== "object" || Object.values(metadata).some(value => typeof value !== "string")) throw new UIError("Metadata must be a JSON object with string values.");
    const id = form.elements.id.value;
    await api(id ? `hosts/${id}` : "hosts", id ? "PATCH" : "POST", {name: form.elements.name.value, os: form.elements.os.value, ips: form.elements.ips.value.split(/[\s,]+/).filter(Boolean), metadata});
    $("#host-dialog").close(); await reload(); notice(id ? "Host updated." : "Host registered.");
  });
});
$("#agent-form").addEventListener("submit", event => {
  event.preventDefault(); const form = event.currentTarget;
  formAction(form, async () => {
    const id = form.elements.id.value;
    const result = await api(id ? `agents/${id}` : "agents", id ? "PATCH" : "POST", {name: form.elements.name.value, host_id: form.elements.host_id.value});
    $("#agent-dialog").close(); await reload();
    if (result.api_key) keyResult(result.api_key, result.agent); else notice("Agent updated.");
  });
});
$("#settings-form").addEventListener("input", updateSettingsState);
$("#settings-discard").addEventListener("click", () => {
  $("#settings-fields").querySelectorAll("input").forEach(input => { input.value = settingsBaseline[input.name]; input.setCustomValidity(""); });
  updateSettingsState();
});
$("#settings-form").addEventListener("submit", event => {
  event.preventDefault(); const form = event.currentTarget;
  if (!form.reportValidity()) return;
  const settings = {...state.settings};
  for (const [name] of Object.entries(settingLabels)) if (form.elements[name]) settings[name] = Number(form.elements[name].value);
  if (Object.entries(settings).some(([name, value]) => name !== "revision" && (!Number.isSafeInteger(value) || value < (name === "file_count" ? 0 : 1)))) { notice("Use whole numbers within the permitted limits.", true); return; }
  const changes = settingChanges();
  if (!changes.length) return;
  const deletes = changes.some(change => change.lowered);
  ask("Save limits", deletes ? "These limits apply to all channels and to existing content. Messages and files that exceed the new limits will be deleted." : "This change does not delete stored content. It applies to all channels.", async () => { await api("settings", "PUT", settings); await reload(); notice(deletes ? "Limits saved. Retention cleanup applied." : "Limits saved."); }, {danger: deletes, changes});
});
$("#confirm-form").addEventListener("submit", event => {
  event.preventDefault(); const action = confirmAction; confirmAction = undefined;
  $("#confirm-dialog").close();
  if (action) action().catch(error => notice(error, true));
});
document.querySelectorAll(".close-dialog").forEach(node => node.addEventListener("click", () => node.closest("dialog").close()));
$("#key-dialog").addEventListener("close", () => { $("#issued-key").value = ""; clearMessage($("#issued-agent")); setMessage($("#copy-key"), "Copy key"); });
$("#copy-key").addEventListener("click", async () => {
  const field = $("#issued-key");
  try { await navigator.clipboard.writeText(field.value); setMessage($("#copy-key"), "Copied"); }
  catch { field.focus(); field.select(); notice("Copy failed. Select the key and copy it manually.", true); }
});
$("#confirm-dialog").addEventListener("close", () => { confirmAction = undefined; });
$("#new-host").addEventListener("click", () => editHost());
$("#new-agent").addEventListener("click", () => editAgent());
$("#refresh").addEventListener("click", () => reload().then(() => notice("Data refreshed.")).catch(error => notice(error, true)));
$("#cleanup-retry").addEventListener("click", () => api("cleanup", "POST", {}).then(reload).then(() => notice("Cleanup retried.")).catch(error => notice(error, true)));
document.querySelectorAll(".filter, .status-filter").forEach(control => control.addEventListener("input", renderLists));
document.querySelectorAll(".empty").forEach(node => { node.dataset.emptyKey = node.dataset.i18n; });
document.querySelectorAll("th[data-sort] button").forEach(node => node.addEventListener("click", () => {
  const list = lists[node.closest(".page").id.slice(5)], key = node.closest("th").dataset.sort;
  list.dir = list.sort === key ? -list.dir : key === "last_access" ? -1 : 1; list.sort = key;
  renderLists();
}));
$("#menu-backdrop").addEventListener("click", () => closeMenu(true));
window.addEventListener("resize", () => closeMenu());
window.addEventListener("scroll", () => { if (!phone.matches) closeMenu(); }, {passive: true});
$("#menu-toggle").addEventListener("click", event => { event.stopPropagation(); setMenu(!$("#console").classList.contains("menu-open")); });
document.addEventListener("click", event => {
  if (!event.target.closest("#account-panel, #menu-toggle")) setMenu(false);
  if (openMenu && !event.target.closest(".menu")) closeMenu();
});
document.addEventListener("keydown", event => {
  if (event.key === "Escape" && $("#console").classList.contains("menu-open")) { setMenu(false); $("#menu-toggle").focus(); }
$("#channel-query").addEventListener("input", () => {
  clearTimeout(channelSearchTimer);
  // Invalidate an in-flight page before the debounce ends.
  ++channels.request; channels.next = ""; channels.busy = true;
  $("#channels-prev").disabled = true; $("#channels-next").disabled = true;
  channelSearchTimer = setTimeout(() => loadChannels(true), 250);
});
$("#channel-kind").addEventListener("change", () => loadChannels(true));
$("#channels-next").addEventListener("click", () => { channels.back.push(channels.cursor); channels.cursor = channels.next; loadChannels(); });
$("#channels-prev").addEventListener("click", () => { channels.cursor = channels.back.pop() || ""; loadChannels(); });
$("#history-order").addEventListener("change", () => loadMessages(true));
$("#messages-next").addEventListener("click", () => { messages.back.push(messages.cursor); messages.cursor = messages.next; loadMessages(); });
$("#messages-prev").addEventListener("click", () => { messages.cursor = messages.back.pop() || ""; loadMessages(); });
});
$("#password-toggle").addEventListener("click", () => setPasswordVisible($("#password").type === "password"));
$("#logout").addEventListener("click", async () => { try { await api("logout", "POST", {}); showLogin(); } catch (error) { notice(error, true); } });
function setNumber(node, value) { node._i18nNumber = value; node.textContent = i18n.formatNumber(value); }
function localizeValidity(control) {
  if (!control.setCustomValidity || control.tagName === "BUTTON") return;
  control.setCustomValidity("");
  const validity = control.validity;
  if (validity.valid) return;
  let key = "validation.generic", params = {};
  if (validity.valueMissing) key = "validation.required";
  else if (validity.badInput || validity.stepMismatch) key = "validation.number";
  else if (validity.rangeUnderflow) { key = "validation.minimum"; params.minimum = Number(control.min); }
  else if (validity.rangeOverflow) { key = "validation.maximum"; params.maximum = Number(control.max); }
  control.setCustomValidity(t(key, params));
}
document.addEventListener("invalid", event => localizeValidity(event.target), true);
document.addEventListener("input", event => { if (event.target.setCustomValidity) event.target.setCustomValidity(""); }, true);
window.addEventListener("agentpost:languagechange", () => {
  // Update text in place: no rerender, form reset, modal close or API request.
  document.querySelectorAll("[data-relative]").forEach(node => { node.textContent = relativeTime(node.dataset.relative); node.title = i18n.formatDate(node.dataset.relative); });
  document.querySelectorAll("[data-aria-key]").forEach(node => node.setAttribute("aria-label", t(node.dataset.ariaKey, node._ariaParams || {})));
  showRefreshed();
  document.querySelectorAll(".unit-hint").forEach(node => { const input = node.previousElementSibling; node.textContent = formatHint(input.name, Number(input.value)); });
  document.querySelectorAll("#host-count, #agent-count, #group-count, #overview-revision").forEach(node => { if (node._i18nNumber !== undefined) node.textContent = i18n.formatNumber(node._i18nNumber); });
  document.querySelectorAll("[data-i18n]").forEach(node => { if (node._supportCode) node.textContent += ` ${t("Support code: {code}", {code: node._supportCode})}`; });
  document.querySelectorAll("input, select, textarea").forEach(control => { if (control.validity.customError) localizeValidity(control); });
});
window.addEventListener("hashchange", route);
(async () => {
  try { const session = await api("session"); csrf = session.csrf_token; await reload(); $("#login-view").hidden = true; $("#console").hidden = false; route(); }
  catch { showLogin(); }
})();
  document.querySelectorAll("[data-absolute]").forEach(node => { node.textContent = i18n.formatDate(node.dataset.absolute); });

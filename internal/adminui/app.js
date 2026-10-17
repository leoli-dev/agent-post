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
function showLogin() {
  csrf = "";
  $("#console").hidden = true;
  $("#login-view").hidden = false;
  $("#password").value = "";
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
function button(text, action) {
  const node = el("button", undefined, "quiet"); setMessage(node, text);
  node.type = "button";
  node.addEventListener("click", async () => {
    node.disabled = true;
    try { await action(); } catch (error) { notice(error, true); }
    finally { node.disabled = false; }
  });
  return node;
}
function statusCell(row, enabled) { const node = el("span", undefined, enabled ? "status" : "status off"); setMessage(node, enabled ? "Enabled" : "Disabled"); cell(row).append(node); }
function route() {
  const names = ["overview", "hosts", "agents", "groups", "settings"];
  const page = names.includes(location.hash.slice(1)) ? location.hash.slice(1) : "overview";
  names.forEach(name => { $(`#${name}`).hidden = name !== page; });
  document.querySelectorAll("[data-page]").forEach(a => {
    if (a.dataset.page === page) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  });
}
function ask(title, description, action) {
  setMessage($("#confirm-title"), title);
  setMessage($("#confirm-description"), description);
  setMessage($("#confirm-action"), title);
  confirmAction = action;
  $("#confirm-dialog").showModal();
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
function render() {
  const hosts = byName(state.hosts);
  const agents = byName(state.agents);
  const groups = byName(state.groups);
  const hostName = id => hosts.find(h => h.id === id)?.name || id;
  const agentName = id => agents.find(a => a.id === id)?.name || id;
  setNumber($("#host-count"), hosts.length);
  setNumber($("#agent-count"), agents.filter(a => a.enabled).length);
  setNumber($("#group-count"), groups.length);
  const cleanup = state.cleanup || {};
  setMessage($("#cleanup-summary"), cleanup.pending_files ? "Files awaiting deletion: {count}." : "No files awaiting deletion.", {count: cleanup.pending_files || 0});
  const cleanupErrors = {"physical file deletion failed": "cleanup.physical", "staging cleanup failed": "cleanup.staging", "unreferenced file cleanup failed": "cleanup.orphan"};
  if (cleanup.last_error) setMessage($("#cleanup-error"), cleanupErrors[cleanup.last_error] || "cleanup.generic");
  else clearMessage($("#cleanup-error"));
  $("#host-rows").replaceChildren(...hosts.map(host => {
    const row = el("tr");
    cell(row, host.name).append(el("small", host.id));
    if (host.os) cell(row, host.os); else setMessage(cell(row), "Unspecified");
    const ips = cell(row); (host.ips || []).forEach(ip => ips.append(el("div", ip)));
    statusCell(row, host.enabled);
    const actions = cell(row);
    actions.append(button("Edit", () => editHost(host)), button(host.enabled ? "Disable" : "Enable", () => ask(host.enabled ? "Disable host" : "Enable host", host.enabled ? "Agents on this host will no longer authenticate until the host is enabled." : "Agents on this host can authenticate with valid keys and allowed IP addresses.", async () => { await api(`hosts/${host.id}`, "PATCH", {enabled: !host.enabled}); await reload(); })));
    return row;
  }));
  $("#hosts-empty").hidden = hosts.length > 0;
  $("#agent-rows").replaceChildren(...agents.map(agent => {
    const row = el("tr");
    cell(row, agent.name).append(el("small", agent.id));
    cell(row, hostName(agent.host_id));
    if (agent.last_access) { const node = cell(row, i18n.formatDate(agent.last_access)); node.dataset.i18nDate = agent.last_access; } else setMessage(cell(row), "Never accessed");
    statusCell(row, agent.enabled);
    const actions = cell(row);
    actions.append(button("Edit", () => editAgent(agent)), button("Rotate key", () => ask("Rotate key", "The current API key stops working immediately. Update this agent's sessions with the new key.", async () => { const result = await api(`agents/${agent.id}/rotate-key`, "POST", {}); keyResult(result.api_key, agent); })), button("Revoke key", () => ask("Revoke key", "The current key stops working immediately. Issue a new key with Rotate key when the agent needs access again.", async () => { await api(`agents/${agent.id}/revoke-key`, "POST", {}); notice("API key revoked."); })), button(agent.enabled ? "Disable" : "Enable", () => ask(agent.enabled ? "Disable agent" : "Enable agent", "This changes account access. Retained conversations continue to follow the channel's retention rules.", async () => { await api(`agents/${agent.id}`, "PATCH", {enabled: !agent.enabled}); await reload(); })));
    return row;
  }));
  $("#agents-empty").hidden = agents.length > 0;
  $("#group-rows").replaceChildren(...groups.map(group => {
    const row = el("tr"); cell(row, group.name); cell(row, group.id); cell(row, agentName(group.owner_id)); return row;
  }));
  $("#groups-empty").hidden = groups.length > 0;
  $("#settings-fields").replaceChildren(...Object.entries(settingLabels).filter(([name]) => name in state.settings).map(([name, [label, help]]) => {
    const wrap = el("div"); const lbl = el("label"); setMessage(lbl, label); lbl.htmlFor = `setting-${name}`;
    const input = el("input"); input.type = "number"; input.id = `setting-${name}`; input.name = name; input.value = state.settings[name]; input.min = name === "file_count" ? "0" : "1"; input.step = "1"; input.required = true;
    const hint = el("p"); setMessage(hint, help); hint.id = `help-${name}`; input.setAttribute("aria-describedby", hint.id); wrap.append(lbl, input, hint); return wrap;
  }));
  setMessage($("#settings-revision"), "Settings revision {revision}", {revision: state.settings.revision || 1});
}
async function reload() { state = await api("state"); render(); }
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
$("#settings-form").addEventListener("submit", event => {
  event.preventDefault(); const form = event.currentTarget;
  if (!form.reportValidity()) return;
  const settings = {...state.settings};
  for (const [name] of Object.entries(settingLabels)) if (form.elements[name]) settings[name] = Number(form.elements[name].value);
  if (Object.entries(settings).some(([name, value]) => name !== "revision" && (!Number.isSafeInteger(value) || value < (name === "file_count" ? 0 : 1)))) { notice("Use whole numbers within the permitted limits.", true); return; }
  ask("Save limits", "These limits apply to all channels and to existing content. Messages and files that exceed the new limits will be deleted.", async () => { await api("settings", "PUT", settings); await reload(); notice("Limits saved. Retention cleanup applied."); });
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
  document.querySelectorAll("[data-i18n-date]").forEach(node => { node.textContent = i18n.formatDate(node.dataset.i18nDate); });
  document.querySelectorAll("#host-count, #agent-count, #group-count").forEach(node => { if (node._i18nNumber !== undefined) node.textContent = i18n.formatNumber(node._i18nNumber); });
  document.querySelectorAll("[data-i18n]").forEach(node => { if (node._supportCode) node.textContent += ` ${t("Support code: {code}", {code: node._supportCode})}`; });
  document.querySelectorAll("input, select, textarea").forEach(control => { if (control.validity.customError) localizeValidity(control); });
});
window.addEventListener("hashchange", route);
(async () => {
  try { const session = await api("session"); csrf = session.csrf_token; await reload(); $("#login-view").hidden = true; $("#console").hidden = false; route(); }
  catch { showLogin(); }
})();

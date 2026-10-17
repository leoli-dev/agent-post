"use strict";
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
function notice(message, failed = false) {
  clearTimeout(noticeTimer);
  $("#notice").textContent = message;
  $("#notice").classList.toggle("failure", failed);
  $("#notice").hidden = false;
  noticeTimer = setTimeout(() => { $("#notice").hidden = true; }, failed ? 12000 : 5000);
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
  const response = await fetch(`/admin/api/${path}`, {
    method,
    credentials: "same-origin",
    headers: {"Content-Type": "application/json", "X-CSRF-Token": csrf},
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401 && path !== "login") showLogin();
    const detail = data.error || data;
    throw new Error(detail.message || detail.code || `Request failed (${response.status}).`);
  }
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
  const node = el("button", text, "quiet");
  node.type = "button";
  node.addEventListener("click", async () => {
    node.disabled = true;
    try { await action(); } catch (error) { notice(error.message, true); }
    finally { node.disabled = false; }
  });
  return node;
}
function statusCell(row, enabled) { cell(row).append(el("span", enabled ? "Enabled" : "Disabled", enabled ? "status" : "status off")); }
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
  $("#confirm-title").textContent = title;
  $("#confirm-description").textContent = description;
  $("#confirm-action").textContent = title;
  confirmAction = action;
  $("#confirm-dialog").showModal();
}
function keyResult(key, agent) {
  $("#issued-key").value = key;
  $("#issued-agent").textContent = agent ? `Agent ID: ${agent.id}` : "The previous key is no longer valid.";
  $("#key-dialog").showModal();
}
function render() {
  const hosts = state.hosts || [];
  const agents = state.agents || [];
  const groups = state.groups || [];
  const hostName = id => hosts.find(h => h.id === id)?.name || id;
  const agentName = id => agents.find(a => a.id === id)?.name || id;
  $("#host-count").textContent = hosts.length;
  $("#agent-count").textContent = agents.filter(a => a.enabled).length;
  $("#group-count").textContent = groups.length;
  const cleanup = state.cleanup || {};
  $("#cleanup-summary").textContent = cleanup.pending_files ? `${cleanup.pending_files} files awaiting deletion.` : "No files awaiting deletion.";
  $("#cleanup-error").textContent = cleanup.last_error || "";
  $("#host-rows").replaceChildren(...hosts.map(host => {
    const row = el("tr");
    cell(row, host.name).append(el("small", host.id));
    cell(row, host.os || "Unspecified");
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
    cell(row, agent.last_access ? new Date(agent.last_access).toLocaleString() : "Never accessed");
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
    const wrap = el("div"); const lbl = el("label", label); lbl.htmlFor = `setting-${name}`;
    const input = el("input"); input.type = "number"; input.id = `setting-${name}`; input.name = name; input.value = state.settings[name]; input.min = name === "file_count" ? "0" : "1"; input.step = "1"; input.required = true;
    const hint = el("p", help); hint.id = `help-${name}`; input.setAttribute("aria-describedby", hint.id); wrap.append(lbl, input, hint); return wrap;
  }));
  $("#settings-revision").textContent = `Settings revision ${state.settings.revision || 1}`;
}
async function reload() { state = await api("state"); render(); }
function editHost(host) {
  const form = $("#host-form"); form.reset();
  form.elements.id.value = host?.id || "";
  form.elements.name.value = host?.name || "";
  form.elements.os.value = host?.os || "";
  form.elements.ips.value = (host?.ips || []).join("\n");
  form.elements.metadata.value = JSON.stringify(host?.metadata || {}, null, 2);
  $("#host-title").textContent = host ? "Edit host" : "Register host";
  form.querySelector(".form-error").textContent = "";
  $("#host-dialog").showModal();
}
function editAgent(agent) {
  if (!state.hosts.length) { notice("Register a host before creating an agent.", true); return; }
  const form = $("#agent-form"); form.reset();
  form.elements.id.value = agent?.id || "";
  form.elements.name.value = agent?.name || "";
  $("#agent-host").replaceChildren(...state.hosts.map(host => { const option = el("option", host.name); option.value = host.id; return option; }));
  if (agent) form.elements.host_id.value = agent.host_id;
  $("#agent-title").textContent = agent ? "Edit agent" : "Create agent";
  form.querySelector(".form-error").textContent = "";
  $("#agent-dialog").showModal();
}
async function formAction(form, action) {
  const submit = form.querySelector('[type="submit"]'); submit.disabled = true;
  const errorNode = form.querySelector(".form-error"); if (errorNode) errorNode.textContent = "";
  try { await action(); } catch (error) { if (errorNode) errorNode.textContent = error.message; else notice(error.message, true); }
  finally { submit.disabled = false; }
}
$("#login-form").addEventListener("submit", event => {
  event.preventDefault();
  formAction(event.currentTarget, async () => {
    $("#login-error").textContent = "";
    try {
      const result = await api("login", "POST", {password: $("#password").value}); csrf = result.csrf_token;
      await reload(); $("#password").value = ""; $("#login-view").hidden = true; $("#console").hidden = false; route();
    } catch (error) { $("#login-error").textContent = error.message; }
  });
});
$("#host-form").addEventListener("submit", event => {
  event.preventDefault(); const form = event.currentTarget;
  formAction(form, async () => {
    const metadata = JSON.parse(form.elements.metadata.value || "{}");
    if (!metadata || Array.isArray(metadata) || typeof metadata !== "object" || Object.values(metadata).some(value => typeof value !== "string")) throw new Error("Metadata must be a JSON object with string values.");
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
  if (action) action().catch(error => notice(error.message, true));
});
document.querySelectorAll(".close-dialog").forEach(node => node.addEventListener("click", () => node.closest("dialog").close()));
$("#key-dialog").addEventListener("close", () => { $("#issued-key").value = ""; $("#issued-agent").textContent = ""; });
$("#confirm-dialog").addEventListener("close", () => { confirmAction = undefined; });
$("#new-host").addEventListener("click", () => editHost());
$("#new-agent").addEventListener("click", () => editAgent());
$("#refresh").addEventListener("click", () => reload().then(() => notice("Data refreshed.")).catch(error => notice(error.message, true)));
$("#cleanup-retry").addEventListener("click", () => api("cleanup", "POST", {}).then(reload).then(() => notice("Cleanup retried.")).catch(error => notice(error.message, true)));
$("#logout").addEventListener("click", async () => { try { await api("logout", "POST", {}); showLogin(); } catch (error) { notice(error.message, true); } });
window.addEventListener("hashchange", route);
(async () => {
  try { const session = await api("session"); csrf = session.csrf_token; await reload(); $("#login-view").hidden = true; $("#console").hidden = false; route(); }
  catch { showLogin(); }
})();

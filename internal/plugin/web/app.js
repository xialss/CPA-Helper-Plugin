/* CPA management contracts; display credentials are never written into policy. */
"use strict";
(() => {
  const base = "/v0/management/plugins/cpa-helper-plugin/v1";
  const $ = id => document.getElementById(id);
  let token = "", snapshot = null, health = null, dirty = false, busy = false, editing = null, session = 0;
  let keys = [], credentials = [], models = [];
  const clone = value => structuredClone(value);
  const icons = () => lucide.createIcons();
  const unique = values => [...new Set(values)].sort();
  const maskKey = value => !value ? "" : value.length > 10 ? value.slice(0, 6) + "..." + value.slice(-4) : "***";
  function panelValue(name) {
    let raw = localStorage.getItem(name);
    if (!raw) return null;
    if (raw.startsWith("enc::v1::")) {
      const secret = new TextEncoder().encode("cli-proxy-api-webui::secure-storage|" + location.host + "|" + navigator.userAgent);
      const binary = atob(raw.slice(9));
      raw = new TextDecoder().decode(Uint8Array.from(binary, (c, i) => c.charCodeAt(0) ^ secret[i % secret.length]));
    }
    try { return JSON.parse(raw); } catch { return raw; }
  }
  function panelKey() {
    const stored = panelValue("cli-proxy-auth")?.state;
    const key = stored?.managementKey || panelValue("managementKey");
    if (typeof key !== "string" || !key.trim()) throw new Error("CPA 登录会话不可用，请返回 CPA 管理面板登录后重新打开插件");
    const apiBase = stored?.apiBase || panelValue("apiBase") || panelValue("apiUrl");
    if (apiBase && new URL(apiBase, location.origin).origin !== location.origin) throw new Error("CPA 登录会话属于另一台服务器，请从当前 CPA 管理面板打开插件");
    return key.trim();
  }
  function notice(text, error = false) { $("message").hidden = !text; $("message").textContent = text; $("message").classList.toggle("error", error); }
  function element(tag, text, className) { const node = document.createElement(tag); if (text !== undefined) node.textContent = text; if (className) node.className = className; return node; }
  function iconButton(icon, label, action) { const b = element("button", undefined, "icon"); b.title = label; b.setAttribute("aria-label", label); const i = element("i"); i.dataset.lucide = icon; b.append(i); b.onclick = action; return b; }
  function newID() { const bytes = new Uint8Array(16); crypto.getRandomValues(bytes); return [...bytes].map(x => x.toString(16).padStart(2, "0")).join(""); }
  async function api(path, { method = "GET", body, headers = {}, credential = token } = {}) {
    const generation = session;
    const response = await fetch(path, { method, cache: "no-store", headers: { Authorization: `Bearer ${credential}`, ...(body ? { "Content-Type": "application/json" } : {}), ...headers }, body: body ? JSON.stringify(body) : undefined });
    if (generation !== session) throw new Error("连接已变更");
    let result; try { result = await response.json(); } catch { throw new Error(`响应格式错误 (${response.status})`); }
    if (!response.ok) throw new Error(result?.error?.message || result?.error || `请求失败 (${response.status})`);
    return result;
  }
  async function confirmAction(title, text) { $("confirm-title").textContent = title; $("confirm-text").textContent = text; $("confirm").returnValue = ""; $("confirm").showModal(); return new Promise(resolve => $("confirm").addEventListener("close", () => resolve($("confirm").returnValue === "ok"), { once: true })); }
  async function action(fn) {
    if (busy) return; busy = true; updateButtons();
    try { await fn(); } catch (e) { notice(e.message, true); } finally { busy = false; updateButtons(); }
  }
  function updateButtons() {
    for (const id of ["refresh", "sync", "add-group"]) $(id).disabled = busy || !snapshot;
    for (const control of $("editor-form").querySelectorAll("input, select, button")) control.disabled = busy;
  }
  async function persist(next) {
    const expectedRevision = snapshot.policy_revision + 1;
    next.policy_revision = expectedRevision; next.generated_at = new Date().toISOString();
    const accepted = await api(base + "/policy", { method: "PUT", body: next, headers: { "If-Match": String(snapshot.policy_revision), "Idempotency-Key": newID() } });
    if (accepted?.policy_revision !== expectedRevision) throw new Error("保存响应修订不一致");
    const latestHealth = await api(base + "/health");
    if (latestHealth?.policy_revision !== expectedRevision) throw new Error("健康状态修订不一致");
    snapshot = next; health = latestHealth; dirty = false; notice("已保存并生效"); render();
  }
  function replaceByID(items, value) {
    const index = items.findIndex(item => item.id === value.id);
    if (index < 0) return [...items, value];
    const result = items.slice(); result[index] = value; return result;
  }
  async function load() {
    const [p, h] = await Promise.all([api(base + "/policy"), api(base + "/health")]);
    snapshot = p; health = h; dirty = false;
    $("workspace").hidden = false;
    $("connection").textContent = "已连接"; $("connection").classList.add("ready"); render();
  }
  function parseList(config, name) { const value = config[name] ?? []; if (!Array.isArray(value)) throw new Error(`CPA 目录格式错误: ${name}`); return value; }
  function configuredCredentials(config) {
    const out = [], counters = new Map();
    const fingerprint = (kind, parts) => {
      const idBase = kind + ":" + sha256(kind + parts.map(p => "\0" + String(p ?? "").trim()).join("")).slice(0, 12);
      const count = counters.get(idBase) || 0; counters.set(idBase, count + 1);
      return "sha256:" + sha256("cpa-key-billing:credential:v1\0" + idBase + (count ? "-" + count : ""));
    };
    const add = (kind, parts, provider, disabled, label) => { const ref = fingerprint(kind, parts); out.push({ ref, provider, source: "ai-providers", label, status: disabled ? "disabled" : "active" }); };
    const fields = [["gemini-api-key", "gemini"], ["interactions-api-key", "gemini-interactions"], ["claude-api-key", "claude"], ["codex-api-key", "codex"], ["xai-api-key", "xai"], ["meta-api-key", "meta"]];
    for (const [field, provider] of fields) for (const entry of parseList(config, field)) {
      if (!entry["api-key"] && !entry["base-url"]) continue;
      const sorted = Object.keys(entry.headers || {}).sort().map(k => k + "\0" + entry.headers[k] + "\0").join("");
      add(provider + ":apikey", [entry["api-key"], entry["base-url"], entry["proxy-url"], entry.prefix, sorted], provider, entry.disabled || (entry.weight ?? 1) <= 0, [entry.name || provider, entry["base-url"], maskKey(entry["api-key"])].filter(Boolean).join(" · "));
    }
    for (const entry of parseList(config, "openai-compatibility")) {
      if (entry.disabled) continue;
      const name = (entry.name || "").trim().toLowerCase() || "openai-compatibility";
      const provider = name === "openai-compatibility" || name.startsWith("openai-compatible-") ? name : "openai-compatible-" + name;
      const entries = entry["api-key-entries"] || [];
      if (!entries.length) add("openai-compatibility:" + name, [entry["base-url"]], provider, entry.disabled, [entry.name || provider, entry["base-url"]].filter(Boolean).join(" · "));
      for (const item of entries) add("openai-compatibility:" + name, [item["api-key"], entry["base-url"], item["proxy-url"]], provider, entry.disabled || item.disabled || (item.weight ?? 1) <= 0, [entry.name || provider, entry["base-url"], maskKey(item["api-key"])].filter(Boolean).join(" · "));
    }
    for (const entry of parseList(config, "vertex-api-key")) {
      if (!entry["api-key"] && !entry["base-url"]) continue;
      add("vertex:apikey", [entry["api-key"], entry["base-url"], entry["proxy-url"]], "vertex", entry.disabled || (entry.weight ?? 1) <= 0, [entry.name || "vertex", entry["base-url"], maskKey(entry["api-key"])].filter(Boolean).join(" · "));
    }
    return out;
  }
  async function syncDirectory() {
    const [keyData, config, directory] = await Promise.all([api("/v0/management/api-keys"), api("/v0/management/config"), api("/v0/management/auth-files")]);
    const rawKeys = keyData["api-keys"];
    if (!Array.isArray(rawKeys) || rawKeys.some(k => typeof k !== "string")) throw new Error("CPA API Key 目录格式错误");
    keys = rawKeys.filter(k => k.trim()).map(k => ({ id: sha256("cli-proxy-api:caller-scope:v1\0" + k.trim()), display: maskKey(k) }));
    if (!Array.isArray(directory.files)) throw new Error("CPA 认证文件目录格式错误");
    const inventory = new Map(directory.files.filter(f => f.id && !f.runtime_only && f.source !== "memory" && f.source !== "config" && !String(f.source || "").startsWith("config:")).map(f => {
      const ref = "sha256:" + sha256("cpa-key-billing:credential:v1\0" + f.id.trim());
      return [ref, { ref, source: "auth-files", provider: (f.provider || f.type || "").toLowerCase(), label: [f.name || f.id, f.label, f.email || f.account].filter(Boolean).join(" · "), status: f.disabled ? "disabled" : f.status }];
    }));
    for (const c of configuredCredentials(config)) inventory.set(c.ref, c);
    credentials = [...inventory.values()];
    render();
    if (rawKeys.some(k => k.trim())) {
      const data = await api("/v1/models", { credential: rawKeys.find(k => k.trim()).trim() });
      if (!Array.isArray(data.data)) throw new Error("CPA 模型目录格式错误");
      models = unique(data.data.map(m => m.id).filter(x => typeof x === "string"));
    } else models = [];
    notice("CPA 目录已同步");
  }
  function render() {
    if (!snapshot) return;
    $("revision").textContent = "编辑后点击保存生效";
    const merged = new Map(keys.map(k => [k.id, k])); for (const k of snapshot.keys) merged.set(k.id, { ...merged.get(k.id), ...k });
    $("key-count").textContent = merged.size; $("group-count").textContent = snapshot.groups.length;
    const query = $("key-search").value.trim().toLowerCase(); $("key-rows").replaceChildren();
    let visible = 0;
    for (const item of merged.values()) {
      if (!(item.id + " " + (item.label || "") + " " + (item.display || "")).toLowerCase().includes(query)) continue; visible++;
      const k = snapshot.keys.find(k => k.id === item.id); const tr = element("tr");
      const keyText = item.display || "CPA 中已不存在的 Key";
      const label = element("td");
      if (item.label) label.append(element("strong", item.label), element("small", keyText)); else label.textContent = keyText;
      tr.append(label);
      const status = element("td"); status.append(element("span", k?.enabled === false ? "停用" : "启用", "badge" + (k?.enabled === false ? " off" : ""))); tr.append(status);
      tr.append(element("td", (k?.group_ids || []).map(id => snapshot.groups.find(g => g.id === id)?.name || id).join("、") || "无绑定"));
      tr.append(element("td", `${health?.active?.[item.id] || 0} / ${k?.max_concurrency || "不限"}`));
      const edit = element("td"); edit.append(iconButton("sliders-horizontal", "编辑 Key 策略", () => openEditor("key", k || { id: item.id, label: "", enabled: true, group_ids: [], max_concurrency: 0, rule: {} }))); tr.append(edit); $("key-rows").append(tr);
    }
    $("keys-empty").hidden = visible > 0; $("group-list").replaceChildren();
    for (const group of snapshot.groups) {
      const row = element("div", undefined, "group-row"), details = element("div"); details.append(element("strong", group.name)); if (group.note) details.append(element("p", group.note, "group-note")); details.append(element("p", `${snapshot.keys.filter(k => k.group_ids.includes(group.id)).length} 个 Key 绑定`, "group-bindings")); row.append(details, iconButton("pencil", "编辑 " + group.name, () => openEditor("group", group))); $("group-list").append(row);
    }
    $("groups-empty").hidden = snapshot.groups.length > 0;
    $("health-details").replaceChildren();
    for (const [label, value] of [["状态", health.status], ["契约", health.contract_version], ["插件版本", health.plugin_version], ["最近同步", new Date(health.accepted_at).toLocaleString()], ["当前并发", Object.values(health.active || {}).reduce((a, b) => a + b, 0)], ["最近错误", health.last_error || "无"]]) $("health-details").append(element("dt", label), element("dd", String(value)));
    updateButtons(); icons();
  }
  function choiceList(container, choices, allowName, denyName, object = false) {
    const rule = editing.value.rule; const values = [...choices, ...(rule[allowName] || []), ...(rule[denyName] || [])];
    const serialize = v => object ? `${v.source}/${v.provider}` : v;
    const query = container === "model-options" ? $("model-name").value.trim().toLowerCase() : "";
    const map = new Map(values.filter(v => !query || serialize(v).toLowerCase().includes(query)).map(v => [serialize(v), v])); $(container).replaceChildren();
    const available = new Set(choices.map(serialize));
    for (const [name, value] of [...map].sort((a, b) => a[0].localeCompare(b[0]))) {
      const row = element("label", undefined, "option"); let label = name;
      if (container === "credential-options") { const entry = credentials.find(c => c.ref === value); label = entry ? `${entry.label}${entry.status === "disabled" ? "（已停用）" : ""}` : "CPA 中已不存在的凭据"; }
      if (container === "category-options") label = `${value.source === "auth-files" ? "认证文件" : "API 提供商"} · ${value.provider}${available.has(name) ? "" : "（CPA 中已不存在）"}`;
      const select = element("select"); select.setAttribute("aria-label", label);
      for (const [id, text] of [["none", "未选择"], ["allow", "允许"], ["deny", "禁止"]]) { const option = element("option", text); option.value = id; select.append(option); }
      const has = field => (rule[field] || []).some(v => serialize(v) === name);
      select.value = has(denyName) ? "deny" : has(allowName) ? "allow" : "none"; select.dataset.mode = select.value;
      if (container !== "model-options" && !available.has(name)) for (const option of select.options) option.disabled = option.value !== "none" && option.value !== select.value;
      select.onchange = () => { for (const field of [allowName, denyName]) rule[field] = (rule[field] || []).filter(v => serialize(v) !== name); if (select.value !== "none") rule[select.value === "allow" ? allowName : denyName].push(value); select.dataset.mode = select.value; if (container !== "model-options" && !available.has(name) && select.value === "none") row.remove(); };
      row.append(element("span", label), select); $(container).append(row);
    }
  }
  function renderChoices() {
    choiceList("model-options", models, "models", "denied_models");
    choiceList("category-options", credentials.filter(c => c.source && c.provider).map(c => ({ source: c.source, provider: c.provider })), "credential_providers", "denied_credential_providers", true);
    choiceList("credential-options", credentials.map(c => c.ref), "credential_ids", "denied_credential_ids");
  }
  function openEditor(kind, value) {
    if (busy) return;
    dirty = false;
    editing = { kind, value: clone(value) }; $("editor-title").textContent = kind === "key" ? "Key 策略" : "路由规则";
    $("group-note-field").hidden = kind !== "group"; $("edit-note").value = value.note || "";
    $("edit-label").required = kind === "group"; $("edit-label-title").textContent = kind === "key" ? "备注" : "名称";
    $("edit-key-value").hidden = kind !== "key"; $("edit-key-value").textContent = kind === "key" ? keys.find(k => k.id === value.id)?.display || "CPA 中已不存在的 Key" : "";
    $("edit-label").value = value.label || value.name || ""; $("key-fields").hidden = kind !== "key"; $("edit-enabled").checked = value.enabled !== false; $("edit-limit").value = value.max_concurrency || 0;
    $("bindings").replaceChildren(); for (const g of snapshot.groups) { const label = element("label", undefined, "check"), checkbox = element("input"); checkbox.type = "checkbox"; checkbox.value = g.id; checkbox.checked = (value.group_ids || []).includes(g.id); label.append(checkbox, document.createTextNode(g.name)); $("bindings").append(label); }
    $("delete-entry").hidden = kind === "group" ? !snapshot.groups.some(g => g.id === value.id) : !snapshot.keys.some(k => k.id === value.id);
    $("editor-error").textContent = ""; renderChoices(); icons(); $("editor").showModal();
  }
  async function saveEditor() {
    if (busy) return;
    dirty = true; $("editor-error").textContent = "";
    const value = clone(editing.value), next = clone(snapshot);
    if (editing.kind === "key") {
      const limit = Number($("edit-limit").value);
      if (!$("edit-limit").value || !Number.isSafeInteger(limit) || limit < 0) { $("editor-error").textContent = "并发上限必须为非负整数，尚未保存"; return; }
      value.label = $("edit-label").value.trim(); value.enabled = $("edit-enabled").checked; value.max_concurrency = limit; value.group_ids = [...$("bindings").querySelectorAll("input:checked")].map(c => c.value);
      next.keys = replaceByID(next.keys, value);
    } else {
      value.name = $("edit-label").value.trim();
      value.note = $("edit-note").value.trim();
      if (!value.name) { $("editor-error").textContent = "请输入规则名称，尚未保存"; return; }
      next.groups = replaceByID(next.groups, value);
    }
    busy = true; updateButtons(); $("editor-error").textContent = "正在保存…";
    try { await persist(next); editing.value = value; $("editor").close(); }
    catch (e) { $("editor-error").textContent = "保存失败：" + e.message + "。请修正后重试；如修订冲突，请重新加载页面。"; }
    finally { busy = false; updateButtons(); }
  }
  $("editor-form").oninput = () => { dirty = true; };
  $("editor-form").onchange = () => { dirty = true; };
  $("editor-form").onsubmit = async event => { event.preventDefault(); await saveEditor(); };
  $("delete-entry").onclick = async () => {
    const { kind, value } = editing;
    if (kind === "group" && snapshot.keys.some(k => k.group_ids.includes(value.id))) { $("editor-error").textContent = "请先解除所有 Key 的规则绑定"; return; }
    if (!await confirmAction("移除策略", kind === "key" ? "移除后，此 Key 将不再受插件额外限制。" : "此路由规则将从策略中移除。")) return;
    await action(async () => {
      const next = clone(snapshot);
      if (kind === "key") next.keys = next.keys.filter(k => k.id !== value.id); else next.groups = next.groups.filter(g => g.id !== value.id);
      await persist(next); $("editor").close();
    });
  };
  $("model-name").oninput = renderChoices;
  async function closeEditor() {
    if (busy) return;
    if (dirty && !await confirmAction("放弃修改", "尚未保存的修改将被丢弃。")) return;
    dirty = false; $("editor").close();
  }
  $("close-editor").onclick = closeEditor;
  $("editor").oncancel = event => { event.preventDefault(); void closeEditor(); };
  $("refresh").onclick = () => action(async () => { await load(); notice("已刷新"); });
  $("sync").onclick = () => action(syncDirectory);
  $("add-group").onclick = () => openEditor("group", { id: newID(), name: "", rule: {} });
  $("key-search").oninput = render;
  for (const b of document.querySelectorAll("[data-tab]")) b.onclick = () => { for (const tab of document.querySelectorAll("[data-tab]")) { const selected = tab === b; tab.setAttribute("aria-selected", String(selected)); $(tab.dataset.tab + "-view").hidden = !selected; } };
  window.addEventListener("beforeunload", event => { if (dirty) { event.preventDefault(); event.returnValue = ""; } });
  icons(); updateButtons();
  action(async () => { token = panelKey(); await load(); await syncDirectory(); });
})();

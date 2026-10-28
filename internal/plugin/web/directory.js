/* CPA v8.0.45 metadata adapter; model ownership always comes from host.auth.models. */
"use strict";
function cpaCredentialDirectory(config, directory) {
  if (!directory || !Array.isArray(directory.files)) throw new Error("CPA 凭据目录格式错误");
  const providers = config["api-keys"] ?? {};
  if (!providers || typeof providers !== "object" || Array.isArray(providers)) throw new Error("CPA 上游配置格式错误");
  const configured = new Map(), counters = new Map(), seen = new Set();
  const trim = value => String(value ?? "").trim();
  const reference = id => "sha256:" + sha256("cpa-key-billing:credential:v1\0" + id.trim());
  const mask = value => !value ? "" : value.length > 10 ? value.slice(0, 6) + "..." + value.slice(-4) : "***";
  for (const [provider, groups] of Object.entries(providers)) {
    if (!Array.isArray(groups)) throw new Error(`CPA 上游配置格式错误: ${provider}`);
    for (const group of groups) {
      if (!group || typeof group !== "object" || Array.isArray(group)) throw new Error(`CPA 上游分组格式错误: ${provider}`);
      // CPA preserves explicit null lists for keyless OpenAI-compatible groups.
      const keys = provider === "openai-compatibility" && group.keys === null ? [] : group.keys;
      if (!Array.isArray(keys)) throw new Error(`CPA 上游分组格式错误: ${provider}`);
      for (const entry of keys.length ? keys : provider === "openai-compatibility" ? [group] : []) {
        if (!entry || typeof entry !== "object") throw new Error(`CPA 上游凭据格式错误: ${provider}`);
        // CPA omits indexes for entries removed by its configuration sanitizer.
        if (entry.auth_index == null) continue;
        if (typeof entry.auth_index !== "string" || !entry.auth_index.trim()) throw new Error("CPA 凭据索引格式错误");
        if (entry["api-key"] != null && typeof entry["api-key"] !== "string") throw new Error("CPA 上游 Key 格式错误");
        const key = trim(entry["api-key"]), base = trim(group["base-url"]);
        const inherited = field => entry[field] ?? group[field];
        let kind, parts, runtimeProvider = provider;
        if (provider === "openai-compatibility") {
          if (group.disabled) continue;
          const name = trim(group.name).toLowerCase() || "openai-compatibility";
          runtimeProvider = name === "openai-compatibility" || name.startsWith("openai-compatible-") ? name : "openai-compatible-" + name;
          kind = "openai-compatibility:" + name;
          parts = keys.length ? [key, base, entry["proxy-url"]] : [base];
        } else {
          if (provider === "interactions") runtimeProvider = "gemini-interactions";
          if (!["gemini", "interactions", "claude", "codex", "xai", "meta", "vertex"].includes(provider)) throw new Error(`CPA 上游类型不受支持: ${provider}`);
          kind = runtimeProvider + ":apikey";
          const headers = Object.create(null);
          for (const [name, value] of Object.entries(inherited("headers") || {})) if (trim(name) && trim(value)) headers[trim(name)] = trim(value);
          const sorted = Object.keys(headers).sort().map(name => name + "\0" + headers[name] + "\0").join("");
          let prefix = trim(inherited("prefix")).replace(/^\/+|\/+$/g, "");
          if (prefix.includes("/")) prefix = "";
          const effectiveBase = base || (provider === "meta" ? "https://api.meta.ai/v1" : "");
          parts = [key, effectiveBase, inherited("proxy-url")];
          if (provider !== "vertex") parts.push(prefix, sorted);
          const dedup = provider === "vertex" ? provider + "\0" + key + "|" + effectiveBase : kind + parts.map(p => "\0" + trim(p)).join("");
          if (["gemini", "interactions", "vertex"].includes(provider)) {
            if (seen.has(dedup)) continue;
            seen.add(dedup);
          }
        }
        // CPA omits some config/disabled entries from its file inventory. This
        // bridge supplies metadata only; host.auth.models must confirm each ID.
        const idBase = kind + ":" + sha256(kind + parts.map(p => "\0" + trim(p)).join("")).slice(0, 12);
        const count = counters.get(idBase) || 0; counters.set(idBase, count + 1);
        const id = idBase + (count ? "-" + count : "");
        configured.set(id, {
          ref: reference(id), auth_id: id, auth_index: entry.auth_index, source: "ai-providers", provider: runtimeProvider,
          label: [entry.name || group.name || provider, base, mask(key), "索引 " + entry.auth_index].filter(Boolean).join(" · "),
          status: (entry.disabled || (entry.weight ?? 1) <= 0) ? "disabled" : "active",
        });
      }
    }
  }
  const ids = new Set();
  const files = directory.files.map(file => {
    if (!file || typeof file.id !== "string" || !file.id.trim() || typeof file.auth_index !== "string" || !file.auth_index.trim()) throw new Error("CPA 凭据缺少宿主 ID 或索引");
    if (ids.has(file.id)) throw new Error("CPA 凭据目录存在重复标识");
    ids.add(file.id);
    const label = configured.get(file.id);
    if (label && label.auth_index !== file.auth_index) throw new Error("CPA 配置与运行时凭据索引不一致");
    configured.delete(file.id);
    const fromConfig = file.runtime_only || file.source === "memory" || file.source === "config" || String(file.source || "").startsWith("config:");
    return {
      ref: reference(file.id), auth_id: file.id, auth_index: file.auth_index,
      source: fromConfig ? "ai-providers" : "auth-files",
      provider: (file.provider || file.type || "").toLowerCase(),
      label: label?.label || [file.name || file.id, file.label, file.email || file.account, "索引 " + file.auth_index].filter(Boolean).join(" · "),
      status: (file.disabled || (file.weight ?? 1) <= 0) ? "disabled" : file.status,
    };
  });
  return [...files, ...configured.values()];
}

// Keep even model-free Host identities in double-read comparisons.
function cpaRegisteredModels(registered) {
  if (!registered || !Array.isArray(registered.credentials)) throw new Error("CPA 凭据模型目录格式错误");
  const validText = value => typeof value === "string" && value.trim() && value === value.trim() && !/[\0\r\n\t]/.test(value);
  const seen = new Set();
  return registered.credentials.map(entry => {
    if (!entry || typeof entry.ref !== "string" || !/^sha256:[0-9a-f]{64}$/.test(entry.ref) || seen.has(entry.ref) || !validText(entry.auth_index) || !validText(entry.provider) || entry.provider !== entry.provider.toLowerCase() || !Array.isArray(entry.models) || entry.models.some(model => !validText(model))) throw new Error("CPA 凭据模型目录格式错误");
    seen.add(entry.ref);
    const models = [...new Map(entry.models.map(model => [model.toLowerCase(), model])).values()].sort();
    return { ref: entry.ref, auth_index: entry.auth_index, provider: entry.provider, models };
  }).sort((a, b) => a.ref.localeCompare(b.ref));
}

// Only registered public IDs and routing attributes cross the storage boundary.
function cpaModelCatalog(credentials, envelope, registered) {
  const metadata = new Map();
  for (const credential of credentials) {
    if (metadata.has(credential.ref)) throw new Error("CPA 凭据元数据存在重复标识");
    metadata.set(credential.ref, credential);
  }
  const seen = new Set(), entries = [];
  for (const entry of cpaRegisteredModels(registered)) {
    const credential = metadata.get(entry.ref);
    // A Host-only empty set owns no models. Do not invent source/status for it.
    if (!credential && entry.models.length === 0) continue;
    if (!credential || !entry.auth_index || entry.auth_index !== credential.auth_index || entry.provider !== credential.provider || !["auth-files", "ai-providers"].includes(credential.source)) throw new Error("CPA 注册凭据与元数据不一致，请重新同步");
    seen.add(entry.ref);
    entries.push({ ref: entry.ref, source: credential.source, provider: entry.provider, enabled: credential.status !== "disabled", models: entry.models });
  }
  if (seen.size !== metadata.size) throw new Error("CPA 凭据元数据未被宿主注册目录完整覆盖，请重新同步");
  return { ...envelope, credentials: entries };
}

// Ignore observation timestamps and transient cooldowns when checking identities.
function cpaDirectoryIdentity(credentials) {
  return JSON.stringify(credentials.map(({ ref, auth_index, source, provider, status }) => ({ ref, auth_index, source, provider, enabled: status !== "disabled" })).sort((a, b) => a.ref.localeCompare(b.ref)));
}

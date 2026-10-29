# CPA v8.0.47 注册协议协商修复

日期：2026-10-29。以下保留诊断和实施阶段的原始证据。用户后续已授权提交并发布 v0.1.11，当前发布进度、版本升级兼容和正式产物验收以 [v0.1.11 记录](v0.1.11-release.md)为准；下文“尚未提交/发布”描述的是当时阶段。

## 问题与根因

用户环境：CPA v8.0.47，commit `90c447d`，BuiltAt `2026-10-29T00:27:33Z`；Plugin v0.1.10。首次日志为 `plugin.register failed: CPA schema 7 is required`，后续 `returned invalid metadata or no capabilities` 是同一次注册返回空结果的连带告警。

- Plugin 目前在 `internal/plugin/app.go` 将宿主 `schema_version` 与 SDK 的 7 做严格相等比较。
- 官方 CPA v8.0.47 的 `sdk/pluginabi/types.go` 将宿主最大 schema 提升至 8；native C ABI 仍为 1。新增的 raw-body patch 能力是显式 opt-in。
- `internal/pluginhost/rpc_client.go:registerRPCPlugin` 发送宿主最大 schema，只拒绝插件声明的 schema 大于宿主上限，随后按插件声明设置 adapter。Plugin 可以继续声明 7，不需要为了启动改为声明 8。
- v0.1.10 发布时的真实验证仅针对 CPA v8.0.45，不覆盖用户的 v8.0.47。

源码依据：[v8.0.47 schema](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.47/sdk/pluginabi/types.go)、[官方注册协商](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.47/internal/pluginhost/rpc_client.go)、[v8.0.45...v8.0.47](https://github.com/router-for-me/CLIProxyAPI/compare/v8.0.45...v8.0.47)。Context7 已检索官方项目文档，但索引中的 schema 描述较旧，本次判断以精确 tag 源码和官方二进制为准。

## 修复范围与验收

1. 保持现有 SDK v8.0.45 / C ABI 1 / 插件 RPC schema 7，接受宿主最大 schema ≥7；仍拒绝更低版本，并在错误中带上收到的 schema。
2. 注册和热更新必须采用同一判断；宿主为 8 或更高时不把宿主最大值直接回显成插件版本，也不增加 raw-body patch/响应核验回调。
3. 拒绝不兼容宿主不得打开状态目录或破坏当前配置。策略、目录 producer、Key TTL 和元数据格式不变。
4. 用已发布 DLL 与官方 v8.0.47 复现；修复后用同一 Host 验证真实加载、管理 API、策略/目录/调度/并发、流式/取消及两套 TTL fixture，另回归官方 v8.0.45 和 Edge。

不修改 CPA、Helper、真实部署或已发布 v0.1.10 资产。Linux 动态加载、真实 VPS 内存与实际计费未在本机验证。

## 证据

- 官方 v8.0.47 Windows amd64 ZIP SHA256 已核对为 `8e23cbb2f9f695411b7df46320855820b352fb886c3c3bdf45ab4634f2f14632`，与 GitHub release asset digest 一致。
- 已发布 Plugin DLL 使用 v0.1.10 正式下载产物，SHA256 `83d26745bb54176c04f6b64dba6661ab9c93965ac9068f854fd3e7c3ebdb166a`。
- 修复前真实复现：官方 v8.0.47 + 正式 v0.1.10 DLL，`TestCPACompatibility` 在加载阶段失败（35.620 秒），包含用户所报相同的 `host.go:1127` / `host.go:1139` 两条告警。
- 修复前新增单测同样失败：宿主 8/9 的注册和热更新均报 `CPA schema 7 is required`，旧宿主错误也未报告收到的 schema；修复后该矩阵全部通过。
- 本地修复 DLL SHA256：`488a579379e6c93b176c3a8f189de71e6c7fb17874072ffb5778b41554439347`。此产物仍使用现有开发版本字段，不是新的正式 release，不能只凭版本号区分。
- 官方 v8.0.47 的三套真实 CPA/TTL fixture **45.050 秒通过**；管理 smoke、旧状态升级、模型目录、403/429/503、调度/并发、流式取消及 TTL 基础和顶层矩阵均通过。
- 官方 v8.0.47 + 同一修复 DLL 的 Edge smoke 通过，包含 Key/TTL 保存、目录同步、旧核验入口和接口移除。
- `go fmt ./...`、`go vet ./...`、`go test ./... -count=1`、`go test -race ./... -count=1` 均通过；只含 CRLF 的无关 fmt 噪声经逻辑 diff 核对后恢复。
- 官方 v8.0.45 使用同一修复 DLL，三套真实 CPA/TTL fixture **40.169 秒通过**；保留原目标加载、管理和请求行为。
- 最终 6 份 Markdown 文档的 40 个本地链接及 `git diff --check` 通过；测试 CPA 进程已清理。无 commit、push、tag 或 release 写入，用户提供的未跟踪 `AGENTS.md` 保留。

验证命令（分别将 `CPA_BINARY` 指向 checksum 核对过的两版官方二进制，将 `CPA_PLUGIN_BINARY` 指向同一本地修复 DLL）：

```powershell
go test ./integration -run '^(TestCPACompatibility|TestClaudeUniformTTLProduction(TopLevel)?)$' -count=1 -timeout 240s
```

Edge 使用官方 v8.0.47：`npm run test:ui`。注册回归矩阵：`go test ./internal/plugin -run '^TestRegistrationNegotiatesHostMaximumSchema$' -count=1`。完整检查已执行上述 fmt/vet/all/race，未用可选 integration 的 skip 代替真实 Host 结果。

## 用户追加：避免每次 CPA 更新都手动适配

用户明确要求日常 CPA 更新不应依赖特定 schema 白名单。进一步将协议声明改为插件自己的 `RPCSchemaVersion=7`，与 SDK 的最新 `pluginabi.SchemaVersion` 解耦。宿主最大 schema 只检查下限；注册返回值和 capabilities 均声明实际实现的 7，升级 SDK 不能自动提高门槛或启用新功能。

回归扩展为宿主 0/1/6/7/8/9/100/65535/uint32 最大值、注册/热更新和合成未知字段；接受所有满足下限的宿主，保持 Body 省略、无 raw-body patch/响应核验回调。CPA 版本字段只记录固定数据契约来源，不用作运行宿主版本检查。C ABI 1、必需公开回调和管理接口语义仍须满足。

另用工作区外临时 modfile 构建相同源码与 v8.0.47 SDK，验证 SDK 最新 schema 变为 8 后仍接受 Host 7、声明插件 7：**通过**（注册及移除核验回归，2.273 秒）。首次模块代理下载超时，另一代理返回 HTTP/2 错误，改用 GitHub 官方 tag source archive 并在临时 modfile 内本地 replace；没有修改项目 go.mod/go.sum 或临时篡改 SDK 的 schema 常量。

使用独立协议常量重建的最终 DLL SHA256 为 `c66212187d94cc7888082002029c010f655882cac81c4d5ed97d8e3b41510528`，fmt、vet、全量和 race 通过。官方 v8.0.47 三套真实 CPA/TTL fixture **57.473 秒通过**，同一 DLL 的 v8.0.47 Edge 通过；官方 v8.0.45 三套最终回归 **48.945 秒通过**。7 份 Markdown 的 42 个本地链接、项目依赖文件未变化及 diff 检查通过。原有已发布 v0.1.10 资产仍未更新。

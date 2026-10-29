# CPA-Helper Plugin

`CPA-Helper Plugin` 是 CLIProxyAPI (CPA) 的第一方动态插件。它在 CPA 内部针对每个 API Key 执行策略控制，并为外部管理端提供带版本控制的管理契约。

初始产品边界如下：

```text
外部管理端 (可选) -> 策略快照/API -> CPA-Helper Plugin (数据面)
                                        -> CPA 请求生命周期
CPA 官方插件管理 -> 注册、生命周期与插件资源
```

本插件对 CPA-Helper 没有代码级或运行时依赖。即使外部管理端离线，插件也会继续使用最后一次被接受的策略快照，并通过其管理端点报告健康状态与版本号（revision）。

## 功能特性

- API Key 分组与稳定的分组归属
- 分组与 Key 维度的模型允许/拒绝规则
- 模型目录按凭据路由过滤，支持独有模型、共享模型与公开别名/前缀
- 上游凭据选择前的请求准入控制
- 明确的拒绝原因与状态码
- 针对单个 Key 的实例本地并发限制及生命周期清理
- 面向外部管理端的通用管理 API
- 单请求 Claude TTL（v0.1.10）：按 Key 统一 5m/1h，并提供来源明确的执行元数据；需满足[配套配置契约](docs/claude-uniform-ttl.md)

计费、余额、订阅以及用量统计仍归 CPA-Helper 所有。本插件不消费用量队列，也不直接代理模型流量。TTL 实现通过唯一的 `usage.handle` 观察点关联执行元数据，不复制 token 数量或金额，不建立第二套用量账本；Helper 消费端须按[交接文档](docs/claude-uniform-ttl-helper-handoff.md)显式适配新 provenance。

## 构建与安装

需要 Go 1.26.10+、匹配架构的 C 编译器，以及启用了插件支持的 CPA。SDK 固定 v8.0.45（C ABI 1 / 插件 RPC schema 7），Windows 使用 MinGW-w64 UCRT 工具链。v0.1.11 修复了旧 v0.1.10 在 CPA v8.0.47 上的注册失败，按宿主最大 schema 协商；已验证的 Host 为 v8.0.45 和 v8.0.47。发布状态和正式产物验收见 [v0.1.11 记录](docs/v0.1.11-release.md)，根因及修复证据见 [协商修复](docs/cpa-v8.0.47-schema-negotiation.md)。

兼容按协议能力判断：宿主支持 C ABI 1、至少 RPC schema 7 及插件所依赖的接口即可协商，schema 没有上限白名单。插件独立声明自己实现的 schema 7；CPA 增加 schema 8、9 或后续兼容协议时继续使用该契约，升级 SDK 也不会自动抬高要求。文档中的 CPA 固定版本用于复现和数据契约来源，不是运行版本锁。真实 ABI、必需回调或管理接口出现破坏性变化时才需要适配。

```powershell
go build -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin
```

在 Linux amd64 上：

```sh
CGO_ENABLED=1 go build -buildmode=c-shared -o dist/cpa-helper-plugin.so ./cmd/cpa-helper-plugin
```

停止 CPA，将对应的动态链接库放入其 `plugins` 目录中，并添加如下配置：

```yaml
plugins:
  enabled: true
  dir: plugins
  configs:
    cpa-helper-plugin:
      enabled: true
      state_dir: plugins/cpa-helper-state
```

重启 CPA 并访问 `/v0/resource/plugins/cpa-helper-plugin/ui` 或打开 CPA Helper 插件菜单。插件会复用同源 CPA 控制面板已保存的登录会话，并自动加载 CPA 目录；它没有独立的登录表单。
CPA 重载配置时会卸载已禁用或未选中的插件。请在持久化配置中保留上述两处 `enabled: true`；仅把动态库留在目录中不会启用插件。重新启用后会加载原有策略状态。
编辑 Key 或规则后，点击“保存”即可一次性应用所有更改。关闭编辑器不会自动保存。规则除名称外还支持可选备注。
保存失败时更改内容仍会保持可见以便修正。并发数为 0 表示不设限制。已通过认证但未特别配置策略的 Key 默认允许访问；即使 CPA-Helper 离线，显式配置的规则依然保持生效。

对于 Docker 部署，请将 `/CLIProxyAPI/plugins` 进行绑定挂载（bind-mount），以确保动态库和策略状态在容器重建后仍可保留。第三方插件源安装与更新工作流请参见 `docs/plugin-store.md`。

当路由缩小了 CPA 的候选凭据集时，插件会在该子集内执行加权轮询（weighted round robin）选择。插件绝不会跨优先级层级检索：如果 CPA 仅提供了高优先级凭据，即使存在已授权的低优先级凭据，请求仍可能返回 503。未经过验证集成前，请勿同时启用其他具有竞争关系的调度器插件。

模型列表过滤会将 Key／分组的模型允许和拒绝规则应用于 OpenAI、Claude、Gemini 和 Codex 列表；身份可辨识时，禁用的 Key 返回空列表。存在凭据限制时，模型还必须至少关联一个已启用且满足合并凭据规则的上游；共享模型仍有获准上游时保留，全部被禁止时隐藏。隐藏模型的详情由 CPA 返回 404。目录查询不执行调度、不占并发、不调用上游；实际请求仍可能因 CPA 的优先级、临时不可用或上游错误失败。

升级后打开插件面板，确认“CPA 目录已同步”。面板通过插件 `/v1/credential-models` 调用官方 `host.auth.models`，以真实注册 ID 取得模型关联，再与 CPA 配置及凭据元数据精确匹配，将公开 ID 与凭据指纹原子保存到 `state_dir/model-catalog.json`。它不保存原始密钥、auth ID 或管理登录凭据，不按可重复的 `auth_index` 合并凭据。修改 CPA 凭据、模型、别名或前缀后，需要点击“同步 CPA 目录”；只修改插件规则会立即按已有目录重新判断。同步失败保留上次有效目录，有凭据限制但没有可用关联时返回 `model_catalog_unavailable`，未覆盖当前模型时返回 `model_catalog_out_of_sync`。DeepSeek Harness 自定义模型 API 使用 `/v1` base URL 时，重新获取模型会请求 `/v1/models`；已保存的旧目录需在 DSH 中重新获取并保存。

“运行状态”中的“刷新 CPA 模型并同步”会显式调用一次 CPA 全局模型刷新，再同步并确认插件目录。普通同步及页面打开不会触发模型来源刷新。刷新失败保留旧目录；若 CPA 已刷新而后续同步失败，页面会分别报告两步状态并保留未保存的编辑。双读检测观察到的配置、身份和模型变化，但 CPA 没有全局原子目录修订号，不能保证跨读取的强一致性。

CPA 可能保留没有元数据且无注册模型的 Host 条目（例如停用的 AIStudio 内存凭据）。这类经过完整校验、不拥有任何模型的条目不纳入目录，同步通知和运行状态显示排除数量；不推断其来源或启用状态。已知元数据的空模型凭据仍保留，未知的非空模型所有者或任何身份错配仍使整个同步失败。双读也比较所有未纳入目录的条目。

有效的插件 0.1.10、0.1.9 / CPA v8.0.45 和 0.1.8 / CPA v8.0.32 目录可兼容读取，健康状态显示其原始来源和需要重同步；启动不改写文件，下一次成功同步才写入新目标的下一修订。未知或损坏状态仍需恢复备份。

使用单一请求头携带 Key（`Authorization`、`x-api-key` 或 `x-goog-api-key`）时按规则过滤。CPA v8.0.45 仍未向列表钩子提供已认证身份或 URL；缺少请求头身份（例如 URL 参数认证）或存在多个不同请求头 Key 时，保留 CPA 原始目录，并通过 `X-CPA-Helper-Model-List: unfiltered-identity-unavailable` 和日志标明原因。CPA 仍先执行认证；这只允许已通过 CPA 认证的请求放宽目录展示，不放宽实际生成权限。错误请求头 Key 与有效 URL Key 混用仍可能套用错误的展示规则。策略不可用或目录解析失败继续返回 JSON `error` 和 `X-CPA-Helper-Error`；受宿主接口限制，HTTP 状态仍为 200。模型列表不是权限安全边界。

升级前请务必备份状态目录。若已有状态文件缺失或损坏，插件将返回 503 而非直接重置权限。策略回滚会发布一个新的版本（revision）；二进制文件或状态降级则需要在停止 CPA 时恢复对应的备份。

## 模型列表过滤开关

生成请求的插件错误提示使用中文：禁用 Key 返回 `401/api_key_disabled`，
仅在请求头 Key 与 CPA 已认证身份匹配时显示前六后四的脱敏值；短 Key 全量遮盖，
宿主未提供原始 Key 时只说明禁用原因。`403/model_forbidden` 会说明命中的
Key 独立拒绝规则、分组名称及 ID，或未进入合并允许列表。并发超限返回
`429/concurrency_limit`：“当前 API Key 已达设定并发上限”。
模型列表仍受原开关及宿主状态码限制，CPA 自身和上游返回的错误不由插件翻译。

在 CPA 官方插件管理中，打开 CPA Helper 的配置，将
`model_list_filter_enabled` 设为 `false` 可关闭模型列表过滤，设为 `true`
可重新启用；未设置时默认启用。保存后由 CPA 热更新，无需关闭整个插件。
关闭只恢复 CPA 原始模型目录，实际生成的模型权限、凭据路由和并发限制继续生效。
也可通过官方管理接口 `PATCH /v8/management/config/plugins/configs/cpa-helper-plugin`
提交 `{"model_list_filter_enabled":false}`；需要 CPA 管理密钥。
插件 `/v1/capabilities` 返回当前配置、插件版本 0.1.11 和数据契约来源 CPA v8.0.45；后者不是宿主运行版本门禁。
CPA 官方插件管理接口还会通过配置字段的 `default` 返回插件实际采用的默认值。

## v0.1.10 的移除变更

自 v0.1.10 起移除响应模型核验。插件不再注册原始响应及流式核验回调，不再保存请求摘要、核验状态或暂存生成响应；请求拦截前后均省略请求体。模型列表过滤继续使用普通响应回调。

旧 `response_model_mismatch` 配置仅为升级兼容而接受，加载时告警并忽略；可从 CPA 配置中删除。核验界面和 `/response-model/validate` 接口已删除。发布版 v0.1.9 仍包含旧实现，需要升级至 v0.1.10 才包含移除变更。原因及验收见 [移除记录](docs/response-verification-removal.md)，发布进度及产物验证见 [v0.1.10 记录](docs/v0.1.10-release.md)。

## 代码仓库指南

- `docs/architecture.md`: 模块边界与请求生命周期
- `docs/compatibility.md`: 锁定的 CPA ABI 及兼容性矩阵
- `docs/management-contract.md`: 通用管理端同步契约
- `docs/plugin-store.md`: 第三方插件源与发布工作流
- `docs/testing.md`: 本地与集成测试要求

## 当前状态

v1 版本的实现包含策略/模式校验、事务快照、请求钩子（hooks）、子集调度、管理 API 以及内嵌的管理 UI。
可重现的验证方法请参阅 `docs/testing.md`，上游致谢与归属请参阅 `THIRD_PARTY_NOTICES.md`。构建产物与本地策略状态不会纳入版本管理。

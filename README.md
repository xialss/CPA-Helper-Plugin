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
- 上游凭据选择前的请求准入控制
- 明确的拒绝原因与状态码
- 针对单个 Key 的实例本地并发限制及生命周期清理
- 面向外部管理端的通用管理 API
- 上游响应模型核验：非流式/SSE 拦截提醒、允许映射与可视化配置

计费、余额、订阅以及用量统计仍归 CPA-Helper 所有。本插件不消费用量队列，不注册用量观察者（usage observer），也不直接代理模型流量。

## 构建与安装

需要 Go 1.26+、C 编译器，以及启用了插件支持的 CPA v8.0.12。只支持发布时验证过的最新 CPA 正式版，不维护旧版适配器；每次构建仍锁定精确版本。版本与验证记录见 `docs/compatibility.md`。

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
编辑 Key 或规则后，点击“保存”即可一次性应用所有更改。关闭编辑器不会自动保存。规则除名称外还支持可选备注。
保存失败时更改内容仍会保持可见以便修正。并发数为 0 表示不设限制。已通过认证但未特别配置策略的 Key 默认允许访问；即使 CPA-Helper 离线，显式配置的规则依然保持生效。

对于 Docker 部署，请将 `/CLIProxyAPI/plugins` 进行绑定挂载（bind-mount），以确保动态库和策略状态在容器重建后仍可保留。第三方插件源安装与更新工作流请参见 `docs/plugin-store.md`。

当路由缩小了 CPA 的候选凭据集时，插件会在该子集内执行加权轮询（weighted round robin）选择。插件绝不会跨优先级层级检索：如果 CPA 仅提供了高优先级凭据，即使存在已授权的低优先级凭据，请求仍可能返回 503。未经过验证集成前，请勿同时启用其他具有竞争关系的调度器插件。

模型列表过滤会将 Key／分组的模型允许和拒绝规则应用于 OpenAI、Claude、Gemini 和 Codex 列表；身份可辨识时，禁用的 Key 返回空列表。列表不判断上游凭据的路由可用性。使用单一请求头携带 Key（`Authorization`、`x-api-key` 或 `x-goog-api-key`）时按规则过滤。CPA v8.0.3 未向列表钩子提供已认证身份或 URL；缺少请求头身份（例如 URL 参数认证）或存在多个不同请求头 Key 时，保留 CPA 原始目录，并通过 `X-CPA-Helper-Model-List: unfiltered-identity-unavailable` 和日志标明原因。CPA 仍先执行认证；这只允许已通过 CPA 认证的请求放宽目录展示，不放宽实际生成权限。错误请求头 Key 与有效 URL Key 混用仍可能套用错误的展示规则。策略不可用或目录解析失败继续返回 JSON `error` 和 `X-CPA-Helper-Error`；受宿主接口限制，HTTP 状态仍为 200。模型列表不是权限安全边界。

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
插件 `/v1/capabilities` 返回当前配置、插件版本 0.1.5 和目标 CPA v8.0.12。

## 响应模型核验

在插件面板的“响应核验”页启用并保存。支持流式开关、拦截或审计模式、无法核验时的行为、
大小写与思考后缀处理、跳过列表，以及客户端模型到允许响应模型的显式映射。
默认关闭，新配置只影响后续请求。配置由 CPA 官方插件管理保存，重启后保留。

插件在协议转换前读取上游声明模型，已知不一致时替换结果并返回
`upstream_model_mismatch`；没有实现请求前模型不匹配拒绝。
非流式错误含请求模型、响应模型和请求 ID；流式发送一次错误并屏蔽后续内容。
HTTP 状态受宿主 ABI 限制通常仍为 200，已发生的上游调用和费用不能撤销。
Responses 流返回 Codex 可识别的终止错误分类，避免把核验拒绝当作临时故障自动重试；
原始原因保留在 `verification_code` 和消息中，CPA 收到终止错误后取消该上游流。
Responses 拦截模式先暂存全部文本和工具调用，成功终止并完成核验后才整体放行，防止结尾才声明错误模型时工具已执行；因此客户端会等待整次响应完成后再显示内容，内存占用随响应大小增长。
Chat、Claude、Gemini 流式请求逐块核验，不因协议类型在执行前拒绝；已经发送的内容无法撤回。Responses 暂存上限在面板中以 MiB 配置，默认单请求 2 MiB、并发总计 8 MiB。
同请求体并发、缺少模型或原始响应无法关联时按“无法核验”配置处理，不冒认一致。
这只能检查上游声明，不能证明实际使用了该模型。详细边界和 YAML 示例见
[`docs/response-model-mismatch.md`](docs/response-model-mismatch.md)。

## 代码仓库指南

- `docs/architecture.md`: 模块边界与请求生命周期
- `docs/compatibility.md`: 锁定的 CPA ABI 及兼容性矩阵
- `docs/management-contract.md`: 通用管理端同步契约
- `docs/plugin-store.md`: 第三方插件源与发布工作流
- `docs/testing.md`: 本地与集成测试要求

## 当前状态

v1 版本的实现包含策略/模式校验、事务快照、请求钩子（hooks）、子集调度、管理 API 以及内嵌的管理 UI。
可重现的验证方法请参阅 `docs/testing.md`，上游致谢与归属请参阅 `THIRD_PARTY_NOTICES.md`。构建产物与本地策略状态不会纳入版本管理。

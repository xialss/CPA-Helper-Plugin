# CPA-Helper 交接：单请求 Claude TTL

更新：2026-10-28。接收工作区：`D:/Project/CPA-Helper`。本文是后续实施输入，不代表 Helper 已完成修改。

2026-10-29 发布追加：v0.1.11 修复宿主 schema 协商，插件协议仍为 7、数据契约来源 `cpa_version` 仍为 v8.0.45；它不是实际 Host 版本门禁。接收方如校验 Plugin producer，应加入 0.1.11 的已知管理/目录组合，并保留以下能力探测、revision 和 TTL 来源语义。Host 新增兼容 schema 不需要 Helper 改写策略版本或目录 CPA 字段。发布与正式产物结果见 [v0.1.11 记录](v0.1.11-release.md)，本轮没有修改 Helper。

## 结论与范围

**需要修改 CPA-Helper。** Plugin 可以独立执行已保存的 Key TTL，但当前 Helper 既不接受 Plugin 0.1.9、0.1.10 的兼容组合，也没有 TTL 编辑、执行元数据关联和 provenance 消费流程。只更新 Plugin 不会自动完成 Helper 的按 TTL 计费。

不修改 CPA。沿用 CPA v8.0.45 stock usage queue 的 token 数量；Plugin 只提供执行级 TTL 归属，不能成为第二份 token 账本。

用户已收窄的需求优先于旧任务：单次请求全部 5m 或全部 1h，Key 策略优先。保留 Helper 现有合法 `mixed` 上游数据的消费能力，但本次 Plugin 不生成 mixed 分桶。`enforced_request` 不等于上游实际 TTL。

## 追加交接：响应核验已移除

同日用户明确要求删除响应模型核验。Plugin v0.1.10 已删除该能力，详见 [移除记录](response-verification-removal.md)及[发布记录](v0.1.10-release.md)。已发布 v0.1.9 仍包含旧实现；确认已知版本组合后仍须按实际能力区分。

- Helper 应移除或按能力隐藏响应核验页面、配置 DTO 和保存逻辑；新构建的 `/capabilities.modules` 不含 `response_model_mismatch`，也不返回对应配置，`/health` 不再返回 `response_model_tracked_requests`。
- 停止调用 `POST /v1/response-model/validate`（新构建返回 404），以及 CPA 官方配置接口下的 `response_model_mismatch` 写入。若仍兼容旧 Plugin，必须按实际能力区分，不能把新构建的 404 当临时失败重试或显示保存成功。
- CPA 中已有的旧配置仅告警后忽略，不阻止启动，也不会恢复回调。可由管理员清理；Plugin 不自动修改宿主配置。
- 回归要求：新构建正常连接、其他页面和 TTL/Key 保存可用、无核验入口或请求；旧版兼容若保留需单独测试。Helper 本轮仍未修改，接收方应将本项加入其任务和验收。

## 接收方先读

Helper 工作区（本轮只读，未改动其源码或 Trellis 状态）：

1. `AGENTS.md`、`.trellis/workflow.md`、`.trellis/spec/guides/project-overview.md`、backend/frontend 的 spec index。
2. `.trellis/spec/backend/cpa-helper-plugin.md`：管理代理、Key 所有权、完整快照、revision、幂等和读回。
3. `.trellis/spec/backend/usage-pricing.md` §28：现有实际 TTL、价格、事实修复和 quota 契约。
4. `.trellis/tasks/10-27-claude-ttl-request-rewrite/prd.md`、`task.json`、`implement.jsonl`、`check.jsonl`。
5. `.trellis/tasks/10-27-api-key-rule-sync-claude-cache/research/current-cpa-compatibility.md` 和 `.trellis/tasks/archive/2026-10/10-27-claude-cache-ttl-billing/design.md`。

旧任务要求修改 queue 的实际 TTL/mixed 字段，不能照抄执行。启动 Helper 实施时应先更新其 PRD/design/implement/task，使其明确采用本文的补充元数据契约；不能把旧调研当作当前 CPA 能力证明。

Plugin 工作区：

- [需求](claude-uniform-ttl-prd.md)、[实施计划](claude-uniform-ttl-plan.md)、[部署和接口](claude-uniform-ttl.md)。
- [实施验收及未测边界](claude-uniform-ttl-verification.md)、[本轮审查](claude-uniform-ttl-review.md)。
- `internal/plugin/claude_ttl*.go`、`internal/snapshot/ttl_metadata.go`、`integration/ttl_production_test.go`。

## 当前 Helper 差距与文件入口

| 工作项 | 当前证据 | 需要交付 |
| --- | --- | --- |
| 版本兼容 | `backend/internal/app/cpa_helper_plugin.go:pluginUsesV8Management` 仅接受 0.1.0～0.1.8 | 保留旧版本组合，新增已核验的 0.1.9、0.1.10 / v8.0.45 管理和目录兼容；不放开所有未来版本 |
| 目录组合 | `backend/internal/app/cpa_helper_plugin_catalog.go` 固定 0.1.8 / v8.0.32，仍按单凭据查询 | 为 0.1.9、0.1.10 接入 `/credential-models` 的 Host 实际 ID/模型集合、严格元数据 join 和完整双读；保留旧组合，不能只替换共享常量 |
| TTL 能力探测 | 当前没有 `claude_cache_ttl`、`ttl_source` 或 `execution_id` 消费代码 | 新能力探测、严格 DTO、错误映射；发布版本号本身不足以证明 TTL 能力 |
| Key 策略 | `normalizePluginPolicyKey`、`replacePluginKey`、`/policy/key/{local_hash}`；使用 RawMessage 保留字段 | 校验可选 TTL、保留未知字段、支持显式清除并验证读回，不降低所有权/revision 保证 |
| Key UI | `frontend/src/shared/types/api.ts`、`features/plugin/api/cpaHelperPluginApi.ts`、`features/api-keys/views/ApiKeysView.vue`、`features/plugin/views/PluginRouteRulesView.vue` | 类型及自身 Key 编辑器增加“继承 CPA / 5m / 1h”；能力不可用要可见，不显示虚假生效 |
| 队列接入 | `collector.go` 出队后直接 `saveUsageMessage`；`usage.go:normalizeUsage` 未单独提取 execution_id | 保持原始队列去重身份，持久化待关联上下文，在扣款之前解析 TTL 事实 |
| 留存与 provenance | 现有 `UsageRecord`、usage/facts 表只有 TTL 和桶，没保存新来源 | 新迁移保存来源、执行关联及状态，贯通 API/报表和修复流程 |
| 金额一致性 | `saveUsageMessage` 提交 usage/fact 后立即调用 `quota.go:applyQuotaCharge`；已扣款记录按 usage_record_id 幂等返回 | 明确 pending 与首次扣款顺序，防止晚补 TTL 只改报表而不改原扣款 |

以上行号会随修改变化，实施时按函数名定位。当前前后端有保留未知字段的机制，不能误称所有旧 DTO 都会丢字段；真正缺口是没有 TTL 的显式业务语义与验收。

## Plugin 契约

### 兼容和能力

- 数据契约来源和 SDK 固定 CPA v8.0.45，C ABI 1、插件 RPC schema 7、策略 contract v1；v0.1.11 已验证 Host v8.0.45/v8.0.47，宿主最大 schema ≥7，不设上限白名单。
- TTL 首发版本为 0.1.10，证据见 [发布记录](v0.1.10-release.md)；后续 0.1.11 保留该能力并修复 schema 协商，见顶部追加。**已发布 0.1.9 DLL 不包含本次 TTL 能力**。新版本仍须结合以下 feature probe，而非只凭版本号启用。
- 先验证已知管理兼容组合，再检测 `GET /capabilities`：`modules` 包含 `claude_cache_ttl`，`policy_fields` 包含 `keys[].claude_cache_ttl`。
- 调用 `GET /claude-ttl/requirements`，要求 `metadata_version == "v1"`，并检查所需规则/配置方式；health 中 `claude_cache_ttl.status` 为 `configured` 才具备已校验配置。`disabled` / `unavailable` 不能显示“TTL 已生效”。
- `configured` 只表示通过固定配置文件校验；不是 live provider 接受 TTL 的证明。实际绝对配置路径和任意文件漂移后重载的前提见部署文档。
- 当前 Helper 的旧 0.1.8 目录契约、0.1.9 管理兼容和 0.1.10 管理/目录兼容是独立测试点；增加 TTL feature probe 不能代替它们。

### Key 字段

```json
{"claude_cache_ttl":"1h"}
```

仅允许 Key 层级的 `"5m"`、`"1h"`、空字符串或省略；不接受 null、其他字符串、数字或布尔。空字符串/省略继承 CPA。不要给旧 Key 默认填 5m，否则会改变原行为。

特别注意：Helper 的 `replacePluginKey` 对现有 Key 做 merge，**省略字段不会删除旧 TTL**。推荐 per-key API 清除时明确提交 `"claude_cache_ttl":""`，由 Plugin 的规范化快照省略空值；或设计有测试的显式删除协议。不能只在 Vue 对象中 `delete` 后提交并报告清除成功。

复用自身 Key 所有权校验、caller-scope 映射、完整快照、`If-Match`、唯一幂等键和目标 revision/字段读回。普通用户不能访问其他 Key；管理密钥、配置路径和所有 Key 的执行元数据不透传浏览器。

### 执行元数据查询

管理前缀：`/v0/management/plugins/cpa-helper-plugin/v1`，沿用 Helper 后端已有 CPA 管理地址/密钥，不新增浏览器秘密。

`POST /claude-ttl/executions`，每批 1～1000 个不重复的规范小写 UUID，请求上限 64 KiB：

```json
{"execution_ids":["e229118e-a1be-4fa4-93ea-7b003d5b9761"]}
```

成功结果：

```json
{
  "metadata_version":"v1",
  "retention_hours":24,
  "records":[{
    "execution_id":"e229118e-a1be-4fa4-93ea-7b003d5b9761",
    "caller_scope":"<64 lowercase hex characters>",
    "ttl":"1h",
    "ttl_source":"enforced_request",
    "policy_revision":7,
    "observed_at":"2026-10-28T08:00:00Z"
  }],
  "missing_execution_ids":[]
}
```

关联只能使用 stock queue 的 **`execution_id`**；`request_id`/TraceID 不能代替。验证所有返回 ID 属于本批次，不能重复或同时出现在 records/missing；检查必填字段、来源、TTL、revision、时间及 caller scope。新增来源或 metadata 版本应显式不支持，不自动信任。

caller scope 使用现有 `pluginCallerScope` 算法：`SHA256("cli-proxy-api:caller-scope:v1\0" + trimmedRawKey)`；它不等于 Helper 的本地 APIKeyHash。仅在后端临时使用原队列的 Key 完成映射，持久化可比较的 scope，不另存或记录原始 Key。缺少可靠身份或 scope 不匹配时隔离该元数据，保留原 usage，不跨 Key 套用。

持久待办应复用 Helper 现有受保护的 usage 存储和脱敏规则，不建立额外的明文 Key 日志或副本。
如果 inbox 使用脱敏消息，须在脱敏前计算并单独保留原去重身份与 scope，后续不能根据脱敏或补充后的 JSON 重新计算去重键。

Plugin 的 usage observer 与 stock queue 异步运行。短暂 missing 不等于最终不存在；已落盘事实从观察时间起保留 24 小时，重启可读。过期、未观察、不可关联均返回 missing；存储损坏/不可读返回 503。无上游 token 数量、金额和“是否命中”的补充字段。

## 消费与扣款的实施顺序

这是接入必须先解决的部分，不能仅在 `normalizeUsage` 中填一个 TTL 字符串：

1. **先持久化原消息和关联上下文**。队列已经出队，不能因 metadata IO 失败把消息丢掉。保留现有原始规范 JSON 的去重身份；不要把 metadata 合并回 raw 后重新算 hash，否则同一个执行会得到第二个 dedupe_key。作用域至少包含 collector origin 与 execution_id，避免切换 CPA 实例错误关联。
2. **再解析补充事实**。在现有 collector 生命周期内批量查询，对短暂 missing 做有界重试；待办状态必须落盘，重启可恢复。不得为了补查反复消费 stock queue、复制 token 或新增 usage producer。重试预算/截止时间须明确写入 Helper 设计并用可控时钟测试，不能超过 Plugin 留存窗口后仍宣称可恢复。
3. **最后按确定事实首次扣款**。推荐将原消息先写入 inbox/待处理状态，成功解析或达到已定义终态后，才通过现有共享 calculator 创建 usage/fact 并首次调用 `applyQuotaCharge`；不要先按 legacy 扣一次再静默换成 1h。若选择先创建 usage 记录，必须增加显式 pending gate，使 quota 和费用聚合不会把尚未确定的记录当作已完成。具体表结构由 Helper 现有迁移/事务模式决定。
4. **终态可见**。区分 metadata 的 pending、matched、missing/expired、unsupported、error/conflict；这些是补充事实状态，不得覆写原请求的 failed 标志。对无法取得新事实的记录保留 §28 unknown/legacy 语义和明确来源，不用当前 Key 配置猜测；终态确定后冻结首次入账依据。
5. **不自动重扣旧账**。现有 §28 修复 usage/fact 不会重放 quota。元数据在终态扣款后才到达，不能只更改计价 TTL 导致历史报表和扣款不一致。若要允许这种后补修正，需另行设计有审计、幂等差额调整；本次默认不引入该扩展。接口可展示“迟到事实/未用于已入账金额”的区别。

§28 的 unknown 走 legacy 通用缓存价格属于既有产品语义，不等于确定的 5m/1h 归属。UI/诊断应说明未取得 TTL 的原因；不能把 metadata 服务失败作为“确认 5m”的快捷路径。

## TTL 与 provenance 优先级

| 原 usage | 补充事实 | 处理 |
| --- | --- | --- |
| cache creation 为 0 | 任意 | TTL 对外仍为 null，不制造缓存写入量；可独立保留执行审计 |
| 有可信上游 5m/1h/mixed 及合法桶 | 任意 | 保留实际事实及其优先级；与策略不同则可见冲突，不覆盖上游事实 |
| 只有正数 aggregate，实际 TTL unknown | 同执行/同 Key 的有效 enforced_request=5m | 全量归属 5m，数量仅来自 aggregate，source 为 enforced_request |
| 只有正数 aggregate，实际 TTL unknown | 有效 enforced_request=1h | 全量归属 1h，source 为 enforced_request；1h 价格 null 仍为 unpriced，0 仍为明确免费 |
| 正数 aggregate | missing/过期/不支持/冲突/无法验证身份 | unknown，保留已有 legacy 行为及原因，不查询当前 Key 来补历史 |
| 非 Claude 或 count_tokens | 任意 | 不套用 Claude TTL 事实，原语义不变 |

保存实际来源与策略来源的区别，可新增 `cache_creation_ttl_source` 及关联信息，也可采用独立关联表；**这些是待 Helper 新增的字段，不是当前 CPA queue 字段**。建议区分 upstream 与 enforced_request，unknown 的原因另存。必须贯通 usage API、记录详情、analytics facts/修复触发器和审计；不要只在 UI 根据非空 TTL 推断来源。

## 实施计划与验收

| 阶段 | 工作 | 完成门槛 |
| --- | --- | --- |
| H1 | 修订 Helper 任务文档；0.1.9、0.1.10/v8.0.45 管理与目录兼容；能力探测 | 旧 0.1.8 回归通过；0.1.9 不误判有 TTL；0.1.10 按 probe 启用且不显示响应核验；未知组合显式拒绝 |
| H2 | Key 类型、校验、所有权 API、三态 UI、健康提示 | 保存/读回/清除、旧 Key 缺省、跨用户拒绝、revision 冲突、失败保留草稿 |
| H3 | 原消息/inbox、execution_id 与 scope、批查、持久 pending/provenance | queue 先到、metadata 先到、乱序、缺失、重启、超时、损坏、跨 Key 均有 fixture |
| H4 | 归属与共享计价、首次扣款顺序、facts/API/报表 | 明细、小时聚合、quota 同源；重复/重试不双计数或双扣款；late-after-final 不静默改账 |
| H5 | 管理代理 + stock queue + Plugin DTO 端到端，部署和回滚文档 | 后端全量与前端 lint/build/i18n、真实 SFC smoke 和跨层金额断言通过 |

至少增加以下测试：

- compatibility table 覆盖旧版本、0.1.9/v8.0.45、0.1.10/v8.0.45、目标错配、未知版本；metadata feature 的 on/off/缺字段/未知版本。
- 两个 Key、普通/流式、5m/1h、同体并发、重试多 execution、policy 热更新：按 execution_id 和冻结 revision 关联，不串 Key。
- 0 cache creation、可信实际 TTL 优先、冲突、metadata missing/503、过期、先队列后事实、进程重启恢复。
- 原 raw hash 不变、重复入队/补查不重复扣费；pending 后恢复只首次扣一次；终态后的迟到事实不改变已冻结计价依据。
- 价格 1h=null/0/正数、时间定价、长上下文、明细/小时/排名和 quota 的金额一致性；保留已有 mixed 上游测试。
- provenance-only 或 TTL-only 修复若被支持，能触发正确事实/小时失效；不默默重放历史 quota ledger。
- 每 Key 清除时检查最终 Plugin policy 不再有有效 TTL，而非只看 Helper PUT 200。

Helper 检查按其项目规范执行：后端 `go fmt ./...`、`go test ./... -count=1`；前端 `npm run lint`、`npm run build`，文案变化加 `npm run test:i18n` 和相关 smoke。Plugin 本地回归不能替代 Helper 测试；当前未做跨服务计费联调。

## 发布和回滚边界

先发布兼容但默认关闭的 Helper 接入，再安装含新能力的 Plugin、核验专用 payload/实际配置路径并重启 CPA，最后为 Key 开启 TTL。不能自动覆盖任意现有 payload。

回滚前停止新增受控流量并处理/记录 pending，清除 Key TTL，按 Plugin 文档恢复配置与配套 policy/state 备份。Helper 已落库 provenance 和已入账依据应保留；回退迁移需要使 analytics 成本缓存失效，但不能重扣 quota。未关联/过期事实明确 unknown，不承诺可从 Plugin 或当前 Key 重建历史。

Helper 源码、数据库、配置、任务状态或部署未修改。最初审查阶段未提交或发布；后续用户已授权提交并发布 Plugin v0.1.10，实际进度及产物证据见 [发布记录](v0.1.10-release.md)。

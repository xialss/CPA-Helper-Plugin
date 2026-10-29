# Claude 单请求 TTL 团队审查

2026-10-29：用户要求移除 TTL；当前工作树已删除该功能和专用测试。本文件仅保留历史审查证据，当前状态见 [TTL 移除记录](claude-ttl-removal.md)。

本记录保留审查阶段的原始证据；后续 v0.1.10 提交、发布及正式产物验证见 [发布记录](v0.1.10-release.md)。

日期：2026-10-28。范围是当前未提交的单请求统一 TTL 实现；不修改 CPA，不修改真实部署，不提交或发布。

## 分工与完成条件

| 审查者 | 范围 | 状态 |
| --- | --- | --- |
| review_a | 请求冻结、晚期规则、配置约束、公开 ABI、调度和生命周期 | 完成；发现 P1、交叉复现 P3，并复审两项修复及 Helper 交接 |
| review_b | usage 精确关联、元数据留存/持久化、管理 API、并发和错误语义 | 完成；发现并修复 P3，交叉复审 P1 |
| reproduce_fix | UI/策略协议、集成测试、文档可部署性、Helper 当前实现差距 | 已交付；P1 修复前真实复现与修复后 80/80、新旧矩阵验证；只编辑生产测试文件 |
| 主 agent | 交叉核对结论、最小复现与修复、回归验证、Helper 交接 | 完成；修复 P1、核查全部增量，最终 DLL 独立复跑全量与真实 Host/browser |

审查包含未跟踪的新 Go/文档文件，不能仅以 `git diff` 作为范围。历史 CPA v8.0.45 目录审查结果不算本轮结果。

## 判断依据

- 单次请求只采用 5m 或 1h；Key 策略优先，不支持混合 TTL 推导。
- 只使用 CPA v8.0.45 公开插件接口；不改 CPA，不引入第二个 usage 数量来源。
- `enforced_request` 是请求策略归属，不得改称上游实际 TTL。
- 缺失、冲突、配置漂移和不支持的情况应明确可见；不得用当前 Key 配置补历史事实。
- confirmed finding 必须包含触发条件、代码位置和可复现证据；修复后再运行对应回归。

## 发现与处置

### TTL-RF1 / P1：顶层 automatic cache_control 绕过统一 TTL

- 发现：`requiredTTLPayload` 只覆盖 tools/system/messages 块路径，漏掉 Messages 的顶层 `cache_control`。反向 Key TTL 会产生混合最终 wire；既有 64 请求矩阵没有根控制输入，不能证明此情形。
- 复现：review_a 用官方 Anthropic API/SDK 文档和 pinned CPA pipeline 识别；reproduce_fix 新增真实 CPA 测试，用修复前生产 DLL 跑 8 个反向场景，全部失败。默认 profile 的 Key5m/root1h 最终为 `[1h bare-5m]`，Key1h/root5m 为 `[5m 1h]`；CLI profile 分别为 `[1h bare-5m bare-5m]`、`[5m 1h 1h]`。合成上游明确拒绝混合控制，返回 400。
- 修复：根 `cache_control.type=ephemeral` 的独立 payload `match` 条件规则，1h 覆写根 TTL、5m 删除根 TTL；保留三条块路径。匹配最终 prepared body，不自行解析或保存请求体；未带根控制时不创建根控制。
- 配置同步：requirements、严格配置校验、真实 fixture 和部署 YAML 同步；缺少根规则或去掉条件的配置拒绝注册。
- 状态：已关闭。扩展 80 场景全部通过，原 64 请求矩阵新增无根不创建断言并通过；两位 reviewer 复审无新增 finding。

### TTL-RF2 / P3：幂等写入静默接受损坏的 observed_at

- 发现：已有元数据其他字段均有效但 `observed_at` 缺失/零值时，`Get` 会报告损坏，`Put` 的幂等分支却返回成功，usage observer 不记录本次错误。
- 复现：review_b 以 Go overlay 注入最小回归，实际失败；review_a 独立运行同一复现也失败。未以猜测替代损坏状态测试。
- 修复：Put/Get 使用相同 `validTTLMetadata` 完整性校验，坏文件显式失败；正常重复仍不刷新首次 observed_at 或延长留存窗口。不覆盖或自动修补损坏文件。
- 状态：已关闭。缺失/零时间两条回归先失败、后通过；合法重复及重开保留首次时间。review_a 独立复核通过，全量及 race 检查通过。

### 排除的候选

嵌套 `tool_result.content` 内部缓存标记：SDK 的复用 TextBlock 类型不足以证明 Anthropic 支持这种位置。
固定 CPA 的 `internal/translator/claude/openai/chat-completions/claude_openai_request_test.go:1068` 中有该限制的回归证据。
不为未被 provider 支持的输入扩张控制面，也不新增无依据的拒绝逻辑。正常外层 tool_result 块已由 message.content 路径覆盖。

## 验证与交接

主 agent 在两项修复和全部测试落盘后重建最终 DLL，独立运行下列检查，不以审查者的报告替代最终产物验证：

| 检查 | 本轮实际结果 |
| --- | --- |
| `go fmt ./...`、`go vet ./...` | 通过；仅清理无内容差异的 formatter 换行噪声 |
| `go test ./... -count=1` | 通过；无 Host 环境变量的 opt-in 测试在该命令中 skip |
| `go test -race ./... -count=1` | 通过；Go 并发检查，不代表 DLL 自身带 race instrumentation |
| `go build -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin` | Go 1.26.10 / UCRT GCC 16.2.0，成功 |
| 真实 `TestCPACompatibility` | 最终 DLL / 官方 CPA v8.0.45，10.52s 通过，含管理鉴权/配置默认值及既有目录、路由、生命周期回归 |
| 真实 `TestClaudeUniformTTLProduction` | 18.52s 通过，原 64 请求基础矩阵和并发/重试/热更新/取消/count_tokens/漂移等增强场景 |
| 真实 `TestClaudeUniformTTLProductionTopLevel` | 80/80 通过；两种 profile、普通/流式、根 bare/5m/1h/空对象/无根、explicit；原 stock 数量与 metadata 精确关联 |
| 上述三套真实 Go fixture | 同一命令退出 0，package 共 46.753s；均为隔离临时 CPA，不是 skip |
| `npm run test:directory`、JavaScript syntax | 13/13，app.js / browser-smoke.mjs 语法检查通过 |
| `npm run test:ui` | Edge 完整 smoke 通过，含现有 TTL 保存/清除、失败保留草稿、目录/刷新/请求回归 |
| `git diff --check` | 通过 |

真实 Host 命令：

```powershell
$env:CPA_BINARY = Join-Path $env:TEMP 'codex-cpa-v8.0.45/extracted/cli-proxy-api.exe'
$env:CPA_PLUGIN_BINARY = 'D:/Project/CPA-Helper Plugin/dist/cpa-helper-plugin.dll'
go test ./integration -run '^(TestCPACompatibility|TestClaudeUniformTTLProduction(TopLevel)?)$' -count=1 -timeout 240s -v
```

原实现基线与其余验收映射见 [实施验收](claude-uniform-ttl-verification.md)。
Helper 的既有“上游实际 TTL”消费者不能自动消费 Plugin 补充元数据，需独立交接与验收。
详见[完整 Helper 交接](claude-uniform-ttl-helper-handoff.md)：版本/目录兼容、Key 的清除语义、execution_id/scope 关联、原 raw 去重、持久 pending 与首次扣款顺序。

确认的 2 项缺陷均已关闭，未留未修复 finding。Helper 工作区仍干净，任务仍为 planning；没有宣称完成 Helper 计费联调。
真实 provider 接受/命中/账单、Linux/Docker、长时负载与跨进程故障注入仍未运行。未提交、暂存、推送、发布或修改 CPA/真实部署。

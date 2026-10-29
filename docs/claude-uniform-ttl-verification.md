# Claude 单请求 TTL 验收记录

2026-10-29：用户要求移除 TTL；当前工作树已删除该功能和专用测试。本文件仅保留历史验收证据，当前状态见 [TTL 移除记录](claude-ttl-removal.md)。

本记录保留实施阶段的原始证据；后续 v0.1.10 提交、发布及正式产物验证见 [发布记录](v0.1.10-release.md)。

日期：2026-10-28；范围：Plugin 工作区的未发布实现，不修改 CPA 或真实部署。
需求：[PRD](claude-uniform-ttl-prd.md)；配置/对接：[契约](claude-uniform-ttl.md)。

团队 review 后修复了顶层 automatic `cache_control` 遗漏和损坏时间的幂等校验遗漏。
新增顶层 80/80 场景、原 64 基础与增强矩阵、既有兼容 suite、全量/race、最终 DLL 及 Edge 已复跑通过；
精确命令、结果与修复前失败证据见[审查记录](claude-uniform-ttl-review.md)。下表保留初次实施验收数据。
Helper 接收方按[详细交接](claude-uniform-ttl-helper-handoff.md)独立实施，尚未完成跨服务计费联调。

## 环境

- Windows amd64；Go 1.26.10；MinGW-w64 UCRT GCC 16.2.0；CGO_ENABLED=1。
- 固定 SDK 与官方 CPA v8.0.45，源码 revision `ad8bdd3f20bef44f0684b8a6096a216b2c76040d`，C ABI 1 / RPC schema 7。
- 使用此前已核对 checksum 的官方 Windows CPA 和本轮重建的生产 DLL；测试均为临时目录、loopback、合成凭据与上游。
- 不使用真实账号、余额、上游账单或 CPA 管理密码；不运行 live deployment smoke。

## 检查结果

| 检查 | 结果与边界 |
| --- | --- |
| `go fmt ./...` | 通过；清理工具造成的无内容换行噪声，保留原工作区文件格式 |
| `go vet ./...` | 通过 |
| `go test ./...` | 通过；未配置环境变量的真实 Host 测试在这条命令中 skip，单独运行的结果见下两行 |
| `go test -race ./...` | 通过；覆盖 Go 状态/并发单测，不代表动态库本身被 race instrumentation |
| `go build -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin` | 通过，真实 Host 可加载 |
| `TestCPACompatibility`（真实 CPA） | 通过，11.33s；管理鉴权/默认值、目录、协议、响应模型核验、调度/权重、取消/并发、禁启用、重启与回滚 |
| `TestClaudeUniformTTLProduction`（真实 CPA） | 通过，17.95s；64 请求基础矩阵（含 explicit 空/5m/1h），另有并发、取消、重试、count_tokens、热更新、漂移与 metadata 管理鉴权 |
| JavaScript syntax checks | app.js、browser-smoke.mjs 通过 |
| `npm run test:directory` | 13/13 通过 |
| `npm run test:ui`（Edge） | 通过；新增 TTL 缺省/1h/5m/清除保存，重新打开读取、失败保留草稿及原目录/请求行为 |
| 视检 | mobile-editor.png 和 health-desktop.png 已查看；TTL 控件、提示和固定保存按钮可见，无溢出 |
| `git diff --check` | 通过 |

旧兼容 fixture 首次因硬编码“3 个配置字段”失败。已更新为完整的 4 字段断言，并校验新增 TTL object 的默认值；
没有删除既有检查或把注册成功当作通过。修正后真实兼容 suite 完整通过。

## 验收映射

| PRD | 证据 |
| --- | --- |
| A01 | policy/claude_ttl_test：缺省、5m/1h、非法字符串、null/数值/布尔；snapshot 事务：非法 TTL 不改变修订，重启保留合法 TTL；既有旧状态回归 |
| A02 | 生产 DLL + 两种 fingerprint 配置；原生 Claude、Chat、普通与流式；mock 最终 wire 检查所有断点，stock queue 保留 0/120 原始总数 |
| A03 | explicit 空模式无断点；非 Claude body 不变且查询返回 missing；真实 count_tokens 与 lifecycle，单测不生成 TTL 事实 |
| A04 | 客户端伪造两个保留 header；每 profile 24 个同体跨 Key 并发；stock api_key/caller_scope 强匹配；失败重试之间热更新，旧执行保留修订 2/1h，新请求采用修订 3/5m |
| A05 | 唯一 usage.handle；精确 usage/queue.execution_id；管理查询记录无 token 数量；不修改原 stock 队列或金额 |
| A06 | 真实取消/重试 fallback 后查询与请求清理；单测覆盖完成后延迟 usage、同 selection 多执行、重复通知、未应用选择不继承 applied、存储失败、过期清理；已落盘记录重开可读 |
| A07 | 真实 TTL metadata 路由匿名访问 401；边界单测校验未知字段、非法/重复 ID、尾部 JSON；缺失 ID 显式 missing |
| A08 | 必要规则、绝对路径、调度配置一致性单测；漂移返回 503 且不触达上游；漂移锁定后恢复旧文件仍需重载；默认停用不创建 metadata 目录 |
| A09 | fmt/vet/test/race、动态库、真实 CPA 两套 suite 和 Edge；完整基础与增强矩阵均通过 |

## 契约与明确限制

- `enforced_request` 是单请求策略归属，不能冒充实际上游 TTL。数量仍只取 stock queue。
- 管理员必须声明正确的实际宿主配置路径；当前 ABI 不能证明该文件与全部 runtime 状态原子一致。
  摘要校验、已观察漂移锁定不能替代这一配置前提，也不允许其他插件随后改写 TTL。
- 第一版只支持 requirements 给出的专用 payload 对象；已有自定义 payload 不自动覆盖或合并。
- 24 小时内已落盘事实可读；重启前尚未关联/持久化、过期或关联缺失时保持 unknown。
- Helper 工作区及实际计费消费端本轮未修改；本轮交付其显式对接契约，未宣称完成跨服务计费联调。
- 未运行真实 Anthropic/Claude OAuth 账号、provider TTL 接受/命中/真实账单、Linux/Docker、长时间压力与跨进程崩溃注入。
  `fill-first` 使用公开 builtin 契约并接受严格配置校验，真实 Host 矩阵的调度模式为 round-robin。
- 不提交、推送、建新 PR、合并或发布；不改变已发布 v0.1.9 的产物。新能力通过 module/metadata_version 检测。

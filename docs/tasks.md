# 项目任务清单

## 当前实施：v0.1.12 审查、提交与发布

- 用户授权：派发一个 subagent review 当前全部变更，修复发现的问题后提交、发布新版本；包含此前 TTL 移除和运行状态精简/版本显示。无需修改 CPA、Helper 或真实部署。
- REL012-01 完成：一个只读 subagent 发现不可用目录来源显示 undefined；主 agent 在真实 Edge 复现并修复，两版回归通过；原 agent 复核关闭，未发现新增问题。
- REL012-02 完成：版本统一为 0.1.12，显式保留 0.1.11 策略及目录读取/回执兼容；schema、当前说明、升级清理及发布文档已更新。
- REL012-03 完成：fmt/vet/all/race、目录 13/13、最终 DLL 的官方 v8.0.45（26.227 秒）/v8.0.47（26.106 秒）及两版 Edge 通过；暂存审查后提交 `765f8779e167884f8830a55980da9d55509f2245`，分支与附注标签已推送。
- REL012-04 完成：[workflow](https://github.com/xialss/CPA-Helper-Plugin/actions/runs/40832878720) 四个 job 成功；[v0.1.12](https://github.com/xialss/CPA-Helper-Plugin/releases/tag/v0.1.12) Latest、中文说明已读回核对。正式三项资产及库元数据通过，发布 DLL 的官方 v8.0.45（26.089 秒）/v8.0.47（26.045 秒）及两版 Edge 通过。未合并或部署，Linux 动态加载未测。
- 记录：[v0.1.12](v0.1.12-release.md)。以下“未提交/发布”是各初始实施阶段的历史边界，本轮以该发布记录为准。用户本地 AGENTS.md 排除在提交之外。

## 当前实施：精简运行状态与修正 CPA 版本显示

- 用户授权：删除运行状态的“契约”“模型目录修订”“模型目录状态”，修正宿主 v8.0.47 却显示 v8.0.45 的目录来源。
- UI-STATUS-01 完成：三项只从界面移除；目录来源的 CPA 版本取自官方管理响应 `X-CPA-VERSION`，未报告时明确显示；目录 `cpa_version` 仍用于原契约校验。
- UI-STATUS-02 完成：JS syntax、嵌入资源 DLL 构建、官方 CPA v8.0.45/v8.0.47 的 Edge 及 diff 检查通过；覆盖实际版本、缺失响应头、恢复显示及目录契约不变。保留先前 TTL 移除变更，未提交或发布。

## 当前实施：移除 TTL，保留其余功能

- 用户授权：2026-10-29“移除 TTL 相关功能，其余保留”；本轮只修改 Plugin 工作区，不提交、推送、发布或修改 CPA/Helper/真实部署。
- TTL-RMV-01 完成：移除 TTL 请求/调度/usage 状态、配置校验、元数据存储、管理入口和 UI；保留 schema 协商、模型权限/列表、凭据路由、并发及原管理契约。
- TTL-RMV-02 完成：旧 YAML 明确告警；旧策略/历史/幂等回执兼容只读，新 TTL 写入明确拒绝；状态与已有元数据不自动删除。真实 DLL 已验证两种旧 producer、回执重试及不改写状态。
- TTL-RMV-03 完成：专用探针及过期测试已删除，移除/升级回归、fmt/vet/all/race、目录 13/13 通过；官方 CPA v8.0.47 fixture 21.543 秒、v8.0.45 22.454 秒及 v8.0.47 Edge 通过，包含管理 smoke 和其余请求路径。
- TTL-RMV-04 完成：当前文档已更新，历史契约加废弃标记，CPA payload 和 Helper 清理交接已记录；14 份文档 84 个本地链接、3 份 JSON 及 diff 检查通过。未提交或发布，Linux 动态加载、VPS 内存及真实计费未测。
- 实施及验收：[TTL 移除记录](claude-ttl-removal.md)。以下 TTL 任务、发布记录为历史证据，不表示本次工作树仍提供该功能。

## 当前实施：v0.1.11 提交与发布

- 用户授权：2026-10-29“提交并发布新版本”；范围为协议协商修复、必要版本/状态兼容、提交推送、现有双平台发布流程及正式产物验证。不修改 CPA、Helper 或真实部署，不自动合并。
- REL011-01 完成：版本与当前文档统一为 0.1.11，补 v0.1.10 状态升级兼容；保留插件 schema 7、宿主至少 7 且不设上限的规则。
- REL011-02 完成：fmt/vet/all/race、目录 13/13、v8.0.47 Edge、新版 SDK 隔离回归通过；最终版本 DLL 在官方 v8.0.47 三套 fixture 77.056 秒、v8.0.45 78.493 秒通过。暂存审查后提交 `a1b47066a6d3cefe78b3bedfc06d7ab5f2997fda`。
- REL011-03 完成：修复分支与附注标签已推送；[workflow](https://github.com/xialss/CPA-Helper-Plugin/actions/runs/40805822770) 四个 job 全部成功；[正式 Release](https://github.com/xialss/CPA-Helper-Plugin/releases/tag/v0.1.11) 已发布为 Latest，中文说明已补齐并读回核对。
- REL011-04 完成：正式 3 项资产大小/digest/checksum、ZIP 单根库和 Go/SDK/版本/源码 clean 元数据均通过；发布 DLL 在官方 v8.0.47 三套 fixture 59.499 秒、v8.0.45 60.360 秒和 v8.0.47 Edge 通过。未部署 VPS，Linux 动态加载和真实计费联调未测。
- 记录：[v0.1.11](v0.1.11-release.md)。下文“尚未提交/发布”是修复阶段历史状态，本轮以实际发布记录为准。用户 AGENTS.md 和临时产物排除在提交之外。

## 当前修复：CPA v8.0.47 注册失败

- 用户反馈：2026-10-29，CPA v8.0.47 / commit 90c447d 与 Plugin v0.1.10 报 `CPA schema 7 is required`。第二条 invalid metadata 是注册失败的连带结果。
- SCH47-01 完成：核对宿主 schema 8 / 插件声明 7 的源码契约；官方 v8.0.47 + 正式 v0.1.10 DLL 真实复现相同两条日志，新增单测修复前失败。
- SCH47-02 完成：检查改为宿主最大值至少 7，旧宿主错误包含收到的值；注册/热更新的 0/1/6/7/8/9 矩阵验证状态不被错误协商改变、保持 schema 7 和 Body 省略，不增加新回调。
- SCH47-03 完成：fmt/vet/test/race；官方 v8.0.47 三套 CPA/TTL fixture 45.050 秒、v8.0.45 40.169 秒及 v8.0.47 Edge 均通过；6 份文档 40 个本地链接及 diff 检查通过。仅本地修复，尚未提交或发布，Linux 动态加载未测。
- SCH47-04 完成：插件独立 `RPCSchemaVersion=7`，与 SDK 最新 schema 解耦；补宿主 100/65535/uint32 最大值、未知注册字段和 capabilities 一致性矩阵。工作区外使用官方 v8.0.47 SDK（最新 schema 8）编译验证通过，仍接受 Host 7，项目 go.mod/go.sum 不变；最终 DLL 的 v8.0.47 三套 CPA/TTL fixture 57.473 秒、v8.0.45 48.945 秒及 v8.0.47 Edge 通过；7 份文档 42 个本地链接和 diff 通过。规则、实现与验证均尚未发布。
- 保留 SDK、策略和目录 producer 不变；不修改 CPA、Helper 或用户部署。修复未发布前不能宣称 GitHub v0.1.10 已支持 v8.0.47。
- 记录：[schema 协商修复](cpa-v8.0.47-schema-negotiation.md)。

## 当前实施：v0.1.10 提交与发布

- 用户授权：2026-10-28“提交并发布新版本”；范围为已完成的 TTL、响应核验移除、必要版本兼容、提交/推送及 GitHub release，不修改 CPA、Helper 或真实部署，不自动合并分支。
- REL010-01 完成：版本/说明/schema/Helper 交接统一为 0.1.10，保留 v0.1.9 策略及目录读取并补升级回归；用户 AGENTS.md 和临时产物不纳入提交。
- REL010-02 完成：fmt/vet/test/race、目录 13/13、Edge、最终 DLL 三套真实 CPA fixture（51.668 秒）均通过；暂存审查后提交 `a8884145e881d43c49523b3e29a65a84104de309`。
- REL010-03 完成：分支及附注标签 v0.1.10 已推送；[workflow](https://github.com/xialss/CPA-Helper-Plugin/actions/runs/40700747573) 四个 job 全部成功；[正式 Release](https://github.com/xialss/CPA-Helper-Plugin/releases/tag/v0.1.10) 已发布为 Latest，中文说明已补齐。
- REL010-04 完成：正式 3 项资产大小/digest/checksum、ZIP 单根库、Go/SDK/版本/源码 clean 元数据均通过；发布 DLL 三套真实 CPA/TTL fixture 40.469 秒和 Edge smoke 通过，详见发布记录。未部署 VPS、未做 Linux 动态加载或真实计费联调。
- 本轮记录：[v0.1.10](v0.1.10-release.md)。此前“未发布/不提交”是各实施阶段的历史边界，本轮授权以上述发布任务为准。

## 当前实施：移除响应模型核验

- 用户授权：2026-10-28“直接把响应核验功能砍掉”；仅修改 Plugin，不改 CPA、不修改真实部署、不提交或发布。
- 背景：用户运行发布版 v0.1.9，核验未开启；该版本仍无条件注册原始响应和流式回调，原始响应 RPC 会逐帧携带完整请求体。
- 范围及验收：[移除记录](response-verification-removal.md)。既有 TTL 工作保留。
- RMV-01 完成：核验实现、响应状态/缓冲、回调注册及专用运行时辅助方法已删除；请求前后均省略 Body；旧配置显式告警并忽略。
- RMV-02 完成：管理接口、能力/健康字段、配置表单和独立脚本已删除；当前文档与 Helper 交接已更新。
- RMV-03 完成：最终 DLL 的三套真实 CPA fixture（含顶层 TTL 80/80）49.959 秒通过；fmt/vet/test/race、目录 13/13、Edge、文档/JSON/diff 均通过。边界与一次性验证问题见[移除记录](response-verification-removal.md)。

## 当前实施：Claude 单请求统一 TTL

- 用户授权：2026-10-28“开始实现”；范围为 Plugin 实现及对接契约，不改 CPA、不修改真实部署、不提交或发布。
- 已采纳：单次请求全为 5m 或全为 1h；Key 策略优先。旧混合 TTL 限制不再作为实现阻塞。
- 需求：[PRD](claude-uniform-ttl-prd.md)；阶段：[实施计划](claude-uniform-ttl-plan.md)；已有证据：[可行性验证](claude-ttl-feasibility.md)。
- TTL-01 完成：单请求契约、显式固定配置校验、公开 selection_id → execution_id 已用生产 DLL 验证。
- TTL-02 完成：可选 Key 字段及严格事务校验；64 请求基础矩阵、跨 Key 并发、热更新/重试冻结与漂移拒绝。
- TTL-03 完成：唯一 usage.handle、元数据查询与持久化、延迟/取消、重复/冲突/过期/未知处理测试通过。
- TTL-04 完成：Key 编辑 UI、健康/能力、[Helper 对接及部署契约](claude-uniform-ttl.md)；Edge 与视检通过。
- TTL-05 完成：fmt/vet/test/race、目录 13/13、真实生产 DLL TTL suite、既有 CPA suite 和 browser smoke；详见[验收记录](claude-uniform-ttl-verification.md)。
- 本轮边界：Helper 消费端、真实 provider/账单与 Linux/Docker 未测；已有任意自定义 payload 不自动合并；未提交、推送或发布。

### TTL 团队审查与 Helper 交接

- 用户授权：2026-10-28 启动不超过 5 个 subagent 审查当前变更并修复；本轮使用 3 位审查者。
- TTL-R01 完成：三位独立审查并交叉确认 P1 根控制遗漏、P3 损坏元数据幂等校验遗漏；两项修复均复审通过。
- TTL-R02 完成：修复前实际失败；修复后根控制 80/80、原 64 基础与增强矩阵、损坏时间幂等回归及合法重复不延长留存均通过。
- TTL-R03 完成：已核对 CPA-Helper 当前代码和 §28，[完整交接](claude-uniform-ttl-helper-handoff.md)已复审；Helper 工作区及 planning 状态未修改。
- TTL-R04 完成：fmt/vet/test/race、最终 DLL、三套真实 Host fixture、13 项目录测试与 Edge smoke；详见[团队审查记录](claude-uniform-ttl-review.md)。


## 已完成任务：CPA v8.0.45 兼容升级

以下是已发布 v0.1.9 的历史任务与证据；其中响应核验、请求 Body 关联的旧要求已被当前移除任务替代。

- 更新日期：2026-10-28。
- 需求依据：[PRD](cpa-v8.0.45-prd.md)；执行顺序：[实施计划](cpa-v8.0.45-plan.md)。
- 本清单是该升级的状态入口；需求及验收定义以 PRD 为准，已支持版本以 [compatibility.md](compatibility.md) 为准。
- 当前状态：兼容实现、团队交叉审查、v0.1.9 发布及正式产物核验完成；[PR #6](https://github.com/xialss/CPA-Helper-Plugin/pull/6) 已合并（本轮只读核对 origin/main 的 merge commit `00464a7d5a228a63fa0ff90abceeb71166cce488`，没有执行合并）。完整证据见 [验收记录](cpa-v8.0.45-verification.md) 和 [审查记录](cpa-v8.0.45-review.md)。
- 当前授权范围：2026-10-28 用户明确要求提交、提 PR、发布新版本；允许推送当前已审查分支及 v0.1.9 标签，由现有 workflow 构建并发布，不修改真实部署或自动合并 PR。
- 状态：`完成` 有交付/证据；`待办` 尚未开始；`进行中` 有实际实施；`阻塞` 需记录具体条件。

| ID | 任务与完成条件 | 依赖 | 状态 | 验收/阶段 |
| --- | --- | --- | --- | --- |
| CPA845-001 | 固化范围、版本影响、设计决策、回滚、验收矩阵与任务依赖，并校验文档链接 | — | 完成 | P0；本轮三个规划文档 |
| CPA845-002 | 实施前更新兼容记录为 v8.0.45 升级中；确认候选插件版本、固定源码契约和旧状态样本 | 001 | 完成 | P1；compatibility 升级中记录、0.1.9 候选、catalog_upgrade_test 旧格式样本 |
| CPA845-003 | 固定 SDK v8.0.45 / Go 1.26.10，schema 7 注册及 capabilities 一致，仅拦截后省略 Body | 002 | 完成 | P2；注册及真实 capabilities、原始响应关联和重配置通过 |
| CPA845-004 | 旧 0.1.8/v8.0.32 目录兼容读取、可观察重同步状态、下一 revision 原子更新及回滚测试 | 003 | 完成 | P3；unit 与真实 CPA 7→8 升级/重启/旧收据/策略不变通过 |
| CPA845-005 | 用真实 Host callback 夹具确定 ID/模型及元数据完整性，覆盖共享 index、停用、文件、配置和 keyless | 003 | 完成 | P4；官方源码及真实 browser inventory 验证必要元数据桥接 |
| CPA845-006 | 接入真实凭据模型关联，缩减经证据可替代的 ID 桥接和逐凭据查询，补双读与失败事务测试 | 004、005 | 完成 | P4；Host 单测、目录 10 项及 browser 双读变化/失败不发布通过 |
| CPA845-007 | 增加用户主动刷新 CPA 模型再同步，区分各阶段失败并保留旧目录和草稿 | 006 | 完成 | P5；真实 local-model 刷新、失败阶段、次数和草稿保留通过 |
| CPA845-008 | 补足模型注册/排除/前缀、策略/调度/生命周期、核验/大块流等契约回归 | 003、004、006 | 完成 | P6；真实完整回归、排除/前缀、192KiB 流单测通过 |
| CPA845-009 | 运行 fmt、vet、Go 单测、race、JavaScript 目录测试和动态库构建，记录实际工具链 | 007、008 | 完成 | P6；Go 1.26.10 / UCRT GCC 16.2.0，必需检查均通过 |
| CPA845-010 | 校验官方 CPA v8.0.45 产物，运行隔离真实兼容 fixture 和管理 smoke，含旧状态升级 | 009 | 完成 | P6；官方 checksum 匹配；真实 fixture 非 skip 通过 |
| CPA845-011 | 浏览器验证普通/显式同步、失败提示、编辑保留和布局，标注未测平台/场景 | 010 | 完成 | P6；Edge fixture、桌面/390px 健康页与编辑器截图通过 |
| CPA845-012 | 完成文档/schema 一致性、验收证据映射及最终 diff 检查；通过后更新支持记录 | 011 | 完成 | P7；版本/schema、13 个 Markdown 文档互链、JSON 和 diff 检查通过 |

## 已确认边界

- 目标为 CPA v8.0.45 / C ABI 1 / RPC schema 7；不新增 schema 6 Host 适配器。
- 策略指纹命名空间及生成请求的数据面约束不变；不增加 Usage 观察点或第二套调度器。
- `host.auth.models` 是真实 ID/模型关联来源；index 非唯一，元数据缺失不能靠猜测补齐。
- 仅省略拦截后 Body；拦截前请求体关联仍需保留。
- 已知旧目录需可读取并可重同步；未知/损坏状态不得自动清空或伪造成功。
- 显式刷新不绑定自动同步；失败不能部分发布目录或丢弃编辑。
- 不改生产环境、不使用真实凭据。实施与审查阶段未提交/推送/发布；后续发布按本文件新增授权及发布任务执行。

## 本轮证据与环境观察

- 已检查工作区规则、文档结构和现有 diff；未发现 `.trellis`、嵌套 docs 指令或既有任务清单，因此新增本文件。
- 用户提供的未跟踪 `AGENTS.md` 保留，不纳入本轮内容修改。
- 固定 tag 核查：v8.0.32 `911f49cbd49f4fe2516979754a7488a3fd14fe64`；v8.0.45 `ad8bdd3f20bef44f0684b8a6096a216b2c76040d`。
- 当前基线 `go test -mod=readonly ./...` 退出码 0；没有配置真实 CPA_BINARY 的结果不属于新版本兼容证明。
- 当前基线 `npm run test:directory`：9 项通过。尚未针对新回调/新版本改写或运行测试。
- 默认 Go proxy 获取 v8.0.45 SDK 的尝试连接超时，退出码 1；确认 `go.mod`、`go.sum` 无改动。
  这不是 SDK 已升级，也不是本轮文档工作的阻塞；CPA845-003 开始时需重新处理下载和工具链条件。
- 上述是实施前基线。实施时通过进程级 `GOPROXY=https://goproxy.cn,direct` 成功取得 SDK v8.0.45 和 Go 1.26.10；保留 Go checksum 校验。
- 新 Host/迁移内部单测通过；JavaScript 目录测试 10 项通过。未将这些结果替代真实 CPA 验证。
- 官方 Windows amd64 v8.0.45 archive SHA-256：`b049739ca8cd1885c59bdf57de6adf12452e9ab9907f50117906c11fadffff1b`，与 release checksums 一致。
- 首次真实加载失败：默认 GCC 8.1 生成的 DLL 被 Windows 拒绝（not a valid Win32 application）。已确定工具链原因，改用现有 UCRT GCC 16.2.0，并将编译器复制到无空格临时路径用于 race 链接；不改部署。
- 最终 UCRT DLL 实际加载、真实兼容/管理 fixture 与 Edge fixture 全部通过；修正了旧测试对 Chat 错误后结束帧的假设，核实当前 Host 在错误分块后取消执行。
- 验收 A-01～A-15 的测试映射与实际未运行项见 [验收记录](cpa-v8.0.45-verification.md)。Linux/Docker、真实 Codex-client、live DSH/外部 provider 和终端 flush 故障注入未运行，未借用历史结果。

## 状态更新规则

1. 开始实施时，先读本清单和依赖文档，检查工作区，再将实际开始的任务改为 `进行中`。
2. 每完成一个阶段更新任务状态及简短证据；不要仅根据代码写完就勾选测试/兼容任务。
3. 发现阻塞时记录命令、错误、影响任务和可继续的独立工作；不要把跳过写成通过。
4. 设计发生实质变化先更新 PRD/计划，再更新任务；不得只在聊天中留下决策。
5. CPA845-012 完成前，不将候选版本标记 verified；提交及发布必须有单独明确授权。

## 追加任务：本次变更团队交叉 review

- 授权：2026-10-28 用户要求两名 agent 交叉审查，第三名 agent 复现并修复确认问题。
- 审查状态与逐项证据：[交叉审查记录](cpa-v8.0.45-review.md)。上一轮实现/验收记录保持不变。
- 当前状态：两名 reviewer 交叉确认一项 P2；第三名 agent 独立真实复现并修复，完整本机回归通过，双方修复复审通过；R-01 已关闭，无其他可行动 finding。

| ID | 任务与完成条件 | 依赖 | 状态 | 证据/说明 |
| --- | --- | --- | --- | --- |
| CPA845-R01 | 两名 reviewer 独立检查全部变更，给出有证据的缺陷、风险或无发现结论 | CPA845-012 | 完成 | 两人独立确认同一 runtime-only 无模型 P2 |
| CPA845-R02 | 交换独立结果，对每项交叉确认、反驳或记录待补证 | R01 | 完成 | 交叉确认本轮引入，普通删除 tombstone 猜测已排除 |
| CPA845-R03 | 第三名 agent 复现确认问题，先取得失败证据再实施最小修复 | R02 | 完成 | 真实 CPA 复现退出 1；最小新增回归先失败；PRD/plan 先更新后实施无模型非所有者窄例外，不猜来源/状态 |
| CPA845-R04 | 运行针对性测试及必要回归，记录修复前/后和未运行范围 | R03 | 完成 | 目录 13/13、fmt/vet/all/race、重建 DLL、真实 CPA integration、增强 Edge smoke 和桌面/窄屏视检均通过；未运行边界见 review |
| CPA845-R05 | 两名 reviewer 复审修复，核对最终 diff、文档与任务状态 | R04 | 完成 | 双方独立 13/13 并只读复审通过；完整 Host 双读及最终可见计数确认，无新增 finding；不提交/发布，保留用户 `AGENTS.md` |

## 追加任务：提交、PR 与 v0.1.9 发布

- 授权：2026-10-28 用户要求“提交，提 PR，发布新版本”。
- 来源：当前已审查分支 `compatible/cpa-v8.0.45`；PR 目标为 `main`，本轮不自动合并。
- 发布流程：推送 `v0.1.9` 标签，运行仓库现有 `.github/workflows/release.yml`；不上传本机构建物替代官方流水线产物。
- 产物：Linux amd64 ZIP、Windows amd64 ZIP 和 `checksums.txt`。检查资产完整性、SHA-256、ZIP 根目录布局，并用下载的正式 Windows DLL 复跑真实 CPA 兼容性和浏览器 smoke。
- 用户提供的未跟踪 `AGENTS.md`、本地测试文件和构建产物不纳入提交。历史 PRD、实施及审查记录中的“未发布”描述记录的是当时阶段。

| ID | 任务与完成条件 | 依赖 | 状态 | 证据/说明 |
| --- | --- | --- | --- | --- |
| CPA845-REL01 | 确认版本未占用、远程分支和发布流程，复跑发布前必要检查 | R05 | 完成 | 发布前确认 v0.1.9 未占用；Go fmt/vet/all/race、目录 13/13、JS/diff、重建 DLL、真实 CPA integration（12.42s）及 Edge smoke 均通过 |
| CPA845-REL02 | 精确暂存任务文件、检查 staged diff、提交并推送，创建 PR | REL01 | 完成 | 源码提交 `1e98b18461898f200642677931ce44df8f5e534d`；[PR #6](https://github.com/xialss/CPA-Helper-Plugin/pull/6)；用户 AGENTS.md 未提交，PR 未合并 |
| CPA845-REL03 | 推送 v0.1.9 标签并确认 validate、双平台构建与发布 workflow 成功 | REL02 | 完成 | [release workflow](https://github.com/xialss/CPA-Helper-Plugin/actions/runs/40653287131) 四个 job 全部成功；[v0.1.9](https://github.com/xialss/CPA-Helper-Plugin/releases/tag/v0.1.9) 已正式发布并标记 Latest |
| CPA845-REL04 | 下载并验证正式资产，重测正式 Windows DLL，完善发布说明与记录 | REL03 | 完成 | 三个资产 GitHub SHA-256、两个 ZIP 与 checksums/根目录布局一致；Go 1.26.10 / SDK v8.0.45 / 内嵌源码 revision 一致；正式 Windows DLL 真实 CPA fixture（10.88s）与完整 Edge smoke 通过；详见验收记录第 6 节 |

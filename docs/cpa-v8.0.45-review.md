# CPA v8.0.45 变更交叉审查记录

- 日期：2026-10-28。
- 请求：两名 agent 独立审查并交叉复核，由第三名 agent 复现确认问题、修复并验证。
- 范围：本次 v8.0.45 升级的全部未提交代码、测试、配置和文档，包含新建未跟踪文件；用户提供的 `AGENTS.md` 保留原样。
- 需求与基线：[PRD](cpa-v8.0.45-prd.md)、[实施计划](cpa-v8.0.45-plan.md)、[上一轮验收](cpa-v8.0.45-verification.md)。
- 当前状态：两名 reviewer 交叉确认一项 P2；第三名 agent 已独立真实复现、修复并完成回归。两名 reviewer 均复审通过，R-01 已关闭；未发现其他可行动缺陷。
- 边界：不提交、暂存、推送、建 PR、发布或修改真实部署；只使用合成凭据及隔离测试环境。

## 1. 分工与流程

| 角色 | 负责内容 | 写入权限/状态 |
| --- | --- | --- |
| `review_a` | Go ABI/schema、Host callback、旧目录/收据/持久化、生命周期与错误语义；检查全部变更 | 只读；交叉确认和修复复审完成 |
| `review_b` | 浏览器目录与元数据关联、共享 index/停用/file/keyless、双读、刷新/草稿、测试及文档；检查全部变更 | 只读；交叉确认和修复复审完成 |
| `reproduce_fix` | 对审查结果建立最小复现，区分有效问题和误报，最小修复并记录失败前/修复后证据 | 唯一业务/测试写入者；失败复现、修复及本轮完整验证完成 |
| 主 agent | 传递独立结果、协调交叉复核、核对修复与最终结果 | 只读协调；不与修复 agent 并发编辑 |

1. 两名 reviewer 分别检查 diff、新文件、需求及固定 CPA v8.0.45 外部契约，输出带路径/行号、触发条件和证据的问题。
2. 交换独立结果；对每项记录确认、反驳或待补证，不将重复或推测直接算作缺陷。
3. 复现 agent 先运行最小失败测试或隔离复现，确认原因及本轮引入关系，再实施必要修复。
4. 修复后运行相关测试及受影响的必要回归；两名 reviewer 复审修复，主 agent 检查差异和证据。
5. 在本文及 [任务清单](tasks.md) 记录最终结论和未验证边界；没有证据的成功不得写为通过。

## 2. 审查基线

- 构建目标：插件 0.1.9 候选，CPA v8.0.45，C ABI 1 / RPC schema 7；本轮尚未发布。
- 工作树在审查前已有本次升级改动；审查不重置或覆盖这些变更。
- 本轮主 agent 只读重跑：`go test ./internal/...` 通过；`npm run test:directory` 10 项通过；`git diff --check` 通过。
- 已确认复现工具：Go 1.26.10、MinGW-w64 UCRT GCC 16.2.0、已校验的官方 v8.0.45 Windows amd64 可执行文件。
- 上一轮完整 Go/race/真实 CPA/Edge 结果仍作为历史基线；新增修复必须另记本轮复测结果，不能直接沿用。

## 3. 问题与处理

### R-01 / P2：停用的无模型 runtime-only 条目阻断整个目录同步

- 原位置：`internal/plugin/web/directory.js:93–97`，新增全 Host 集合与元数据精确 join。
- 触发：AIStudio `/v1/ws` 建立 runtime-only 凭据，停用后仍保留在 Host manager 中；官方凭据 inventory 隐藏该条目，`host.auth.models` 仍返回它且 `models: []`。
- 影响：一个不拥有任何模型的停用内存条目即可使所有无关凭据/模型无法重同步，重试不能消除。
- 交叉结论：两名 reviewer 独立确认。固定 SDK 的 Host inventory 构建器隐藏停用 runtime-only，而模型 callback 使用完整 manager list；`get_runtime` 同样隐藏、`get` 要求物理文件，不能可靠补齐来源/状态。普通删除会移出 manager，不属于此问题。
- 引入关系：旧 HEAD 只遍历元数据集合；空元数据可发布空目录，不会出现该阻塞。本轮新增严格 join 引入了回归。
- 第三名 agent 独立复现：官方 CPA v8.0.45 + 当前 DLL，在全新临时配置中建立 `/v1/ws`，PATCH 官方 status 为停用；观测 `beforeHost=1`、`afterMetadata=0`、`afterHost=1`、`models=[[]]`。
  对真实 `directory.js` 的“目录同步不应被无所有者阻断”断言失败，退出码 1，实际错误为“CPA 注册凭据与元数据不一致，请重新同步”。进程已清理，只使用合成管理 Key。
- 设计修复：先更新 PRD/plan；仅排除合法 Host 独有且模型严格为空的非所有者，保持已知元数据及所有非空模型关联的完整校验；完整 Host 集合仍双读，UI 显示本次排除计数，不猜测来源/状态。
- 具体修复：`directory.js` 的 `cpaRegisteredModels` 先校验全部 Host 身份及模型，再由 `cpaModelCatalog` 仅排除未知元数据且空模型条目；已知空模型条目仍保存。`app.js` 比较完整规范化 Host 集合，包含未纳入目录条目的增删、指纹/index/provider 与模型变化。未知非空所有者、畸形/重复项、已知元数据错配、元数据未被 Host 覆盖仍拒绝整次同步。
- 可观察性：普通同步通知、刷新最终通知和运行状态区域均显示未纳入目录的无模型条目数。该数值是当前浏览器针对已确认 catalog revision 的同步观测；不是 server `/health` 新字段、持久化状态或“已停用”推断，其他 revision 不显示旧计数。
- 失败→通过：新增最小回归先得到目录测试 10 通过 / 1 失败；实施修复和边界用例后 13/13 通过。新增真实 Edge 夹具同样创建并停用 runtime-only 凭据，验证元数据隐藏、Host 空模型保留、同步不受阻且所有已知凭据保留。
- 修复复审：`review_a` 与 `review_b` 分别只读复核并独立重跑目录测试 13/13，通过；两人均确认校验边界、完整双读和最终 UI 计数正确，没有新增 finding。R-01 / P2 关闭。

## 4. 验证与剩余边界

以下为修复后重新运行的结果，不沿用上一轮历史结果：

| 检查 | 本轮结果 | 证据/范围 |
| --- | --- | --- |
| `go fmt ./...` / `go vet ./...` | 通过 | Go 1.26.10；仅清理本次 formatter 产生且无内容差异的行尾噪音 |
| `go test ./...` / `go test -race ./...` | 通过 | UCRT GCC 16.2.0，CGO=1；未设置 CPA_BINARY 的真实 fixture 按约定跳过 |
| `npm run test:directory` | 通过，13/13 | 新增无所有者、严格拒绝边界和完整 Host 身份双读回归；两位 reviewer 独立同样通过 |
| JS 语法与 `git diff --check` | 通过 | app/directory/browser smoke；无未预期格式修改 |
| UCRT `c-shared` DLL 重建 | 通过 | `dist/cpa-helper-plugin.dll`；产物仍在忽略范围内，不纳入版本控制 |
| 显式 CPA_BINARY 的 `go test -v ./integration -count=1 -timeout 90s` | 通过 | 官方 v8.0.45 实际动态加载及管理/多协议/路由/生命周期/旧目录升级；TestCPACompatibility 15.61s |
| `npm run test:ui` | 通过 | 新 DLL + 官方 CPA + 合成凭据；真实 `/v1/ws` runtime-only 停用回归、普通/刷新最终计数、被排除项 index 和空→非空变化时中止并保留旧目录 |
| 1440px / 390px 运行状态截图 | 已视检 | 计数在最终刷新通知及健康区域可见；窄屏无横向溢出 |

浏览器 fixture 保持旧有文件/共享 index/keyless/停用凭据、模型排除与前缀、失败原子性、刷新草稿、配置保存/重载、合成上游路由等断言。测试使用隔离目录/loopback mock；CPA、WebSocket 与 mock 上游已清理，没有操作真实部署。

Linux/Docker、真实 Codex-client、live DSH/外部 provider、终端 HTTP flush 故障注入等上一轮未运行项仍未运行，不能以本轮 Windows 结果替代；固定源码与可重现夹具的结论不扩展为这些平台已验证。

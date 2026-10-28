# CPA v8.0.45 实施与验收记录

- 日期：2026-10-28。
- 范围：[PRD](cpa-v8.0.45-prd.md)、[实施计划](cpa-v8.0.45-plan.md)、[任务入口](tasks.md)。
- 插件：0.1.9 候选；本轮未提交、推送、打标签、发布或更新部署。
- CPA：v8.0.45，固定提交 `ad8bdd3f20bef44f0684b8a6096a216b2c76040d`。
- 接口：C ABI 1 / RPC schema 7；SDK `github.com/router-for-me/CLIProxyAPI/v8 v8.0.45`。
- 环境：Windows amd64，Go 1.26.10，MinGW-w64 UCRT GCC 16.2.0，Edge/Playwright。

## 1. 实际设计与范围

1. 注册和重配置严格要求 schema 7，capabilities 使用 SDK 常量。只声明
   `request_interceptor_omit_body_after`；转换前原始响应仍需拦截前 Body 摘要关联。
2. 新增管理 GET `/credential-models`，在原管理 callback 上下文中调用
   `host.auth.models`，以真实 Host ID 生成原有指纹；对外不返回原始 ID。
   全量格式/重复验证失败返回 502，不降级为成功空目录或旧查询。
3. 模型归属由 Host 决定；保留配置元数据桥接以取得缺失的来源和停用信息。
   原因：固定源码 `internal/pluginhost/auth_callbacks.go` 的 `buildHostAuthFileEntry`
   会省略部分无文件路径或停用运行时条目，模型 callback 则不含 source/status。
   精确按指纹/provider/index 校验，不能仅按 index 或位置猜测。
4. 移除 N 次逐凭据模型管理查询，改为两次完整 Host 模型读取；双读同时核对配置、
   路由身份、来源、启用状态和模型集合。它只能发现观察到的变化，不承诺注册表强一致。
5. 旧目录只对格式 1、contract v1、producer 0.1.8 / CPA v8.0.32 开放兼容读取。
   不自动改写磁盘、时间、修订或收据；健康信息暴露来源与 `refresh_required`。
   新发布仍严格要求当前 envelope，使用旧 revision + 1 原子持久化。
6. 显式“刷新 CPA 模型并同步”仅调用一个官方刷新路由一次；普通同步和页面打开不刷新来源。
   依次验证刷新、同步及健康确认；显示部分成功并保留草稿/旧目录。
7. 未增加 Usage、FrontendAuth、executor、重放、第二套调度或其他控制面。

## 2. 产物与检查

官方文件 `CLIProxyAPI_8.0.45_windows_amd64.zip` 的 SHA-256：

```text
b049739ca8cd1885c59bdf57de6adf12452e9ab9907f50117906c11fadffff1b
```

与官方 v8.0.45 release `checksums.txt` 匹配；使用隔离解压的官方可执行文件。
启动记录版本 8.0.45、commit ad8bdd3f；没有依赖本机既有 CPA 服务。

| 命令/检查 | 实际结果 | 说明 |
| --- | --- | --- |
| `go fmt ./...` | 通过 | 最终清理无语义格式变动，保留既有行尾约定 |
| `go vet ./...` | 通过 | Go 1.26.10 |
| `go test ./...` | 通过 | 不设置 CPA_BINARY 的此命令中，真实 fixture 按约定跳过 |
| `go test -race ./...` | 通过 | CGO=1，UCRT 编译器及 PATH；编译器位于无空格路径 |
| `npm run test:directory` | 通过，10 项 | ID 桥接、共享 index、Host 模型拼接、失败、隐私等 |
| `go build -trimpath -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin` | 通过 | 新 DLL 在官方 CPA 中实际加载成功 |
| `go test -v ./integration -count=1 -timeout 90s` | 通过 | 显式 CPA_BINARY；真实动态 ABI、管理 smoke、业务与升级回归 |
| `npm run test:ui` | 通过 | 同一官方 CPA、新 DLL、合成凭据、loopback mock；Edge |
| 桌面/390px 窄屏截图 | 已检查 | Key 页、编辑器及新增刷新/来源健康区域；无横向溢出 |
| JSON/文档互链与 `git diff --check` | 通过 | 最终收尾检查；无产物/状态文件纳入变更 |

所有隔离测试只使用合成 Key、临时目录及 mock upstream；未读取或修改真实部署凭据。
Go fixture 自行停止 CPA 并清理临时状态。浏览器截图位于忽略的 `test-results/`。

## 3. 验收矩阵

以下路径相对仓库根目录；测试结果均在上述目标和环境下取得。

| 验收 | 证据 | 结果 |
| --- | --- | --- |
| A-01 | `TestCPACompatibility` 实际注册/重配；`TestExactHostSchemaRequired` 拒绝 schema 0/4/5/6/8 且不打开状态 | 通过 |
| A-02 | `go.mod`、`configured` 注册断言、真实 `/capabilities` ABI=1/schema=7/版本检查及 DLL | 通过 |
| A-03 | 注册仅 omit-after；原始响应/歧义/动态核验单测和真实多协议核验/完成清理 | 通过 |
| A-04 | `TestRegisteredCredentialModelsUseActualIDsAndRedact`；browser 真实配置/文件/停用/keyless/共享 index | 通过 |
| A-05 | browser 真实 Claude 排除模型、file/team 前缀、共享模型；integration 公开别名和所有者过滤 | 通过 |
| A-06 | Host 畸形/重复/失败全量拒绝；JS 缺失/错配拒绝；browser Host 失败及双读中模型变化，旧快照不变 | 通过 |
| A-07 | Host 响应无原始 ID，catalog/policy 序列化秘密扫描；不记录请求内容/摘要 | 通过 |
| A-08 | `TestLegacyCatalogReadAndExplicitUpgrade`；真实 CPA 旧来源可读、不改磁盘、revision 7→8 | 通过 |
| A-09 | 旧幂等回放/换内容冲突/写失败/不确定持久化单测；真实升级重启后旧收据仍可重试 | 通过 |
| A-10 | `TestStoredCatalogCompatibilityIsReadOnlyAndNarrow`、损坏收据测试及原有 corruption 测试；真实升级不改变策略 | 通过 |
| A-11 | browser 打开/普通同步 refresh 计数为 0，三次显式操作各 1 次 POST；真实 local-model 刷新 | 通过 |
| A-12 | browser 刷新失败、刷新成功同步失败、全部成功；旧快照和未保存核验草稿保留 | 通过 |
| A-13 | 真实 3:1 子集权重、403/429/503、取消、重启/回滚、单插件/全局禁用再启用及生命周期清理 | 通过 |
| A-14 | 真实 Chat/Responses/Claude/Gemini 核验、真实截断、Responses EOF；`TestResponsesLargeHostChunkPreservesVerifiedOutput` 192KiB 单块/合块、原有缓冲上限测试 | 通过 |
| A-15 | 本文检查表、真实 fixture 和 Edge UI；最终任务与版本文档一致性检查 | 通过（限本机已测平台） |

## 4. 调试记录与边界

- 默认 Go proxy 曾连接超时；后用进程级 `GOPROXY=https://goproxy.cn,direct` 成功取得
  SDK 与自动工具链，未关闭 checksum/SumDB 校验。
- 初次使用旧 MinGW GCC 8.1 虽构建成功，但 Windows 拒绝 DLL（not a valid Win32 application）。
  改用已有 UCRT GCC 16.2.0，复制到无空格临时目录后，真实加载和 race 均通过。
- 新真实 fixture 首次暴露旧测试的结束序列假设：v8.0.45 OpenAI handler 在错误分块后
  调用取消并终止，而非继续转发 finish_reason。已据固定源码核实并更新断言；真实截断
  在匹配且未拒绝的流上单独验证。没有通过修改错误形状或伪造结束帧隐藏失败。
- 浏览器夹具曾在隐藏的核验 tab 清理输入而超时；修正测试导航后完整通过，未增加等待预算。
- 最后增加 capabilities 固定契约断言后再次运行真实集成通过；补充排除模型和健康区域截图后
  再次运行完整浏览器 fixture 通过。
- `npm run test:response-ui` / `test:codex-verification` 的 Docker/Linux 与真实 Codex-client
  路径本轮未运行；历史验证结果不转记为 v8.0.45 通过。
- 未测试 live DSH profile、真实 provider OAuth/外部模型源网络刷新、终端 HTTP flush 故障注入、
  Linux/musl、多节点共享状态或部署升级。实际刷新通过 `--local-model` 路径，失败阶段为受控 HTTP 夹具。
- 仍保留列表钩子缺认证身份/URL、HTTP 状态不能覆盖等既有宿主限制。升级或二进制回滚必须备份并恢复配套状态；
  兼容读取旧目录不意味着旧二进制能接受新目录。

## 5. 团队审查修复增量

上一节之前是首次实现验收；本节及 [交叉审查记录](cpa-v8.0.45-review.md) 为随后三名 agent 的审查与修复结果。

- 两名 reviewer 独立发现并交叉确认同一 P2：停用的 AIStudio runtime-only 凭据不在官方元数据 inventory 中，但仍由 Host 返回空模型集合，导致升级新增的精确 join 阻断全目录同步。第三名 agent 用官方 CPA + 旧 DLL 独立复现退出 1，新增目录回归先得到 10 通过 / 1 失败。
- 先修订 PRD R-03 和计划，再仅排除完整校验后缺少元数据且模型严格为空的 Host 非所有者；不推断来源/状态、不丢弃模型所有者，已知空模型条目保留。完整 Host 集合包括排除项参与双读。普通/刷新最终通知及运行状态显示当前浏览器已确认 revision 的排除计数，不新增持久化字段或 server `/health` 契约。
- 修复后重新运行：目录 13/13、JS 语法、fmt/vet/all Go/race、UCRT DLL 重建、显式官方 CPA integration 和增强 Edge fixture 均通过。真实 Edge 测试覆盖停用 runtime-only 条目、同步与最终刷新计数、排除项 index/空→非空双读变化及旧快照保留；1440px/390px 截图已视检，无横向溢出。
- 两名 reviewer 均只读复审通过并独立重跑 13/13，关闭 P2，无其他可行动 finding。R01–R05 全部完成；Windows 以外和真实外部服务等未运行边界保持不变，未提交、发布或改部署。

# 移除 Claude TTL 功能

后续授权：用户已要求对当前变更派发一个 subagent 审查、修复问题并提交发布 v0.1.12。下文“未提交/发布”和本地 DLL 散列保留为初始实施证据；最终版本、发布与正式产物验收以 [v0.1.12 记录](v0.1.12-release.md)为准。

日期：2026-10-29。用户要求“移除 TTL 相关功能，其余保留”。本轮不提交或发布，不修改 CPA、CPA-Helper 或真实部署；GitHub v0.1.11 仍包含历史 TTL 功能。

## 范围与验收

1. 删除 TTL 请求冻结、随机 token、selection 映射、请求头改写、调度委托、固定配置文件校验、usage 回调及执行元数据读写/清理。请求前后均省略 Body，after 钩子直接放行；没有 TTL 相关请求开销或 usage capability。
2. 删除 TTL 管理路由、capabilities/health/config 字段、Key 编辑选项和运行状态。旧 `/claude-ttl/requirements`、`/claude-ttl/executions` 返回 404。
3. 保留模型规则、分组、模型列表过滤、目录同步/刷新、凭据子集调度、并发限制、请求完成清理、事务策略与目录 API，以及已修复的 schema 协商。响应模型核验继续保持移除。
4. 旧 `claude_cache_ttl` YAML 仅解码并告警后忽略，不访问 host_config_path。旧策略中合法的 TTL 字段只作为历史兼容数据保留，用于原样序列化和旧幂等回执校验；引擎及 GET /policy 不携带，非空 TTL 的新写入明确拒绝。策略回滚保留其他限制但不恢复 TTL。启动不改写磁盘状态，旧元数据不读、不写、不删。
5. 删除 TTL 专用实现、可行性探针与旧功能测试；新增注册/回调/管理/UI 移除回归，旧状态/回执/回滚/失败事务回归，并实际加载 DLL 在两版已验证官方 CPA 上检查其余功能。

## 实施顺序

先记录外部契约和兼容边界，再移除请求与管理实现、添加最小旧状态读取兼容；更新 UI 与回归，最后运行项目检查和真实 Host/Edge 验证。版本号暂不变更，发布另行授权。

## 外部配置及 Helper 交接

本插件曾要求管理员在 CPA 配置中手工安装以 `X-CPA-Helper-Claude-TTL` 为条件的 payload.override / payload.filter 规则（包含顶层和嵌套 cache_control.ttl），以及 Plugin 的 `claude_cache_ttl` 配置块。移除插件 TTL 不会自动删除 CPA 自身的规则。管理员应备份并删除这些专用规则和插件配置块，保留其他 payload 配置，再重载 CPA；遗留规则仍可按请求头触发，不能把它们当作已由插件停用。插件不再注入、清除或认证这两个历史 TTL 头。

CPA-Helper 若尚未接入 TTL，无需实现历史交接；若已接入，应以 capabilities 为准停用 TTL 编辑、元数据查询和 `enforced_request` 关联，历史计费数据仍保留。不得从当前 Key 策略推断旧请求 TTL，不得将接口 404 当已取得 TTL。CPA 自身记录的 token/缓存计数及其既有消费流程继续由 CPA/Helper 维护。本轮不改动另一个工作区。

v0.1.12 版本交接：若 Helper 对普通策略/模型目录的 plugin_version 使用白名单，需新增 0.1.12；policy v1、cpa_version=v8.0.45 及现有保留模块的管理形状继续沿用。运行版本应独立读取 CPA 信息，不能把固定 cpa_version 作为宿主版本锁。

## 回滚

升级前备份完整状态目录和 CPA 配置，停止 CPA 后替换库。启动不会清理旧 state.json 的 TTL 字段和 ttl-metadata 文件。正常新策略提交或回滚会移除当前策略的 TTL，仅保留历史快照/回执必要数据。二进制回滚需恢复匹配的插件、CPA 配置和升级前状态备份，避免恢复策略与外部 payload 不一致。

## 验证

实施完成，未提交或发布。环境：Windows amd64、Go 1.26.10、UCRT GCC 16.2.0；SDK 仍固定 CPA v8.0.45。

| 检查 | 结果 |
| --- | --- |
| `go fmt ./...`、`go vet ./...` | 通过；七个无关文件只有行尾变化，经逻辑 diff 核对后恢复 |
| `go test ./... -count=1` | 全包通过；无 CPA_BINARY 时真实 Host fixture 明确跳过，实际 Host 结果见下两行 |
| `go test -race ./... -count=1` | 全包通过 |
| 最终 DLL + 官方 CPA v8.0.47，`TestCPACompatibility` | 21.543 秒通过 |
| 同一 DLL + 官方 CPA v8.0.45，`TestCPACompatibility` | 22.454 秒通过 |
| `npm run test:directory` | 13/13 通过 |
| JS syntax、`npm run test:ui` | 通过；Edge 使用官方 v8.0.47，桌面/移动布局及无 TTL 编辑器已核对 |
| 文档/JSON、`git diff --check` | 14 份 Markdown、84 个本地链接、3 份 JSON 通过；无 whitespace 错误 |

两版真实 fixture 验证动态加载及 schema 协商、管理认证/资源/默认值、模型列表过滤、凭据路由与子集权重、并发及生命周期、流式取消/截断、多协议响应放行、重启/回滚和重新启用。移除回归覆盖：启用但指向不存在配置文件的旧 YAML 仍能加载，TTL 配置/能力/健康消失，旧路由 404；真实磁盘的 0.1.10 和 0.1.11 TTL 策略保留原权限、producer/revision、精确回执，新 TTL 写入 400，启动/重试/拒绝不改变 state.json，历史元数据路径不被触碰。

单测另覆盖旧策略回滚不恢复 TTL、正常更新和持久回执重放、损坏 TTL 状态仍不可用，以及请求前后响应为空、无 TTL header/body 修改、无显式调度委托、无 usage capability、usage 方法 404、完成后释放并发。最初新增测试的 SDK 类型名与脚本重名变量已修正后通过上述全部检查。

本地最终 DLL（`-trimpath -buildmode=c-shared`）SHA256：`43a3284ce90c661db5d0176205b4496eeb85bf416af966b0d9d4aa1222ca71a2`，仅为本轮工作树产物。测试 CPA 进程已清理。没有修改 go.mod/go.sum、CPA/Helper 工作区或真实部署；不宣称 VPS 实际内存下降、Linux 动态加载或真实计费联调已经验证。

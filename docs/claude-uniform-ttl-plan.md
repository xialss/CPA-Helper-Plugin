# Claude 单请求 TTL 实施计划

2026-10-28。需求以 [PRD](claude-uniform-ttl-prd.md) 为准；机制证据见 [可行性验证](claude-ttl-feasibility.md)。

| 阶段 | 交付与完成条件 | 状态 |
| --- | --- | --- |
| P1 | 固化单请求范围、旧策略兼容、ABI 与配置前提；核对公开 ID 关联 | 完成；生产 DLL 双 profile 基础矩阵和 execution_id 对应通过 |
| P2 | Key 字段、严格校验、冻结状态、晚期规则契约及失败行为 | 完成；64 请求基础矩阵、重试/热更新及漂移拒绝通过 |
| P3 | usage.handle 补充执行元数据、鉴权查询、延迟/清理/未知语义 | 完成；精确执行关联、持久化、未知/冲突、延迟及取消测试通过 |
| P4 | Plugin Key 编辑器与健康/能力展示，Helper 对接契约与回滚说明 | 完成；Edge 保存/清除/失败保留草稿及桌面/窄屏验证通过 |
| P5 | 单测、并发/race、真实 CPA 生产 DLL fixture、最终验收记录 | 完成；[验收记录](claude-uniform-ttl-verification.md)含命令、边界与未测项 |
| P6 | 三位 subagent 审查、交叉复现与修复、最终产物回归及详细 Helper 交接 | 完成；[审查记录](claude-uniform-ttl-review.md)，P1/P3 均关闭；[Helper 交接](claude-uniform-ttl-helper-handoff.md)已核对当前代码 |
| P7 | 用户另行授权提交和发布，版本/升级兼容、官方构建及正式产物验证 | 完成；v0.1.10 Latest，发布 DLL 三套真实 CPA/TTL fixture 与 Edge 通过；[发布记录](v0.1.10-release.md) |

## 依赖与检查点

- 配置校验采用显式 `host_config_path`：与 CPA 实际启动的完整配置文件一致，注册时校验 payload 和 routing.strategy 并锁定文件摘要。任何文件变化使受控请求失败，需要配置稳定后重载插件。ABI 不提供运行时完整配置读取，不能把本地文件校验包装成全局原子配置锁。
- 第一版只接受 requirements 接口给出的完整 payload 对象；不覆盖已有自定义规则、不猜测任意规则间的冲突。已有额外 payload 规则的部署必须先解决配置兼容，不能自动删除。
- 未限制候选时明确委托与 routing.strategy 一致的 CPA 内置调度，确保公开 selection_id 保留；有路由限制时继续已有选择逻辑。
- 执行元数据保留 24 小时，独立小文件原子写入、定期按需清理；无 token 计数。消费者应在同一消费周期查询，缺失或过期明确 unknown，未关联迟到回调不能补推断。
- 本轮实现 Plugin 与 Helper 对接契约；Helper 消费端及实际计费变更需在 Helper 工作区显式实施，不能宣称该仓库变更已经完成跨服务计费。

- 在实际关联 ID 验证完成前，不把 metadata 接口声明为可用于计费。
- 在最终 payload 规则验证完成前，不把请求偏好声明为 enforced。
- 如官方接口不能满足某项约束，记录具体证据和受影响验收项，先完成独立部分，禁止伪造完成。
- 每完成一个阶段更新本表及 tasks.md；设计变化先记录再实现。
- Helper 的实际 TTL 消费契约保持不变，新 provenance 的消费必须显式接入；本仓库不嵌入 Helper 服务、数据库或 UI。

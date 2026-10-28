# Claude 单请求 TTL 控制 PRD

更新：2026-10-28；状态：Plugin 实施及本机验收完成，进入 v0.1.10 发布验收。目标：CPA v8.0.45 / C ABI 1 / RPC schema 7。发布进度见 [v0.1.10 记录](v0.1.10-release.md)。
证据见[验收记录](claude-uniform-ttl-verification.md)。Helper 消费端属于独立工作区对接，未在本轮修改。

## 需求与边界

- 不修改 CPA 源码；Plugin 保持独立，Helper 通过版本化管理契约消费。
- 每个 Key 可选择 `5m` 或 `1h`，该策略优先于请求中的 TTL，统一已有 ephemeral 断点；不支持单次请求混合 TTL。
- 不移动断点，不把 explicit 空断点请求改成自动缓存。CPA 自动插入的断点也须接受最终 TTL 规则。
- 已有顶层 automatic `cache_control` 同样统一 TTL；没有根控制时不得创建根控制。
- 未配置 TTL 的旧 Key 保持原行为；新字段为可选字段。只有明确配置的 Key 才执行 TTL 策略。
- 非 Claude 上游、内部模型查询、count_tokens 不生成 TTL 计费事实。
- CPA usage queue 是 token 数量的唯一来源；Plugin 仅提供执行 TTL 元数据，不复制 token 数量、不建立第二套账本。
- 元数据来源必须标注 `enforced_request`，不能冒充上游实际 TTL；Helper 的实际 TTL 字段不能静默改变含义。

## 实施约束

1. 使用已验证的 header 条件 payload override/filter，在 CPA 内部缓存处理之后统一 TTL。保留 after-auth Body 省略优化。
2. 保留 Key 的不可逆 caller scope，不保存原始 Key、请求体、上游认证或管理凭据。
3. 请求开始时冻结 TTL 与 policy revision；热更新只能影响新请求。重试不得重新读取当前 Key TTL。
4. 使用公开 SDK 的选择标识关联 usage execution_id；不得用短 TraceID、请求体摘要或时间近似关联。
5. 调度直接委托 CPA 内置策略或使用现有受限候选选择逻辑，不另建调度器。配置必须明确与现有 CPA 调度模式一致。
6. 启用前校验必要 payload 规则；配置缺失、冲突、漂移必须可观察并拒绝受控请求，不能宣称策略已生效。
7. 元数据延迟、重启、找不到关联时显式 unknown；不得根据当前 Key 偏好补写历史 TTL。

## 验收

- A01：旧策略正常读取；非法 TTL 导致整份快照拒绝，revision 不变。
- A02：原生 Claude 与 Chat 转 Claude，普通与流式，两个 fingerprint profile，最终断点全为 Key TTL。
- A03：显式空断点仍为空；非 Claude 与 count_tokens 不受 TTL 改写影响。
- A04：伪造控制 header 无效；不同 Key 相同请求并发无串线；中途热更新不改变冻结 TTL。
- A05：usage.handle 为唯一观察入口，元数据与 stock queue.execution_id 精确对应，token 数量不复制。
- A06：重试、取消、延迟 usage、重复 usage、生命周期清理和重启缺失事实有明确测试及语义。
- A07：管理接口使用 CPA 官方管理鉴权；缺失元数据不能返回伪造 5m。
- A08：配置规则缺失、冲突、漂移可检测；无外部 Helper 时本地策略继续生效。
- A09：fmt、vet、test、race、DLL 构建、真实 CPA fixture 和管理 smoke 通过；未测实际 provider 明确标注。

## 回滚与发布

先移除 Key TTL 设置并停用功能，再恢复原 payload 配置。旧插件不保证读取带新字段的策略，回滚应恢复备份策略。此轮不提交、推送、发布或修改真实部署。

真实上游是否接受 TTL、缓存最小长度、命中率与真实账单不属于合成 fixture 已验证结果。

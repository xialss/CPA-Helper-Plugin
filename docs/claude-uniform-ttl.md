# Claude 单请求 TTL：部署与 Helper 对接

状态：v0.1.10 实现，目标 CPA v8.0.45 / C ABI 1 / RPC schema 7。单次请求只使用一种 TTL。发布及产物验证见 [v0.1.10 记录](v0.1.10-release.md)。

## 行为

策略 Key 新增可选 `claude_cache_ttl`：`5m` 或 `1h`。省略或空字符串保持 CPA 原行为。
字段不能放在 Group；只有 Key 自身决定 TTL。它优先于输入中冲突的 TTL，统一 CPA 准备好的 ephemeral 断点，
不增加或移动断点。`prompt_cache_options.mode=explicit` 且无断点时仍无断点。

非 Claude 协议、内部模型查询、count_tokens 不产生 TTL 事实。请求开始冻结策略修订和 TTL，
中途修改 Key 只影响新请求。重试和凭据 fallback 保留冻结值；配置发生漂移则显式拒绝。
客户端提供的两个保留 header 会被清除，不能选择 TTL 或冒充另一个请求。

只有 `usage.handle` 观察 usage；补充事实不含 token 数量，也不改 stock queue。
`ttl_source=enforced_request` 表示请求策略归属，不是 provider 返回的实际缓存 TTL，更不证明发生缓存命中。

## 部署前提

此版本采用明确、保守的配置契约：

1. `host_config_path` 必须是 **CPA 实际启动使用的完整配置文件的绝对路径**，不是副本。
   这是管理员声明；官方 ABI 没有完整运行时配置读取，Plugin 不能证明一个副本就是宿主正在使用的配置。
2. 完整 `payload` 必须与 `GET /claude-ttl/requirements` 返回的对象相同。
   不支持自动合并任意自定义规则；不要删除已有业务规则以强行通过校验。
3. `routing.strategy` 必须显式设为 `round-robin` 或 `fill-first`，Plugin 的 `delegate_builtin` 必须相同。
   无路由限制时委托同一个 CPA 内置调度，保留公开 selection_id；有限制时继续 Plugin 现有选择逻辑。
4. 不允许其他插件、外部文件回滚工具或运行时管理操作在校验之后再次改写 TTL。
   本地文件摘要校验不是跨宿主、文件 watcher 和多插件的全局原子锁。

先备份配置和 policy，在维护窗口安装如下配置，然后重启 CPA，最后给 Key 配置 TTL。
Plugin 不自行修改 CPA 配置、不保存 CPA 管理密码。没有 Helper 在线连接也能继续应用本地策略。

```yaml
routing:
  strategy: round-robin
plugins:
  enabled: true
  # 保留实际 dir、其他配置项及既有 state_dir
  configs:
    cpa-helper-plugin:
      enabled: true
      claude_cache_ttl:
        enabled: true
        host_config_path: 'D:/actual/cpa/config.yaml'
        delegate_builtin: round-robin
payload:
  override:
    - models:
        - name: '*'
          protocol: claude
          headers:
            X-CPA-Helper-Claude-TTL: 1h
      params:
        'tools.#(cache_control.type=="ephemeral")#.cache_control.ttl': 1h
        'system.#(cache_control.type=="ephemeral")#.cache_control.ttl': 1h
        'messages.#(content)#.content.#(cache_control.type=="ephemeral")#.cache_control.ttl': 1h
    - models:
        - name: '*'
          protocol: claude
          headers:
            X-CPA-Helper-Claude-TTL: 1h
          match:
            - cache_control.type: ephemeral
      params:
        cache_control.ttl: 1h
  filter:
    - models:
        - name: '*'
          protocol: claude
          headers:
            X-CPA-Helper-Claude-TTL: 5m
      params:
        - 'tools.#(cache_control.type=="ephemeral")#.cache_control.ttl'
        - 'system.#(cache_control.type=="ephemeral")#.cache_control.ttl'
        - 'messages.#(content)#.content.#(cache_control.type=="ephemeral")#.cache_control.ttl'
    - models:
        - name: '*'
          protocol: claude
          headers:
            X-CPA-Helper-Claude-TTL: 5m
          match:
            - cache_control.type: ephemeral
      params:
        - cache_control.ttl
```

根 `cache_control` 是 automatic caching 的已有控制，也必须统一 TTL。
新增根规则通过最终 payload 的 `cache_control.type=ephemeral` 条件匹配，不能改为无条件写入；
否则会给原来没有根控制的请求创建新控制。三条块路径与两条根条件规则需要完整安装。

完整文件的 **任何** 内容变化（含无关配置、注释）都会使旧摘要失效。
受控 Claude 请求返回 503 `claude_ttl_unavailable`；健康状态显示 `unavailable` 及原因。
配置稳定后，使用 CPA 官方插件重载/重启流程重新注册校验；不要只以配置保存成功作为生效依据。
一旦观察到漂移，该 guard 会锁定失败；单纯恢复旧文件内容不能代替重新校验。
旧的 in-flight 请求仍持有旧 guard，配置切换时可能显式失败，不能悄悄执行另一个 TTL。

能力/健康中的 `configured` 仅代表已校验固定配置文件，不代表 live provider 已接受 TTL。
启用失败、payload 不匹配、调度模式不一致不会更新当前有效插件配置。

## 执行元数据协议 v1

所有路径都在 CPA 已鉴权的 `/v0/management/plugins/cpa-helper-plugin/v1` 下。
下游 API Key 不能访问这些接口。

`GET /claude-ttl/requirements` 返回规范 payload、两个保留 header 名及 metadata_version。
`POST /claude-ttl/executions` 接受 1～1000 个不重复、规范小写 UUID，JSON 最大 64 KiB：

```json
{"execution_ids":["e229118e-a1be-4fa4-93ea-7b003d5b9761"]}
```

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

记录只在相同请求已进入经校验的 Claude 执行路径并设置 TTL 标记、且 usage.selection_id 精确关联成功后生成。
关联链：请求冻结随机 token → scheduler.selection_id → usage.RequestID → queue.execution_id。
不使用 queue.request_id、短 TraceID、请求体哈希或时间窗口猜测关联。
`caller_scope` 是 policy.CallerScope 的不可逆身份，可用于防串 Key 检查。

- stock queue 和 Plugin usage observer 异步交付，先看见队列、暂未查到元数据是允许状态。
- Helper 应先持久化原队列，再在同一消费流程有界补查，不为等待元数据重复记 token。
- 元数据保留观察后 24 小时，写入 `<state_dir>/ttl-metadata`；原子落盘，同执行 ID 重复通知幂等，冲突拒绝。
- 请求/nonce 在生命周期完成时释放；选择关联在 24 小时内保留以支持迟到和同 selection 的多个执行。
- 重启后已落盘事实可读；重启前未完成关联、未收到 usage 或尚未持久化的事实可能缺失，不能重建为当前偏好。
- 过期/未观察/无关联/非 Claude 返回 missing；存储损坏或读写失败显式错误，不伪造 5m。
- 保留期清理在功能启用时的启动和写入时触发，最多每小时扫描一次；停用后仅提供旧事实读取，不新增目录或清理写入。请求/选择清理不启动独立 scheduler 或 usage producer。
- `health.claude_cache_ttl.unknown_usage` 统计未关联的 Claude generation usage，可能含未配置 TTL 的请求；它不等于计费错误数。

## Helper 消费端必须显式适配

后续实施的文件入口、版本兼容、清除语义、原队列去重与首次扣款顺序、迁移/验收要求见[完整 Helper 交接](claude-uniform-ttl-helper-handoff.md)。

Helper 现有“上游实际 TTL”字段和 §28 契约不应直接复用为该新事实的来源。
本轮只更改 Plugin 工作区；Helper 应在自身任务中执行以下契约：

1. 确认已知兼容版本（本次为 0.1.10/v8.0.45），并通过 capabilities 的 `claude_cache_ttl` module 和 `metadata_version=v1` 检测能力；0.1.9 不含 TTL，不能只凭版本字段跳过能力探测。
2. 使用 **stock queue.execution_id** 查询，校验 caller scope 和执行 ID；原队列仍是唯一 token 数量来源。
3. 保留已有 upstream TTL 的优先级；将该补充事实单独标为 `enforced_request`，明确展示归属来源。
4. 对正数 cache creation 且单请求 TTL 事实有效的记录，可按明确产品契约把全部数量归入对应桶；
   不得把该归属改称上游观测。数量为 0 时保留现有空 TTL 语义。
5. 缺失、超时、冲突或 provenance 不支持时保留 unknown 和既有 legacy 行为，不能读取当前 Key 默认值补历史账。
6. 独立实现 Key 的 TTL 编辑、权限边界、计价/展示和端到端测试。本仓库测试不能替代该验证。

## 回滚

先清除所有 Key 的 TTL 策略，再停用 Plugin TTL 设置、恢复原 payload 和 routing 配置并重启 CPA。
若回退到旧 Plugin，恢复升级前 policy/state 备份：旧严格 decoder 不保证读取新字段。
元数据不含 token 和金额，不能作为独立账本回放。

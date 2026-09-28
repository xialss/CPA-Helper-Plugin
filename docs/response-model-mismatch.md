# 上游响应模型核验

## 目标与实现状态

当前版本为插件 0.1.4，唯一开发目标 CPA v8.0.3（C ABI 1 / RPC schema 6）。
仅比较客户端请求模型与上游响应声明的模型，不实现请求前模型不匹配拒绝。
检测用于发现可观察的模型替换，不能证明上游填写的模型名真实，也不能撤销上游执行、费用或已发送的流内容。

## 数据来源

`request.intercept_before` 仅记录请求 ID、客户端模型、请求体 SHA-256 和当时配置；现有 Key、路由和并发准入仍照常执行。
`response.normalize_before` 在 CPA 协议转换前读取原始响应的 `model`、`modelVersion`、`message.model`、`response.model`、`response.modelVersion` 或 `interaction.model`。
不读取生成文本、工具结果或 usage 队列；不使用 CPA 路由模型冒充上游响应模型。

CPA v8.0.3 的转换前钩子没有请求 ID，只提供原始请求体。因此插件使用仅驻留内存的请求体摘要关联唯一的在途请求：
相同原始请求体同时在途时，将整组标为无法关联，不把其中一个请求的模型当作另一个请求的结果。
即使其中一个请求先完成，其余请求的歧义标记也不会解除。
启用、关闭、跳过核验的请求都参与关联，避免配置切换造成错误归属。
若其他插件改写原始请求体导致摘要不一致，或执行器未经过该转换钩子，则按“无法核验”处理。
`request.complete` 清理请求状态与摘要索引；状态不落盘，不记录原始请求内容或摘要。

## 客户端行为

- 非流式：已知不匹配时用 JSON 错误替换原正文，返回 `upstream_model_mismatch`、请求模型、上游声明模型及请求 ID。
  加入 `X-CPA-Helper-Model-Mismatch: true` 与 `X-CPA-Helper-Error`，清除旧长度/编码头。
- 流式：通过 `response.intercept_stream_chunk` 替换第一块被拒绝的内容为错误；后续块全部 `DropChunk`，不会因后续模型变化或重新配置而恢复放行。
  OpenAI Chat 钩子接受裸 JSON（HTTP handler 添加 SSE），Claude/Gemini 接受 SSE 错误帧。
  Responses 使用 `response.failed` 终止帧，错误中包含 `status: 403`；CPA 将该终止错误传给客户端并取消执行上下文。
- 已有上游失败不改写。正常匹配结果保持 CPA 原正文。日志包含请求 ID、请求/上游模型、实际决定、原因、流式标记及配置动作，不包含请求内容、Key 或凭据。
- 当前响应 ABI 无直接状态码覆盖或通用取消接口，因此 HTTP 状态通常仍为 200；
  客户端应识别 JSON/SSE 错误。流式错误的显示方式取决于客户端。
  Responses 的终止帧由 CPA handler 负责取消上游；其他协议不据此承诺主动取消，已产生的费用也不能撤销。
- 缺少客户端模型、上游模型、原始响应或关联有歧义时，使用独立错误码 `response_model_unverifiable`，绝不误称为模型不匹配。关联歧义始终按不可核验处理，不受 `unknown_action=pass` 放行设置覆盖；`audit` 仍只记录不拦截。
- Responses 流在 reject 模式下暂存全部文本和工具调用，直到成功终止事件完成核验才整体放行；即使开头匹配、结尾模型变化，也不会提前执行工具。不匹配立即丢弃暂存内容并返回终止错误。无法核验的配置在成功终止时判定。
  代价是客户端等待整次响应完成后才显示内容，内存占用随响应大小增长。上游失败仅返回失败事件；取消或缺少终止事件时丢弃暂存内容，由 CPA 保留连接错误语义。
  审计模式不暂存。严格模式默认拒绝其他流式协议；关闭严格模式后它们仍逐块核验，无法撤回此前已发送的块。
- 此版本验证 HTTP 非流式与 SSE。WebSocket 观察接口不能拦截输出，不承诺 WebSocket 拦截。

### Codex 自动重试修复

Codex 会将 `response.failed` 中未知错误码视为可重试错误，旧版直接发送
`upstream_model_mismatch` 导致客户端自动重连。仅附加 `retryable: false` 或 403
不足以改变 Codex 对 SSE 的分类。Responses 流因此采用 Codex 识别的终止分类
`error.code: invalid_prompt`、`error.type: invalid_request_error`，把原始业务错误码
保留在 `verification_code` 和中文消息前缀中。这是响应核验拒绝，不表示用户提示词有误。
非流式、Chat、Claude 和 Gemini 保留原业务错误码。

真实 Codex CLI 0.155.1 + CPA v8.0.3 测试中，旧版产生 3 次上游请求；修复后
立即不匹配、稍后不匹配、未知拒绝均仅产生 1 次请求，上游收到取消，客户端直接结束为失败。
不会伪造正常完成或禁用全局重试。独立会话、用户手动再次提交的请求仍是新请求；
Responses 暂存修复进一步防止“最后才发现不匹配、工具已执行”；如果整个响应都没有模型声明，unknown_action=pass 仍允许最终放行。

## 面板与配置

打开 **CPA Helper → 响应核验**，编辑后点击“保存核验配置”。
面板先调用插件校验接口，再通过 CPA 官方配置 API 持久化，最后确认插件热更新成功。
配置错误不会部分应用；进行中的请求固定使用开始时的配置。默认关闭。

```yaml
response_model_mismatch:
  enabled: false
  stream_enabled: true
  responses_only_stream: true
  max_buffer_bytes: 16777216
  max_total_buffer_bytes: 67108864
  action: reject
  unknown_action: pass
  case_sensitive: false
  ignore_thinking_suffix: true
  ignored_models: []
  accepted_models: {}
```

| 字段 | 行为 |
| --- | --- |
| enabled | 总开关 |
| stream_enabled | 是否核验流式请求 |
| responses_only_stream | 严格模式下仅允许 Responses 流式协议；关闭后其他协议仍按逐块策略处理 |
| max_buffer_bytes | Responses reject 模式的单请求暂存上限，范围 1 MiB–256 MiB |
| max_total_buffer_bytes | 所有并发 Responses 请求的暂存总上限，不能小于单请求上限，最大 1 GiB |
| action | reject：拦截并提醒；audit：仅日志审计、原样转发 |
| unknown_action | pass：保留一般无法核验的响应并记录原因；reject：拦截并提醒无法核验；关联歧义始终拦截；audit 模式总是原样转发 |
| case_sensitive | 是否区分名称大小写 |
| ignore_thinking_suffix | 是否去掉末尾思考强度括号后缀 |
| ignored_models | 精确匹配的客户端模型豁免列表 |
| accepted_models | 请求模型到允许的响应模型数组的单向映射 |

比较始终忽略首尾空白及 Gemini 的 `models/` 前缀，不隐式接受日期版本、同系列名称、别名或模型池替换。
需要容许别名时显式填写映射，例如 `public-model: [provider-model, provider-model-2026]`。
`auto` 不自动改成路由模型；需要跳过时显式加入豁免列表。

配置由 CPA 管理，与插件策略快照分离。外部 CPA-Helper、SQLite、计费服务均不参与决定。
移除新配置节点并恢复原动态库可回滚；CPA/插件需使用相互验证的发布版本。
若已保存 0.1.4 版本的策略快照，降级时还需在 CPA 停止后恢复旧插件支持的策略备份。

## 验证

在 CPA 日志页搜索 `[响应核验]`（需要 CPA `logging-to-file: true`）。通过官方
`host.log` 写入宿主日志；日志通道故障会明确输出 stderr 错误及原诊断，不影响拦截。
注册/配置更新记录当前开关与动作，每个请求记录实际决定，流式相同决定只记一次：

- `result=blocked`：已选择错误响应替换；不是上游终止或客户端确认收到了提醒。
- `result=audit_mismatch`：发现不一致，仅审计未拦截。
- `result=unknown_pass`：无法核验，按配置放行；不能视为匹配成功。
- `result=matched`：成功输出已核验，请求完成时记录，包含显式映射匹配。
- `result=skipped`：查看 `reason`（disabled、stream_disabled、ignored_model 等）。
- `result=not_checked`：没有经过可检查的成功输出，例如上游错误或提前取消。
- `result=buffering`：暂存 Responses 内容，尚未放行文本或工具调用。
- `result=buffer_discarded`：缺少已核验终止事件，已丢弃暂存内容。
- `result=upstream_error`：丢弃暂存内容，只返回上游失败事件。

`requested_model`/`actual_model` 显示名称，`request_id` 用于关联请求。
关联歧义时不记录无法归属的上游名称。流中先未知后不匹配会分别记录决定变化。
无需开启记录完整请求正文的 `request-log`。日志保留与轮转沿用 CPA。

单元测试覆盖真实/改写模型差异、协议字段、一次提醒、拒绝锁定、未知/歧义、配置事务、请求配置固定及并发清理。
真实 CPA fixture 覆盖 OpenAI Chat 非流式/流式、Responses 非流式/流式，以及经 OpenAI 上游转换的 Claude/Gemini 流式，
核验错误提醒、内容屏蔽、允许映射与未知拒绝，并保留原有准入/取消/重启/回滚测试。
浏览器测试使用隔离 Docker CPA，验证面板保存、非法输入、刷新、CPA 重启持久化与桌面/移动布局。
具体命令和平台结果见 `testing.md` 与 `compatibility.md`。

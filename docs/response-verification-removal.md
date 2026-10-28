# 移除响应模型核验

本记录保留移除阶段的原始证据；后续 v0.1.10 提交、发布及正式产物验证见 [发布记录](v0.1.10-release.md)。

## 授权与目标

2026-10-28 用户报告 GitHub 发布版 v0.1.9 在 VPS 上大量占用内存，核验功能未开启；随后明确要求移除该功能。目标为删除响应模型核验及其资源开销，保留模型列表过滤、请求权限、调度、并发和 Claude TTL。目标 Host 仍为 CPA v8.0.45 / C ABI 1 / RPC schema 7。

## 已确认的开销路径

发布版无条件注册 `response_before_translator` 和 `response_stream_interceptor`。CPA v8.0.45 的 `sdk/translator/registry.go` 每个流式翻译帧调用 `NormalizeResponseBefore`；`internal/pluginhost/adapters_usage_translation.go` 克隆原始/翻译后请求体，再由 `rpc_client.go` JSON 编码并通过 C ABI 交给插件。插件即使关闭核验，也先接收、复制并反序列化这些载荷。因此长提示词与长输出组合会放大分配、复制和 GC 开销。此处为固定源码分析，尚无用户 VPS 的堆采样，不能据此宣称已证明永久内存泄漏或全部 OOM 原因。

## 实施顺序

1. 先记录兼容契约，再删除核验器、原始响应观察、流式缓冲、请求关联、核验日志及专用单测。
2. 移除两类回调注册与调度；请求拦截前后均声明省略 Body。普通响应回调仅用于模型列表过滤。
3. 移除管理验证接口、公开配置元数据、能力/健康字段、核验界面和专用 smoke 脚本。保留严格的其他配置校验。
4. 仅为升级解码旧 `response_model_mismatch` YAML，记录移除警告；不验证、不保存、不使用其内容。即便旧配置为 enabled，也不能注册或执行核验。
5. 替换核验验收为移除回归，运行真实 CPA 及 TTL、浏览器测试，更新当前使用说明和 Helper 交接。

## 验收标准

- 注册中没有原始响应/流式拦截能力、没有核验配置；请求前后省略 Body。
- 旧配置能启动且不恢复功能；其他未知配置仍报错。
- 旧管理验证接口返回 404；能力、健康及 UI 不再暴露核验入口。
- 上游声明模型与请求模型不同时响应透传，流式输出不因插件核验缓冲或中断；上游错误、截断和取消保持 CPA 语义。
- 模型列表过滤、请求限制、生命周期清理和 TTL 元数据通过回归。
- fmt / vet / test / race、真实 CPA 兼容 fixture、TTL fixture、目录及浏览器 smoke 完成后记录实际结果。

## 升级、回滚与边界

本次不改已有 policy/catalog/TTL 状态格式，不会重写旧核验 YAML。管理员可从配置中删除旧字段。回滚使用匹配的旧插件/CPA 与状态备份；回滚后旧核验实现和开销会重新出现。外部 Helper 应按能力隐藏或删除核验管理入口，不再调用已移除的验证接口；详见 [Helper 交接](claude-uniform-ttl-helper-handoff.md)。不修改 Helper、CPA 或真实部署，不自动提交、发布。发布版 v0.1.9 仍包含旧实现，必须使用后续包含本次修改的构建才能获得效果。

## 验证记录

2026-10-28，Windows amd64、Go 1.26.10、UCRT GCC 16.2.0、CGO_ENABLED=1。使用已校验的官方 CPA v8.0.45 和本次重建的生产 DLL，全部为隔离临时目录、合成 Key 和 mock 上游。

| 检查 | 结果 |
| --- | --- |
| `go fmt ./...` | 通过；清理与本次逻辑无关的 CRLF 格式噪声；最后移除辅助方法后对目标文件再次 gofmt |
| `go vet ./...` | 通过 |
| `go test ./...` | 通过；未设置 CPA 环境变量时的可选真实 Host 测试在此命令内 skip，单独执行结果见下行 |
| `go test -race ./...` | 通过；覆盖 Go 状态/并发测试，不代表生产 DLL 已被 race instrumentation |
| `go build -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin` | 通过，最终 DLL 已实际加载 |
| `go test ./integration -run '^(TestCPACompatibility\|TestClaudeUniformTTLProduction(TopLevel)?)$' -count=1 -timeout 240s` | 最终构建三套真实 Host fixture 通过，包耗时 49.959 秒；含 TTL 原 64 基础/增强矩阵与顶层 80/80 场景 |
| `npm run test:directory` | 13/13 通过 |
| `node --check internal/plugin/web/app.js`、`node --check scripts/browser-smoke.mjs` | 通过 |
| `npm run test:ui` | 最终 DLL 的 Edge smoke 通过；旧 enabled/reject 配置、无核验入口/能力、旧接口 404、配置未被重写、Key/TTL 草稿、目录、权限及布局均通过 |
| 文档/JSON/diff | 9 份当前文档的本地链接、package/schema JSON 及 `git diff --check` 通过；移动端健康页无核验标签且无横向溢出 |

最终 Windows DLL 大小 10,289,841 字节，SHA-256：`feb6caa7f665454ab4c72c17435e425b0a34bb85d7cdd00403fcd7b5f6541adc`。此产物在忽略的 `dist/` 中，不是新的 GitHub release。

移除回归证明：旧配置加载与热更新无法恢复原始响应/流式 hook；旧 RPC 方法返回 404；请求前后均声明省略 Body；配置元数据、capabilities、health 和管理路由不再暴露功能。真实 Host 检查 Chat/Responses 非流式与流式、Claude/Gemini 流式的模型不同/缺失/变化时透传。Responses（普通及 Codex originator）在上游仍保持打开时收到 `hello`，随后取消并释放并发；真实截断仍报告错误，干净 EOF 正常完成。模型列表过滤、路由权重、403/429/503、重启、回滚及插件卸载/重载也通过。

验证过程中的问题已解决：首次新增 Responses 断言误将合法 `"error":null` 当作业务错误，改为识别错误对象后兼容测试通过。首次 `go vet ./...` 发现忽略的 `dist/cpa-v8.0.17-review/probe/cpa_test.go` 是旧版临时探针，仍引用已删除类型；完整保留到工作区外 `D:/Temp/cpa-helper-legacy-probe-20261028/cpa_test.go` 后，全量检查通过。未保留虚假的旧类型或跳过当前项目包来掩盖错误。

未运行：用户 VPS 上的实测/堆采样、Linux/Docker 运行、真实 Codex 客户端、真实 provider 和 Helper 计费联调。上述结果证明移除和兼容回归，不量化 VPS 内存下降或宣称已排除其他 OOM 原因。没有修改 CPA、Helper、真实部署、Git 提交、PR 或 release。

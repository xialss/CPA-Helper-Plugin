# CPA-Helper Plugin

`CPA-Helper Plugin` 是 CLIProxyAPI (CPA) 的第一方动态插件。它在 CPA 内部针对每个 API Key 执行策略控制，并为外部管理端提供带版本控制的管理契约。

初始产品边界如下：

```text
外部管理端 (可选) -> 策略快照/API -> CPA-Helper Plugin (数据面)
                                        -> CPA 请求生命周期
CPA 官方插件管理 -> 注册、生命周期与插件资源
```

本插件对 CPA-Helper 没有代码级或运行时依赖。即使外部管理端离线，插件也会继续使用最后一次被接受的策略快照，并通过其管理端点报告健康状态与版本号（revision）。

## 功能特性

- API Key 分组与稳定的分组归属
- 分组与 Key 维度的模型允许/拒绝规则
- 上游凭据选择前的请求准入控制
- 明确的拒绝原因与状态码
- 针对单个 Key 的实例本地并发限制及生命周期清理
- 面向外部管理端的通用管理 API

计费、余额、订阅以及用量统计仍归 CPA-Helper 所有。本插件不消费用量队列，不注册用量观察者（usage observer），也不直接代理模型流量。

## 构建与安装

需要 Go 1.26+、C 编译器，以及启用了插件支持的 CPA v7.2.143 或 v7.3.7。CPA v6 版本和 `no-plugin` 构建版本无法加载此插件。确切的版本锁定请参见 `docs/compatibility.md`。

```powershell
go build -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin
```

在 Linux amd64 上：

```sh
CGO_ENABLED=1 go build -buildmode=c-shared -o dist/cpa-helper-plugin.so ./cmd/cpa-helper-plugin
```

停止 CPA，将对应的动态链接库放入其 `plugins` 目录中，并添加如下配置：

```yaml
plugins:
  enabled: true
  dir: plugins
  configs:
    cpa-helper-plugin:
      enabled: true
      state_dir: plugins/cpa-helper-state
```

重启 CPA 并访问 `/v0/resource/plugins/cpa-helper-plugin/ui` 或打开 CPA Helper 插件菜单。插件会复用同源 CPA 控制面板已保存的登录会话，并自动加载 CPA 目录；它没有独立的登录表单。
编辑 Key 或规则后，点击“保存”即可一次性应用所有更改。关闭编辑器不会自动保存。规则除名称外还支持可选备注。
保存失败时更改内容仍会保持可见以便修正。并发数为 0 表示不设限制。已通过认证但未特别配置策略的 Key 默认允许访问；即使 CPA-Helper 离线，显式配置的规则依然保持生效。

对于 Docker 部署，请将 `/CLIProxyAPI/plugins` 进行绑定挂载（bind-mount），以确保动态库和策略状态在容器重建后仍可保留。第三方插件源安装与更新工作流请参见 `docs/plugin-store.md`。

当路由缩小了 CPA 的候选凭据集时，插件会在该子集内执行加权轮询（weighted round robin）选择。插件绝不会跨优先级层级检索：如果 CPA 仅提供了高优先级凭据，即使存在已授权的低优先级凭据，请求仍可能返回 503。未经过验证集成前，请勿同时启用其他具有竞争关系的调度器插件。

升级前请务必备份状态目录。若已有状态文件缺失或损坏，插件将返回 503 而非直接重置权限。策略回滚会发布一个新的版本（revision）；二进制文件或状态降级则需要在停止 CPA 时恢复对应的备份。

## 代码仓库指南

- `docs/architecture.md`: 模块边界与请求生命周期
- `docs/compatibility.md`: 锁定的 CPA ABI 及兼容性矩阵
- `docs/management-contract.md`: 通用管理端同步契约
- `docs/plugin-store.md`: 第三方插件源与发布工作流
- `docs/testing.md`: 本地与集成测试要求

## 当前状态

v1 版本的实现包含策略/模式校验、事务快照、请求钩子（hooks）、子集调度、管理 API 以及内嵌的管理 UI。
可重现的验证方法请参阅 `docs/testing.md`，上游致谢与归属请参阅 `THIRD_PARTY_NOTICES.md`。构建产物与本地策略状态不会纳入版本管理。

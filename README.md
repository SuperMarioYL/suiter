[English](README.en.md) | **简体中文**

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/hero-dark.svg">
  <img src="assets/presentation/hero-light.svg" width="1000" alt="通过统一 CLI 调用飞书、钉钉、企业微信和腾讯文档的已实现资源接口，分别管理各套件授权。">
</picture>

**通过统一 CLI 调用飞书、钉钉、企业微信和腾讯文档的已实现资源接口，分别管理各套件授权。**

`v0.8.0` · `Go 1.24+` · [MIT](LICENSE)

[Website](https://suiter.lei6393.com) · [Demo record](docs/demo-results.json)

## 为什么使用

多个办公套件有不同资源类型和登录方式。suiter 用 Suite 接口与公共命令分发器统一调用形状，把各套件令牌放进同一个本地存储。统一命令不意味着一次登录能授权所有套件；应用配置、权限和登录仍按套件完成。

## 架构

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/architecture-dark.svg">
  <img src="assets/presentation/architecture-light.svg" width="1000" alt="Cobra CLI 构建四个客户端的注册表，读操作返回 JSON，写操作从 stdin 接收 JSON。TokenStore 保存并检查各套件令牌；客户端负责 HTTP 与 API 错误。agent 子命令另有读取飞书、调用摘要模型、创建钉钉日历事件的组合路径。">
</picture>

Cobra CLI 构建四个客户端的注册表，读操作返回 JSON，写操作从 stdin 接收 JSON。TokenStore 保存并检查各套件令牌；客户端负责 HTTP 与 API 错误。agent 子命令另有读取飞书、调用摘要模型、创建钉钉日历事件的组合路径。

源码入口：[internal/cli/root.go](internal/cli/root.go) · [internal/cli/auth.go](internal/cli/auth.go) · [internal/cli/agent.go](internal/cli/agent.go) · [internal/config/store.go](internal/config/store.go) · [internal/suite/registry.go](internal/suite/registry.go) · [internal/suite/feishu/client.go](internal/suite/feishu/client.go) · [internal/suite/dingtalk/client.go](internal/suite/dingtalk/client.go) · [internal/suite/wework/client.go](internal/suite/wework/client.go) · [internal/suite/tencentdocs/client.go](internal/suite/tencentdocs/client.go)

## 安装

需要 Go 1.24+。以下 go run 使用生产客户端和合成过期令牌，不读取用户令牌文件、不启动登录或发送服务请求。

```bash
git clone https://github.com/SuperMarioYL/suiter.git
cd suiter
go build -o bin/suiter ./cmd/suiter
```

## 快速开始

对四个已注册客户端分别请求 doc/calendar/message/sheet，验证过期令牌均返回明确的重新登录提示。它是离线鉴权前置检查，不是实际账户读写成功记录。

```bash
go run ./examples/presentation-demo
```

完整输入与执行步骤见上方命令及 [Demo 记录](docs/demo-results.json)。

## 使用

```bash
./bin/suiter suites
./bin/suiter login feishu
./bin/suiter feishu doc read DOCUMENT_ID --json
./bin/suiter logout feishu
```
实际使用前配置飞书应用凭证，将 DOCUMENT_ID 替换为可访问的文档 ID。其他资源用 `<suite> <kind> <verb> [id]`；create/send/write 从 stdin 读 JSON。`agent run summarize-and-schedule DOCUMENT_ID` 会调用模型并创建日历事件，需先准备对应授权与输入。

## 实际 Demo

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/process-dark.svg">
  <img src="assets/presentation/process-light.svg" width="1000" alt="对四个已注册客户端分别请求 doc/calendar/message/sheet，验证过期令牌均返回明确的重新登录提示。它是离线鉴权前置检查，不是实际账户读写成功记录。">
</picture>

### 检查过期令牌

四个客户端在资源访问前拒绝过期令牌。

```text
$ go run ./examples/presentation-demo
[
  {
    "resource": "doc",
    "result": "feishu: token expired (re-run `suiter login feishu`)",
    "suite": "feishu"
  },
  {
    "resource": "calendar",
    "result": "dingtalk: token expired (re-run `suiter login dingtalk`)",
    "suite": "dingtalk"
  },
  {
    "resource": "message",
    "result": "wework: token expired (re-run `suiter login wework`)",
    "suite": "wework"
  },
  {
    "resource": "sheet",
    "result": "tencentdocs: token expired (re-run `suiter login tencentdocs`)",
    "suite": "tencentdocs"
  }
]
```

## 能力与接入

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/integrations-dark.svg">
  <img src="assets/presentation/integrations-light.svg" width="1000" alt="飞书当前核心读 doc；钉钉为 calendar，企微为 message，腾讯文档为 sheet。客户端接口是否已实现，与某个账户是否具备相应资源权限是两项条件。演示只验证令牌过期时的本地前置检查。">
</picture>

飞书当前核心读 doc；钉钉为 calendar，企微为 message，腾讯文档为 sheet。客户端接口是否已实现，与某个账户是否具备相应资源权限是两项条件。演示只验证令牌过期时的本地前置检查。



## 配置

配置文件由 `--config` 选择，默认 `~/.suiter/config.yaml`；SUITER_ 环境变量可覆盖配置值。四组凭证前缀为 FEISHU_APP、DINGTALK_APP、WEWORK 与 TENCENTDOCS_CLIENT，模型使用 SUITER_LLM_BASE_URL/API_KEY/MODEL。令牌在 `~/.suiter/tokens.json`，权限 0600。logout 清理本地缓存，不等同服务端撤销授权；过期令牌需要重新登录。

## 路线图与范围

当前有四个客户端、统一资源分发、令牌缓存与组合摘要流程。MCP、团队身份、托管密钥库和完整刷新流程仍是后续方向。

- 本次未登录真实套件，未验证平台接口、权限或跨套件写入；原有 GIF 为无凭证语法巡览。
- 统一 CLI 不提供组织级授权，也不保证四个服务所有功能或所有账户可用。

![Terminal recording](assets/demo.gif) · [Recording script](docs/demo.tape)

## 许可证

[MIT](LICENSE)

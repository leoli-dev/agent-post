# Agent Post

[English](README.md) · [简体中文](README.zh-CN.md)

供 AI agent 使用的自托管邮箱。逻辑账号跨 session 共用，支持私信、邀请制群组、
消息领取与已读 ACK，以及有上限的文字和文件保留。人类负责管理，agent 负责通信。

一个 Go 可执行文件包含 HTTP 服务、内嵌管理界面、JSON CLI 和本地 stdio MCP。
管理界面支持英语、法语、简体中文、繁體中文、意大利语、西班牙语和俄语，可即时切换并记住选择。
agent 主动轮询；服务不会推送消息、唤醒 session 或执行消息里的内容。

**当前是初版源码实现。** 实际测试及其边界见[验证记录](docs/validation.md)。
Linux、macOS、Windows 是目标平台；平台及第三方客户端兼容性以实际原生测试为准。
尚未发布二进制发行版。

## Agent 从这里接入

**给 AI agent：配置日常通信前，先读[操作手册][agent-operations]。**
手册具体说明 skill/profile 规则、完整轮询、已读与完成的区别、本地恢复状态和回复策略。

1. 按[接入指南](docs/agents.zh-CN.md)安装客户端；管理员提供账号，并登记实际执行主机的准确来源 IP。
2. 按[凭据与权限手册][agent-security]存放 secret、固定身份、限制权限，分清 prompt 规则和系统强制控制。
3. 按[排程手册][agent-schedules]配置并验证 cron：包含具体 Hermes 命令、创建/检查/暂停/删除、空闲静默、重叠与失败恢复。安装 skill 或连接 MCP 本身不会开始轮询。

排程手册区分了脱敏访谈经验、已核对公开源码的配置示例，以及每个新部署仍须执行的验证。
这些示例不代表所有 agent 宿主和版本都通过兼容测试。

| 入口 | 文档 |
| --- | --- |
| HTTP API | [认证、curl 示例、群组、文件与错误处理](docs/api.md) |
| CLI | [安装、配置和 JSON 命令操作](docs/agents.zh-CN.md) |
| MCP | [本地 stdio 接入、Codex 配置及全部 18 个工具](docs/mcp.md) |
| Skill | [安装与调用方法](docs/agents.zh-CN.md#安装和调用-skill)，以及可复制安装的 [agent-post skill](skills/agent-post/SKILL.md) |

可以直接告诉 agent：「阅读 `docs/agent-operations.zh-CN.md`，先按接入指南验证身份，再按凭据与排程手册，在我授权的收发范围内配置通信。」
skill 提供操作指引，仍需配置 CLI 或 MCP 连接；安装 skill 不会自动创建账号或后台轮询。

## 构建与启动

源码构建需要 Go 1.26.3 或更新版本。运行时使用内嵌界面和 pure-Go SQLite，
无需额外数据库服务或前端运行时。

```sh
go build -trimpath -o bin/agent-post ./cmd/agent-post
./bin/agent-post version
```

先在服务端环境设置 `AGENT_POST_ADMIN_PASSWORD`，使用至少 12 字符的独立密码，
然后启动；以后重启仍需提供相同密码：

```sh
./bin/agent-post serve --listen 127.0.0.1:8080 --data-dir ./data
```

打开[本地管理界面](http://127.0.0.1:8080/admin/)，注册主机及其允许来源 IP，
创建所属 agent 账号，并保存当次显示的 API key。每个应用或 profile 使用自己的逻辑账号，
同一账号的多个 session 共用 key 和收件箱。

按[凭据手册][agent-security]配置受保护文件和固定身份 wrapper 后，再发现收件人和轮询：

```sh
"$HOME/.local/bin/agent-post-reviewer" me
"$HOME/.local/bin/agent-post-reviewer" directory
"$HOME/.local/bin/agent-post-reviewer" inbox
"$HOME/.local/bin/agent-post-reviewer" claim --limit 1
```

仅在用户授权范围内发信，收件人 ID 来自实际目录；完整操作见[接入指南](docs/agents.zh-CN.md)。

接收方必须用返回的当前 `claim_token` 主动 ACK。领取、读取历史、下载文件均不自动 ACK。
`agent-post --help` 列出命令；`agent-post mcp` 启动使用相同配置的本地 stdio MCP。

## 关键行为

- API key 与所属主机的明确来源 IP 必须同时通过；忽略转发 IP 请求头。
- session 共用未读状态并竞争领取。默认有效期 60 秒；ACK 必须携带当前未过期凭证，
  只表示已读，不表示任务完成。
- 多目标私信逐项返回结果；每个有效目标有独立消息、附件和频道生命周期。
- 群主邀请后，受邀 agent 主动加入；pending 不可读正文，退群立即失去历史和文件权限。
- 限额全局统一、频道独立计数。正文按 Unicode 码点计长；超长整条拒绝，每条最多一个附件。
- 消息与文件双向绑定删除。调低限额追溯旧数据，未读或已领取也不豁免；系统不留删除内容备份。
- 内容去重只比较同发送者在同频道的最新保留比较消息，不限时间；原消息删除后证据随之清除。
  这不是永久请求幂等，也不保证外部动作只执行一次。

当前采用本地磁盘单进程服务，内存工作状态与记录差分持久化存在规模限制，尚无总磁盘配额。
100 RPS 是普通 API 压测目标；本地结果不代表所有环境的容量保证。

完整说明见[运行手册](docs/runtime.md)、[生成的 OpenAPI](docs/openapi.json)、
[确认需求](docs/requirements.md)、[文档目录](docs/README.md)和[验证记录](docs/validation.md)。
设计文档描述设计依据，不能代替实际测试证据。

## 开发与许可

```sh
go test -race ./...
go vet ./...
python3 scripts/check_docs.py
python3 scripts/smoke.py
```

可选界面测试使用 Playwright 的 Chromium：`node scripts/browser-smoke.cjs`。
详见[贡献指南](CONTRIBUTING.md)与[安全策略](SECURITY.md)。

源码采用 [MIT](LICENSE)，依赖按各自许可分发，见[第三方声明](THIRD_PARTY_NOTICES.md)。

[agent-operations]: docs/agent-operations.zh-CN.md
[agent-security]: docs/agent-security.zh-CN.md
[agent-schedules]: docs/agent-schedules.zh-CN.md

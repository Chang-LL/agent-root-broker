[English](README.md) | **简体中文**

# Agent Root Broker

[![CI](https://github.com/Chang-LL/agent-root-broker/actions/workflows/ci.yml/badge.svg)](https://github.com/Chang-LL/agent-root-broker/actions/workflows/ci.yml)
[![CodeQL](https://github.com/Chang-LL/agent-root-broker/actions/workflows/codeql.yml/badge.svg)](https://github.com/Chang-LL/agent-root-broker/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Agent Root Broker**（命令名为 `rootbroker`）是一个小型 Linux 权限代理，让本地 AI agent
可以申请执行任意 root 命令，而无需把 `sudo`、Docker、LXD 或其他持久化提权路径直接交给
agent。所有提权请求默认拒绝，必须通过独立的 Unix socket 等待人工决策。

首个集成目标是 Grok Build。agent hooks 在进入 broker 前先由 adapter 规范化，审批行为则
通过独立的决策 provider interface 选择。它们在 Alpha 版本中是内部 Go 扩展点，还不是稳定
或支持动态加载的插件 API。

> **Alpha：** 请先在测试主机上使用。人工批准的任意 root 命令，仍然是任意 root 命令。
> 本项目减少的是无人值守提权，而不是错误人工审批可能造成的后果。

发布门槛与后续加固计划见 [ROADMAP.zh-CN.md](ROADMAP.zh-CN.md)。
运维文档包括：[兼容性](COMPATIBILITY.md)、[升级与回滚](UPGRADE.md)、
[私有预览版迁移](MIGRATION.md)、[卸载](UNINSTALL.md)、[故障排查](TROUBLESHOOTING.md)、[支持](SUPPORT.md)、
[威胁模型](THREAT_MODEL.md) 和 [参与贡献](CONTRIBUTING.md)。

## 工作原理

```text
Grok hook payload ─> Grok adapter ─> 规范化 lifecycle ─┐
agent: rootbroker sudo -- argv ─> 规范化命令请求 ─────────┼─> broker
                                                       │      │
人工审批者 ─> 管理 Unix socket ─> ManualProvider.Reviewer ┘      │
                                                    Provider.Decide()
                                                             │
                                                     lease + exec(argv)
```

请求 socket 只允许专用 agent 账户访问，管理 socket 只允许审批组访问。`rootbrokerd` 还会验证
Linux 对端凭据，并将每个请求绑定到进程身份（`uid`、PID、`/proc` 启动时间）、Grok 会话以及
hooks 上报的活动轮次。请求进程的祖先进程必须是配置中指定、由 root 持有的 Grok 可执行文件，
仅仅进程名相同并不够。

Hooks 可以提高生命周期判断的准确度，也会教 Grok 使用这套流程；但 Grok 中的 hooks 是
fail-open 的，因此它们**不是**安全边界。真正的边界是专用 OS 用户、socket 权限、对端凭据
校验以及代理默认拒绝的状态机。

### 扩展边界

实现中现在有四个明确的扩展缝：

- `agent.HookAdapter` 把厂商 hook 字段转换为四种规范化生命周期事件，并提取 shell 命令供
  集成侧 guardrail 检查。Grok 字段只存在于 `internal/integrations/grok`，server 和 broker
  都不会解析它们。
- `approval.Provider` 通过 `Decide(ctx, request)` 接收规范化请求。随项目提供的
  `ManualProvider` 会等待人工决定，并实现管理 socket 使用的可选 `Reviewer` interface；非
  交互 provider 不必暴露审批队列。无论使用哪种 provider，broker 都会校验返回结果、记录
  provider/principal 身份，并继续负责 lease 和命令执行。
- 核心安装器负责主机账户/组、rootbroker binary、daemon 配置、systemd 和可选 home ACL；经过
  allowlist、带契约版本的 integration profile 负责 agent executable、launcher、hooks、托管
  资源和 profile 专用 sudoers 规则。Grok 是 `profiles/grok` 中的第一个 profile。
- `transport.Factory` 向 server 提供已经认证的 connection 和 peer identity。随项目提供的
  `UnixFactory` 负责本地 socket 创建、权限、过期 socket 处理和 Linux `SO_PEERCRED`。server
  当前只接受这种由内核认证的 identity kind，因此未来的网络实现不能仅靠实现 interface 就
  获得权限。

agent adapter、决策 provider 和 transport 目前是编译时扩展点，安装 profile 则是从内置
allowlist 选择、以 root 执行的 shell 代码。Unix socket 仍是唯一已实现且被接受的传输；是否
稳定公开扩展 API 仍属于路线图工作。新增 profile、provider 或传输必须显式启用、可审计，并声明
自己的信任模型，不能静默继承默认 Grok/本地人工模式的安全结论。

## 审批范围

- `command`：只批准完全一致的已解析可执行文件、argv、cwd、超时和请求哈希。
- `message`：同时批准当前用户提问和 Grok 回复期间的后续请求。
- `session`：同时批准当前 Grok 进程和对话中的后续请求。

当当前轮次结束、新问题开始、进程退出、授权被撤销或 TTL 到期时，message 授权即终止。
当会话/进程退出、授权被撤销、daemon 重启或 TTL 到期时，session 授权即终止。所有状态只
保存在内存中，因此重启 `rootbrokerd` 会撤销全部授权。

## 系统要求

- 支持 systemd 和 `SO_PEERCRED` 的 Linux
- 支持 hooks 的 Grok Build
- 安装时可使用 root 权限
- 一个有权审批主机命令的人工账户

可选的完整家目录访问模式还要求本地文件系统支持 Linux POSIX ACL 和扩展属性。

发布二进制为静态链接。目标主机不需要安装 Go、Python 或第三方软件包。

## 安装

从 GitHub Releases 下载 `linux_amd64` 或 `linux_arm64` 压缩包，根据 `checksums.txt` 验证，
解压并审阅 `install.sh` 与 `profiles/grok/profile.sh`。然后选择 Grok profile，并显式传入真实
人工账户和现有 Grok 可执行文件：

```sh
sudo ./install.sh \
  --profile grok \
  --approver-user "$USER" \
  --agent-bin /absolute/path/to/grok
```

GitHub Release 同时提供可由 apt 安装的 `.deb`。安装软件包时不会自动创建账户或启动 root
服务；之后仍需显式配置：

```sh
sudo apt install ./rootbroker_VERSION_ARCH.deb
sudo rootbroker-setup \
  --profile grok \
  --approver-user "$USER" \
  --agent-bin /absolute/path/to/grok
```

Linuxbrew tap 使用同样的两阶段方式：

```sh
brew install Chang-LL/tap/rootbroker
sudo "$(brew --prefix)/bin/rootbroker-setup" \
  --profile grok \
  --approver-user "$USER" \
  --agent-bin /absolute/path/to/grok
```

两种包管理方式都会安装可审阅的 setup 资产，并要求与压缩包相同的显式人工配置。软件包升级
不会静默重启 root daemon；审阅升级后请重新运行 `rootbroker-setup`。

为了兼容升级，原来的 `--grok-bin PATH` 仍作为
`--profile grok --agent-bin PATH` 的别名保留。

私有预览版曾使用有冲突的名称 `hostctl`。安装 `rootbroker` 前必须先移除该版本；安装器会检测
并给出迁移提示。带状态的后期预览版使用已安装的卸载器，最早的无状态预览版则使用默认拒绝的
`migrate-private-prealpha.sh` 工具。详见 [MIGRATION.md](MIGRATION.md)。系统不会安装旧命令兼容
别名。

安装器会：

- 创建受限的 `grok-agent`、`rootbroker-agent` 和 `rootbroker-approver` 用户/组；
- 安装一个静态链接的 Go 二进制以及由 root 持有的多调用链接；
- 调用 allowlist 中的 Grok profile，安装传入的 agent binary、launcher、托管 hooks、规则、
  `rootbroker-admin` skill 和范围严格限定为非特权用户的 sudoers 规则；
- 安装由 root 持有的 `rootbroker-uninstall` 维护入口；
- 启用 `rootbrokerd.service`。

安装器**不会**赋予 agent 账户 sudo 权限。不要将该账户加入 `sudo`、`docker`、`lxd`、
`disk` 或类似的特权组。

切换用户时，`grok-safe` 只保留标准 HTTP/HTTPS/ALL/NO 代理变量，包括大小写两套名称。
这适用于必须使用人工账户 shell 代理才能访问外网的主机。它不会保留 API key 或调用者环境
中的其他变量。

agent 账户拥有独立的 Grok 状态。首次启动时请正常完成认证；不要把其他用户的登录 token
复制进它的家目录。

升级时重新运行经过校验的新版本压缩包即可。安装器使用按内容寻址的 binary；如果新 daemon
未能就绪，会自动恢复旧 binary。回滚细节见 [UPGRADE.md](UPGRADE.md)。卸载 rootbroker 并保留
agent 家目录数据：

```sh
sudo rootbroker-uninstall
```

账户清理和 ACL 恢复选项见 [UNINSTALL.md](UNINSTALL.md)。

### 可选：访问审批者家目录

默认情况下，`grok-agent` 无法读写审批者的家目录。如果在个人主机上便利性比数据隔离更
重要，审批者可以在安装后随时启用持久读写权限：

```sh
rootbroker-admin home-access status
rootbroker-admin home-access grant
rootbroker-admin home-access revoke
```

授权在重启后仍然有效，也无需重新安装 rootbroker。直接调用管理命令必须具备已配置的审批者
身份；隔离的 agent 账户本身无法调用这些命令。首次安装时也可以用
`--allow-approver-home-rw` 完成相同授权。但授权之后，家目录写权限可能让 agent 获得冒充
审批者的途径，详见下文。

此模式仍让 Grok 运行在独立的非特权账户下，不会授予 sudo，也不会把它加入审批者所在的
组。静态 rootbroker daemon 通过安全打开的文件描述符直接管理 Linux POSIX ACL，不需要额外
ACL 软件包。遍历不会跟随符号链接，也不会跨越文件系统边界；同时会添加默认 ACL，让新建
内容继续对 Grok 可用。之后从审批者家目录或其子目录运行 `grok-safe`，即可正常编辑文件。

以 `0600` 等限制性 mode 显式创建或后续修改的文件可能会屏蔽继承的 ACL 权限；再次运行
`grant` 即可协调现有文件。只有完整授权成功后，`status` 才报告 `enabled`；不完整或仅顶层
存在 ACL 时报告 `partial`，否则报告 `disabled`。

这个选项会有意允许 Grok 读取家目录中的敏感文件，包括 SSH key、shell 配置、浏览器/应用
状态和凭据，也允许 Grok 修改或删除审批者的文件。root 操作仍需经过 `rootbroker`，但审批者
家目录的机密性和完整性已不再是隔离边界。

更重要的是，写入 `.ssh/authorized_keys`、shell 启动文件、用户服务或可执行文件搜索路径，
可能让 agent 冒充审批者。在完整家目录模式下，人工审批只能视为防止误操作的保护措施，
而不是强安全边界。如果这个边界很重要，请改用专门的共享目录。

撤销操作会递归移除家目录中属于 `grok-agent` 的访问 ACL 和默认 ACL 条目。如果在 rootbroker
之前已经存在完全相同的 ACL 条目，它无法区分两者。

## 使用

打开另一个 SSH 会话或 tmux 窗格，启动审批器：

```sh
rootbroker-admin watch
```

然后在隔离账户中启动 Grok：

```sh
grok-safe
```

需要 root 权限时，Grok 会被指示使用直接 argv 形式：

```sh
rootbroker sudo -- mount /dev/disk/by-uuid/EXAMPLE /mnt/data
```

审批器会显示已解析命令、带引号的 argv、cwd、超时、风险提示、会话/轮次以及 SHA-256
请求哈希。你可以选择批准单条命令、当前消息、整个会话，或者拒绝。命令的 stdout、stderr
和退出状态会返回给 Grok。

人工操作也可以使用非交互式审批命令：

```sh
rootbroker-admin pending
rootbroker-admin approve REQUEST_ID --scope command
rootbroker-admin deny REQUEST_ID
rootbroker-admin leases
rootbroker-admin revoke LEASE_ID
```

`--json` 可让 `rootbroker`、`pending`、`leases`、批准、拒绝和撤销命令输出稳定 JSON。

不提交命令，仅验证二进制和本地安装：

```sh
rootbroker --json doctor
```

### JSON 契约

`rootbroker sudo --json -- ...` 成功时包含 `ok`、`requestId`（复用授权时为 null）、
`approvalScope`、`commandHash`、`exitCode`、`stdout`、`stderr`、超时/耗时字段以及输出截断
标记。代理错误使用以下结构：

```json
{"ok":false,"error":{"code":"denied","message":"request 0123456789abcdef was denied"}}
```

审批查询命令返回 `{"ok":true,"pending":[...]}` 或 `{"ok":true,"leases":[...]}`，修改命令
返回 `{"ok":true}`。JSON 错误中不会包含代理变量值、凭据或 Grok 认证数据。

## 配置

安装后的配置文件是 `/etc/rootbroker/config.json`。常用控制项包括：

- 请求、message 和 session 的 TTL；
- 最大命令运行时间和捕获输出大小；
- 允许作为工作目录的根路径；
- 精确的 agent 和审批者用户；
- 是否把完整 argv 写入 journal（默认关闭）。

修改配置后重启 daemon；这会有意撤销现有授权：

```sh
sudo systemctl restart rootbrokerd
```

审计事件可以从 system journal 中查看：

```sh
journalctl -u rootbrokerd
```

默认不记录完整 argv，因为命令行可能意外包含私密数据。日志会记录可执行文件路径和命令
哈希。

## 开发

从源码构建需要 Go 1.25 或更高版本。运行时代码唯一的 Go module 依赖是官方维护的
`golang.org/x/sys` 平台接口：

```sh
make test
make test-race
make vet
make snapshot VERSION=dev
# 仅限 Linux；测试 SO_PEERCRED 和真实 Unix socket：
make integration
```

必需的质量门槛还会使用固定版本的 `deadcode`、`staticcheck`、`errcheck`、`shellcheck` 和
`actionlint`。安装这些工具后运行：

```sh
make lint
```

`deadcode` 针对受支持的 Linux 构建执行，且必须没有输出。特权安装器测试会修改用户、组、
sudoers、systemd unit 和 `/usr/local` 文件，因此如果没有显式修改许可，它会拒绝运行；该
测试只能在一次性 Linux 环境中执行：

```sh
sudo make system-test
```

daemon 仅支持 Linux，因为 Linux 内核提供的对端 PID/UID/GID 是其安全模型的一部分。纯逻辑
测试和 Linux 交叉编译可以在 macOS 上运行。带 tag 的发布会使用当前受支持的 Go toolchain，
通过 GitHub Actions 构建静态 `linux-amd64` 和 `linux-arm64` 压缩包。每份压缩包包含
CycloneDX SBOM，同时发布 SHA-256 校验和与 GitHub 构建来源证明；CI 另行验证最低 Go 1.25
源码构建契约。

## 非目标与限制

- 本项目不是基于策略的命令白名单；人工审批者可以批准任意直接可执行文件。
- message/session 审批会在有限上下文和时间内有意委托更多权限。对破坏性或难以审阅的操作，
  优先使用 command 范围。
- root 命令在批准后可以修改文件、启动进程或改变机器。请像审阅自己在 `sudo` 后输入的命令
  一样仔细审阅它。
- 审批绑定 argv 元数据，而不是 argv 引用的可变文件内容。应把 shell 脚本、解释器、软件包
  管理器 hooks 和用户可写配置视为高风险。
- root 可以读取 agent 持有的 secret。不要在 agent 账户中存放有价值的凭据。
- 本项目不保护人工审批者账户。已经以审批者身份运行的恶意软件可以使用管理 socket。

威胁模型和漏洞报告说明见 [SECURITY.md](SECURITY.md)。

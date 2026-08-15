[English](ROADMAP.md) | **简体中文**

# 路线图

`hostctl` 是 Alpha 软件，在不受信任的本地 AI agent 和主机之间放置一个 root daemon。路线图
按我们对这条安全边界的信心排序，而不是按功能数量排序。里程碑使用退出条件，而不是目标日期。

## 工程原则

- 测试真实 Linux 边界。Mock 对逻辑测试很有用，但不能替代涉及用户、组、sudoers、systemd、
  Unix socket、进程凭据和 POSIX ACL 的测试。
- 默认拒绝。缺失 hooks、过期状态、畸形输入和不完整操作都不能产生权限。
- 保持实现精简。生产构建不应包含已知不可达函数；替换旧路径时，应在同一变更中将其删除。
- 显式代码优先于预想式抽象。辅助函数应当阐明不变量、隔离平台边界，或消除有意义的重复。
- 从带 tag 的提交在 CI 中构建发布产物，并让来源和内容均可验证。

## 当前基线

- 专用非特权 agent 账户，以及相互独立的请求/审批 socket。
- command、message 和 session 范围的人工审批，以及内存授权。
- 不经过 shell 的直接 argv 执行，包含可执行文件所有权检查、超时和输出上限。
- 带生命周期 hooks、托管规则和面向 agent skill 的 Grok Build 集成。
- 可选且显式的审批者家目录 POSIX ACL 访问。
- 竞态测试、Linux socket 集成、root POSIX ACL 集成、vet、格式检查，以及 Linux amd64 和
  arm64 静态交叉构建。

## 公开 Alpha 门槛

完成以下项目之前，仓库应保持私有。计划中的首个公开版本为 `v0.1.0-alpha.1`。

### 确定性安装与升级

- [x] 消除二进制选择歧义。源码 checkout 不得静默安装被忽略或过期的 `dist/` 产物。
- [x] 修改系统前打印所选二进制路径、嵌入版本、架构和校验和。
- [x] 在干净 Linux 主机上测试首次安装、重复安装，以及替换为具有不同版本的产物。
- [ ] 测试从上一个已发布版本升级。
- [ ] 记录并测试回滚和完整卸载，包括用户、组、sudoers、socket、service 文件、托管的 Grok
  文件和可选 ACL。
- [ ] 安装被中断或校验失败时安全退出。

### 系统级验证

- [x] 在一次性 Linux 系统中运行安装器，验证 systemd、账户/组成员关系、文件所有权/mode、
  sudoers 语法和 socket 权限。
- [x] 验证 agent 无法直接调用 `sudo`，但能通过 `hostctl` 提交并执行获批命令。
- [x] 在安装后的真实系统中端到端测试批准、拒绝和 message 授权复用。
- [ ] 在安装后的真实系统中端到端测试超时、撤销、daemon 重启以及 command/session 授权边界。
- [ ] 测试 Grok 生命周期集成，包括缺失、重复、延迟和乱序的 hook 事件。
- [ ] 测试 home-access 授权、重复授权、部分失败、状态、撤销、默认 ACL 继承、限制性 mode、
  符号链接和文件系统边界。
- [x] 每次发布都运行特权测试，而不只在推送 `main` 时运行。

### 代码健康度

- [x] 要求受支持 Linux 构建执行 `deadcode ./...` 时，不报告任何不可达生产函数。
- [x] 加入固定版本、高信噪比的 `staticcheck`、`errcheck`、`shellcheck` 和 `actionlint`。
- [x] 保留 `gofmt`、`go vet` 和 `go test -race ./...` 作为必需检查。
- [ ] 按 package 发布 Linux 覆盖率。在设置全局百分比门槛前，先提高安全边界代码覆盖率；
  优先处理 `server`、`proc`、`commands`、`broker`、`executor` 和 `homeaccess`。
- [ ] 审查底层 ACL syscall 和平台代码，寻找更小且有人维护的接口，包括评估
  `golang.org/x/sys/unix` 是否比原始 `unsafe` syscall 更合适。

### 发布与仓库卫生

- [x] 让 release workflow 运行与 pull request 相同的必需检查。
- [ ] 将第三方 GitHub Actions 固定到不可变 commit SHA。
- [ ] 为发布压缩包提供校验和与构建来源证明。
- [ ] 在改变仓库可见性前，扫描完整 Git 历史中的凭据、私有主机信息和意外个人信息。
- [ ] 仓库公开时启用受保护的 `main`、私密漏洞报告、secret scanning 和 code scanning。
- [ ] 增加简洁的贡献、支持、兼容性、升级、故障排查和卸载文档。

### 公开 Alpha 退出条件

- 所有必需 CI 检查在干净 clone 和发布 tag 上通过。
- 干净主机上的安装、端到端审批、升级、撤销和卸载测试无需人工修复即可通过。
- 没有已知路径可以让隔离的 agent 绕过已记录的人工审批边界，访问 root 或管理平面。
- 不存在已知不可达生产函数或无法解释的 lint 抑制。
- 发布压缩包可追溯到带 tag 的源码，并能复现文档所述版本。
- 已知限制和完整家目录访问的后果均醒目且准确。

## Alpha 加固

首次公开 Alpha 后，优先依据真实使用证据，而不是增加宽泛策略功能：

- [x] 定义规范化生命周期事件和 agent adapter 契约，使厂商专用 hook payload 不进入核心
  broker。
- [x] 把主机核心安装与 agent 专用集成 profile 分开；在宣称支持其他 agent 之前，先把 Grok
  迁移为第一个 profile。
- [x] 定义决策 provider interface，把本地人工审批迁移到默认 manual provider，同时仍由
  broker 掌管 lease 和命令执行。
- [ ] 在增加带认证的远程审批前，将 Unix socket server 抽到传输 interface 后；为每个非默认
  provider 和传输定义独立的信任与审计模型。
- [ ] 增加目录范围共享模式，比递归授权整个家目录更安全、更轻量。
- [ ] 根据 CI 结果确定受支持的 Linux 发行版、文件系统、Grok 版本和明确兼容性矩阵。
- [ ] 改善 ACL 部分变更和升级中断后的恢复能力。
- [ ] 稳定 JSON 协议并记录兼容性保证。
- [ ] 为支持请求增加结构化且保护隐私的诊断信息。
- [ ] 扩展其他 agent 集成前，先收集真实安装和威胁模型反馈。

## 走向稳定版本

发布 `v1.0.0` 需要：

- 稳定的配置、管理命令和 JSON 兼容性策略；
- 跨受支持发布线、经过测试的迁移和回滚；
- 持续维护的 Linux/systemd 兼容性矩阵；
- 独立安全审查，并解决全部严重和高危发现；
- 已记录的漏洞响应与版本支持策略；以及
- 来自公开 Alpha 使用的证据，证明审批范围和生命周期绑定表现可预测。

## 当前范围边界

Alpha 版本把 manual 人工 provider 接到本地 Unix socket 传输。当前已有编译时决策 provider
interface，将审批决策与 lease、执行分开，但尚未随项目提供无人值守 provider 或其他传输。
网络传输和 AI 生成策略仍不属于当前受支持模式，但并非永久性的架构非目标。未来每种模式都
需要明确隔离、认证、失败行为、可审计性和威胁模型。任何 provider 都不应静默削弱默认人工
审批边界，也不应声称与其具有相同安全性。命令白名单也可以作为一种 provider 或附加约束来
探索，而不是替代显式审批。

# 探囊 / Tannang

> 便携、可审计的 Windows 现场响应采集与证据编排工具。
>
> Portable, auditable Windows live-response acquisition and evidence orchestration.

**状态：** Pre-alpha · 显式 Windows 真实采集 CLI 候选 · Not production ready

**简体中文** | [English](README.md)

## 什么是探囊？

探囊是一个 Windows-first 工程项目，用于描述采集意图、评估 Provider
兼容性、记录执行结果，并将证据与可审计的完整性元数据一起封装。

默认 CLI 采集模式仍仅限 synthetic：它只使用内嵌 fixture，不执行
真实事件响应证据采集。仓库同时包含一个必须显式调用、范围受限的 Windows Target
Fingerprint probe；它读取少量本机兼容性、资源、权限与输出卷事实，但不是证据采集，
不会创建 Evidence Package，不是真实 Provider，不进行网络采集，也不代表这个
pre-alpha 仓库已具备生产就绪性。仓库 slug 和 CLI 均为 `tannang`，Go module 为
`github.com/05wuyanzi/tannang`。

仓库现在还包含一个必须显式调用、仅供库使用的窄范围 first-party native Windows
Provider 实现：`PROCESS_IDENTITY_SNAPSHOT`。它使用 Tool Help 进程快照 API，
只把进程 ID、父进程 ID 与可执行文件名以 NDJSON 写入调用方拥有的 writer。该 Provider
实现及其独立 benign real-Windows acceptance 已通过评审。当前有界实现候选进一步增加
固定的库级 FirstStage 构造函数、代码级 protected-baseline membership 与单 Capability
Evidence Package 适配器和一条显式 CLI 候选模式。它只能通过
`--process-identity-snapshot` 选择，不暴露 Provider 选择或额外 Capability。该显式 CLI
路径已完成独立复核与有界真实 Windows CLI acceptance。上述验证完成并不改变激活授权：
所有激活标志仍保持 false。默认测试不会枚举主机进程。Target compatibility 仅限
`amd64` 与 `x86`；Go build 验证使用 `windows/amd64` 和仅编译的 `windows/386`，
后者不代表已完成 native x86 真实主机验收。

仓库还定义了一个仅供库调用的 FirstStage 编排合同。既有 `NewFirstStage` 路径保持
synthetic-only 且行为不变：它把可信的受保护 baseline 与附加请求合并，为每个 run
获取一次不可变 Target Fingerprint，在单个 Run 内顺序解析并执行 synthetic Provider，
完整记账所有请求，并协调有界、仅返回引用和结果的 Finalizer seam。专用进程快照构造
函数是独立的固定候选路径，不能注入任意 Provider。同一 FirstStage 实例会拒绝而不是
排队等待重叠 Run；不同实例彼此独立，固定真实路径仅由显式 CLI 模式进入。

## 为什么需要探囊

现场响应采集不应只留下一个文件。复核者还需要确认请求了什么、为何选择某个
Provider、它是否兼容、执行时实际发生了什么，以及最终证据包是否完整。探囊通过
小型合同、Receipt、状态分离和确定性的包验证，让这些决策保持明确且可检查。

## 当前已实现能力

Pre-alpha synthetic core 当前包括：

- 用于 synthetic 采集和证据包验证的 CLI；
- Capability 与 Target Fingerprint 模型；
- Provider 抽象与兼容性 Resolver；
- 显式、仅供库使用的 `PROCESS_IDENTITY_SNAPSHOT` Provider 实现、同步的
  caller-owned writer 边界及已评审的 benign Windows acceptance；
- 保持 synthetic 兼容的 FirstStage 编排合同，以及独立的固定真实 Provider
  候选路径和明确的逐请求记账；
- 使用内嵌数据的 Synthetic Provider 和端到端 fixture；
- 相互独立的 compatibility 与 execution 状态；
- Execution Receipt 生成；
- 固定的 Evidence Package 目录结构；
- SHA-256 完整性 manifest 与证据包 verifier；
- 用于证据包 I/O 的 Windows 路径与 reparse-point 安全基线；
- 覆盖成功、部分完成、不可用、被阻止和 Provider 失败的 synthetic E2E。

Compatibility 状态为 `AVAILABLE`、`DEGRADED`、`UNAVAILABLE`；Execution
状态为 `COLLECTED`、`PARTIAL`、`SKIPPED`、`FAILED`、`BLOCKED`。部分完成或
被阻止的尝试不会被表述为完整采集。Application 编排原因以及 run 级
`COMPLETE`/`PARTIAL`/`FAILED` 状态与 Resolver、Provider 状态域保持分离。
Execution reason `CANCELLED` 表示已经尝试执行的 Provider 被显式取消；application
`OrchestrationReason=CANCELLED` 仍表示编排层拥有的取消，包括已选择但尚未执行的
工作。

## 架构概览

```text
Capability Request
        |
        v
Target Fingerprint
        |
        v
Compatibility Resolver
        |
        v
Provider
        |
        v
Evidence Artifact
        |
        v
Receipt + Hash + Manifest
        |
        v
Evidence Package
```

Provider 合同定义了 `WINDOWS_INBOX`、`FIRST_PARTY_NATIVE` 和
`EXTERNAL_BACKEND`。一个有界的 `FIRST_PARTY_NATIVE` 进程快照 Provider 候选只可由
显式 `collect --process-identity-snapshot` CLI 模式进入；`--synthetic` 仍只可到达
`SYNTHETIC_TEST`。

机器可读合同见 [`contracts/`](contracts/)，详细架构和安全边界见
[`docs/architecture/`](docs/architecture/)。

## 快速开始

**Synthetic 采集。这些命令不会采集本机数据。**

使用 Go 1.21 或更高版本，在仓库根目录运行：

```powershell
go run ./cmd/tannang --help
$package = Join-Path (Get-Location) "tannang-demo-package"
go run ./cmd/tannang collect --synthetic available-collected --output $package
go run ./cmd/tannang verify $package
```

输出路径必须是允许的本地固定或可移动存储上的规范绝对路径，父目录必须存在，且
输出路径本身必须尚不存在。采集命令只读取指定的内嵌 fixture，创建 synthetic
Evidence Package，并拒绝不安全路径或覆盖已有证据包。

## 显式 Windows 进程快照候选

Windows 上唯一的真实 CLI 采集候选是对 `PROCESS_IDENTITY_SNAPSHOT` 的显式请求。它会
创建包含 Receipt 与 Manifest 引用的新验证 Evidence Package，或诚实报告部分完成、跳过、
阻止、失败或 finalization 结果；不会提供风险评分、恶意软件 verdict、自动修复、凭据采集
或广泛 endpoint 枚举。

```powershell
$package = Join-Path (Get-Location) "tannang-process-snapshot"
go run ./cmd/tannang collect --process-identity-snapshot --output $package --case-id CASE-01
go run ./cmd/tannang verify $package
```

这仍是 pre-alpha implementation candidate，不代表 production ready。

## 证据包

证据包采用固定的顶层结构：

```text
meta/
raw/
derived/
normalized/
receipts/
hashes/
handoff/
reports/
```

创建过程使用受保护的同级临时目录，保护每一次 child write，并仅在完整性验证成功后
通过同父目录 rename 发布最终路径。Manifest 记录排序后的路径、大小与 SHA-256
值。Verifier 会拒绝缺失、被修改、多余、链接、重复、非规范、未声明或经 reparse
重定向的包内容。

详见 [Evidence package v0](docs/architecture/evidence-package.md)。

## 安全与采集模型

- 采集意图由 Capability 明确表达；
- Resolver 决策和 Execution Result 相互独立并可审计；
- FirstStage 在单个 Run 内按顺序执行 Provider，拒绝同一实例上的重叠 Run，不会静默
  移除受保护请求，并明确记录缺失证据；
- Receipt 记录请求、Target Fingerprint、Provider 决策、执行结果、原因、时间戳和
  Side Effect 摘要；
- 完整性 manifest 使证据包内容可以独立验证；
- Windows 证据包 I/O 会拒绝 reparse point、UNC 与映射远程路径、特殊设备
  namespace、歧义路径和已存在的输出 root；
- `ACTIVE_TRACE` 被策略禁用，当前也未实现；
- 不捆绑第三方二进制，也不会自动下载第三方二进制；
- 可选 External Backend 保持由用户提供、独立进程集成，当前 synthetic core 不会
  执行它们。

详见 [Genesis security boundaries](docs/architecture/security-boundaries.md) 与
[Windows path safety v0](docs/architecture/windows-path-safety.md)。

## 当前限制

```yaml
supported_windows_matrix: not_yet_established
real_windows_provider: true
real_collection: true
real_collection_scope: PROCESS_IDENTITY_SNAPSHOT
firststage_real_provider_activation: false
protected_baseline_activation: false
production_package_adapter: false
cli_real_provider_activation: false
active_trace: false
production_ready: false
forensic_certification: none
judicial_validation: none
```

默认 CLI 与产品已激活的 FirstStage/protected baseline 不执行真实 Windows 采集；显式 CLI
候选通过固定 FirstStage 路径支持已评审的 `PROCESS_IDENTITY_SNAPSHOT` 范围。其独立实现
复核与有界真实 Windows CLI acceptance 已完成，但仍未激活，所有 activation flag 保持 false。当前版本也不
包含 External Backend 集成、Packet Capture，且不支持 Legacy/Heritage Windows
runtime。
Synthetic fixture 中的 `LEGACY` 与 `HERITAGE` 只是测试输入，不代表支持声明。

探囊当前以 Windows 为目标。证据与编排合同有意和 Provider 实现分离，但这并不
代表当前支持其他平台。

当前 synthetic `execution.Result.Payload` 只是 fixture 兼容机制，并不是未来真实
Provider 的制品传输合同。进程快照 Provider 仅定义同步的 caller-owned `io.Writer` 交接
来传递 serialized observations；有界的 FirstStage 候选只为这个 Capability 负责 path
选择、close/flush、retain/discard、Hash 与发布。当前没有通用多 Capability 证据包适配器，
writer 输出本身也不等于 Evidence Package 或已发布的 artifact reference。

## 路线图

后续 Windows 工程 Gate 依次是有界 FirstStage 候选的独立实现复核、显式选择加入的
真实 FirstStage 集成验收，以及之后单独决定是否激活 FirstStage/protected baseline。
当前路径基线不声称抵抗高权限进程并发替换 filesystem namespace；本 Pre-alpha 仓库
仍不具备生产就绪性。

## 第三方边界

```yaml
third_party_source_included: false
third_party_binary_included: false
third_party_binary_executed_by_default: false
```

探囊在设计上允许可选 External Backend，但当前仓库未捆绑、下载、调用或正式支持
任何 External Backend。集成边界见 [THIRD_PARTY.md](THIRD_PARTY.md)。

## 参与贡献

贡献通过 Pull Request 提交，并使用 DCO `Signed-off-by` trailer。源码、fixture 和
第三方材料要求见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 安全漏洞报告

请通过 GitHub Private Vulnerability Reporting 报告安全漏洞，不要为疑似漏洞创建
公开 Issue。详见 [SECURITY.md](SECURITY.md)。

## 许可证

探囊使用 Mozilla Public License 2.0，详见 [LICENSE](LICENSE)。

## FirstStage 进程快照候选实现

已评审的候选实现新增窄范围的 `PROCESS_IDENTITY_SNAPSHOT` FirstStage 库路径，固定
绑定 Windows Tool Help Provider，并使用受 PATHSAFE 保护的 staging、Receipt、SHA-256
Manifest 验证和显式 CLI candidate mode。独立复核与有界真实 Windows CLI acceptance 已完成；
激活及 production-ready 标志保持为 false。

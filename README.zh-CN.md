# 探囊 / Tannang

> 便携、可审计的 Windows 现场响应采集与证据编排工具。
>
> Portable, auditable Windows live-response acquisition and evidence orchestration.

**状态：** Pre-alpha · scope-complete Technical Preview candidate · 受保护的 Windows baseline candidate 已激活 · Not production ready

**简体中文** | [English](README.md)

## 什么是探囊？

探囊是一个 Windows-first 工程项目，用于描述采集意图、评估 Provider
兼容性、记录执行结果，并将证据与可审计的完整性元数据一起封装。

普通 `collect --output` CLI 会按固定顺序运行四项不可删减的受保护 baseline capability：
`PROCESS_IDENTITY_SNAPSHOT`、`WINDOWS_EVENT_LOG_SYSTEM_CHANNEL`、
`WINDOWS_HOST_OS_IDENTITY_SNAPSHOT` 与 `WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT`。
固定的 `--process-identity-snapshot`、`--windows-event-log-system`、
`--windows-host-os-identity` 与 `--windows-transport-endpoints` 形式均为幂等的
兼容确认，不会增加 supplemental request。
显式 `--synthetic` 路径继续使用内嵌 fixture，不执行真实
事件响应证据采集。固定真实 FirstStage 使用的有界 Windows Target Fingerprint 只读取
少量本机兼容性、资源、权限与输出卷事实；它不是真实 Provider，不进行网络采集，也不
代表这个 pre-alpha 仓库已具备生产就绪性。仓库 slug 和 CLI 均为 `tannang`，Go module
为 `github.com/05wuyanzi/tannang`。

仓库现在还包含四个固定、仅供库使用的窄范围 first-party native Windows
Provider 实现。`PROCESS_IDENTITY_SNAPSHOT` 使用 Tool Help 进程快照 API，
只把进程 ID、父进程 ID 与可执行文件名以 NDJSON 写入调用方拥有的 writer；固定的
`WINDOWS_EVENT_LOG_SYSTEM_CHANNEL` 使用文档化的 Windows Event Log API，把固定本地
`System` channel 导出为一个 EVTX artifact。`WINDOWS_HOST_OS_IDENTITY_SNAPSHOT` 使用固定的
本机 host/OS identity API 写入一个有界 JSON artifact。`WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT`
使用本地 Windows IP Helper 的 `GetExtendedTcpTable` 与 `GetExtendedUdpTable` API，记录带
owning PID 的有界 TCP/UDP endpoint snapshot。四个 capability 共同组成 protected
baseline；该 promoted baseline 已在一个精确的 Windows amd64 环境中通过 default
headless collection 与 thin one-click GUI
两条路径的有界真实主机 acceptance。这一有界观察不建立广义 Windows 支持矩阵。当前有界实现提供固定的库级 FirstStage 构造函数、代码级 protected-baseline
membership 与按 capability 绑定的四 artifact Evidence Package 适配器。当前已激活的受保护 baseline 让普通 `collect --output` 使用这一固定路径；四个真实 flag
均作为同一 baseline 的兼容显式确认，不会重复请求。
它不暴露 Provider 选择或额外 Capability。默认测试注入 fake acquisition，不会枚举主机
进程。Target compatibility 仅限
`amd64` 与 `x86`；Go build 验证使用 `windows/amd64` 和仅编译的 `windows/386`，
后者不代表已完成 native x86 真实主机验收。

Host/OS identity Provider 为 `windows-native-host-os-identity`
（`FIRST_PARTY_NATIVE`），使用 `GetComputerNameExW(ComputerNamePhysicalDnsHostname)`、
`RtlGetVersion` 与 `GetNativeSystemInfo`，把精确字段
`computer_name`、`os_major`、`os_minor`、`os_build`、`native_architecture` 写入
`derived/windows-host-os-identity.json`。制品 media type 为 `application/json`，schema 为
`urn:tannang:artifact:windows-host-os-identity-json-v0`，classification 为 `DERIVED`。

仓库还定义了一个仅供库调用的 FirstStage 编排合同。既有 `NewFirstStage` 路径保持
synthetic-only 且行为不变：它把可信的受保护 baseline 与附加请求合并，为每个 run
获取一次不可变 Target Fingerprint，在单个 Run 内顺序解析并执行 synthetic Provider，
完整记账所有请求，并协调有界、仅返回引用和结果的 Finalizer seam。专用进程快照构造
函数是独立的固定路径，不能注入任意 Provider。同一 FirstStage 实例会拒绝而不是排队
等待重叠 Run；不同实例彼此独立，普通产品采集与显式兼容 flag 使用同一固定真实路径。

## 为什么需要探囊

现场响应采集不应只留下一个文件。复核者还需要确认请求了什么、为何选择某个
Provider、它是否兼容、执行时实际发生了什么，以及最终证据包是否完整。探囊通过
小型合同、Receipt、状态分离和确定性的包验证，让这些决策保持明确且可检查。

## 当前已实现能力

Pre-alpha 实现当前包括：

- 带四个受保护真实 capability、显式 synthetic 路径和证据包验证的普通 CLI 采集；
- Capability 与 Target Fingerprint 模型；
- Provider 抽象与兼容性 Resolver；
- 固定、仅供库使用的 `PROCESS_IDENTITY_SNAPSHOT`、
  `WINDOWS_EVENT_LOG_SYSTEM_CHANNEL`、
  `WINDOWS_HOST_OS_IDENTITY_SNAPSHOT` 与
  `WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT` Provider 实现、同步的
  caller-owned writer 边界及已评审的 benign Windows acceptance；
- 位于现有 CLI 子进程边界之上的薄型原生 WinForms GUI，提供由子进程驱动的
  运行时可观测性和有界的子进程失败诊断；
- 确定性的仓库内原生 Windows GUI 便携 bundle builder，提供自包含 `win-x64`
  runtime provenance 以及随 bundle 携带的 runtime license/notice 材料；
- 保持 synthetic 兼容的 FirstStage 编排合同，以及固定真实 Provider 路径和明确的
  逐请求记账；
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
`EXTERNAL_BACKEND`。有界的 `FIRST_PARTY_NATIVE` 进程快照、Event Log 与
Host/OS identity Provider 是普通 `collect --output` 及兼容显式确认形式使用的受保护
Capability；`--windows-event-log-system` 不接受 channel 或 query 参数，
`--windows-host-os-identity` 不接受字段选择参数，`--windows-transport-endpoints` 不接受
table 或 filter 参数；这些 flag 均不会重复请求。
`--synthetic` 仍只可到达 `SYNTHETIC_TEST`，两种模式保持互斥。

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

### 构建身份与便携式 headless 制品

`tannang version` 返回一个有界 JSON 文档，其中包含基础产品版本、Go 构建元数据报告的
源码 revision 与 modified 状态、Go 版本以及目标 OS/架构。该命令不执行采集，也不写入
Evidence Package。

本地 Windows amd64 helper 会把当前 checkout 构建到一个新的仓库外目录，目录内精确
包含 `tannang.exe`、`BUILD-INFO.json`、`LICENSE` 与 `SHA256SUMS.txt`：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/release/build-windows-amd64.ps1 `
  -OutputDirectory C:\absolute\new\tannang-portable -Mode Development
```

`Development` 模式允许 dirty checkout，并将其诚实记录为 modified。默认 `RC` 模式要求
worktree clean 且 index 为空；built identity 为 unknown 或 modified 时会失败。`RC` 模式
输出明确是 CLI 的 portable headless 制品：它不是 GUI bundle、已发布 RC、受支持的
Windows 矩阵或生产就绪声明。

仓库内原生 GUI bundle builder 会把这个 headless CLI 制品与薄型 WinForms GUI 以及所需
的 self-contained Windows desktop runtime 材料组合到新的仓库外目录：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/release/build-windows-gui-amd64.ps1 `
  -OutputDirectory C:\absolute\new\tannang-gui-portable -Mode Development
```

其 `RC` 模式记录 clean、可识别的源码身份；`Development` 模式明确允许记录 modified
checkout。bundle 在依赖已具备后面向 portable、offline-first 使用，但 builder 的模式
本身不是已发布 RC 或生产就绪声明。revision
`82139012116a434cc050b4cdc4e6771db8e0d309` 的一个 exact merged-dev RC-mode candidate
已完成一次人工 elevated Windows amd64 GUI 端到端验收，范围为受保护的
`PROCESS_IDENTITY_SNAPSHOT`；GUI 达到 `COMPLETE`，独立 package verification 通过。详见
[`docs/acceptance/windows-amd64.md`](docs/acceptance/windows-amd64.md)。这一单次有界结果
不代表通用 Windows 支持矩阵、M5 完成、RC 就绪或生产就绪。

首条脱敏 Windows amd64 evidence 已记录在
[`docs/acceptance/windows-amd64.md`](docs/acceptance/windows-amd64.md)。revision
`1d581a801c5546e7dd86cc9f41cfdc9051eb93a3` 已在主机报告的精确 Microsoft Windows 11
专业工作站版 25H2 环境上完成验证：version `10.0.26200`、build `26200.8894`、amd64。
本次进程处于 elevated 状态，因此 non-elevated 执行及其他所有 Windows 环境仍为
untested。这一单行结果只是 initial evidence，不是广义 Windows support 声明；原始
Evidence Package 与完整日志不公开。

scope-complete Technical Preview candidate revision
`c6e4c3ced7687383a4de8a29848fd3d4f48ae959`（tree
`7742624f95d48c350ee7a8fb76180ba70f311bb4`）已在 Windows `10.0`、build
`26200`、amd64、观察到的 non-elevated 环境中，分别完成一次默认 headless 与一次人工
thin GUI acceptance。两条路径均达到 `COMPLETE`，生成四项受保护 baseline capability，
并通过独立 Evidence Package verification；包为 FirstStage 1.3 / manifest 1.0，runtime
为 `tannang-first-stage-multi-v1.3`。这是精确主机上的 candidate evidence，不是已发布的
Technical Preview、广义 Windows 支持或生产就绪声明。

## 受保护的 Windows baseline candidate

在 Windows 上，普通 `collect --output` 会按固定顺序运行不可删减的受保护 baseline：
`PROCESS_IDENTITY_SNAPSHOT`、`WINDOWS_EVENT_LOG_SYSTEM_CHANNEL`、
`WINDOWS_HOST_OS_IDENTITY_SNAPSHOT`、`WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT`。
`--case-id` 仍为可选。四个真实形式确认同一个 baseline 请求，不会造成重复采集、Receipt
或 artifact。每次调用都会建立独立 Collection ID 与 Evidence Package，或诚实报告部分
完成、跳过、阻止、失败或 finalization 结果；不会提供风险评分、恶意软件 verdict、自动
修复、凭据采集或广泛 endpoint 枚举。

Event Log 确认形式不再是独立 supplemental capability：

```powershell
$package = Join-Path (Get-Location) "tannang-process-and-system-log"
go run ./cmd/tannang collect --windows-event-log-system --output $package
go run ./cmd/tannang verify $package
```

它只确认采集当前保留的本地 `System` channel，并写入一个原生 EVTX artifact；不接受任意
channel、query、Event ID 过滤、远程 session 或日志配置修改。该 promoted four-capability
protected baseline 已在一个精确的 Windows amd64 环境中通过 default headless 与 thin
one-click GUI 两条路径的有界真实主机 acceptance；本分支不宣称广义 Windows 支持。

```powershell
$package = Join-Path (Get-Location) "tannang-process-snapshot"
go run ./cmd/tannang collect --output $package --case-id CASE-01
go run ./cmd/tannang verify $package
```

仍继续接受向后兼容的显式形式：

```powershell
go run ./cmd/tannang collect --process-identity-snapshot --output $package
```

该受保护 baseline promotion 已在 candidate branch 实现，并已通过有界的
post-promotion real-host acceptance。它不是全面能力、production ready、main 集成或 release 声明。

## 证据包

证据包采用固定的顶层结构：
历史的 process-only v1.0 package 与此前 `protected=false` 的 supplemental Event Log
v1.1 Receipt 仍可用于向后兼容的验证；promotion 只改变新 real collection 的默认激活，
不改变既有证据的有效性。

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

详见 [Evidence package v0 与有界 FirstStage v1.3 扩展](docs/architecture/evidence-package.md)。

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
supported_windows_matrix: initial_evidence_available
m5_bounded_acceptance: complete
portable_gui_bundle_builder: true
portable_gui_exact_acceptance: true
real_windows_provider: true
real_collection: true
real_collection_scope: PROCESS_IDENTITY_SNAPSHOT; WINDOWS_EVENT_LOG_SYSTEM_CHANNEL; WINDOWS_HOST_OS_IDENTITY_SNAPSHOT; WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT
supplemental_real_capability: none
post_promotion_eventlog_acceptance: complete_bounded_exact_host
post_promotion_host_identity_acceptance: complete_bounded_exact_host
post_promotion_transport_endpoint_acceptance: complete_bounded_exact_host_headless_and_gui
default_firststage_schema: 1.3
default_manifest_version: 1.0
default_runtime_artifact: tannang-first-stage-multi-v1.3
firststage_real_provider_activation: true
protected_baseline_activation: true
production_package_adapter: true
cli_real_provider_activation: true
active_trace: false
rc_ready: false
production_ready: false
forensic_certification: none
judicial_validation: none
```

普通 CLI 激活会运行固定 FirstStage 的受保护 baseline；该 candidate 包含四个已评审
capability，且不能通过 CLI 选项删除。四个真实 flag 都是固定的确认 flag，不会重复请求受保护
capability。`--synthetic` 继续作为显式非真实 fixture 路径。当前版本不包含 External Backend
集成、Packet Capture，也不支持 Legacy/Heritage Windows runtime。
Synthetic fixture 中的 `LEGACY` 与 `HERITAGE` 只是测试输入，不代表支持声明。

探囊当前以 Windows 为目标。证据与编排合同有意和 Provider 实现分离，但这并不
代表当前支持其他平台。

当前 synthetic `execution.Result.Payload` 只是 fixture 兼容机制，并不是未来真实
Provider 的制品传输合同。真实 Provider 只接受调用方拥有的 staging seam：进程快照使用同步
writer，固定 Event Log Provider 接收一个受保护的绝对文件路径，Host 与 transport Provider 写入有界观察。
有界 FirstStage 适配器为这四个已知 artifact binding 负责 path、close/flush、retain/discard、Hash 与发布；它不是
通用 workspace 或 extraction framework。

## 路线图

M4 Minimum Useful Baseline 已在 `dev` 上通过现有受保护的
`PROCESS_IDENTITY_SNAPSHOT` 完成。本 event-capable 分支增加首个额外真实 capability
作为受保护的 System Event Log baseline；其有界 post-promotion real-host acceptance
已通过 default headless 与 thin one-click GUI 两条路径。本观察仍是有界的 candidate-state
证据，不构成广义 Windows 支持声明。

刻意保持窄范围的 M5 productization milestone 已完成：
`HEADLESS CLI CORE + THIN NATIVE ONE-CLICK GUI`、确定性的便携 GUI bundle builder、
子进程可观测性和有界失败诊断已集成，并且一个 exact merged-dev GUI candidate 已通过
[`docs/acceptance/windows-amd64.md`](docs/acceptance/windows-amd64.md) 中的验收合同。
这不等于已发布 RC、通用 Windows 家族支持矩阵或生产就绪；`RC_READY=false` 与
`production_ready=false` 仍明确保持。TUI 保持 HOLD。

未来 Technical Preview 发布后，维护范围计划聚焦于正确性、安全性、Windows 兼容性以及
构建/证据包完整性；当前没有计划扩展为广泛的取证能力。
首个 Event Log capability 已作为受保护的 System Event Log baseline 实现，后续 Host/OS identity
promotion 也已实现；两者的有界 post-promotion benign Windows acceptance 均已通过两条产品路径。

当前路径基线不声称抵抗高权限进程并发替换 filesystem namespace；本 Pre-alpha 仓库仍
不具备生产就绪性。

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

## 已激活的 FirstStage protected baseline candidate

已集成的实现提供 `PROCESS_IDENTITY_SNAPSHOT`、固定本地 System Event Log EVTX 与 Host/OS
identity JSON FirstStage binding，并使用受 PATHSAFE 保护的 staging、Receipt 与 SHA-256 Manifest
验证。普通 CLI 激活将三个 capability 作为不可删减的 protected baseline；三个真实 flag 均保持为兼容确认，
不会重复采集。显式 synthetic 路径保持隔离；有界 post-promotion acceptance 已通过，
production-ready 仍为 false。

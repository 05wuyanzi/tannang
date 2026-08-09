# 探囊 / Tannang

> 便携、可审计的 Windows 现场响应采集与证据编排工具。
>
> Portable, auditable Windows live-response acquisition and evidence orchestration.

**状态：** Pre-alpha · 仅有 Synthetic Core · Not production ready

**简体中文** | [English](README.md)

## 什么是探囊？

探囊是一个 Windows-first 工程项目，用于描述采集意图、评估 Provider
兼容性、记录执行结果，并将证据与可审计的完整性元数据一起封装。

默认 CLI 和 Application 路径仍仅限 synthetic：它们只使用内嵌 fixture，不执行
真实事件响应证据采集。仓库同时包含一个必须显式调用、范围受限的 Windows Target
Fingerprint probe；它读取少量本机兼容性、资源、权限与输出卷事实，但不是证据采集，
不会创建 Evidence Package，不是真实 Provider，不进行网络采集，也不代表这个
pre-alpha 仓库已具备生产就绪性。仓库 slug 和 CLI 均为 `tannang`，Go module 为
`github.com/05wuyanzi/tannang`。

仓库还定义了一个仅供库调用的 FirstStage 编排合同：它把可信的受保护 baseline 与
附加 synthetic 请求合并，为每个 run 获取一次不可变 Target Fingerprint，在单个 Run
内按顺序解析并执行选定的 synthetic Provider，完整记账所有请求，并协调一个有界、
仅返回引用和结果的 Finalizer seam。同一 FirstStage 实例会拒绝而不是排队等待重叠
Run；不同实例彼此独立。该合同未接入 CLI，也没有增加任何真实 Stage-1 Capability 或
证据采集。

## 为什么需要探囊

现场响应采集不应只留下一个文件。复核者还需要确认请求了什么、为何选择某个
Provider、它是否兼容、执行时实际发生了什么，以及最终证据包是否完整。探囊通过
小型合同、Receipt、状态分离和确定性的包验证，让这些决策保持明确且可检查。

## 当前已实现能力

Pre-alpha synthetic core 当前包括：

- 用于 synthetic 采集和证据包验证的 CLI；
- Capability 与 Target Fingerprint 模型；
- Provider 抽象与兼容性 Resolver；
- 仅限 synthetic、库级的 FirstStage 编排合同及明确的逐请求记账；
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
`EXTERNAL_BACKEND`，但目前尚未实现这些类型的真实 Provider。当前唯一实现的是
`SYNTHETIC_TEST`，且仅限使用内嵌数据进行测试。

机器可读合同见 [`contracts/`](contracts/)，详细架构和安全边界见
[`docs/architecture/`](docs/architecture/)。

## 快速开始

**仅限 Synthetic。这些命令不会采集本机数据。**

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
real_windows_provider: false
real_collection: false
active_trace: false
production_ready: false
forensic_certification: none
judicial_validation: none
```

当前版本不包含真实 Windows 采集、External Backend 集成、Packet Capture，也不支持
Legacy/Heritage Windows runtime。Synthetic fixture 中的 `LEGACY` 与 `HERITAGE`
只是测试输入，不代表支持声明。

探囊当前以 Windows 为目标。证据与编排合同有意和 Provider 实现分离，但这并不
代表当前支持其他平台。

当前 synthetic `execution.Result.Payload` 只是 fixture 兼容机制，并不是未来真实
Provider 的制品传输合同。真实 Provider 制品传输有意推迟到第一个真实 Capability
Gate 再定义；当前版本也没有生产级多 Capability 证据包适配器。

## 路线图

下一阶段的 Windows 工程工作预计先确定受支持的目标矩阵并评估更强的
handle-relative hardening，之后才会引入范围明确的真实 Provider。当前路径基线不
声称抵抗高权限进程并发替换 filesystem namespace；本 Pre-alpha 版本尚未实现或
支持任何真实 Provider。

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

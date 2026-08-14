# 探囊 / Tannang

> 便携、可审计的 Windows 现场响应采集与证据编排工具。
>
> Portable, auditable Windows live-response acquisition and evidence orchestration.

**Status:** Pre-alpha · Protected process-identity baseline active · Not production ready

**English** | [简体中文](README.zh-CN.md)

## What is Tannang?

Tannang is a Windows-first project for describing acquisition intent,
evaluating provider compatibility, recording execution outcomes, and packaging
evidence with auditable integrity metadata.

The normal `collect --output` CLI runs one non-removable protected
baseline capability: `PROCESS_IDENTITY_SNAPSHOT`. The explicit `--synthetic`
path remains available for embedded fixtures and does not perform real
incident-response evidence collection. The bounded Windows Target Fingerprint
used by the fixed real FirstStage reads limited local compatibility, resource,
privilege, and output-volume facts; it is not a Provider, performs no network
collection, and does not make this pre-alpha repository production ready. The
repository slug and CLI are `tannang`; the Go module is
`github.com/05wuyanzi/tannang`.

The repository contains a fixed library-level implementation for one narrow
first-party native Windows Provider:
`PROCESS_IDENTITY_SNAPSHOT`. It uses the Tool Help process snapshot API and
emits only process ID, parent process ID, and executable name as NDJSON to a
caller-owned writer. The Provider implementation and its separate benign
real-Windows acceptance have passed review. The bounded implementation provides
a fixed library-level FirstStage constructor, code-level protected-baseline
membership, and a single-capability Evidence Package adapter. The active
protected baseline uses that fixed path for normal
`collect --output`; `--process-identity-snapshot` remains a compatible explicit
confirmation of the same baseline and does not duplicate the request. It does
not expose Provider selection or extra capabilities. Default tests inject fake
acquisition and do not enumerate host processes.
Target compatibility is limited
to `amd64` and `x86`; Go build validation uses `windows/amd64` and compile-only
`windows/386`, which is not a native x86 real-host acceptance claim.

The repository also defines a library-only FirstStage orchestration contract.
The existing `NewFirstStage` path remains synthetic-only and behaviorally
unchanged. It merges a trusted protected baseline with additive requests,
acquires one immutable Target Fingerprint, resolves and executes selected
synthetic Providers sequentially within each Run, accounts for every request,
and coordinates a bounded reference-only Finalizer seam. The dedicated process
snapshot constructor is a separate, fixed path and cannot inject an arbitrary
Provider. One FirstStage instance rejects overlapping Runs rather than queuing
them; separate instances are independent. Normal product collection and its
explicit compatibility flag use this same fixed real path.

## Why Tannang exists

Live-response acquisition needs more than an artifact. Reviewers should be
able to determine what was requested, why a provider was selected, whether it
was compatible, what actually happened, and whether the resulting package is
intact. Tannang keeps those decisions explicit through small contracts,
receipts, status separation, and deterministic package verification.

## Current capabilities

The pre-alpha implementation currently provides:

- normal CLI collection with one protected real baseline, an
  explicit synthetic path, and package verification;
- Capability and Target Fingerprint models;
- a Provider abstraction and compatibility Resolver;
- a fixed, library-only `PROCESS_IDENTITY_SNAPSHOT` Provider implementation
  with a synchronous caller-owned writer boundary and reviewed benign Windows
  acceptance;
- a synthetic-compatible FirstStage orchestration contract plus a fixed
  real-provider FirstStage path with explicit per-request accounting;
- an embedded Synthetic Provider with end-to-end fixtures;
- separate compatibility and execution states;
- execution receipt generation;
- a fixed Evidence Package layout;
- a SHA-256 integrity manifest and package verifier;
- a Windows path and reparse-point safety baseline for package I/O; and
- synthetic end-to-end coverage for successful, partial, unavailable,
  blocked, and provider-failure outcomes.

Compatibility uses `AVAILABLE`, `DEGRADED`, and `UNAVAILABLE`. Execution uses
`COLLECTED`, `PARTIAL`, `SKIPPED`, `FAILED`, and `BLOCKED`. A partial or blocked
attempt is never presented as complete collection. Application orchestration
reasons and run `COMPLETE`/`PARTIAL`/`FAILED` states remain separate from those
Resolver and Provider domains.
Execution reason `CANCELLED` means an attempted Provider was explicitly
cancelled; application `OrchestrationReason=CANCELLED` continues to describe
orchestration-owned cancellation, including selected work that was not run.

## Architecture overview

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

The provider contract defines `WINDOWS_INBOX`, `FIRST_PARTY_NATIVE`, and
`EXTERNAL_BACKEND`. The bounded `FIRST_PARTY_NATIVE` process-snapshot Provider
is the sole protected capability used by normal `collect --output` and by the
compatible explicit `--process-identity-snapshot` form. `SYNTHETIC_TEST` remains
the only class reachable through `--synthetic`; the modes are mutually
exclusive.

See [`contracts/`](contracts/) for machine-readable contracts and
[`docs/architecture/`](docs/architecture/) for the detailed architecture and
security boundaries.

## Quick start

**Synthetic collection. These commands do not collect data from the host.**

With Go 1.21 or later, run from the repository root:

```powershell
go run ./cmd/tannang --help
$package = Join-Path (Get-Location) "tannang-demo-package"
go run ./cmd/tannang collect --synthetic available-collected --output $package
go run ./cmd/tannang verify $package
```

The output path must be a canonical absolute path on allowed local fixed or
removable storage, its parent must exist, and the output itself must not exist.
Collection reads the named embedded fixture, creates a synthetic Evidence
Package, and refuses unsafe paths or overwrite.

## Protected Windows process snapshot baseline

On Windows, normal `collect --output` runs the non-removable protected baseline,
which currently contains only `PROCESS_IDENTITY_SNAPSHOT`. `--case-id` remains
optional. The existing `--process-identity-snapshot` form confirms the same
single baseline request and does not cause duplicate acquisition, Receipt, or
artifact creation. Each invocation creates an independent Collection ID and
Evidence Package, or reports honest partial, skipped, blocked, failed, or
finalization outcomes. It does not provide risk scoring, malware verdicts,
remediation, credential collection, or broad endpoint enumeration.

```powershell
$package = Join-Path (Get-Location) "tannang-process-snapshot"
go run ./cmd/tannang collect --output $package --case-id CASE-01
go run ./cmd/tannang verify $package
```

The backward-compatible explicit form remains accepted:

```powershell
go run ./cmd/tannang collect --process-identity-snapshot --output $package
```

This active protected baseline is integrated on `dev`. It completes the
intentionally narrow M4 Minimum Useful Baseline, not a comprehensive
capability, production-ready, main integration, or release claim.

## Evidence package

A package uses this fixed top-level layout:

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

Package creation uses a guarded temporary sibling, protects every child write,
and publishes the final path by same-parent rename only after integrity
verification succeeds. The manifest records sorted paths, sizes, and SHA-256
values. Verification rejects missing, modified, extra, linked, duplicated,
non-canonical, undeclared, or reparse-directed package content.

See [Evidence package v0](docs/architecture/evidence-package.md) for details.

## Security and collection model

- Acquisition intent is represented explicitly by a Capability.
- Resolver decisions and execution results remain separate and auditable.
- FirstStage runs Providers sequentially within a Run, rejects an overlapping
  Run on the same instance, never silently removes protected requests, and
  records missing evidence explicitly.
- Receipts record the request, target fingerprint, provider decision, outcome,
  reason, timestamps, and side-effect summary.
- The integrity manifest makes package contents independently verifiable.
- Windows package I/O rejects reparse points, UNC and mapped remote paths,
  special device namespaces, ambiguous paths, and existing output roots.
- `ACTIVE_TRACE` is policy-disabled and is not currently implemented.
- No third-party binary is bundled or downloaded automatically.
- Optional external backends remain user-supplied, separate-process
  integrations and are not executed by the current synthetic core.

See [Genesis security boundaries](docs/architecture/security-boundaries.md)
and [Windows path safety v0](docs/architecture/windows-path-safety.md).

## Current limitations

```yaml
supported_windows_matrix: not_yet_established
real_windows_provider: true
real_collection: true
real_collection_scope: PROCESS_IDENTITY_SNAPSHOT
firststage_real_provider_activation: true
protected_baseline_activation: true
production_package_adapter: false
cli_real_provider_activation: true
active_trace: false
production_ready: false
forensic_certification: none
judicial_validation: none
```

The normal CLI activation runs the fixed FirstStage protected
baseline before any supplemental capability; that baseline currently contains
only the reviewed `PROCESS_IDENTITY_SNAPSHOT` scope and cannot be removed by a
CLI option. `--synthetic` remains an explicit non-real fixture path. There is no
external backend integration, packet capture, or supported Legacy/Heritage
Windows runtime in this release.
`LEGACY` and `HERITAGE` values in synthetic fixtures are test inputs, not
support declarations.

Tannang currently targets Windows. Its evidence and orchestration contracts
are intentionally separated from provider implementations; this does not
imply support for any additional platform.

The current synthetic `execution.Result.Payload` is fixture compatibility, not
a transport contract for real Provider artifacts. The process-snapshot
Provider defines only a synchronous caller-owned `io.Writer` handoff for
serialized observations; the bounded FirstStage implementation owns path choice,
close/flush, retain/discard, hashing, and publication only for this one
Capability. There is no generic multi-capability package adapter, and writer
output is not by itself an Evidence Package or published artifact reference.

## Roadmap

M4 Minimum Useful Baseline is complete on `dev` with the existing protected
`PROCESS_IDENTITY_SNAPSHOT`. It intentionally closes with one real capability;
additional real capabilities are future additive expansion, not prerequisites
for M4 completion.

The next product stage is M5 MVP RC. Its bounded focus is productizing the
already-working `HEADLESS CLI CORE + THIN NATIVE ONE-CLICK GUI` for portable,
offline-first execution and operator-facing workflow, establishing supported
Windows matrix evidence, and, where required by MVP scope, the thin native
one-click GUI. TUI remains HOLD. These are roadmap goals, not implemented
claims.

The current path baseline does not claim resistance to privileged concurrent
namespace races, and this pre-alpha repository remains non-production.

## Third-party boundary

```yaml
third_party_source_included: false
third_party_binary_included: false
third_party_binary_executed_by_default: false
```

Tannang is designed to allow optional external backends, but none is bundled,
downloaded, invoked, or officially supported by the current repository. See
[THIRD_PARTY.md](THIRD_PARTY.md) for the integration boundary.

## Contributing

Contributions use Pull Requests and DCO `Signed-off-by` trailers. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the source, fixture, and third-party
requirements.

## Security reporting

Report vulnerabilities through GitHub Private Vulnerability Reporting. Do not
open a public Issue for a suspected vulnerability. See
[SECURITY.md](SECURITY.md).

## License

Tannang is licensed under the Mozilla Public License 2.0. See
[LICENSE](LICENSE).

## Active FirstStage protected baseline

The integrated implementation provides a narrow library-level
`PROCESS_IDENTITY_SNAPSHOT` FirstStage path with a fixed Windows Tool Help
Provider, guarded staging, receipts, and SHA-256 manifest verification. The
normal CLI activation uses that capability as its sole non-removable
protected baseline; the explicit real flag remains compatible and the explicit
synthetic path remains isolated. Production-readiness remains false.

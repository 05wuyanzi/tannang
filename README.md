# 探囊 / Tannang

> 便携、可审计的 Windows 现场响应采集与证据编排工具。
>
> Portable, auditable Windows live-response acquisition and evidence orchestration.

**Status:** Pre-alpha · Synthetic core only · Not production ready

**English** | [简体中文](README.zh-CN.md)

## What is Tannang?

Tannang is a Windows-first project for describing acquisition intent,
evaluating provider compatibility, recording execution outcomes, and packaging
evidence with auditable integrity metadata.

The default CLI and Application path remain synthetic-only: they use embedded
fixtures and do not perform real incident-response evidence collection. The
repository also contains an explicitly invoked, bounded Windows Target
Fingerprint probe. It reads limited local compatibility, resource, privilege,
and output-volume facts; it is not evidence acquisition, creates no Evidence
Package, is not a real Provider, performs no network collection, and does not
make this pre-alpha repository production ready. The repository slug and CLI
are `tannang`; the Go module is `github.com/05wuyanzi/tannang`.

The repository also defines a library-only FirstStage orchestration contract.
It merges a trusted protected baseline with additive synthetic requests,
acquires one immutable Target Fingerprint, resolves and executes selected
synthetic Providers sequentially within each Run, accounts for every request,
and coordinates a bounded reference-only Finalizer seam. One FirstStage
instance rejects overlapping Runs rather than queuing them; separate instances
are independent. It is not wired into the CLI and does not add a real Stage-1
Capability or evidence collection.

## Why Tannang exists

Live-response acquisition needs more than an artifact. Reviewers should be
able to determine what was requested, why a provider was selected, whether it
was compatible, what actually happened, and whether the resulting package is
intact. Tannang keeps those decisions explicit through small contracts,
receipts, status separation, and deterministic package verification.

## Current capabilities

The pre-alpha synthetic core currently provides:

- a CLI for synthetic collection and package verification;
- Capability and Target Fingerprint models;
- a Provider abstraction and compatibility Resolver;
- a synthetic, library-only FirstStage orchestration contract with explicit
  per-request accounting;
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
`EXTERNAL_BACKEND`. No real provider for those classes is implemented yet.
`SYNTHETIC_TEST` is the only current provider implementation and is restricted
to testing with embedded data.

See [`contracts/`](contracts/) for machine-readable contracts and
[`docs/architecture/`](docs/architecture/) for the detailed architecture and
security boundaries.

## Quick start

**Synthetic only. These commands do not collect data from the host.**

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
real_windows_provider: false
real_collection: false
active_trace: false
production_ready: false
forensic_certification: none
judicial_validation: none
```

There is no real Windows acquisition, external backend integration, packet
capture, or supported Legacy/Heritage Windows runtime in this release.
`LEGACY` and `HERITAGE` values in synthetic fixtures are test inputs, not
support declarations.

Tannang currently targets Windows. Its evidence and orchestration contracts
are intentionally separated from provider implementations; this does not
imply support for any additional platform.

The current synthetic `execution.Result.Payload` is fixture compatibility, not
a transport contract for future real Provider artifacts. Real Provider artifact
transport is intentionally undefined until the first real Capability Gate.
There is no production multi-capability package adapter in this release.

## Roadmap

The next Windows-focused engineering work is expected to establish the
supported target matrix and evaluate stronger handle-relative hardening before
introducing narrowly scoped real providers. The current path baseline does not
claim resistance to privileged concurrent namespace races, and no real
Provider is implemented or supported by this pre-alpha release.

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

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

The current repository implements a synthetic control path only. It uses
embedded fixtures and does not inspect or collect data from the local Windows
host. The repository slug and CLI are `tannang`; the Go module is
`github.com/05wuyanzi/tannang`.

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
- an embedded Synthetic Provider with end-to-end fixtures;
- separate compatibility and execution states;
- execution receipt generation;
- a fixed Evidence Package layout;
- a SHA-256 integrity manifest and package verifier; and
- synthetic end-to-end coverage for successful, partial, unavailable,
  blocked, and provider-failure outcomes.

Compatibility uses `AVAILABLE`, `DEGRADED`, and `UNAVAILABLE`. Execution uses
`COLLECTED`, `PARTIAL`, `SKIPPED`, `FAILED`, and `BLOCKED`. A partial or blocked
attempt is never presented as complete collection.

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

```console
go run ./cmd/tannang --help
go run ./cmd/tannang collect --synthetic available-collected --output ./tannang-demo-package
go run ./cmd/tannang verify ./tannang-demo-package
```

The output path must not already exist. Collection reads the named embedded
fixture, creates a synthetic Evidence Package, and refuses to overwrite an
existing package.

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

Package creation uses a temporary sibling directory and publishes the final
path only after integrity verification succeeds. The manifest records sorted
paths, sizes, and SHA-256 values. Verification rejects missing, modified,
extra, linked, duplicated, non-canonical, or undeclared package content.

See [Evidence package v0](docs/architecture/evidence-package.md) for details.

## Security and collection model

- Acquisition intent is represented explicitly by a Capability.
- Resolver decisions and execution results remain separate and auditable.
- Receipts record the request, target fingerprint, provider decision, outcome,
  reason, timestamps, and side-effect summary.
- The integrity manifest makes package contents independently verifiable.
- `ACTIVE_TRACE` is policy-disabled and is not currently implemented.
- No third-party binary is bundled or downloaded automatically.
- Optional external backends remain user-supplied, separate-process
  integrations and are not executed by the current synthetic core.

See [Genesis security boundaries](docs/architecture/security-boundaries.md).

## Current limitations

```yaml
supported_windows_matrix: not_yet_established
real_windows_provider: not_yet_enabled
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

## Roadmap

The next Windows-focused engineering work is expected to establish the
supported target matrix, complete and benign-test path containment and
reparse-point behavior, and then introduce narrowly scoped real providers only
after their safety boundaries are verified. None of that work is implemented
or supported by the current pre-alpha release.

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

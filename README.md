# 探囊 / Tannang

Portable, auditable Windows live-response acquisition and evidence orchestration.

Status: **pre-alpha**

Tannang is a Windows-first project. The current repository contains only a
synthetic execution path used to develop and verify the control plane,
receipts, integrity manifest, and evidence-package contract. It does not
collect data from a Windows host.

## Current status

```yaml
production_ready: false
supported_windows_matrix: not_yet_established
forensic_certification: none
judicial_validation: none
third_party_binaries_included: false
current_real_windows_collection: not_yet_enabled
```

Linux, macOS, Android, and iOS are not currently supported or promised on the
near-term roadmap. Upper-layer package and receipt contracts avoid unnecessary
platform-specific fields so future platform research remains possible.

## Synthetic CLI

The development CLI requires an explicit embedded fixture and output path:

```text
tannang collect --synthetic available-collected --output <new-package-path>
tannang verify <package-path>
```

Available fixtures exercise successful, partial, unavailable,
privilege-blocked, and provider-failure outcomes. The synthetic path does not
inspect host identity, processes, network state, event logs, users, or other
host evidence. Active tracing is not executable.

## Compatibility and providers

Capabilities describe requested evidence. Providers describe how a capability
could be fulfilled. The resolver evaluates target fingerprint, requirements,
policy, side effects, and quality without assigning a permanent absolute
priority to provider classes.

The provider classes are:

- `WINDOWS_INBOX`
- `FIRST_PARTY_NATIVE`
- `EXTERNAL_BACKEND`
- `SYNTHETIC_TEST`, test-only and not a supported production provider

Compatibility and execution are recorded separately. Partial and blocked
results are never presented as complete collection.

## Module path

The Tannang v0.x public module path is frozen as:

```text
github.com/05wuyanzi/tannang
```

The canonical repository is `https://github.com/05wuyanzi/tannang`.

## License

Tannang is licensed under the Mozilla Public License 2.0. See `LICENSE`.

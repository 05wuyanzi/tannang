# Windows amd64 acceptance evidence

Tannang is public pre-alpha software. This document records bounded evidence
for exact environments; it is not a general Windows support promise, an RC
publication, or a production-readiness statement.

## Matrix state

The current state is `initial_evidence_available`. This means at least one
exact Windows amd64 environment has completed the acceptance contract.
`not_yet_established` means no such evidence exists. Neither state claims a
supported Windows family or version range.

Matrix rows use these states:

- `TESTED_PASS`: the exact environment completed the portable-artifact,
  protected-baseline, package-integrity, and verification checks.
- `TESTED_DEGRADED`: an actual run produced a contract-defined partial or
  degraded outcome while scope, identity, package integrity, and verification
  remained valid.
- `TESTED_FAIL`: an actual run violated the acceptance contract.
- `UNTESTED`: no acceptance evidence exists for the environment.
- `UNSUPPORTED_BY_CURRENT_ARTIFACT_CONTRACT`: the environment is outside the
  Windows amd64 portable-artifact contract. This must not be used for an
  untested Windows amd64 environment.
- `UNKNOWN`: the environment or evidence is insufficient to classify.

## Initial evidence row

| Field | Value |
| --- | --- |
| Acceptance contract | `TANNANG_M5_WINDOWS_AMD64_ACCEPTANCE_V1` |
| Evidence reference | `20260817T110818Z-M5-WINDOWS-AMD64-1d581a8` |
| Validated at (UTC) | `2026-08-17T11:08:44.4008005Z` |
| Host-reported product | `Microsoft Windows 11 专业工作站版` |
| Edition ID | `ProfessionalWorkstation` |
| Display version | `25H2` |
| OS version | `10.0.26200` |
| Build and UBR | `26200.8894` |
| OS / process architecture | `X64 / X64` |
| Observed privilege | `ELEVATED_ADMINISTRATOR` |
| Source revision | `1d581a801c5546e7dd86cc9f41cfdc9051eb93a3` |
| Source tree | `0f8db8e33e2fcbc2d1ce52f0724dc5064ec112c5` |
| Artifact class | `HEADLESS_PORTABLE_WINDOWS_AMD64` |
| Build mode / modified | `RC / false` |
| Collection scope | `PROCESS_IDENTITY_SNAPSHOT` |
| Execution / degradation | `COLLECTED / NONE` |
| Package manifest / verify | `PASS / PASS` |
| Result | `TESTED_PASS` |

Portable artifact SHA-256 values:

| File | SHA-256 |
| --- | --- |
| `tannang.exe` | `6fc6e437bf3a8f8d3cdba4833514d473af6de7a8053df647435fa4d445c3eccf` |
| `BUILD-INFO.json` | `c3a840b017e583780e554d1aa1aa1ed424ac540fc1a12925ad859766d4b056c3` |
| `LICENSE` | `fab3dd6bdab226f1c08630b1dd917e11fcb4ec5e1e020e2c16f83a0a13863e85` |
| `SHA256SUMS.txt` | `0d0a7db755baa666b553798f7ad85465e5fb04346e311a6de7af53d7356e4c83` |

The observed process was elevated. Non-elevated execution remains `UNTESTED`,
so this row does not establish standard-user support. The raw Evidence Package
and complete logs remain private. The evidence reference is an opaque ID, not
a public path or a substitute for raw-evidence custody.

## Claim boundary

The evidence supports only this statement: Tannang revision
`1d581a801c5546e7dd86cc9f41cfdc9051eb93a3` was validated as a portable Windows
amd64 headless artifact on the exact environment above.

It does not establish support for Windows 10 or Windows 11 generally, all
modern or legacy Windows versions, x86, arm64, non-elevated execution, an
installer, a GUI, RC publication, release readiness, or production readiness.

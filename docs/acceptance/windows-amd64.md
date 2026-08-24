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

## Portable GUI evidence row

| Field | Value |
| --- | --- |
| Acceptance contract | `TANNANG_M5_GUI_WINDOWS_AMD64_ACCEPTANCE_V1` |
| Evidence reference | `20260824T134206Z-M5-GUI-WINDOWS-AMD64-8213901` |
| Validated at (UTC) | `2026-08-24T13:42:06Z` |
| Host-reported product | `Microsoft Windows 11 专业工作站版` |
| Edition ID | `ProfessionalWorkstation` |
| Display version | `25H2` |
| OS version | `10.0.26200` |
| Build and UBR | `26200.8894` |
| OS / process architecture | `X64 / X64` |
| Elevation evidence | `HUMAN_ATTESTED_UAC_ELEVATED_GUI_LAUNCH` |
| Source revision | `82139012116a434cc050b4cdc4e6771db8e0d309` |
| Source tree | `d1c244120ec52903c1e47ad5e68ac75859426456` |
| Artifact class | `PORTABLE_GUI_WINDOWS_AMD64` |
| Build mode / modified | `RC / false` |
| Runtime | `win-x64 / net10.0-windows / .NET 10.0.11 / self-contained` |
| Collection scope | `PROCESS_IDENTITY_SNAPSHOT` |
| Provider | `windows-toolhelp-process-snapshot` |
| Compatibility | `AVAILABLE` |
| Execution / degradation | `COLLECTED / NONE` |
| GUI terminal state | `FINISHED: COMPLETE` |
| Package manifest / verify | `PASS / PASS` |
| Result | `TESTED_PASS` |

Portable GUI artifact SHA-256 values:

| File | SHA-256 |
| --- | --- |
| `Tannang.Gui.exe` | `711261df3877462dfc40a6b393fc9971d48697cd9b60dc1c3076ad984d5111e6` |
| `tannang.exe` | `49cb85db644907e5711f6730290906311dcc136e55c1f17ae9bb745a59848780` |
| `SHA256SUMS.txt` | `41a11af0dd033bb6304a5809cad4cb4c2084ae5452bc4c996d4e6e025729028d` |

This row records one exact merged-dev RC-mode portable GUI acceptance for the
protected `PROCESS_IDENTITY_SNAPSHOT` scope. It is additive to the headless
row above and does not establish general Windows 10 or Windows 11 support,
non-elevated support, x86 or arm64 real-host support, an installer, an RC
publication, release readiness, or production readiness.

## Claim boundary

The evidence supports two bounded statements: revision
`1d581a801c5546e7dd86cc9f41cfdc9051eb93a3` was validated as a portable Windows
amd64 headless artifact on the exact environment above, and revision
`82139012116a434cc050b4cdc4e6771db8e0d309` was validated as a portable GUI
Windows amd64 RC-mode candidate on the exact environment in the second row.

It does not establish support for Windows 10 or Windows 11 generally, all
modern or legacy Windows versions, x86, arm64, non-elevated execution, an
installer, RC publication, release readiness, or production readiness.

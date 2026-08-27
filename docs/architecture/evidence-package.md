# Evidence package v0 and FirstStage v1.3 multi-artifact extension

Every package has this fixed top-level layout:

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

The genesis build may leave `raw` or `derived` empty, but `meta/package.json`
always declares the complete layout and records capability, target fingerprint,
provider selection, compatibility, execution, reason, acquisition semantics,
classification, timestamps, and side effects.

`hashes/manifest.json` contains every package directory with its direct hashed
file count, plus sorted package-relative paths, sizes, and SHA-256 values for every
regular package file except the manifest itself. Empty directories and the
self-exclusion are explicit. Verification rejects missing, modified, extra,
linked, duplicated, non-canonical, or undeclared files and directories.

On Windows, package creation requires a canonical absolute path on allowed
local storage. It validates the existing parent chain, creates and revalidates
a temporary sibling under that parent, and guards every package-relative
directory and file creation. The manifest is generated and the complete
temporary tree is verified before a no-overwrite, same-parent rename publishes
the caller-selected final path. Existing output paths and files are never
overwritten.

Verification treats the package as untrusted input. It rejects reparse points
in the root, ancestor chain, or package tree before reading the manifest, and
rechecks each regular file before opening and hashing it. See
[Windows path safety v0](windows-path-safety.md) for policy and limitations.

Handoff status is non-executing. The genesis package does not grant downstream
execution authority and does not invoke another capability.

## FirstStage finalization seam

The library-only synthetic FirstStage contract does not redesign this package.
Its application-owned Finalizer seam describes
only the outcome FirstStage must validate: verification status, an opaque
package reference, exactly one receipt reference per accepted request, and an
optional artifact reference per request.

Synthetic orchestration tests use fake Finalizers and require no filesystem
materialization. The seam does not define artifact bytes, paths, readers,
streams, workspaces, package layout, or hashing. `evidence.Create` remains the
single-capability synthetic implementation above. The fixed real FirstStage
binding adds the bounded v1.3 package shape for the four known real artifacts;
it is not a generic workspace manager. Historical v1.1 packages remain a
separately verifiable compatibility contract.

## Process identity snapshot writer boundary

The explicit library-only `PROCESS_IDENTITY_SNAPSHOT` Provider implementation
adds a
narrow synchronous handoff, not package construction. It writes compact NDJSON
records to one caller-owned `io.Writer` and returns an `execution.Result`.
`execution.Result.Payload` remains synthetic compatibility only.

The caller must construct a fresh, empty, exclusive, single-use, uncommitted,
and wholly discardable candidate sink. `COLLECTED` and `PARTIAL` permit the
caller to retain that candidate; `FAILED`, `BLOCKED`, and `SKIPPED` require
whole-sink discard. Any writer failure is `FAILED/PROVIDER_ERROR`, even after
complete rows, and the Provider does not claim physical rollback. Retainable
`PARTIAL` output can arise only when the last successful write ended on a
complete NDJSON line.

The immediate descriptor contains only media type `application/x-ndjson` and
the per-record schema ID. It defines no RAW/DERIVED classification. The
package adapter, not the Provider, owns PATHSAFE staging, path choice,
close/flush, retain/discard enforcement, SHA-256, Artifact and Receipt
references, Manifest creation, and publication. The bounded FirstStage
candidate implements that authority for the process identity, fixed System
Event Log EVTX, and host/OS identity JSON artifacts in the promoted protected
baseline;
Provider output alone remains an unpublished observation or file.

The bounded FirstStage adapter keeps historical v0 process-only packages
verifiable. The protected `WINDOWS_EVENT_LOG_SYSTEM_CHANNEL` capability uses
v1.3 receipt/package semantics for new promoted collections, reserves
`raw/windows-event-log-system.evtx` inside the protected staging root, retains
only a successfully exported ordinary file, publishes one receipt per
accepted request, verifies the unchanged v1.0 SHA-256 Manifest, and keeps the
downstream handoff disabled. The promoted four-capability baseline has passed
bounded post-promotion benign real-host acceptance; this remains not a
production-readiness claim.
Historical v1.1 supplemental Event Log receipts with `protected=false` remain
valid for backward-compatible verification; promotion changes default activation
for new real collections, not the validity of prior evidence.

## Host/OS identity artifact binding

`WINDOWS_HOST_OS_IDENTITY_SNAPSHOT` uses `STATE_SNAPSHOT` semantics and the
fixed `FIRST_PARTY_NATIVE` Provider `windows-native-host-os-identity`. Its
caller-owned artifact is `derived/windows-host-os-identity.json`, classified as
`DERIVED`, with media type `application/json` and content schema
`urn:tannang:artifact:windows-host-os-identity-json-v0`. The JSON object contains
exactly `computer_name`, `os_major`, `os_minor`, `os_build`, and
`native_architecture`. The Provider uses local Windows APIs only and does not
choose paths, hash bytes, publish references, or alter host configuration.

## Transport endpoint artifact binding

`WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT` uses `STATE_SNAPSHOT` semantics and the
fixed `FIRST_PARTY_NATIVE` Provider `windows-iphlpapi-transport-endpoints`.
It reads only the local Windows IP Helper tables through
`GetExtendedTcpTable` and `GetExtendedUdpTable` for TCP4, TCP6, UDP4, and UDP6,
retaining owning PID as raw evidence. Its caller-owned artifact is
`derived/windows-transport-endpoints.ndjson`, classified as `DERIVED`, with
media type `application/x-ndjson` and content schema
`urn:tannang:artifact:windows-transport-endpoint-record-v0`.

The provider preserves fixed table order and native row order, uses bounded
retries and transient memory, and never silently truncates a table. Candidate
bytes are discarded unless all four tables are acquired and serialized
successfully; no partial subtable artifact is retained. The capability performs
no remote network I/O, DNS/name resolution, probing, scanning, packet capture,
active trace, or process enrichment.

New normal collections use FirstStage schema `1.3`, manifest `1.0`, and runtime
artifact `tannang-first-stage-multi-v1.3`. Verification continues to accept
historical process-only v1.0 packages, two-capability v1.1 packages, Host/OS
identity v1.2 packages, and historical v1.3 supplemental Network receipts with
`protected=false`. The current promoted four-capability producer records
`protected=true`; historical protection values remain verifiable and are not
rewritten.

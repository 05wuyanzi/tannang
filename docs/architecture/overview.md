# Genesis architecture

Tannang is a Windows-first, platform-extensible evidence orchestration project.
The current CLI and Application collection path still prove only the control
path below with embedded synthetic data:

```text
CLI -> Capability -> Target Fingerprint -> Resolver -> Synthetic Provider
    -> Execution Result -> Receipt -> Evidence Package -> SHA-256 Verification
```

Capability and Provider are separate contracts. A user asks for evidence by
Capability ID. The resolver evaluates provider declarations against a supplied
Target Fingerprint and policy. Provider class does not create a permanent
priority order.

Compatibility describes whether a provider can satisfy a request. Execution
describes what happened during one attempt. The two fields are never collapsed.
`PARTIAL`, `SKIPPED`, `FAILED`, and `BLOCKED` are not complete collection.

`SYNTHETIC_TEST` is a test-only provider class. It reads named files embedded at
build time and performs no host probes, command execution, network access, or
external backend invocation.

## Minimal real target fingerprint

The `internal/fingerprint` package also exposes an explicit, read-only Windows
target probe for future Compatibility Resolver and Provider Selection work. It
is not wired into the CLI, Application collection path, a Provider, or Evidence
Package creation. Calling it does not acquire incident-response evidence.

The probe records only raw compatibility and resource context: actual Windows
version/build, native and process architecture, logical processor count,
physical memory, current-token elevation, and volume facts for an output path
that satisfies the existing PATHSAFE contract. Optional CPU pressure uses one
bounded 250 ms `GetSystemTimes` sample. Field-level `KNOWN`, `UNAVAILABLE`,
`FAILED`, and `UNSUPPORTED` states keep missing values distinct from measured
zero values.

The Windows implementation uses local first-party APIs without PowerShell,
WMI, child processes, networking, installation, or elevation. `RtlGetVersion`
supplies OS compatibility context; an OS build is not treated as proof that an
unrelated API or Provider is available. Raw facts do not assign resource
classes, runtime lanes, or Provider policy. Non-Windows builds return a
fail-closed unsupported result and do not imply Linux or macOS support.

The v0.x Go module path is `github.com/05wuyanzi/tannang`.

## Narrow process identity Provider candidate

The repository contains one explicitly invoked, library-only
`FIRST_PARTY_NATIVE` Provider candidate for
`PROCESS_IDENTITY_SNAPSHOT/STATE_SNAPSHOT`. It uses
`CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)`, `Process32FirstW`, and
`Process32NextW` sequentially and closes its one snapshot handle. Each NDJSON
record contains exactly process ID, parent process ID, and executable name in
the API's enumeration order. It does not call `OpenProcess`, enrich records,
sort, deduplicate, spawn child processes, use the network, or request
elevation.

The Provider's target-compatibility identifiers are `amd64` and `x86`.
Windows ABI builds use Go's separate `windows/amd64` and `windows/386`
identifiers; `windows/386` is compile-only until a real x86 host is separately
accepted. No arm64 support is claimed.

The optional synchronous `provider.StreamingRunner` writes serialized
observations to a caller-owned `io.Writer`. The caller must construct a fresh,
empty, exclusive, single-use, uncommitted, and wholly discardable sink. The
Provider never chooses a path, closes or flushes the writer, hashes bytes,
publishes a reference, or retains the writer after return. `COLLECTED` and
`PARTIAL` are retainable; `FAILED`, `BLOCKED`, and `SKIPPED` require whole-sink
discard. Writer failure always returns `FAILED/PROVIDER_ERROR`, never
`PARTIAL`, because a partial final line may remain physically present.

Execution `CANCELLED` means an attempted Provider was explicitly cancelled.
It is separate from application `OrchestrationReason=CANCELLED`, which also
accounts for selected work that was never launched. The candidate is not wired
into the CLI or FirstStage, is not protected baseline, and has no production
package adapter. Default tests use a fake Windows API and do not enumerate host
processes; real-host acceptance is a separate opt-in Gate.

## First-stage orchestration contract

`internal/application.FirstStage` is a library-only synthetic orchestration
contract and is not wired into the CLI. Trusted configuration supplies a
non-empty protected baseline; ordinary RunRequest input may add requests but
cannot remove or create protected membership. Requests are merged and ordered
deterministically by `EARLY`, `NORMAL`, `LATE`, protected membership, and
Capability ID.

One run creates an immutable `COL-` UUIDv4 Collection ID, keeps optional Case ID
separate, checks mandatory startup and output prerequisites, and invokes the
Target Fingerprint exactly once. The retained fingerprint is cloned for
Resolver, Provider, and Finalizer boundaries. Resolver retains compatibility
policy, while FirstStage executes at most one selected Provider per request and
does so strictly sequentially within a Run with no automatic fallback. One
FirstStage instance accepts at most one active Run and rejects overlap rather
than queuing it; separate instances remain independent. The active-Run gate
retains no completed-Run Collection ID history and is not a process-global
scheduler.

Every accepted request receives exactly one CapabilityRecord. Compatibility,
actual Provider execution, and application-owned OrchestrationReason remain
separate. `attempted=false` plus `SKIPPED/NONE` distinguishes application
accounting from a Provider that ran and returned `SKIPPED`. MissingEvidence is
explicit and prevents run `COMPLETE`.

The application-owned run states are `COMPLETE`, `PARTIAL`, and `FAILED`; they
are not aliases of Provider execution states. Controlled cancellation stops new
Provider launches and uses a separate bounded finalization context when startup
prerequisites already succeeded.

The Finalizer seam accepts copy-isolated accounting facts and returns only
verification, package, receipt, and optional artifact references. This slice
uses fake Finalizers in tests and does not implement a multi-capability package
adapter. Existing synthetic Payload remains fixture compatibility only. The
process-snapshot Provider's caller-owned writer seam is not a package adapter
and does not produce a trusted artifact reference.

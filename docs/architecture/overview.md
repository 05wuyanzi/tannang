# Genesis architecture

Tannang is a Windows-first, platform-extensible evidence orchestration project.
Normal CLI collection activates the event-capable fixed real FirstStage with
two non-removable protected baseline capabilities:
`PROCESS_IDENTITY_SNAPSHOT` and `WINDOWS_EVENT_LOG_SYSTEM_CHANNEL`. The fixed
Event Log form is an idempotent confirmation flag. The explicit synthetic path remains separate and
continues to prove the generic control path with embedded data:

```text
CLI --synthetic -> Capability -> Target Fingerprint -> Resolver -> Synthetic Provider
    -> Execution Result -> Receipt -> Evidence Package -> SHA-256 Verification

CLI collect --output -> fixed process + System Event Log FirstStage
    -> Tool Help + wevtapi Providers -> receipts + artifacts -> Evidence Package verification

CLI collect --windows-event-log-system -> same protected baseline (confirmation only)
    -> run-scoped wevtapi probe -> native EVTX artifact -> v1.1 receipts + manifest verification
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

The `internal/fingerprint` package exposes a bounded, read-only Windows target
probe. The fixed real FirstStage invokes it once for compatibility context;
there is no separate CLI probe or generic Provider-selection surface. Calling
the probe is not itself incident-response evidence acquisition.

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

## Narrow process identity Provider implementation

The repository contains one fixed, library-only
`FIRST_PARTY_NATIVE` Provider implementation for
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
accounts for selected work that was never launched. Normal `collect --output`
and the compatible explicit `--process-identity-snapshot` form both use the same
fixed constructor and code-level protected-baseline membership. The explicit
flag adds no supplemental request, so the protected two-capability package
adapter still executes each accepted capability exactly once. Default
tests use fake acquisition and do not enumerate host processes.

## Fixed System Event Log baseline capability

`WINDOWS_EVENT_LOG_SYSTEM_CHANNEL` is a fixed protected Capability with
`EXISTING_ARTIFACT_EXPORT` semantics. Its only production binding is the
`FIRST_PARTY_NATIVE` Provider `windows-wevtapi-system-channel`, implemented
with the documented `wevtapi.dll` `EvtOpenLog`/`EvtExportLog`/`EvtClose`
procedures. The channel path is always `System`, the session is local and
null, the query is null, and the target is the caller-owned protected staging
path. No arbitrary channel, Event ID filter, remote session, subscription, or
configuration-changing API is exposed.

Before resolver selection, a run-scoped probe opens and closes the fixed
channel without reading event contents. It reports only bounded availability
reasons (`API_UNAVAILABLE`, `TARGET_STATE_RESTRICTED`, or
`PRIVILEGE_REQUIRED`) through the cloned resolver descriptor. The export is
retained as the opaque RAW artifact
`raw/windows-event-log-system.evtx` with media type `application/x-evtx`.
An empty but valid EVTX export is still `COLLECTED`; it is not an incident
judgment. The confirmation flag is mutually exclusive with `--synthetic` and
does not add a duplicate request. The promoted two-capability baseline has
passed bounded real-host acceptance on one Windows 11 25H2 amd64 environment
through both the default headless and thin one-click GUI paths; this does not
establish a general Windows support matrix.

## First-stage orchestration contract

`internal/application.FirstStage` remains a library-owned orchestration contract.
Its existing `NewFirstStage` construction path
remains synthetic-only. Trusted configuration supplies a non-empty protected
baseline; ordinary RunRequest input may add requests but
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

The synthetic Finalizer seam accepts copy-isolated accounting facts and returns
only verification, package, receipt, and optional artifact references. It
remains behaviorally unchanged and uses fakes in tests. The real production
binding supports only the two known capability-keyed artifacts: process NDJSON
and the fixed System-channel EVTX export. It is not a generic workspace or
extraction framework. Existing synthetic Payload remains fixture compatibility
only; Provider output itself is not package authority.

The implementation supplies private fake seams and fixed production bindings
for the protected process baseline plus the protected System Event Log
capability. Its
package session owns one guarded staging tree, up to the two known artifacts,
receipts, manifest verification, and no-overwrite publication. The integrated
activation makes both capabilities the normal CLI's protected baseline;
the Event Log flag is fixed and confirmation-only;
`--synthetic` bypasses the real factory, while the explicit real flag confirms
the same baseline without duplication. The CLI returns a bounded health summary
rather than a serialized `RunResult` or Provider payload. This remains
`PUBLIC_PRE_ALPHA` and is not production ready. The intentionally narrow M4
Minimum Useful Baseline is complete on `dev` with the process capability. The
Event Log baseline promotion is implemented on this candidate and its
bounded post-promotion benign Windows acceptance has passed through both
product paths; neither state is a comprehensive capability, main-integration,
or release-completion claim.

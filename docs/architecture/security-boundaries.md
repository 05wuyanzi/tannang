# Genesis security boundaries

The current event-capable candidate `collect --output` path invokes the fixed
process-snapshot, System Event Log, host/OS identity, and transport endpoint
FirstStage path. Its
non-removable protected baseline contains exactly `PROCESS_IDENTITY_SNAPSHOT`,
`WINDOWS_EVENT_LOG_SYSTEM_CHANNEL`, `WINDOWS_HOST_OS_IDENTITY_SNAPSHOT`, and
`WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT`.
`--process-identity-snapshot`, `--windows-event-log-system`,
`--windows-host-os-identity`, and `--windows-transport-endpoints` confirm the
same baseline without adding requests,
while `--synthetic` remains
a separate fixture-only path and never constructs the real FirstStage. None of
these paths is a production-readiness claim.

- Only simple embedded fixture names are accepted by `--synthetic`.
- On Windows, output must be an explicit canonical absolute path on allowed
  local fixed or removable storage, with an existing safe parent chain.
- UNC, mapped remote, device-namespace, ambiguous, and reparse-point paths are
  rejected.
- Package construction uses a guarded temporary sibling, guards each child
  operation, and publishes by same-parent rename only after verification.
- Final publication fails closed and does not overwrite an existing path.
- Verification rejects any reparse point in the package root, ancestor chain,
  or package tree before reading or hashing package content.
- Active Trace is policy-disabled.
- The fixed real FirstStage Target Fingerprint probe may read only bounded local
  compatibility and resource facts: Windows version/build, architecture, CPU
  count, physical memory, current-token elevation, and resource facts for an
  already PATHSAFE-accepted output location.
- The fingerprint is not evidence acquisition. It does not read process,
  network, event log, user/session, registry-evidence, service, credential, or
  filesystem-evidence content and it creates no Evidence Package artifact.
- No external process, third-party backend, or network client is used.
- No third-party source, binary, or Go module is included.

The fingerprint records raw values and field-level probe status only. It does
not classify a target as healthy, constrained, supported, or suitable; it does
not select a Provider or assign a runtime lane. `RtlGetVersion` supplies actual
OS compatibility context without using manifest-sensitive `GetVersion` or
`GetVersionEx`, but a build number is not generic API-availability proof.

Output-volume probing does not implement another path policy. The caller path
is checked through the existing PATHSAFE contract before and after the local
volume metadata calls. The probe creates no directory or file, never switches
volumes, and does not weaken the existing `PREEXISTING_REDIRECTION_SAFETY`
claim. Optional CPU pressure is one bounded 250 ms sample rather than ongoing
monitoring. The base probe requires no elevation, network access, child
process, PowerShell, WMI, service, registry/security change, or UAC action.

Non-Windows probing returns `UNSUPPORTED` and fails closed. This build behavior
is not a Linux or macOS support claim.

## Explicit process-snapshot Provider boundary

The library-level `PROCESS_IDENTITY_SNAPSHOT` implementation is one of four
narrow real Provider implementations in this slice. It observes only process ID, parent
process ID, and executable name through the Tool Help process snapshot family.
It retains PID 0 and parent PID 0 as observations without interpretation. It
does not use `OpenProcess`, command lines, full paths, owner/SID/token data,
modules, threads, handles, memory, signatures, hashes, networking, child
processes, WMI, PowerShell, privilege changes, or Active Trace.

The Provider is synchronous and sequential. It creates at most one snapshot
handle, holds one `PROCESSENTRY32W` and one encoded line, performs no sorting or
deduplication, and starts no worker pool or background goroutine. Target
compatibility is limited to canonical Fingerprint identifiers `amd64` and
`x86`; Go's separate 32-bit build identifier is `windows/386`, which is only a
compile/ABI claim until real x86-host acceptance occurs.

The caller-owned writer must be fresh, empty, exclusive, single-use,
uncommitted, and wholly discardable. The Provider cannot prove these properties
for an arbitrary writer and does not claim rollback or truncation. Writer
failure always makes the entire candidate sink non-retainable. Only a prefix
ending on a complete NDJSON row may accompany `PARTIAL` caused by API failure,
explicit cancellation, or deadline expiry. Cancellation remains represented by
the existing reason/orchestration fields and is distinct from an attempted
Provider result; no new execution state is introduced.

This seam gives the Provider no filesystem path, close/flush, hashing,
publication, Receipt, Manifest, or package authority. It is reachable through
normal `collect --output` and the four compatible explicit real flags; ordinary
tests use fake acquisition. All four product entry forms merge to the same
code-level protected baseline and cannot request duplicate acquisition. The Provider implementation
and its separate benign Windows acceptance have passed review.

## Fixed host/OS identity Provider boundary

`WINDOWS_HOST_OS_IDENTITY_SNAPSHOT` is a fixed local `STATE_SNAPSHOT` with the
`FIRST_PARTY_NATIVE` Provider `windows-native-host-os-identity`. It calls only
`GetComputerNameExW(ComputerNamePhysicalDnsHostname)`, `RtlGetVersion`, and
`GetNativeSystemInfo`, then writes exactly one bounded JSON object containing
`computer_name`, `os_major`, `os_minor`, `os_build`, and
`native_architecture` to the caller-owned sink. It requires no elevation by
design and performs no network access, child-process execution, inventory
expansion, registry/configuration write, or secret collection. Host evidence is
separate from the Target Fingerprint; the fingerprint remains compatibility
context and does not receive these artifact fields.

The library-only FirstStage contract adds coordination around explicitly
bounded collection. Existing `NewFirstStage` construction still admits only
`SYNTHETIC_TEST` bindings. The separate fixed process-snapshot constructor is
the sole real binding and does not create a generic injection
authority. FirstStage invokes selected Providers sequentially within each Run,
records every accepted request, and has no worker pool, plugin framework,
hidden fallback, or background collection. A
FirstStage instance rejects an overlapping Run instead of queuing it; separate
instances are independent and no process-global scheduler is introduced.

FirstStage requires explicit startup, output-path, Fingerprint, Resolver, and
Finalizer seams. No default startup seam claims a trusted release state. Caller
cancellation stops new Provider launches. When prerequisites have succeeded,
cleanup/finalization uses a separate context bounded by trusted positive
timeout; it cannot continue without that bound.

The synthetic Finalizer boundary carries opaque references only. Existing
synthetic Payload is not authority for real Provider artifact transport. The
narrow process-snapshot writer seam stops at caller-owned candidate bytes and
an execution Result; the bounded FirstStage session, not the Provider, owns the
four-capability package lifecycle. The fixed Event Log Provider receives only a
caller-owned protected staging file path. There is no generic workspace or
extraction framework. The CLI validates the request and output path before
constructing the fixed real FirstStage, invokes only that fixed constructor, and does
not expose Provider, Resolver, backend, supplemental, or baseline-removal
selection.

The integrated implementation adds only the fixed process-identity, System Event
Log, host/OS identity, and transport endpoint package session and its unexported
deterministic test seam. Providers still receive only an `io.Writer`; PATHSAFE owns exclusive
creation, exact-file discard, and no-overwrite publication. The implementation
claims only `PREEXISTING_REDIRECTION_SAFETY`. The active protected baseline
remains `PUBLIC_PRE_ALPHA` and is not production ready. The event-capable branch
adds one fixed local System Event Log artifact, one fixed host/OS identity
artifact, and one fixed transport endpoint artifact without broadening collection
scope; bounded post-promotion real-host
acceptance has passed through the default headless and thin one-click GUI paths.
This is not a comprehensive capability, main integration, or release claim.

## Fixed transport endpoint Provider boundary

`WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT` is a fixed local `STATE_SNAPSHOT` whose
only production Provider is the `FIRST_PARTY_NATIVE`
`windows-iphlpapi-transport-endpoints` binding. It calls only
`GetExtendedTcpTable` and `GetExtendedUdpTable` in `iphlpapi.dll` for the fixed
TCP IPv4, TCP IPv6, UDP IPv4, and UDP IPv6 tables, retaining the raw owning PID
field. It performs no external process execution, PowerShell, WMI, `cmd.exe`,
`netstat.exe`, DNS or name resolution, outbound or remote network I/O, active
trace, packet capture, scanning, probing, or adapter/route/ARP/firewall
inventory. It performs no process enrichment, credential or secret extraction,
automatic elevation, or security/configuration change.

The Provider reads tables sequentially, preserves native row order, uses a
bounded retry policy and bounded transient native buffers, and never silently
truncates rows. Its candidate NDJSON bytes are retained only when all four
required tables are acquired and serialized successfully; otherwise the
candidate is discarded and the existing execution/finalization contract reports
the failure truthfully. Owning PID is raw evidence only: collection makes no
temporal or transactional correlation claim with the process snapshot, and PID
zero for UDP means ownership information is unavailable rather than confirmed
process identity. Network evidence is not added to Target Fingerprint fields.

The current Windows baseline blocks pre-existing reparse-point, junction, and
symbolic-link redirection for synthetic package creation and verification. It
does not claim full resistance to a privileged process concurrently replacing
filesystem namespace entries, and it is not production hardened. See
[Windows path safety v0](windows-path-safety.md).

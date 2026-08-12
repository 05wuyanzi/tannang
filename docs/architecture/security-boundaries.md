# Genesis security boundaries

The pre-alpha CLI and existing generic Application path remain synthetic-only
and are not approved for production or real evidence. A separate explicit
library Provider implementation and its bounded FirstStage candidate do not
change that default boundary or activate the product path.

- Only simple embedded fixture names are accepted.
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
- The explicitly invoked Target Fingerprint probe may read only bounded local
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

The library-only `PROCESS_IDENTITY_SNAPSHOT` candidate is the sole narrow real
Provider implementation in this slice. It observes only process ID, parent
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
explicit cancellation, or deadline expiry. Execution `CANCELLED` is an
attempted-Provider fact and remains distinct from application orchestration
cancellation.

This seam gives the Provider no filesystem path, close/flush, hashing,
publication, Receipt, Manifest, or package authority. It is not wired into the
CLI, and ordinary tests use fake acquisition. The Provider implementation and
its separate benign Windows acceptance have passed review. The bounded
FirstStage candidate adds code-level protected-baseline membership, but the
activation claim remains false until its independent implementation review and
separate opt-in real FirstStage acceptance pass.

The library-only FirstStage contract adds coordination around explicitly
bounded collection. Existing `NewFirstStage` construction still admits only
`SYNTHETIC_TEST` bindings. The separate fixed process-snapshot constructor is
the sole real binding candidate and does not create a generic injection
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
single-capability package lifecycle. There is no generic multi-capability
adapter and no CLI wiring to FirstStage.

The bounded implementation candidate adds only the fixed process-identity
package session and its unexported deterministic test seam. Providers still
receive only an `io.Writer`; PATHSAFE owns exclusive creation, exact-file
discard, and no-overwrite publication. The candidate claims only
`PREEXISTING_REDIRECTION_SAFETY` and remains pending independent review and
real integration acceptance.

The current Windows baseline blocks pre-existing reparse-point, junction, and
symbolic-link redirection for synthetic package creation and verification. It
does not claim full resistance to a privileged process concurrently replacing
filesystem namespace entries, and it is not production hardened. See
[Windows path safety v0](windows-path-safety.md).

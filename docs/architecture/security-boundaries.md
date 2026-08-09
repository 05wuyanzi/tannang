# Genesis security boundaries

The pre-alpha synthetic build is not approved for production or real evidence.

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

The library-only FirstStage contract adds coordination, not collection. This
slice commits no production baseline; its tests use synthetic Capability IDs
only. No planned process, network, session, service, Event Log, or other
Stage-1 evidence group is implemented. It invokes configured Providers
sequentially within each Run, records every accepted request, and has no worker
pool, plugin framework, hidden fallback, or background collection. A
FirstStage instance rejects an overlapping Run instead of queuing it; separate
instances are independent and no process-global scheduler is introduced.

FirstStage requires explicit startup, output-path, Fingerprint, Resolver, and
Finalizer seams. No default startup seam claims a trusted release state. Caller
cancellation stops new Provider launches. When prerequisites have succeeded,
cleanup/finalization uses a separate context bounded by trusted positive
timeout; it cannot continue without that bound.

The Finalizer boundary carries opaque references only. Existing synthetic
Payload is not authority for future real Provider artifact transport, and the
current implementation adds no artifact reader, stream, workspace, writer, or
materialization contract. There is no production multi-capability package
adapter and no CLI wiring to FirstStage.

The current Windows baseline blocks pre-existing reparse-point, junction, and
symbolic-link redirection for synthetic package creation and verification. It
does not claim full resistance to a privileged process concurrently replacing
filesystem namespace entries, and it is not production hardened. See
[Windows path safety v0](windows-path-safety.md).

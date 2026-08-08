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
- No host identity, process, network, event log, user, registry, or filesystem
  evidence is read.
- No external process, third-party backend, or network client is used.
- No third-party source, binary, or Go module is included.

The current Windows baseline blocks pre-existing reparse-point, junction, and
symbolic-link redirection for synthetic package creation and verification. It
does not claim full resistance to a privileged process concurrently replacing
filesystem namespace entries, and it is not production hardened. See
[Windows path safety v0](windows-path-safety.md).

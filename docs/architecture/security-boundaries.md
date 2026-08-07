# Genesis security boundaries

The pre-alpha synthetic build is not approved for production or real evidence.

- Only simple embedded fixture names are accepted.
- Output must be explicit, new, and free of parent-traversal components.
- Package writes fail closed and do not overwrite an existing path.
- Active Trace is policy-disabled.
- No host identity, process, network, event log, user, registry, or filesystem
  evidence is read.
- No external process, third-party backend, or network client is used.
- No third-party source, binary, or Go module is included.

The current path checks reject obvious traversal and links inside a completed
package. Full Windows reparse-point and junction containment is not claimed.
That containment must be implemented and validated before any real Windows
Provider is allowed to write evidence.

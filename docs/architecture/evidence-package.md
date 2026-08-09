# Evidence package v0

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

The library-only FirstStage contract does not redesign this package or call a
new production package builder. Its application-owned Finalizer seam describes
only the outcome FirstStage must validate: verification status, an opaque
package reference, exactly one receipt reference per accepted request, and an
optional artifact reference per request.

Synthetic orchestration tests use fake Finalizers and require no filesystem
materialization. The seam does not define artifact bytes, paths, readers,
streams, workspaces, writers, package layout, or hashing. `evidence.Create`
remains the single-capability synthetic implementation above. A production
multi-capability adapter and real Provider artifact transport are intentionally
deferred to later reviewed Gates.

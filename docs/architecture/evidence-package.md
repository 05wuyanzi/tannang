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

Package creation uses a temporary sibling directory. The final path is created
by rename only after the package passes integrity verification. Existing output
paths are never overwritten.

Handoff status is non-executing. The genesis package does not grant downstream
execution authority and does not invoke another capability.

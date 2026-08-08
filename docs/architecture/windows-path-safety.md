# Windows path safety v0

## Purpose

Tannang's pre-alpha Windows path-safety boundary prevents a caller-selected
Evidence Package path from being redirected through a pre-existing reparse
point, junction, symbolic link, network path, or special device namespace. It
also prevents `tannang verify` from following a reparse point in an untrusted
package tree.

This is a safety baseline for synthetic package I/O. It does not enable a real
Windows Provider or real evidence collection, and it is not production
hardened.

## Threat model

The v0 boundary blocks:

- lexical traversal and non-canonical child paths;
- pre-existing reparse points in an output or input path chain;
- reparse points anywhere in an Evidence Package tree;
- UNC paths and mapped remote drives;
- Win32 device, extended-length, and related special namespaces;
- relative, drive-relative, volume-less rooted, and ambiguous paths; and
- reuse or overwrite of an existing output root or package file.

The boundary provides `PREEXISTING_REDIRECTION_SAFETY` against pre-existing
redirection and common namespace escape. It does not provide or claim
`FULL_PRIVILEGED_CONCURRENT_TOCTOU_HARDENING` against a privileged process
concurrently changing the filesystem namespace between validation and an
operation.

## Protected operations

The guard is part of the active package paths:

```text
tannang collect --synthetic ... --output <absolute-local-path>
        -> validate existing parent chain
        -> create and revalidate a temporary sibling
        -> validate before every child directory and file creation
        -> generate and verify the manifest in the temporary tree
        -> revalidate source, parent, and absent destination
        -> publish by no-overwrite same-parent rename

tannang verify <absolute-local-package-path>
        -> validate root and scan the complete tree
        -> read the manifest through the guard
        -> revalidate each declared or observed file before opening it
```

New files use no-overwrite creation. Path-safety failures are separate from
Provider compatibility and execution states; the CLI returns its dedicated
path-safety exit code.

## Rejected path classes

On Windows, package roots must be canonical absolute drive-letter paths using
backslash separators. The v0 parser rejects:

- relative and drive-relative paths, including `C:folder`;
- volume-less rooted paths, including `\folder`;
- `.` or `..`, empty components, forward slashes, and non-canonical forms;
- trailing spaces or periods in a component;
- alternate-data-stream-style colons, wildcard characters, and other reserved
  Windows filename characters;
- reserved DOS device names and obvious 8.3 alias forms;
- UNC paths and mapped drives classified as remote; and
- `\\?\`, `\\.\`, NT device, and related special namespaces.

The guard does not trim, clean up, or reinterpret rejected input. Ordinary
case differences remain valid.

## Reparse policy

The v0 allowlist is empty. If `FILE_ATTRIBUTE_REPARSE_POINT` is present on any
protected existing component or any package-tree entry, the operation fails.
This covers symbolic links, junctions, mount-point-style objects, and unknown
reparse types without attempting to classify or follow their targets.

## Output root rules

The caller must explicitly select a canonical absolute local path. Local fixed
and removable storage are allowed. UNC paths, mapped remote drives, device
namespaces, and storage that cannot be safely classified are rejected.

The parent directory must already exist and its complete path chain must be
free of reparse points. The output root must not exist. Tannang creates and
revalidates a temporary sibling under the same parent, then writes only guarded
package-relative children beneath it. After manifest generation and complete
verification, the source tree, parent, and absent final destination are checked
again before a no-overwrite rename publishes the final path. Tannang does not
clear an existing destination, copy the tree for publication, or fall back to
another directory or volume.

## Package verify rules

The input root must be an existing real directory on allowed local storage. Its
ancestor chain, root, all directories, and all files must be free of reparse
points. The verifier scans the tree before reading the manifest and checks each
file again before opening and hashing it. Any reparse point makes the entire
verification fail as a path-safety error, rather than a hash mismatch.

Manifest paths must remain canonical package-relative forward-slash paths.
Absolute paths, volumes, backslashes, empty components, `.` and `..` are
rejected before joining them to the root.

## Known limitations

- This pre-alpha boundary is not production hardened.
- Full resistance to a malicious administrator or `SYSTEM` process racing
  namespace changes is not claimed.
- Checks use path-based Windows APIs and repeat validation around package I/O;
  they are not a handle-relative, race-free filesystem transaction.
- All reparse points are rejected, including objects that a future reviewed
  policy might safely allow.
- Network and device-namespace package storage is unsupported.
- The policy does not establish support for non-Windows platforms.

## Future hardening

Future work may evaluate handle-relative traversal, explicit reparse-tag
diagnostics, and a narrowly reviewed allowlist. Any such change requires its
own threat model and real Windows filesystem tests. It must not weaken the v0
fail-closed behavior by silently following unknown reparse points.

## Microsoft references

- [File Attribute Constants](https://learn.microsoft.com/en-us/windows/win32/fileio/file-attribute-constants)
- [GetFileAttributesW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfileattributesw)
- [GetDriveTypeW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getdrivetypew)
- [Naming Files, Paths, and Namespaces](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file)
- [Reparse Points](https://learn.microsoft.com/en-us/windows/win32/fileio/reparse-points)
- [Hard Links and Junctions](https://learn.microsoft.com/en-us/windows/win32/fileio/hard-links-and-junctions)
- [Symbolic Links](https://learn.microsoft.com/en-us/windows/win32/fileio/symbolic-links)

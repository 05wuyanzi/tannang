# Genesis architecture

Tannang is a Windows-first, platform-extensible evidence orchestration project.
The genesis build contains no Windows collector. It proves only the control
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

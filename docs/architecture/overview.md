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

The v0.x Go module path is `github.com/05wuyanzi/tannang`.

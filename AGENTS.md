# Tannang agent rules

- Read the relevant contracts and tests before writing.
- Treat explicitly scoped paths as an allowlist and do not modify forbidden or
  out-of-scope paths.
- Validate formatting, static checks, tests, and public-content boundaries.
- Fail closed on ambiguous paths, state, integrity, or authorization.
- Do not perform remote writes unless explicitly authorized.
- Do not read real host evidence or add real collection behavior by default;
  synthetic tests must remain synthetic.
- Do not add third-party dependencies, source, or binaries without a separate
  reviewed scope.
- Do not execute third-party software by default.

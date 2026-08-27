# Contributing

Tannang is pre-alpha. Maintainers make the final decision on every proposed
change.

## Contribution rules

Contributions are proposed by Pull Request. Each commit must include Developer
Certificate of Origin sign-off using a `Signed-off-by` trailer. A Contributor
License Agreement and copyright assignment are not required.

Published maintainer commits that already anchor completed independent review
or acceptance evidence are not rewritten solely to add a missing trailer. A
missing historical trailer may be remediated only by a later DCO-signed
maintainer commit that explicitly identifies the exact covered commit SHA(s)
and records the signing maintainer's DCO attestation. This history-preservation
exception is historical-only and maintainer-only; it does not attest unrelated
third-party work or waive the `Signed-off-by` requirement for any new commit.

Copied or adapted third-party material must disclose:

- source URL;
- upstream project;
- license; and
- nature of the adaptation.

Vendored third-party source, unreviewed binaries, opaque generated blobs,
credentials, real case evidence, and malware samples are prohibited by default
and require a separately reviewed scope if they are ever proposed.

AI-assisted contributions are allowed. The contributor must understand the
code, remain responsible for its behavior, test it, and check its sources. Full
prompt history is not required.

Fixtures must remain clearly synthetic and must not contain host evidence,
credentials, personal data, malware, or case material.

Run the development validation script or the equivalent Go commands before
requesting review.

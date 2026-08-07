# Security policy

Status: **pre-alpha**

Tannang is not ready for production or real evidence collection. It is not
production hardened, forensic certified, or judicial grade.

## Reporting a vulnerability

Use GitHub Private Vulnerability Reporting for vulnerabilities in first-party
Tannang code. Do not disclose a suspected vulnerability through a public Issue.

Keep reports focused on this repository. Proofs of concept should use the
embedded synthetic fixtures whenever possible. Do not include real case data,
customer evidence, malware, credentials, or maintainer private contact
information.

Vulnerabilities in an optional external backend should be reported through that
upstream project's security process. No fixed response or remediation SLA is
promised.

Security-sensitive changes must fail closed, preserve explicit authorization
boundaries, avoid network access, and include negative tests.

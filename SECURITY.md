# Security Policy

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Email **hello@bastio.com** with:

- A clear description of the issue
- Steps to reproduce (proof-of-concept code if available)
- The version / commit hash you tested against
- Your assessment of severity and impact

You will get an acknowledgment within **2 business days**. We will keep you
updated on remediation progress.

## Disclosure timeline

- **Day 0**: Report received, acknowledgment sent.
- **Day 1-7**: Initial triage. We confirm reproducibility and assess severity.
- **Day 7-90**: Patch development, internal review, release preparation.
- **Day 90**: Coordinated public disclosure. We aim to ship a fix before this
  date. If a fix is not ready, we will discuss with you whether to extend
  the embargo or proceed with a partial mitigation advisory.

We follow a 90-day coordinated disclosure window aligned with industry norms
(Project Zero, MITRE). For low-severity issues that don't enable RCE / data
exfiltration / authentication bypass, we may publish an advisory before a
patch is merged if the mitigation can be applied via configuration alone.

## Scope

In scope:

- The `bastio` Go binaries (`cmd/server`, `cmd/worker`, `cmd/ingest`)
- The shipped React dashboard
- The official Docker images (`ghcr.io/bastio-ai/bastio`)
- The Helm chart at `deploy/helm/bastio/`
- The published SDKs under `sdk/`

Out of scope:

- Third-party integrations not maintained by us
- Self-hosted deployments where the operator has weakened defaults
  (e.g., disabled TLS, exposed admin endpoints to the internet, used
  default secrets in production)
- Issues in dependencies that have already been disclosed upstream;
  please report those to the dependency's maintainer

## Hall of Fame

Researchers who responsibly disclose security issues are credited in our
release notes. We do not yet run a paid bounty program, but we plan to —
expect that to launch alongside our public Cloud GA.

## PGP

If you prefer encrypted communication, request our current PGP key by
emailing **hello@bastio.com** with the subject `pgp request`.

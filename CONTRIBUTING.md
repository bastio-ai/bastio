# Contributing to Bastio

Thanks for taking the time to contribute. This guide covers how to file issues, submit pull requests, and sign off commits.

## Filing issues

- Bugs, feature requests, and questions: open an issue on [github.com/bastio-ai/bastio](https://github.com/bastio-ai/bastio/issues).
- For security vulnerabilities, do **not** file a public issue. Email hello@bastio.ai.
- Include: what you expected, what happened, steps to reproduce, and your environment (Bastio version, Postgres version, OS).

## Developer setup

See [README.md](README.md) for dev environment setup. Summary: `docker-compose up` for infra, `go run ./cmd/server` for the backend, `cd dashboard && npm run dev` for the frontend.

## Sign your commits (DCO)

All commits must be signed off under the [Developer Certificate of Origin (DCO)](https://developercertificate.org/). The DCO is a lightweight statement that you have the right to submit the code under the project's license. There is no paperwork — you just add a `Signed-off-by:` trailer to each commit message.

The easiest way to do this is with the `-s` flag:

```bash
git commit -s -m "your commit message"
```

This appends a line like:

```
Signed-off-by: Your Name <your.email@example.com>
```

Use your real name (not a pseudonym) and an email you can receive mail at. CI will reject unsigned commits.

If you forget, you can add the sign-off retroactively:

```bash
# Last commit
git commit --amend --signoff

# Multiple commits (interactive rebase)
git rebase --signoff HEAD~N
```

## Pull request process

1. Fork the repo and create a branch from `main`.
2. Make your changes. Keep commits focused and well-described.
3. Ensure tests pass: `go test ./...` (backend), `npm test && npm run typecheck` (dashboard).
4. Sign off every commit (`git commit -s`).
5. Open a pull request. Fill in the PR template.
6. CI must pass before review.

## What we're looking for

- Bug fixes with a regression test
- New threat detectors under `internal/security/detection/`
- New LLM provider integrations under `internal/providers/`
- Dashboard improvements that respect the design tokens used by the dashboard
- Documentation improvements, especially around self-hosting

## What belongs elsewhere

Bastio OSS is intentionally minimal. Features below belong in commercial / managed deployments, not here:

- Authentication, SSO, SAML, OIDC (self-hosters control access via network/VPN)
- Billing, usage metering, Stripe integration
- Multi-tenant organization management beyond single-tenant `customer_id` plumbing
- ML-based threat detection (pattern- and heuristic-based detection is OSS)

If you're unsure, open an issue before investing time.

## License

By contributing, you agree that your contributions will be licensed under:

- [FSL-1.1-ALv2](LICENSE) for code under `cmd/`, `internal/`, `pkg/`, `dashboard/`, `migrations/`, and `deploy/`.
- [MIT](sdk/js/LICENSE) for code under `sdk/`.

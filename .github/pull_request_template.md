## Summary

<!-- What does this PR change and why? -->

## Testing

<!-- How did you verify this works? Include test output, screenshots, or repro steps. -->

## Checklist

- [ ] All commits are signed off with DCO (`git commit -s`) — see [CONTRIBUTING.md](../CONTRIBUTING.md)
- [ ] Tests added or updated (required for bug fixes and new features)
- [ ] `go test ./...` passes locally
- [ ] `npm run typecheck && npm run lint` passes in `dashboard/` (if touched)
- [ ] No new `any` / `interface{}` types introduced
- [ ] No analytics writes go to PostgreSQL (use ClickHouse via the ingest service)
- [ ] Every SQL query includes `customer_id` in the WHERE clause

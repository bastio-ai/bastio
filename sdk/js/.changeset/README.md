# Changesets

Hi! This directory holds [changesets](https://github.com/changesets/changesets)
for the `@bastio/*` SDK packages. A changeset describes a unit of change
— which packages are affected and at what bump level.

## Adding a changeset

From `bastio/sdk/js/`:

```bash
pnpm changeset
```

Pick the affected packages, choose `patch` / `minor` / `major`, and
write a short summary. The tool writes a markdown file into this
directory; commit it along with your PR.

## How releases work

When a PR with changeset files lands on `main`, the `sdk-js` GitHub
Actions workflow opens a "Version Packages" PR that:

1. Consumes the changeset files.
2. Bumps each affected `package.json`.
3. Updates CHANGELOG.md in each package.
4. Pushes git tags.

Merging that PR publishes the bumped packages to npm.

See [`../NOTES.md`](../NOTES.md) for publishing prerequisites (license
review, npm scope bootstrap, secret setup).

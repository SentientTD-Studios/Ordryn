# Contributing

Thanks for contributing to this project.

## Before you start

- Check existing issues and pull requests to avoid duplicate work.
- Open an issue or discuss larger changes before spending significant time on implementation.
- Keep pull requests focused on one change whenever possible.
- Do not include unrelated formatting changes, generated files, or drive-by refactors.

## Branching policy

This repository uses a main-only workflow. The protected `main` branch is the single source of truth.

Create a short-lived branch from the latest `main` branch for every change.

| Branch type | Create from | Pull request target | Purpose |
|---|---|---|---|
| `feat/*` | `main` | `main` | New functionality |
| `fix/*` | `main` | `main` | Bug fixes |
| `chore/*` | `main` | `main` | Maintenance, dependencies, or tooling |
| `docs/*` | `main` | `main` | Documentation updates |
| `hotfix/*` | `main` | `main` | Urgent production fixes; maintainers only |

Examples:

```text
feat/user-notification-settings
fix/invalid-token-error
chore/update-dotnet-sdk
docs/contribution-guidelines
hotfix/1.8.1
```

Delete branches after their pull requests are merged.

## Pull request rules

- All pull requests must target `main`.
- External contributors should work from a fork and target `main`.
- A pull request opened against the wrong target may be asked to retarget and rebase onto the latest `main` before review.
- Pull requests must pass required checks and address review feedback before merging.
- Do not push directly to, force-push to, or delete protected branches.
- Only authorized maintainers may bypass required review rules when necessary.

## Before opening a PR

1. Rebase or merge the latest `main` into your branch.
2. Run the relevant build, tests, formatter, and linter locally.
3. Update tests for behavior changes.
4. Update documentation, configuration examples, or changelog entries where relevant.
5. Write a clear PR description explaining what changed and why.

## Releases

Releases are created from commits already merged into `main`. The git tag is the version; nothing in source is bumped.

1. Ensure the intended changes are merged into `main` and all required checks pass.
2. Create and push an annotated version tag from the release commit, such as `v1.8.0`.
3. Publish the GitHub release from that tag.
4. Release automation builds the Vue distribution and Go server binaries, stamping `version.Version` from the tag via `-ldflags`. Hosts that `git pull` a checkout resolve the same tag at process start (`git describe --tags --abbrev=0`).
5. If a release issue is found afterward, create a new fix branch from `main`, merge it through a pull request, and publish a new version tag.

Do not move or reuse published version tags. Each release should correspond to a specific, reproducible commit.

## Code expectations

- Keep changes small, readable, and testable.
- Prefer clear naming over clever abstractions.
- Add or update tests when changing behavior.
- Do not commit generated build output unless the repository explicitly requires it.
- Do not commit secrets, credentials, local configuration, or private keys.
- Include migration notes when changes affect setup, configuration, APIs, or deployment.

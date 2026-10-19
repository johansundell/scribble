---
name: bump-release
description: Bump version number, run tests, commit and push directly to master, and run make release. Use when asked to bump version, cut a release, publish a release, or /bump-release.
---

# Bump and Release

Workflow for bumping the application version, committing directly to `master`, and publishing a new release via `make release`.

## Prerequisites

1. Working directory must be on `master` branch. In this repo's worktree layout that is the `scribble/master` worktree; a session isolated in another worktree cannot release, so hand the command to the user to run there.
2. Working tree must be clean (no uncommitted or untracked changes).
3. Local `master` must be up to date with `origin/master`.
4. Release tools installed (`make deps` if `github-release`, `gox` or `fyne-cross` are missing).
5. Docker running: `make release` cross-compiles `scribble-gui` with `fyne-cross` in Docker and pushes the `johansundell/scribble-server` image (`docker login` first).
6. GitHub credentials available for `github-release` (e.g., `GITHUB_TOKEN` environment variable).

## Quick Execution

The release workflow is automated via [`scripts/bump-release.sh`](../../../scripts/bump-release.sh) and the `make bump-release` target:

```bash
# Default: bump patch version (e.g. v0.0.8 -> v0.0.9)
make bump-release

# Specify bump level or explicit version:
make bump-release BUMP=minor
make bump-release BUMP=major
make bump-release BUMP=v0.1.0

# Dry-run verification (verifies tests and prints planned actions without pushing or releasing):
make bump-release DRY_RUN=1
```

## Step-by-Step Procedure

If executing manually or troubleshooting, follow these steps in order:

1. **Verify branch and working tree**:
   ```bash
   git checkout master
   git pull origin master
   git status --porcelain
   ```
   *Completion criterion*: Output of `git status --porcelain` is empty, and branch is `master`.

2. **Determine target version**:
   - Inspect `VERSION` in [`Makefile`](../../../Makefile) (e.g. `VERSION := v0.0.8`).
   - Increment the patch segment by default (or minor/major as requested).

3. **Update Makefile**:
   - Edit `VERSION := <new-version>` in `Makefile`.

4. **Verify tests**:
   ```bash
   go test ./...
   ```
   *Completion criterion*: All tests pass. If any test fails, revert `Makefile` and abort.

5. **Commit and push directly to master**:
   ```bash
   git add Makefile
   git commit -m "chore: bump version to <new-version>"
   git push origin master
   ```
   *Completion criterion*: Commit is pushed to `origin/master`. Note: `AGENTS.md` explicitly permits direct commits to `master` for release version bumps.

6. **Execute release**:
   ```bash
   make release
   ```
   *Completion criterion*: `make release` builds cross-platform binaries (server, CLI and GUI), generates checksums, pushes the Docker image, publishes the GitHub Release on `johansundell/scribble` with notes via `github-release`, and pulls the created release tag.

7. **Verify deployment**:
   - Check GitHub release: `gh release view <new-version>`
   - Publishing the release triggers the `Deploy` workflow (`.github/workflows/deploy.yml`), which installs it on the VPS (`docs/deploy-vps.md`). Check its run under Actions; the log ends with `deployed <new-version>`.

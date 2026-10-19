#!/usr/bin/env bash
# Bump version in Makefile, run tests, commit and push to master, and run make release.
#
# Usage:
#   ./scripts/bump-release.sh [patch|minor|major|vX.Y.Z] [--dry-run]
#   make bump-release [BUMP=patch|minor|major|vX.Y.Z] [DRY_RUN=1]
set -euo pipefail

DRY_RUN="${DRY_RUN:-0}"
if [[ "$DRY_RUN" == "true" ]]; then
	DRY_RUN=1
fi
BUMP_ARG=""

while [[ $# -gt 0 ]]; do
	case "$1" in
		--dry-run|-n)
			DRY_RUN=1
			shift
			;;
		--help|-h)
			echo "Usage: $0 [patch|minor|major|vX.Y.Z] [--dry-run]"
			echo "       make bump-release [BUMP=patch|minor|major|vX.Y.Z] [DRY_RUN=1]"
			exit 0
			;;
		-*)
			echo "Error: unknown option: $1" >&2
			exit 2
			;;
		*)
			if [[ -z "$BUMP_ARG" ]]; then
				BUMP_ARG="$1"
			else
				echo "Error: unexpected extra argument: $1" >&2
				exit 2
			fi
			shift
			;;
	esac
done

if [[ -z "$BUMP_ARG" ]]; then
	BUMP_ARG="${BUMP:-patch}"
fi

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$REPO_ROOT" ]]; then
	echo "Error: not inside a git repository." >&2
	exit 1
fi
cd "$REPO_ROOT"

if [[ -n "$(git status --porcelain)" ]]; then
	if [[ "$DRY_RUN" == "1" ]]; then
		echo "Warning: working directory has uncommitted or untracked changes (dry run mode)." >&2
	else
		echo "Error: working directory has uncommitted or untracked changes." >&2
		echo "Please commit or stash your changes before releasing." >&2
		git status --short >&2
		exit 1
	fi
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$CURRENT_BRANCH" != "master" ]]; then
	if [[ "$DRY_RUN" == "1" ]]; then
		echo "Warning: on branch '$CURRENT_BRANCH' (dry run mode permits non-master branches)." >&2
	else
		echo "Error: must be on branch 'master' to bump and release (current: '$CURRENT_BRANCH')." >&2
		exit 1
	fi
fi

if [[ ! -f Makefile ]]; then
	echo "Error: Makefile not found in repository root." >&2
	exit 1
fi

CURRENT_VERSION="$(grep -E '^VERSION :=' Makefile | awk '{print $3}')"
if [[ -z "$CURRENT_VERSION" ]]; then
	echo "Error: could not find VERSION in Makefile." >&2
	exit 1
fi

if [[ ! "$CURRENT_VERSION" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
	echo "Error: current version '$CURRENT_VERSION' in Makefile does not match vMAJOR.MINOR.PATCH format." >&2
	exit 1
fi

MAJOR="${BASH_REMATCH[1]}"
MINOR="${BASH_REMATCH[2]}"
PATCH="${BASH_REMATCH[3]}"

case "$BUMP_ARG" in
	patch)
		NEW_VERSION="v${MAJOR}.${MINOR}.$((PATCH + 1))"
		;;
	minor)
		NEW_VERSION="v${MAJOR}.$((MINOR + 1)).0"
		;;
	major)
		NEW_VERSION="v$((MAJOR + 1)).0.0"
		;;
	v[0-9]*.[0-9]*.[0-9]*)
		NEW_VERSION="$BUMP_ARG"
		;;
	[0-9]*.[0-9]*.[0-9]*)
		NEW_VERSION="v$BUMP_ARG"
		;;
	*)
		echo "Error: invalid bump argument '$BUMP_ARG'." >&2
		echo "Expected: patch, minor, major, or a version string (e.g. v0.0.14 or 0.0.14)" >&2
		exit 2
		;;
esac

if [[ "$NEW_VERSION" == "$CURRENT_VERSION" ]]; then
	echo "Error: new version ($NEW_VERSION) is identical to current version ($CURRENT_VERSION)." >&2
	exit 1
fi

if git rev-parse -q --verify "refs/tags/$NEW_VERSION" >/dev/null 2>&1; then
	echo "Error: git tag '$NEW_VERSION' already exists locally." >&2
	exit 1
fi

if [[ "$DRY_RUN" == "1" ]]; then
	echo "Running tests in dry run mode (go test ./...)..."
	go test ./...
	echo ""
	echo "[DRY RUN] Verification successful."
	echo "[DRY RUN] Current version: $CURRENT_VERSION"
	echo "[DRY RUN] Target version:  $NEW_VERSION"
	echo "[DRY RUN] Actions that would be performed on master:"
	echo "[DRY RUN]   1. git pull origin master"
	echo "[DRY RUN]   2. Update Makefile (VERSION := $NEW_VERSION)"
	echo "[DRY RUN]   3. Run go test ./..."
	echo "[DRY RUN]   4. git add Makefile"
	echo "[DRY RUN]   5. git commit -m \"chore: bump version to $NEW_VERSION\""
	echo "[DRY RUN]   6. git push origin master"
	echo "[DRY RUN]   7. make release"
	exit 0
fi

echo "Pulling latest changes from origin/master..."
git pull origin master

echo "Bumping version from $CURRENT_VERSION to $NEW_VERSION in Makefile..."
sed -i -E "s/^(VERSION := ).*/\1$NEW_VERSION/" Makefile

echo "Running tests (go test ./...)..."
if ! go test ./...; then
	echo "Error: tests failed. Reverting Makefile changes..." >&2
	git checkout -- Makefile
	exit 1
fi
echo "Tests passed."

git add Makefile
git commit -m "chore: bump version to $NEW_VERSION"
echo "Pushing commit to origin master..."
git push origin master

echo "Running make release..."
make release

echo ""
echo "Successfully bumped and released $NEW_VERSION!"

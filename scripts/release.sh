#!/usr/bin/env bash
# Makes a release of Transferly: ./scripts/release.sh X.Y.Z[-prerelease] [--dry-run]
#
# On a clean main that matches origin/main, it takes the notes of the version from CHANGELOG.md, or writes them from the
# Conventional Commits since the last tag when the changelog has no section for it, sets the version of the frontend,
# commits "chore(release): vX.Y.Z", tags it with an annotated tag and pushes both through scripts/push.sh, which runs the
# full suites first. With --dry-run it only prints the notes. See docs/guides/releases.md.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

version="${1:-}"
dry_run="${2:-}"
if ! [[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]; then
  echo "usage: scripts/release.sh X.Y.Z[-prerelease] [--dry-run]" >&2
  exit 2
fi
if [[ "$version" == *-* ]]; then
  IFS='.' read -r -a prerelease_parts <<< "${version#*-}"
  for part in "${prerelease_parts[@]}"; do
    if [[ "$part" =~ ^[0-9]+$ && "$part" != "0" && "$part" == 0* ]]; then
      echo "release: numeric prerelease identifiers cannot start with zero" >&2
      exit 2
    fi
  done
fi
tag="v$version"

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  echo "release: $tag already exists" >&2
  exit 1
fi
last="$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)"
if [ -n "$last" ]; then
  if ! python3 - "${last#v}" "$version" <<'PYSEMVER'
import sys


def precedence(version):
    core, separator, prerelease = version.partition("-")
    identifiers = tuple((0, int(part)) if part.isdigit() else (1, part) for part in prerelease.split(".")) if separator else ()
    return tuple(map(int, core.split("."))), not bool(separator), identifiers


sys.exit(0 if precedence(sys.argv[2]) > precedence(sys.argv[1]) else 1)
PYSEMVER
  then
    echo "release: $tag is not newer than $last" >&2
    exit 1
  fi
fi

# section prints the notes CHANGELOG.md has for a version, without its heading.
section() {
  awk -v want="## [$1]" 'index($0, want) == 1 { found = 1; next } found && /^## \[/ { exit } found { print }' CHANGELOG.md
}

# generated prints notes from the Conventional Commits since the last tag: breaking changes, features and fixes.
generated() {
  local range="${last:+$last..}HEAD"
  local breaking added fixed
  breaking="$(git log --format='%s%n%b%x00' "$range" | tr '\0' '\n' | grep -E '^[a-z]+(\([^)]*\))?!:|^BREAKING CHANGE:' | sed -E 's/^[a-z]+(\([^)]*\))?!: //; s/^BREAKING CHANGE: //; s/^/- /' || true)"
  added="$(git log --format='%s' "$range" | grep -E '^feat(\([^)]*\))?: ' | sed -E 's/^feat(\(([^)]*)\))?: /- \2: /; s/^- : /- /' || true)"
  fixed="$(git log --format='%s' "$range" | grep -E '^fix(\([^)]*\))?: ' | sed -E 's/^fix(\(([^)]*)\))?: /- \2: /; s/^- : /- /' || true)"
  [ -n "$breaking" ] && printf '\n### Breaking changes\n\n%s\n' "$breaking"
  [ -n "$added" ] && printf '\n### Added\n\n%s\n' "$added"
  [ -n "$fixed" ] && printf '\n### Fixed\n\n%s\n' "$fixed"
  return 0
}

notes="$(section "$version")"
written=false
if [ -z "$notes" ]; then
  notes="$(generated)"
  written=true
fi
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "release: nothing to release since ${last:-the first commit}" >&2
  exit 1
fi

echo "Notes of $tag:"
printf '%s\n' "$notes"
if [ "$dry_run" = "--dry-run" ]; then
  exit 0
fi

if [ "$(git branch --show-current)" != "main" ]; then
  echo "release: releases are made from main" >&2
  exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
  echo "release: the working tree has changes; commit or stash them first" >&2
  exit 1
fi
git fetch -q origin main
if [ "$(git rev-parse HEAD)" != "$(git rev-parse origin/main)" ]; then
  echo "release: main differs from origin/main; push or pull first" >&2
  exit 1
fi

if [ "$written" = true ]; then
  heading="## [$version] - $(date -u +%Y-%m-%d)"
  awk -v heading="$heading" -v notes="$notes" 'BEGIN { done = 0 } !done && /^## \[/ { print heading; print notes; print ""; done = 1 } { print } END { if (!done) { print heading; print notes } }' CHANGELOG.md >CHANGELOG.md.next
  mv CHANGELOG.md.next CHANGELOG.md
fi
sed -i -E "0,/\"version\": \"[^\"]*\"/s//\"version\": \"$version\"/" frontend/package.json

git add CHANGELOG.md frontend/package.json
if ! git diff --cached --quiet; then
  git commit -q -m "chore(release): $tag"
fi
git tag -a "$tag" -m "Transferly $tag" -m "$notes"
echo "release: tagged $tag; pushing main and the tag after the full suites"
if ! ./scripts/push.sh origin main "$tag"; then
  echo "release: the push failed; to undo locally: git tag -d $tag && git reset --soft HEAD~1 (if a release commit was made)" >&2
  exit 1
fi
echo "release: $tag is out. Build its image with: TRANSFERLY_VERSION=$tag docker compose build"

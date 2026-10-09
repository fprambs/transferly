# Releases

Transferly is released from `main` with annotated tags `vX.Y.Z` or prerelease tags such as `v1.0.0-rc.1`, following [Semantic Versioning](https://semver.org/). After the gates pass,
CI publishes the matching version and commit image to GHCR. Images from main carry the `main` tag; stable deployments use a release tag
or its immutable digest. The release workflow refuses to replace an existing release image and checks the frontend version against its tag.

## Versions

- While the major version is 0, a minor version (0.6.0) may add features and change behavior or the API, and a patch version (0.5.1)
  only fixes.
- 1.0.0 declares Transferly stable; from then on, breaking changes need a new major version.
- CI builds from main report their commit SHA; local builds default to `dev`.

## Where the version shows

- The binary: `transferly version`, and the `server listening` log line. The image build sets it from its `VERSION` argument with
  `-ldflags -X github.com/fprambs/transferly/internal/version.Version=…`, and labels the image `org.opencontainers.image.version`.
- The API: `GET /api/v1/version`, for signed-in people only, so anonymous visitors cannot read it.
- The interface: the foot of the navigation of Settings.
- `frontend/package.json`, which the release script keeps equal to the tag.

## Making a release

```sh
./scripts/release.sh 0.6.0 --dry-run   # prints the notes
./scripts/release.sh 0.6.0             # commits, tags and pushes
TRANSFERLY_VERSION=v0.6.0 docker compose build
```

The script:

1. Refuses a version that is not newer than the last tag, a branch other than `main`, a working tree with changes, and a `main` that
   differs from `origin/main`.
2. Takes the notes of the version from `CHANGELOG.md` when it has a section `## [X.Y.Z]`. Otherwise it writes them from the
   Conventional Commits since the last tag: commits with `!` or a `BREAKING CHANGE:` footer, `feat` and `fix`. Write the section by
   hand before the release when the notes need more care.
3. Sets the version of `frontend/package.json` and commits `chore(release): vX.Y.Z`.
4. Makes the annotated tag with the notes and pushes `main` and the tag through `scripts/push.sh`, which runs the full suites first.
   When they fail, nothing is pushed, and the script prints how to undo the tag and the commit.


## Candidate validation

Branches named `release/**` run the complete CI gates without publishing images. Freeze the candidate version and commit on that branch, verify the run and repository protections, then integrate it into main before creating a release tag. Numeric prerelease identifiers cannot have leading zeroes. `scripts/release.sh 1.0.0-rc.1 --dry-run` prints candidate notes without creating commits, tags or pushes.

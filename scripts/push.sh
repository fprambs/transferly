#!/bin/sh
# Runs the full coverage suites, then pushes. Use this instead of plain `git push`.
#
# Git opens the SSH session to GitHub before the pre-push hook runs. A long test run
# leaves that session idle and GitHub closes it, so the pack never uploads. Tests run
# here first; the hook only checks that they already passed.
set -eu

cd "$(git rev-parse --show-toplevel)"

docker compose -f compose.dev.yaml run --rm -T tools sh -c './scripts/coverage-go.sh && cd frontend && pnpm test:coverage'

export TRANSFERLY_PUSH_TESTS_DONE=1
export GIT_SSH_COMMAND="${GIT_SSH_COMMAND:-ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=30}"
exec git push "$@"

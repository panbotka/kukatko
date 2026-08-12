#!/usr/bin/env bash
#
# Fails if the working tree carries uncommitted changes.
#
# Run as the last goreleaser before-hook. goreleaser validates the git state in
# its own pipeline *before* those hooks run, so anything a hook leaves behind is
# invisible to it and ships anyway: the frontend build used to delete the tracked
# internal/web/static/dist/.gitkeep, and Go's VCS stamping turned that into
# `github.com/panbotka/kukatko v0.8.0+dirty` on every published binary.
#
# The suffix is the only provenance signal a binary carries, so a build that
# cannot honestly claim to match its tag must stop, not publish. Run it by hand
# too (`./scripts/assert-clean-tree.sh`) to check what a release would see.
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -n "$(git status --porcelain)" ]]; then
  echo "assert-clean-tree: the working tree is dirty — a binary built from it" >&2
  echo "would be stamped +dirty and would not match its tag." >&2
  echo >&2
  git status --porcelain >&2
  exit 1
fi

echo "assert-clean-tree: working tree clean"

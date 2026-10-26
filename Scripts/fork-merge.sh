#!/usr/bin/env bash
# Merge a fetched upstream ref; only the fork's explicit config overlay is automatic.
set -euo pipefail
upstream="${1:-upstream/main}"
if [[ -n "$(git status --porcelain)" ]]; then
  echo 'Fork merge requires a clean working tree' >&2
  exit 1
fi
if git merge-base --is-ancestor "$upstream" HEAD; then
  exit 0
fi
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
git show HEAD:mygo.json > "$scratch/fork.json"
git show "$upstream:mygo.json" > "$scratch/upstream.json"
# Do not create a commit before the config overlay has been applied.
if ! git merge --no-commit --no-ff "$upstream"; then
  conflicts="$(git diff --name-only --diff-filter=U)"
  if [[ "$conflicts" != mygo.json ]]; then
    git merge --abort
    {
      echo '### Upstream merge requires review'
      echo
      while IFS= read -r path; do
        [[ -z "$path" ]] || printf -- '- `%s`\n' "$path"
      done <<< "$conflicts"
      echo
      echo 'See docs/fork.md; source conflicts are never resolved by choosing one side automatically.'
    } | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
    exit 1
  fi
  git checkout --theirs -- mygo.json
fi
if ! python3 Scripts/fork-sync-config.py "$scratch/fork.json" "$scratch/upstream.json"; then
  git merge --abort
  exit 1
fi
git add mygo.json
git commit --no-edit

#!/usr/bin/env bash
set -euo pipefail

# Mirror this repository to its GitHub home (github.com/gradii/<module>) from the
# CNB pipeline. Two things are published, and only from main:
#
#   bash .cnb/mirror-github.sh branch   main, plus every version tag it has
#   bash .cnb/mirror-github.sh tag      the tag that triggered this run
#
# The repository identifies itself through the module path in go.mod, so this
# script is identical in all five repositories. A main push republishes the
# existing tags as well, which is what seeds a new GitHub repository with the
# history its branch did not carry.
mode=${1:-}
case "$mode" in
  branch|tag) ;;
  *) printf 'Usage: %s branch|tag\n' "${BASH_SOURCE[0]}" >&2; exit 2 ;;
esac

# The Gradii module line starts here. Older tags are upstream release history
# and point at commits that predate the native-contract ports, so they are never
# published: nobody may pin a version of these modules that lacks the ports.
first_tag=v1.20.0

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
source_root=$(cd "$script_dir/.." && pwd)
module_name=$(awk '$1 == "module" { print $2; exit }' "$source_root/go.mod")
self=${module_name#github.com/gradii/}
case "$self" in framework|mysql|postgres|sqlite|sqlserver) ;; *)
  printf 'Unsupported module for the GitHub mirror: %s\n' "$module_name" >&2
  exit 1
esac

# vMAJOR.MINOR.PATCH as one comparable number.
version_number() {
  printf '%s\n' "$1" | awk '{ v=$0; sub(/^v/, "", v); split(v, p, "."); printf "%d\n", p[1]*1000000 + p[2]*1000 + p[3] }'
}

# One token per repository, all of them provided by the shared secret file the
# pipeline jobs import.
token_name="DEPLOY_GITHUB_GRADII_$(printf '%s' "$self" | tr '[:lower:]' '[:upper:]')_TOKEN"
token=${!token_name:-}
if [[ -z "$token" ]]; then
  printf '%s is not configured; skipping the GitHub mirror.\n' "$token_name"
  exit 0
fi

remote="https://x-access-token:${token}@github.com/gradii/${self}.git"

# CNB checks out a shallow clone, and GitHub refuses a push from one.
if [[ -f "$(git rev-parse --git-dir)/shallow" ]]; then
  git fetch --unshallow origin || true
fi

# A tag push checks out the tag, not a branch, so the local tag set is not
# guaranteed either way; fetch the version tags the CNB repository has.
if [[ -n "$(git ls-remote --tags origin 'refs/tags/v*' 2>/dev/null)" ]]; then
  git fetch --force origin 'refs/tags/v*:refs/tags/v*'
fi

case "$mode" in
  branch)
    if [[ "${CNB_BRANCH:-}" != main ]]; then
      printf 'This pipeline publishes main only; CNB_BRANCH=%s\n' "${CNB_BRANCH:-}" >&2
      exit 1
    fi
    refspecs="HEAD:refs/heads/main"
    published=0
    while read -r tag; do
      [[ -n "$tag" ]] || continue
      if [[ $(version_number "$tag") -lt $(version_number "$first_tag") ]]; then
        printf 'Skipping %s: the first published tag is %s.\n' "$tag" "$first_tag"
        continue
      fi
      refspecs="$refspecs refs/tags/${tag}:refs/tags/${tag}"
      published=$((published + 1))
    done < <(git tag --list 'v*' | sort -V)
    printf 'Mirroring main and %s version tag(s) to github.com/gradii/%s\n' "$published" "$self"
    git push --force "$remote" $refspecs
    ;;
  tag)
    tag=${CNB_BRANCH:-}
    [[ -n "$tag" ]] || { printf 'CNB_BRANCH is empty; no tag to publish.\n' >&2; exit 1; }
    if [[ $(version_number "$tag") -lt $(version_number "$first_tag") ]]; then
      printf 'Skipping %s: the first published tag is %s.\n' "$tag" "$first_tag"
      exit 0
    fi
    if ! git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
      git fetch --force origin "refs/tags/${tag}:refs/tags/${tag}" || true
    fi
    git rev-parse -q --verify "refs/tags/${tag}" >/dev/null || {
      printf 'Tag %s is not reachable from this checkout; nothing to publish.\n' "$tag" >&2
      exit 1
    }
    printf 'Mirroring tag %s to github.com/gradii/%s\n' "$tag" "$self"
    git push --force "$remote" "refs/tags/${tag}:refs/tags/${tag}"
    ;;
esac

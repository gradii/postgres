#!/usr/bin/env bash
set -euo pipefail

# The same CI helper lives in the framework and each driver repository. The
# current repository always uses CNB's checkout, including a pull-request merge.
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
source_root=$(cd "$script_dir/.." && pwd)
module_name=$(awk '$1 == "module" { print $2; exit }' "$source_root/go.mod")
self=${module_name#github.com/gradii/}
case "$self" in framework|mysql|postgres|sqlite|sqlserver) ;; *)
  printf 'Unsupported CI module: %s\n' "$module_name" >&2
  exit 1
esac

export GIT_TERMINAL_PROMPT=0
export GOWORK=off
workspace_root=$(mktemp -d "${TMPDIR:-/tmp}/gradii-cnb.XXXXXX")
trap 'rm -rf "$workspace_root"' EXIT

fail() { printf '%s\n' "$*" >&2; exit 1; }

repository_config() {
  local name=$1 key url_key ref_key
  key=$(printf '%s' "$name" | tr '[:lower:]' '[:upper:]')
  url_key="GRADII_${key}_URL"
  ref_key="GRADII_${key}_REF"
  repository_url=${!url_key:-}
  repository_ref=${!ref_key:-}
  if [[ "$name" == framework && -z "$repository_url" ]]; then
    repository_url=https://cnb.cool/gradii/framework
  fi
  [[ -n "$repository_url" ]] || fail "Set $url_key to the repository containing github.com/gradii/$name; no published driver fork is assumed."
  [[ -n "$repository_ref" ]] || fail "Set $ref_key to a compatible branch, tag, or commit for github.com/gradii/$name."
  [[ "$repository_url" != -* && "$repository_ref" != -* ]] || fail "Repository URLs and refs must not begin with '-'."
}

checkout_module() {
  local name=$1
  if [[ "$name" == "$self" ]]; then
    checkout_path=$source_root
  else
    repository_config "$name"
    checkout_path="$workspace_root/$name"
    if [[ ! -d "$checkout_path/.git" ]]; then
      printf 'Checking out %s at %s\n' "$name" "$repository_ref"
      git init --quiet "$checkout_path"
      git -C "$checkout_path" remote add origin "$repository_url"
      git -C "$checkout_path" fetch --no-tags --depth 1 origin "$repository_ref"
      git -C "$checkout_path" checkout --quiet --detach FETCH_HEAD
    fi
  fi
  [[ -f "$checkout_path/go.mod" ]] || fail "Missing $name/go.mod at the configured revision."
  local actual
  actual=$(awk '$1 == "module" { print $2; exit }' "$checkout_path/go.mod")
  [[ "$actual" == "github.com/gradii/$name" ]] || fail "Expected github.com/gradii/$name, found $actual; configure a ref containing the driver migration."
}

prepare_workspace() {
  local include_tests=$1 name
  # Validate all configuration before starting any network fetches.
  for name in framework mysql postgres sqlite sqlserver; do
    [[ "$name" == "$self" ]] || repository_config "$name"
  done
  module_paths=()
  for name in framework mysql postgres sqlite sqlserver; do
    checkout_module "$name"
    module_paths+=("$checkout_path")
    if [[ "$name" == framework ]]; then
      framework_path=$checkout_path
    fi
  done
  if [[ "$include_tests" == yes ]]; then
    [[ -f "$framework_path/tests/go.mod" ]] || fail 'Missing the framework/tests integration module.'
    module_paths+=("$framework_path/tests")
  fi
  (
    cd "$workspace_root"
    go work init "${module_paths[@]}"
  )
  export GOWORK="$workspace_root/go.work"
  # These working modules have not necessarily been released under their new
  # names. Override their required versions in the generated CI workspace.
  local dependency version module_path
  for module_path in "${module_paths[@]}"; do
    dependency=$(awk '$1 == "module" { print $2; exit }' "$module_path/go.mod")
    for version in $(awk -v dependency="$dependency" '$1 == dependency && $2 ~ /^v[0-9]/ {print $2}' "${module_paths[@]/%//go.mod}" | sort -u); do
      go work edit "-replace=$dependency@$version=$module_path"
    done
  done
}

require_docker() {
  command -v docker >/dev/null || fail 'This suite requires the CNB docker service and Docker CLI.'
  docker info >/dev/null || fail 'The CNB Docker daemon is unavailable; the database suite cannot run.'
}


case "${1:-}" in
  unit)
    prepare_workspace no
    for path in "${module_paths[@]}"; do
      printf 'Running module tests: %s\n' "$path"
      (cd "$path" && go test -count=1 -timeout 10m ./...)
    done
    ;;
  database)
    prepare_workspace no
    (cd "$framework_path" && go test -count=1 -timeout 10m ./database/...)
    ;;
  integration)
    require_docker
    prepare_workspace yes
    (cd "$framework_path/tests" && go test -v -count=1 -timeout 25m ./...)
    ;;
  *)
    fail 'Usage: bash .cnb/ci.sh {unit|database|integration}'
    ;;
esac

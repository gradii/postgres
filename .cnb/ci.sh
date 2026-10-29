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
ci_mode=${1:-}
workspace_root=$(mktemp -d "${TMPDIR:-/tmp}/gradii-cnb.XXXXXX")

# A file named *.go.test is checked in uncompiled on purpose: it needs a driver module that this
# repository's go.mod must not require, so renaming it into place is the only way to run it, and
# only the modes that build the workspace holding those modules can.
specified_tests_renamed=()
rename_specified_tests_in() {
  local path
  while IFS= read -r path; do
    [[ -z "$path" ]] && continue
    mv "$path" "${path%.test}"
    specified_tests_renamed+=("${path%.test}")
  done < <(find "$source_root" -name '*.go.test' -print 2>/dev/null)
  if [[ ${#specified_tests_renamed[@]} -gt 0 ]]; then
    printf 'Renamed %d specified test file(s) into place\n' "${#specified_tests_renamed[@]}"
  fi
}

rename_specified_tests_out() {
  local path
  for path in ${specified_tests_renamed[@]+"${specified_tests_renamed[@]}"}; do
    mv "$path" "$path.test"
  done
}

cleanup() {
  rename_specified_tests_out
  # The `workspace` mode hands the directory to its caller, so it survives this run.
  [[ "$ci_mode" == workspace ]] || rm -rf "$workspace_root"
}

trap cleanup EXIT

fail() { printf '%s\n' "$*" >&2; exit 1; }

# Every module of this workspace lives at cnb.cool/gradii/<name>, which is also
# its default clone URL: GRADII_<NAME>_URL only has to be set to point somewhere
# else. The GitHub home, github.com/gradii/<name>, is written by the mirror
# pipeline in .cnb/mirror.yml and never read here. The ref stays explicit so that
# a run pins the revision it tested, except for the framework: every driver
# pipeline needs it, so an unset GRADII_FRAMEWORK_REF follows the default branch
# of its home repository instead of failing.
repository_config() {
  local name=$1 key url_key ref_key
  key=$(printf '%s' "$name" | tr '[:lower:]' '[:upper:]')
  url_key="GRADII_${key}_URL"
  ref_key="GRADII_${key}_REF"
  repository_url=${!url_key:-https://cnb.cool/gradii/$name}
  repository_ref=${!ref_key:-}
  [[ -n "$repository_url" ]] || fail "Set $url_key to the repository containing github.com/gradii/$name."
  [[ "$repository_url" != -* ]] || fail "Repository URLs must not begin with '-'."
  if [[ -n "$repository_ref" ]]; then
    [[ "$repository_ref" != -* ]] || fail "Repository refs must not begin with '-'."
  elif [[ "$name" != framework ]]; then
    fail "Set $ref_key to a compatible branch, tag, or commit for github.com/gradii/$name."
  fi
}

checkout_module() {
  local name=$1
  if [[ "$name" == "$self" ]]; then
    checkout_path=$source_root
  else
    repository_config "$name"
    checkout_path="$workspace_root/$name"
    if [[ ! -d "$checkout_path/.git" ]]; then
      if [[ -z "$repository_ref" ]]; then
        local key
        key=$(printf '%s' "$name" | tr '[:lower:]' '[:upper:]')
        repository_ref=$(git ls-remote --symref "$repository_url" HEAD 2>/dev/null | awk '/^ref:/{ sub("^refs/heads/", "", $2); print $2; exit }')
        [[ -n "$repository_ref" ]] || fail "Cannot resolve the default branch of $repository_url; set GRADII_${key}_REF."
        printf 'GRADII_%s_REF is not set; following the default branch %s\n' "$key" "$repository_ref"
      fi
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
  specified)
    # Runs the *.go.test files and nothing else, after the ordinary suites: the rename is
    # scoped to this mode, so no other mode, and no go mod tidy, ever sees those files.
    prepare_workspace no
    rename_specified_tests_in
    if [[ ${#specified_tests_renamed[@]} -eq 0 ]]; then
      printf 'No specified test files (*.go.test) in this checkout\n'
      break
    fi
    # The file list form (`go test file_test.go`) would build those files as a throwaway
    # command-line-arguments package, which cannot import an internal package, so the
    # specified tests are selected by name inside their own package instead.
    specified_dirs=()
    specified_names=()
    for specified_path in ${specified_tests_renamed[@]+"${specified_tests_renamed[@]}"}; do
      specified_dir=$(dirname "$specified_path")
      specified_file_names=$(grep -hoE '^func Test[A-Za-z0-9_]*' "$specified_path" | awk '{print $2}' | paste -sd'|' -)
      specified_index=-1
      specified_i=0
      while [[ $specified_i -lt ${#specified_dirs[@]} ]]; do
        [[ "${specified_dirs[$specified_i]}" == "$specified_dir" ]] && specified_index=$specified_i
        specified_i=$((specified_i + 1))
      done
      if [[ $specified_index -ge 0 ]]; then
        if [[ -n "$specified_file_names" ]]; then
          if [[ -n "${specified_names[$specified_index]}" ]]; then
            specified_names[$specified_index]="${specified_names[$specified_index]}|$specified_file_names"
          else
            specified_names[$specified_index]="$specified_file_names"
          fi
        fi
      else
        specified_dirs+=("$specified_dir")
        specified_names+=("$specified_file_names")
      fi
    done
    specified_i=0
    while [[ $specified_i -lt ${#specified_dirs[@]} ]]; do
      if [[ -n "${specified_names[$specified_i]}" ]]; then
        printf 'Running the specified tests of %s: %s\n' "${specified_dirs[$specified_i]}" "${specified_names[$specified_i]}"
        (cd "${specified_dirs[$specified_i]}" && go test -count=1 -timeout 10m -run "^(${specified_names[$specified_i]})$" .)
      else
        printf 'Running the specified tests of %s (whole package)\n' "${specified_dirs[$specified_i]}"
        (cd "${specified_dirs[$specified_i]}" && go test -count=1 -timeout 10m .)
      fi
      specified_i=$((specified_i + 1))
    done
    ;;
  workspace)
    # Build the workspace and keep it, printing GOWORK for the caller to export
    # before running its own tools (lint, govulncheck) inside it.
    prepare_workspace no
    printf 'GOWORK=%s\n' "$GOWORK"
    ;;
  *)
    fail 'Usage: bash .cnb/ci.sh {unit|database|integration|specified|workspace}'
    ;;
esac

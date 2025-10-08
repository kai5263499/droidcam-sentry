#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
backend_dir="${repo_root}/backend"

if [[ ! -d "${backend_dir}" ]]; then
  echo "backend directory not found at ${backend_dir}" >&2
  exit 1
fi

mod_cache="${GOLANGCI_MOD_CACHE:-${HOME}/go/pkg/mod}"
build_cache="${GOLANGCI_BUILD_CACHE:-${HOME}/.cache/go-build}"

mkdir -p "${mod_cache}" "${build_cache}"

docker run --rm \
  -v "${backend_dir}":/workspace \
  -v "${mod_cache}":/go/pkg/mod \
  -v "${build_cache}":/root/.cache/go-build \
  -w /workspace \
  golangci/golangci-lint:latest \
  golangci-lint run "$@"

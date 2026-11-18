#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  linux)
    sudo apt-get update
    sudo apt-get install -y clang cmake ninja-build libsqlite3-dev libssl-dev pkg-config python3
    ;;
  macos)
    brew install ninja openssl@3 sqlite
    ;;
  *)
    echo "usage: $0 linux|macos" >&2
    exit 2
    ;;
esac

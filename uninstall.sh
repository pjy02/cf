#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  printf '请使用 sudo bash uninstall.sh，或直接运行 sudo cfsync uninstall\n' >&2
  exit 1
fi

if ! command -v cfsync >/dev/null 2>&1; then
  printf '未找到 cfsync，请手动检查 /usr/local/bin/cfsync。\n' >&2
  exit 1
fi

exec cfsync uninstall </dev/tty >/dev/tty 2>&1


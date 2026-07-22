#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  printf '请使用 sudo bash uninstall.sh，或直接运行 sudo cf uninstall\n' >&2
  exit 1
fi

if ! command -v cf >/dev/null 2>&1; then
  printf '未找到 cf，请手动检查 /usr/local/bin/cf。\n' >&2
  exit 1
fi

exec cf uninstall </dev/tty >/dev/tty 2>&1

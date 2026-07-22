#!/usr/bin/env bash
set -Eeuo pipefail

REPO="pjy02/cf"
INSTALL_PATH="/usr/local/bin/cf"
COMPAT_PATH="/usr/local/bin/cfsync"
TMP_DIR=""

cleanup() {
  if [[ -n "${TMP_DIR}" && -d "${TMP_DIR}" ]]; then
    rm -rf -- "${TMP_DIR}"
  fi
}
trap cleanup EXIT

die() {
  printf '安装失败：%s\n' "$*" >&2
  exit 1
}

[[ "$(uname -s)" == "Linux" ]] || die "目前只支持 Linux"
[[ "${EUID}" -eq 0 ]] || die "请使用：curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo bash"

for command in curl tar sha256sum install; do
  command -v "${command}" >/dev/null 2>&1 || die "缺少命令：${command}"
done

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "不支持的 CPU 架构：$(uname -m)" ;;
esac

VERSION="${CFSYNC_VERSION:-}"
if [[ -z "${VERSION}" ]]; then
  LATEST_URL="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
  TAG="${LATEST_URL##*/}"
  [[ "${TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "无法确定最新版本，请确认 GitHub Release 已发布"
  VERSION="${TAG#v}"
else
  VERSION="${VERSION#v}"
  [[ "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "CFSYNC_VERSION 必须是 1.2.3 格式"
  TAG="v${VERSION}"
fi

ASSET="cf_${VERSION}_linux_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
TMP_DIR="$(mktemp -d)"

printf '正在下载 cf %s (%s)...\n' "${VERSION}" "${ARCH}"
curl -fL --retry 3 --connect-timeout 15 -o "${TMP_DIR}/${ASSET}" "${BASE_URL}/${ASSET}"
curl -fL --retry 3 --connect-timeout 15 -o "${TMP_DIR}/checksums.txt" "${BASE_URL}/checksums.txt"

(
  cd "${TMP_DIR}"
  grep -E "^[0-9a-fA-F]{64}  ${ASSET}$" checksums.txt | sha256sum -c -
) || die "SHA256 校验失败"

tar -xzf "${TMP_DIR}/${ASSET}" -C "${TMP_DIR}"
[[ -f "${TMP_DIR}/cf" ]] || die "发布包中缺少 cf"

if [[ -e "${INSTALL_PATH}" || -L "${INSTALL_PATH}" ]]; then
  if ! "${INSTALL_PATH}" version 2>/dev/null | grep -Fq 'github.com/pjy02/cf'; then
    die "${INSTALL_PATH} 已被其他程序占用，为避免覆盖请先确认并处理"
  fi
fi

install -m 0755 "${TMP_DIR}/cf" "${INSTALL_PATH}"
mkdir -p /etc/cf-ip-sync /var/lib/cf-ip-sync
chmod 700 /etc/cf-ip-sync /var/lib/cf-ip-sync

if [[ -e "${COMPAT_PATH}" || -L "${COMPAT_PATH}" ]]; then
  if [[ -L "${COMPAT_PATH}" && "$(readlink -f "${COMPAT_PATH}" 2>/dev/null || true)" == "${INSTALL_PATH}" ]]; then
    :
  elif "${COMPAT_PATH}" version 2>/dev/null | grep -Eq '^cfsync (dev|[0-9]+\.[0-9]+\.[0-9]+) '; then
    rm -f -- "${COMPAT_PATH}"
  else
    printf '警告：%s 已被其他程序占用，未创建兼容命令。\n' "${COMPAT_PATH}" >&2
    COMPAT_PATH=""
  fi
fi
if [[ -n "${COMPAT_PATH}" ]]; then
  ln -sfn "${INSTALL_PATH}" "${COMPAT_PATH}"
fi

printf '程序已安装到 %s\n' "${INSTALL_PATH}"

if [[ -r /dev/tty && -w /dev/tty ]]; then
  exec </dev/tty >/dev/tty 2>&1
  "${INSTALL_PATH}" setup
  "${INSTALL_PATH}" install-service
  if ! "${INSTALL_PATH}" sync --quiet; then
    printf '首次同步失败，请运行 cf 查看详情。\n' >&2
  fi
  printf '\n安装完成。以后输入 cf 即可打开 SSH 面板。\n'
else
  printf '\n当前没有可用交互终端，已跳过首次配置。\n'
  printf '请登录 SSH 后依次运行：\n  sudo cf setup\n  sudo cf install-service\n  sudo cf sync\n'
fi

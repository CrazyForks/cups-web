#!/usr/bin/env bash
# 柯尼卡美能达 bizhub 3000MF 黑白激光打印机驱动：amd64 + arm64 best-effort 安装。
#
# 背景（issue #35）：
# 柯尼卡美能达官方未把任何 Linux 驱动上传 Debian 仓库；其官方下载站
# (konicaminolta.com.cn) 只针对国产化平台（银河麒麟 / UOS）发布了 .deb，
# 且打包成 .7z 压缩格式（不是 .tar.gz）。社区/AUR 也没有维护包。
# 所以这里走的策略是：从官方真实下载点抓 .7z，按宿主架构挑对应 .deb 装。
#
# ────────────────────────────────────────────────────────────────────
# 架构覆盖说明
# ────────────────────────────────────────────────────────────────────
# .7z 解压后顶层目录是中文「银河麒麟」，按架构分四个子目录：
#   amd64/        bizhub3000mfpdrvchn_1.0.0-1_amd64.deb       → amd64
#   arm64/        bizhub3000mfpdrvchn_1.0.0-1_arm64.deb       → arm64
#   loongarch64/  bizhub3000mfpdrvchn_1.0.0-1_loongarch64.deb → 龙芯（本镜像未发布）
#   mips64/       bizhub3000mfpdrvchn_1.0.0-1_mips64el.deb    → MIPS（本镜像未发布）
# 每个架构同时包含 konicaminoltascan1（扫描驱动）的同名 .deb，
# **本脚本不安装扫描驱动**——理由：
#   ① 本仓库是 Web 打印工具，scan 没业务诉求；
#   ② konicaminoltascan1 依赖 sane-utils/libsane 等扫描栈，trixie 上的
#      包名/ABI 跟银河麒麟可能错位，装失败会让 dpkg 回退跑 `apt -f install`
#      拖慢构建甚至误装一堆无用依赖。
# armhf/armel 没有 32-bit ARM 包，脚本入口直接 skip。
#
# ────────────────────────────────────────────────────────────────────
# 下载策略
# ────────────────────────────────────────────────────────────────────
# 真实下载点 public.integration.yamayuri.kiku8101.com 是康佳/麒麟系运营的
# IIS 站点，会 302 redirect 到 CloudFront 签名 URL（带短时效 Expires/
# Signature 参数）。fileId 这一层 GUID 由柯尼卡美能达官方网站发出，
# 长期稳定（截至 2026-05），wget 跟随 redirect 即可。
# fail-fast：下载或 dpkg 任一步失败立即非零退出，避免发布镜像里缺少
# 驱动却静默成功（与 escpr2 / epson-cn / canon-ufr2 同策略）。
# 升级版本：到 https://www.konicaminolta.com.cn/support/drivers/index.html
# 找新版本后抓 fileId / 文件名，更新下方四个变量即可。
#
# 解压依赖：apt 包 `p7zip-full`（提供 /usr/bin/7z），由 Dockerfile 在
# 第一阶段 apt 安装时一并装上。

set -eo pipefail

# ────────────────────────────────────────────────────────────────────
# 架构判断 → 选择 .7z 内的子目录与 .deb 名
# ────────────────────────────────────────────────────────────────────
ARCH="$(dpkg --print-architecture)"
case "${ARCH}" in
    amd64)
        KM_DEB_SUBDIR="amd64"
        KM_DEB_ARCH="amd64"
        ;;
    arm64)
        KM_DEB_SUBDIR="arm64"
        KM_DEB_ARCH="arm64"
        ;;
    *)
        echo "[konica-bizhub] skip: arch=${ARCH} (no ${ARCH} binary; supported: amd64/arm64)"
        exit 0
        ;;
esac

# ────────────────────────────────────────────────────────────────────
# 配置（升级版本时同步更新这一组）
# ────────────────────────────────────────────────────────────────────
KM_VERSION="1.0.0-1"
KM_ARCHIVE="konica-bizhub-3000mf.7z"
KM_FILE_ID="A7F28C6B-534B-4D8F-A2C1-7DA7372FAE98"
KM_URL="https://public.integration.yamayuri.kiku8101.com/publicdownload/download?fileId=${KM_FILE_ID}"
KM_UA="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

# 7z 内部顶层中文目录名 + 架构子目录 + .deb 文件名
# 顶层目录是中文「银河麒麟」，find 兜底比硬编码更稳。
KM_DEB_NAME="bizhub3000mfpdrvchn_${KM_VERSION}_${KM_DEB_ARCH}.deb"

# ────────────────────────────────────────────────────────────────────
# 下载 & 解压 & dpkg
# ────────────────────────────────────────────────────────────────────
if ! command -v 7z >/dev/null 2>&1; then
    echo "[konica-bizhub] FATAL: /usr/bin/7z not found, install 'p7zip-full' first"
    exit 1
fi

BUILD_DIR="$(mktemp -d /tmp/konica-bizhub.XXXXXX)"
trap 'rm -rf "${BUILD_DIR}"' EXIT

cd "${BUILD_DIR}"

echo "[konica-bizhub] arch=${ARCH} → ${KM_DEB_SUBDIR}/${KM_DEB_NAME}"
echo "[konica-bizhub] downloading ${KM_URL}"
# 真实下载链路：IIS → 302 → CloudFront 签名 URL，所以必须 --max-redirect 跟随。
wget --tries=3 --timeout=60 --max-redirect=5 --retry-connrefused \
     --user-agent="${KM_UA}" \
     -O "${KM_ARCHIVE}" "${KM_URL}"

# 7z 解压。-y 自动确认覆盖，-o 指定输出目录。
mkdir -p extracted
7z x -y "-oextracted" "${KM_ARCHIVE}" >/dev/null

# 用 find 兜底定位 .deb：顶层目录是中文，subdir 也可能因厂商重打包变化。
DEB_PATH="$(find extracted -type f -name "${KM_DEB_NAME}" -print -quit 2>/dev/null || true)"

if [ -z "${DEB_PATH}" ]; then
    echo "[konica-bizhub] FATAL: deb file not found in archive"
    echo "[konica-bizhub]   expected: ${KM_DEB_NAME}"
    echo "[konica-bizhub]   archive layout:"
    find extracted -maxdepth 4 -type f -name "*.deb" || true
    exit 1
fi

echo "[konica-bizhub] installing ${DEB_PATH}"

# dpkg -i 失败时用 apt-get -f install 兜底处理依赖（与 install-epson-cn.sh 同模式）。
dpkg -i "${DEB_PATH}" || apt-get install -y -f --no-install-recommends

echo "[konica-bizhub] installed Konica Minolta bizhub 3000MF driver v${KM_VERSION} (${KM_DEB_ARCH})"
rm -rf /var/lib/apt/lists/*

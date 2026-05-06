#!/usr/bin/env bash
# 编译并安装 Epson ESC/P-R 2 驱动。
#
# 支持 ET-18100, L8050, L8160, WF-7840 等新款 Epson 喷墨打印机的完整功能（含无边距打印）。
# Debian 官方仓库不提供此包，从 Epson 源码编译。
#
# 注：apt 阶段的 libcups2-dev / libcupsimage2-dev 不再单独安装——上一步源码编译
# CUPS 的 `make install` 已把 cups/cupsimage 的头文件与 .so 放入 /usr/include 与
# /usr/lib，ESC/P-R 2 的 configure 会通过 cups-config 找到新编译出的 libcups，
# 避免 apt 版 -dev 包回踩覆盖源码装好的 /usr/include/cups/*.h。
#
# ⚠️ 下载策略：
# Epson 官方下载中心（download-center.epson.com）挂在 Akamai CDN 后面，对请求做
# UA/Header/TLS 指纹等多维度风控，HTTP 403 概率高且 UUID 会被 Epson 定期轮换，
# 不适合作为稳定的 CI 构建源。所以这里只从我们自维护的 GitHub Releases 镜像下载
# 源码 tarball，下载失败则脚本以非零退出码结束（fail-fast），避免发布镜像里
# 缺少 ESCPR2 驱动却静默成功。
# 升级版本：① 在本仓库的 Releases 里上传新版 tarball；② 修改下方
# ESCPR2_VERSION 与 ESCPR2_MIRROR_URL。

set -eo pipefail

# ────────────────────────────────────────────────────────────────────
# 配置
# ────────────────────────────────────────────────────────────────────
ESCPR2_VERSION="1.2.39"
ESCPR2_MIRROR_URL="https://github.com/hanxi/cups-web/releases/download/vescpr2-1.2.39/epson-inkjet-printer-escpr2-1.2.39-1.tar.gz"

# ────────────────────────────────────────────────────────────────────
# 下载 & 编译
# ────────────────────────────────────────────────────────────────────
if [ -z "${ESCPR2_MIRROR_URL}" ]; then
    echo "[escpr2] FATAL: ESCPR2_MIRROR_URL is empty"
    exit 1
fi

BUILD_DIR="$(mktemp -d /tmp/escpr2-build.XXXXXX)"
trap 'rm -rf "${BUILD_DIR}"' EXIT

cd "${BUILD_DIR}"

echo "[escpr2] downloading from mirror ${ESCPR2_MIRROR_URL}"
curl -fL --retry 3 --retry-delay 3 -o escpr2.tar.gz "${ESCPR2_MIRROR_URL}"

mkdir src
cd src
tar xzf ../escpr2.tar.gz --strip-components=1
autoreconf -fi

# 强制编译器标准回退到 gnu17，避免 C23 把隐式函数声明视为错误
export CC="gcc -std=gnu17"
export CXX="g++ -std=gnu17"

./configure --prefix=/usr --disable-static \
    CFLAGS="-O2 -std=gnu17" \
    CXXFLAGS="-O2 -std=gnu17"
make -j"$(nproc)"
make install

echo "[escpr2] installed version ${ESCPR2_VERSION}"

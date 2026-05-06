#!/usr/bin/env bash
# 清理编译工具链：CUPS 与 ESC/P-R 2 都已编译安装完毕，构建依赖可以全部清理。
#
# 注意只清理 *-dev 头文件包和编译器，保留运行时动态库（如 libavahi-client3、
# libgnutls30、libssl3、libusb-1.0-0 等），它们是 cupsd 运行期必需的。
#
# ⚠️ 陷阱：源码编译出来的 cupsd 不在 apt 依赖图里，`apt-get autoremove` 不知道
# 它依赖这些运行时库，purge *-dev 后有概率把 libgnutls30 / libldap-* /
# libdbus-1-3 等作为"孤儿"一起删掉，导致 cupsd 启动时 dlopen 失败。
# 解决：先 `apt-mark manual` 把这些运行时库标记为用户显式安装，autoremove
# 就不会碰它们了。
#
# ⚠️ 包名跨版本差异：Debian trixie 升级了 OpenLDAP（libldap-2.5-0 → libldap-2.6-0）
# 并做了 t64 ABI 迁移，部分包名带 t64 后缀；不同架构（amd64/arm64/armhf）
# 实际装到的包名可能也不同。所以用 for 循环逐个 apt-mark，用
# `dpkg-query` 先检查"包已安装"再 mark，不存在的包静默跳过，保证
# 整条命令在任意 Debian 版本 / 架构下都不会因为找不到某个包而整体 exit 100。

set -eux

# ────────────────────────────────────────────────────────────────────
# 1. 把 cupsd 运行期依赖的动态库标记为 manual，防止后续 autoremove 误删
# ────────────────────────────────────────────────────────────────────
RUNTIME_LIBS=(
    libavahi-client3
    libavahi-common3
    libdbus-1-3
    libgnutls30
    libkrb5-3
    libldap-2.5-0 libldap-2.6-0 libldap-2.6-0t64
    libpam0g libpam0t64
    libssl3 libssl3t64
    libsystemd0
    libusb-1.0-0
    zlib1g
)

for pkg in "${RUNTIME_LIBS[@]}"; do
    if dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q "install ok installed"; then
        apt-mark manual "$pkg"
    fi
done

# ────────────────────────────────────────────────────────────────────
# 2. purge 编译期工具链与 -dev 头文件包
# ────────────────────────────────────────────────────────────────────
BUILD_DEPS=(
    build-essential
    autoconf
    automake
    libtool
    pkg-config
    libavahi-client-dev
    libavahi-common-dev
    libdbus-1-dev
    libgnutls28-dev
    libkrb5-dev
    libldap-dev libldap2-dev
    libpam0g-dev
    libssl-dev
    libsystemd-dev
    libusb-1.0-0-dev
    zlib1g-dev
)

for pkg in "${BUILD_DEPS[@]}"; do
    if dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q "install ok installed"; then
        apt-get purge -y "$pkg"
    fi
done

apt-get autoremove -y
apt-get clean
rm -rf /var/lib/apt/lists/*

echo "[cleanup] build deps purged"

#!/usr/bin/env bash
# 安装 printer-driver-gutenprint：仅 amd64/arm64 上安装。
#
# printer-driver-gutenprint 在 trixie armhf 上没有 binary 包（gutenprint 的
# 链接依赖 libgutenprint9 未完成 t64 迁移），仅在 amd64/arm64 上安装。
# armhf 用户仍可通过 printer-driver-all 推荐的其他驱动覆盖大部分打印机。

set -eux

ARCH="$(dpkg --print-architecture)"
if [ "${ARCH}" = "armhf" ] || [ "${ARCH}" = "armel" ]; then
    echo "[gutenprint] skip: arch=${ARCH} (no binary package on trixie)"
    exit 0
fi

apt-get update
apt-get install -y --no-install-recommends printer-driver-gutenprint
apt-get clean
rm -rf /var/lib/apt/lists/*

echo "[gutenprint] installed"

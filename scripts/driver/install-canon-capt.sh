#!/usr/bin/env bash
# 编译并安装 Canon CAPT (LBP2900/LBP2900B) 开源驱动。
#
# 基于逆向工程的开源 CAPT 协议实现（GPL-3.0，alpha 阶段），覆盖 Canon LBP2900/
# LBP2900B 等走 CAPT 协议的旧款 Canon 激光打印机。
# 纯 C 源码，依赖 CUPS 开发头文件（cups-config），全架构编译。
#
# issue #43。源码来自 https://github.com/itapplication/Canon-LBP2900B
#
# ⚠️ 下载策略：
# 该仓库无 release/tag，直接从 GitHub 下载 master 分支的 tarball。
# 如果上游仓库不可用或代码发生破坏性变更导致编译失败，脚本以非零退出码结束
# （fail-fast），避免发布镜像里缺少该驱动却静默成功。

set -eo pipefail

# ── AIO 模式：自行管理编译依赖（单容器部署时 runtime 镜像不含编译工具）──
if [ "${CUPS_AIO:-0}" = "1" ]; then
    echo "[canon-capt] AIO mode: installing build dependencies..."
    apt-get update
    apt-get install -y --no-install-recommends build-essential autoconf automake libtool gcc pkg-config git
    _CUPS_AIO_CLEANUP() {
        echo "[canon-capt] AIO mode: cleaning up build dependencies..."
        apt-get purge -y --auto-remove build-essential autoconf automake libtool gcc pkg-config git 2>/dev/null || true
        apt-get clean 2>/dev/null || true
        rm -rf /var/lib/apt/lists/*
    }
    trap '_CUPS_AIO_CLEANUP' EXIT
fi

# ────────────────────────────────────────────────────────────────────
# 配置
# ────────────────────────────────────────────────────────────────────
CANON_CAPT_REPO="https://github.com/itapplication/Canon-LBP2900B"
CANON_CAPT_BRANCH="master"

# ────────────────────────────────────────────────────────────────────
# 下载 & 编译
# ────────────────────────────────────────────────────────────────────
BUILD_DIR="$(mktemp -d /tmp/canon-capt-build.XXXXXX)"
trap 'rm -rf "${BUILD_DIR}"' EXIT

cd "${BUILD_DIR}"

echo "[canon-capt] downloading source from ${CANON_CAPT_REPO} (branch: ${CANON_CAPT_BRANCH})"
curl -fL --retry 3 --retry-delay 3 -o capt.tar.gz \
    "${CANON_CAPT_REPO}/archive/refs/heads/${CANON_CAPT_BRANCH}.tar.gz"

mkdir src && cd src
tar xzf ../capt.tar.gz --strip-components=1

# 生成 configure（源码仓库不带 configure，需 autotools 生成）
aclocal
autoconf
automake --add-missing

# ──────────────────────────────────────────────────────────────────────
# 编译选项说明
# ──────────────────────────────────────────────────────────────────────
# 使用与 escpr2 类似的宽容 CFLAGS 应对 Debian trixie / GCC 15 可能的编译问题：
# - C23 标准把"隐式函数声明"和"隐式 int"列为构造错误
# - Debian trixie GCC 15 额外开启了 -Werror=implicit-function-declaration
# 为安全起见，对这类 alpha 阶段的第三方代码统一降级为 warning。
CAPT_CFLAGS="-O2 -std=gnu17 \
-Wno-error=implicit-function-declaration \
-Wno-error=implicit-int \
-Wno-error=incompatible-pointer-types"

./configure --prefix=/usr CFLAGS="${CAPT_CFLAGS}"
make -j"$(nproc)"

# ────────────────────────────────────────────────────────────────────
# 安装 filter 和 PPD
# ────────────────────────────────────────────────────────────────────
# rastertocapt filter → CUPS filter 目录
install -m 755 src/rastertocapt /usr/lib/cups/filter/rastertocapt

# PPD → CUPS model 目录（Canon 子目录，与 canon-ufr2 的 PPD 布局一致）
install -d /usr/share/cups/model/Canon
install -m 644 Canon-LBP-2900.ppd /usr/share/cups/model/Canon/Canon-LBP-2900.ppd

# ────────────────────────────────────────────────────────────────────
# 验证
# ────────────────────────────────────────────────────────────────────
if [ ! -f /usr/lib/cups/filter/rastertocapt ]; then
    echo "[canon-capt] FATAL: rastertocapt filter not found after install"
    exit 1
fi

echo "[canon-capt] installed successfully"

#!/bin/bash
set -eo pipefail

DRIVERS_BASE="/opt/cups-drivers"
SCRIPTS_DIR="${DRIVERS_BASE}/scripts"
DATA_DIR="${DRIVERS_BASE}/data"
MULTIARCH_LIBDIR="/usr/lib/$(dpkg-architecture -qDEB_HOST_MULTIARCH 2>/dev/null || echo x86_64-linux-gnu)"

MONITORED_DIRS=(
    /usr/lib/cups
    /usr/share/cups
    /usr/share/ppd
    /lib/firmware
    /usr/share/foomatic
    "${MULTIARCH_LIBDIR}"
)

log() {
    echo "[driver-install] $*"
}

usage() {
    echo "Usage: driver-install <driver-name>"
    echo ""
    echo "Install a printer driver into the CUPS container."
    echo ""
    echo "Available drivers:"
    if [ -d "${SCRIPTS_DIR}" ]; then
        for script in "${SCRIPTS_DIR}"/install-*.sh; do
            [ -f "$script" ] || continue
            name="$(basename "$script" .sh)"
            name="${name#install-}"
            status="not installed"
            if [ -f "${DATA_DIR}/${name}/manifest.txt" ]; then
                status="installed"
            fi
            echo "  ${name}  (${status})"
        done
    else
        echo "  (no driver scripts found in ${SCRIPTS_DIR})"
    fi
    exit 1
}

# --- Argument validation ---
if [ -z "$1" ]; then
    usage
fi

DRIVER_NAME="$1"
INSTALL_SCRIPT="${SCRIPTS_DIR}/install-${DRIVER_NAME}.sh"
DRIVER_DATA="${DATA_DIR}/${DRIVER_NAME}"

if [ ! -f "${INSTALL_SCRIPT}" ]; then
    log "ERROR: Driver '${DRIVER_NAME}' not found."
    log "No install script at: ${INSTALL_SCRIPT}"
    echo ""
    usage
fi

# --- Check if already installed ---
if [ -f "${DRIVER_DATA}/manifest.txt" ]; then
    log "Driver '${DRIVER_NAME}' is already installed."
    log "To reinstall, first remove it: driver-remove ${DRIVER_NAME}"
    exit 1
fi

# --- Record pre-install filesystem state ---
log "Recording pre-install filesystem state..."

# Capture files in monitored directories
: > /tmp/pre-install.txt
for dir in "${MONITORED_DIRS[@]}"; do
    if [ -d "$dir" ]; then
        find "$dir" -type f >> /tmp/pre-install.txt 2>/dev/null || true
    fi
done
sort -u /tmp/pre-install.txt -o /tmp/pre-install.txt

# Capture dpkg state
dpkg --get-selections > /tmp/pre-dpkg.txt 2>/dev/null || true

# --- Run the install script ---
log "Installing driver '${DRIVER_NAME}'..."
export CUPS_AIO=1
bash "${INSTALL_SCRIPT}"
log "Install script completed."

# --- Record post-install filesystem state ---
log "Recording post-install filesystem state..."

: > /tmp/post-install.txt
for dir in "${MONITORED_DIRS[@]}"; do
    if [ -d "$dir" ]; then
        find "$dir" -type f >> /tmp/post-install.txt 2>/dev/null || true
    fi
done
sort -u /tmp/post-install.txt -o /tmp/post-install.txt

# Find new files from monitored directories
comm -13 /tmp/pre-install.txt /tmp/post-install.txt > /tmp/new-files.txt

# --- Capture files from new dpkg packages ---
dpkg --get-selections > /tmp/post-dpkg.txt 2>/dev/null || true

# Find newly installed packages
: > /tmp/new-packages.txt
comm -13 <(awk '{print $1}' /tmp/pre-dpkg.txt | sort) \
         <(awk '/install$/{print $1}' /tmp/post-dpkg.txt | sort) \
         > /tmp/new-packages.txt || true

if [ -s /tmp/new-packages.txt ]; then
    log "New packages detected:"
    while IFS= read -r pkg; do
        log "  - ${pkg}"
        dpkg -L "$pkg" 2>/dev/null | while IFS= read -r f; do
            [ -f "$f" ] && echo "$f"
        done >> /tmp/new-files.txt
    done < /tmp/new-packages.txt
fi

# --- Deduplicate ---
sort -u /tmp/new-files.txt -o /tmp/new-files.txt

if [ ! -s /tmp/new-files.txt ]; then
    log "WARNING: No new files detected after installation."
    log "The driver may have installed files in unmonitored locations."
fi

# --- Persist driver files ---
log "Persisting driver files to ${DRIVER_DATA}..."
mkdir -p "${DRIVER_DATA}"

file_count=0
while IFS= read -r filepath; do
    [ -z "$filepath" ] && continue
    [ -f "$filepath" ] || continue
    dest="${DRIVER_DATA}${filepath}"
    mkdir -p "$(dirname "$dest")"
    cp -a "$filepath" "$dest"
    file_count=$((file_count + 1))
done < /tmp/new-files.txt

# Save manifest
cp /tmp/new-files.txt "${DRIVER_DATA}/manifest.txt"

# Save install metadata
echo "driver=${DRIVER_NAME}" > "${DRIVER_DATA}/metadata.txt"
echo "installed_at=$(date -Iseconds)" >> "${DRIVER_DATA}/metadata.txt"
echo "file_count=${file_count}" >> "${DRIVER_DATA}/metadata.txt"
echo "arch=$(dpkg-architecture -qDEB_HOST_ARCH 2>/dev/null || uname -m)" >> "${DRIVER_DATA}/metadata.txt"

# --- Run ldconfig ---
log "Updating shared library cache..."
ldconfig 2>/dev/null || true

# --- Cleanup ---
rm -f /tmp/pre-install.txt /tmp/post-install.txt /tmp/new-files.txt
rm -f /tmp/pre-dpkg.txt /tmp/post-dpkg.txt /tmp/new-packages.txt

log "Driver '${DRIVER_NAME}' installed successfully. (${file_count} files persisted)"

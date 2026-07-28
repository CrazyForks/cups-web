#!/bin/bash
set -eo pipefail

DRIVERS_BASE="/opt/cups-drivers"
DATA_DIR="${DRIVERS_BASE}/data"

log() {
    echo "[driver-remove] $*"
}

if [ -z "$1" ]; then
    echo "Usage: driver-remove <driver-name>"
    echo ""
    echo "Remove an installed printer driver."
    echo ""
    echo "Installed drivers:"
    if [ -d "${DATA_DIR}" ]; then
        found=false
        for driver_dir in "${DATA_DIR}"/*/; do
            [ -d "$driver_dir" ] || continue
            [ -f "${driver_dir}manifest.txt" ] || continue
            found=true
            echo "  $(basename "$driver_dir")"
        done
        if ! $found; then
            echo "  (none)"
        fi
    else
        echo "  (none)"
    fi
    exit 1
fi

DRIVER_NAME="$1"
DRIVER_DATA="${DATA_DIR}/${DRIVER_NAME}"
MANIFEST="${DRIVER_DATA}/manifest.txt"

if [ ! -f "${MANIFEST}" ]; then
    log "ERROR: Driver '${DRIVER_NAME}' is not installed."
    log "No manifest found at: ${MANIFEST}"
    exit 1
fi

log "Removing driver '${DRIVER_NAME}'..."

# Remove files listed in the manifest from system paths
removed_count=0
missing_count=0

while IFS= read -r filepath; do
    [ -z "$filepath" ] && continue

    if [ -f "$filepath" ]; then
        rm -f "$filepath"
        removed_count=$((removed_count + 1))
    else
        missing_count=$((missing_count + 1))
    fi
done < "${MANIFEST}"

# Clean up empty directories left behind in monitored paths
for dir in /usr/lib/cups /usr/share/cups /usr/share/ppd /lib/firmware /usr/share/foomatic; do
    if [ -d "$dir" ]; then
        find "$dir" -type d -empty -delete 2>/dev/null || true
    fi
done

# Remove the driver data directory
log "Removing persisted driver data..."
rm -rf "${DRIVER_DATA}"

# Update shared library cache
log "Updating shared library cache..."
ldconfig 2>/dev/null || true

log "Driver '${DRIVER_NAME}' removed successfully."
log "  Files removed: ${removed_count}"
if [ $missing_count -gt 0 ]; then
    log "  Files already missing: ${missing_count}"
fi

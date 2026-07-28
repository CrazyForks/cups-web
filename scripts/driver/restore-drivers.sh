#!/bin/bash
set -e

DRIVERS_BASE="/opt/cups-drivers"
DATA_DIR="${DRIVERS_BASE}/data"

# Exit silently if no driver data directory exists
if [ ! -d "${DATA_DIR}" ]; then
    exit 0
fi

# Check if there are any driver subdirectories
shopt -s nullglob
driver_dirs=("${DATA_DIR}"/*)
shopt -u nullglob

if [ ${#driver_dirs[@]} -eq 0 ]; then
    exit 0
fi

restored_count=0
restored_drivers=()

for driver_dir in "${driver_dirs[@]}"; do
    [ -d "$driver_dir" ] || continue

    driver_name="$(basename "$driver_dir")"
    manifest="${driver_dir}/manifest.txt"

    if [ ! -f "$manifest" ]; then
        continue
    fi

    echo "[restore-drivers] Restoring driver: ${driver_name}"
    file_count=0
    error_count=0

    while IFS= read -r filepath; do
        [ -z "$filepath" ] && continue

        source_file="${driver_dir}${filepath}"

        if [ ! -f "$source_file" ]; then
            error_count=$((error_count + 1))
            continue
        fi

        # Create parent directory if needed
        parent_dir="$(dirname "$filepath")"
        if [ ! -d "$parent_dir" ]; then
            mkdir -p "$parent_dir"
        fi

        # Copy preserving permissions, ownership, and timestamps
        cp -a "$source_file" "$filepath"
        file_count=$((file_count + 1))
    done < "$manifest"

    echo "[restore-drivers]   Restored ${file_count} files"
    if [ $error_count -gt 0 ]; then
        echo "[restore-drivers]   WARNING: ${error_count} files missing from backup"
    fi

    restored_count=$((restored_count + 1))
    restored_drivers+=("$driver_name")
done

# Update shared library cache if any drivers were restored
if [ $restored_count -gt 0 ]; then
    echo "[restore-drivers] Updating shared library cache..."
    ldconfig 2>/dev/null || true
    echo "[restore-drivers] Restored ${restored_count} driver(s): ${restored_drivers[*]}"
fi

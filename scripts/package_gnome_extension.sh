#!/usr/bin/env bash
#
# package_gnome_extension.sh - Package GNOME Shell extension for extensions.gnome.org (EGO)
#
# Usage:
#   ./scripts/package_gnome_extension.sh [output_dir]
#
# If output_dir is omitted, the package is placed in the project root directory.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
EXT_DIR="${ROOT_DIR}/gnome-extension"
OUT_DIR="${1:-${ROOT_DIR}}"

print_info() {
    echo -e "\033[1;34m[INFO]\033[0m $1"
}

print_success() {
    echo -e "\033[1;32m[SUCCESS]\033[0m $1"
}

print_warn() {
    echo -e "\033[1;33m[WARNING]\033[0m $1"
}

print_error() {
    echo -e "\033[1;31m[ERROR]\033[0m $1"
}

# 1. Validate extension directory and mandatory files
if [ ! -d "${EXT_DIR}" ]; then
    print_error "Extension directory not found at ${EXT_DIR}"
    exit 1
fi

if [ ! -f "${EXT_DIR}/metadata.json" ]; then
    print_error "metadata.json not found in ${EXT_DIR}"
    exit 1
fi

if [ ! -f "${EXT_DIR}/extension.js" ]; then
    print_error "extension.js not found in ${EXT_DIR}"
    exit 1
fi

# Extract UUID and version from metadata.json
UUID="$(grep -oP '"uuid":\s*"\K[^"]+' "${EXT_DIR}/metadata.json" || echo "apsu@ijin82.github.com")"
VERSION="$(grep -oP '"version":\s*\K[0-9]+' "${EXT_DIR}/metadata.json" || echo "1")"
NAME="$(grep -oP '"name":\s*"\K[^"]+' "${EXT_DIR}/metadata.json" || echo "apsu")"

mkdir -p "${OUT_DIR}"
OUT_DIR="$(cd "${OUT_DIR}" && pwd)"

echo "=========================================================="
echo " Packaging GNOME Shell Extension: ${NAME} (${UUID})"
echo " Version: ${VERSION}"
echo " Source:  ${EXT_DIR}"
echo " Target:  ${OUT_DIR}"
echo "=========================================================="

ARCHIVE_BASENAME="${UUID}.shell-extension.zip"
TARGET_ZIP="${OUT_DIR}/${ARCHIVE_BASENAME}"

# 2. Package extension using gnome-extensions pack if available, otherwise zip fallback
if command -v gnome-extensions >/dev/null 2>&1; then
    print_info "Using official 'gnome-extensions pack' utility..."
    gnome-extensions pack "${EXT_DIR}" --out-dir="${OUT_DIR}" --force
else
    print_warn "'gnome-extensions' tool not found. Falling back to 'zip'..."
    rm -f "${TARGET_ZIP}"
    (
        cd "${EXT_DIR}"
        # Package only runtime files required by GNOME Extensions (exclude docs/git files)
        FILES_TO_PACK=(metadata.json extension.js)
        if [ -f stylesheet.css ]; then
            FILES_TO_PACK+=(stylesheet.css)
        fi
        zip -q -r "${TARGET_ZIP}" "${FILES_TO_PACK[@]}"
    )
fi

# 3. Verify package
if [ ! -f "${TARGET_ZIP}" ]; then
    print_error "Packaging failed: archive ${TARGET_ZIP} was not generated."
    exit 1
fi

print_success "Extension packaged successfully!"
echo ""
echo "Archive details:"
ls -lh "${TARGET_ZIP}"
echo ""
echo "Archive contents:"
unzip -l "${TARGET_ZIP}"
echo ""

# 4. Optional static analysis with shexli
if command -v shexli >/dev/null 2>&1; then
    print_info "Running 'shexli' static analysis..."
    shexli "${TARGET_ZIP}" || true
    echo ""
fi

echo "=========================================================="
echo " Ready for upload to GNOME Extensions!"
echo " URL: https://extensions.gnome.org/upload/"
echo ""
echo " To test install locally from this package:"
echo "   gnome-extensions install --force \"${TARGET_ZIP}\""
echo "=========================================================="

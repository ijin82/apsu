#!/usr/bin/env bash
#
# build_release.sh - Build and package qSync release bundle
# Usage:
#   ./scripts/build_release.sh [version]
#
# If version is omitted, the script prompts interactively (e.g. 1.0.2).
#
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Ask for version if not provided as first argument
VERSION="$1"
if [ -z "${VERSION}" ]; then
    read -rp "Enter release version to create (e.g. 1.0.2): " VERSION
fi

# Strip any leading 'v' for standard formatting, then prefix with 'v' for tags
CLEAN_VERSION="${VERSION#v}"
TAG_NAME="v${CLEAN_VERSION}"
PACKAGE_NAME="qsync-${TAG_NAME}"
ZIP_NAME="${PACKAGE_NAME}-linux-android.zip"
STAGING_DIR="${ROOT_DIR}/release_staging/${PACKAGE_NAME}"

echo "=========================================================="
echo " Packaging qSync Release ${TAG_NAME}"
echo "=========================================================="

# 1. Build Linux CLI binary
echo "[1/4] Compiling Linux CLI binary..."
mkdir -p "${ROOT_DIR}/cli"
(cd "${ROOT_DIR}/cli" && go build -o qsync ./cmd/qsync)

# 2. Build Android native core (arm64-v8a) & APK
echo "[2/4] Building optimized Android arm64-v8a native core & APK..."
# Keep only arm64-v8a in jniLibs for release package
rm -rf "${ROOT_DIR}/android/app/src/main/jniLibs/armeabi-v7a" "${ROOT_DIR}/android/app/src/main/jniLibs/x86_64"
"${ROOT_DIR}/scripts/build_android_native.sh" arm64

echo "Compiling Android application..."
(cd "${ROOT_DIR}/android" && ./gradlew assembleDebug)

APK_PATH="${ROOT_DIR}/android/app/build/outputs/apk/debug/app-debug.apk"
if [ ! -f "${APK_PATH}" ]; then
    echo "ERROR: APK build failed or file not found at ${APK_PATH}" >&2
    exit 1
fi

# 3. Prepare staging directory
echo "[3/4] Assembling release payload..."
rm -rf "${ROOT_DIR}/release_staging"
mkdir -p "${STAGING_DIR}"

cp "${ROOT_DIR}/cli/qsync" "${STAGING_DIR}/qsync"
cp "${APK_PATH}" "${STAGING_DIR}/${PACKAGE_NAME}.apk"
cp -r "${ROOT_DIR}/gnome-extension" "${STAGING_DIR}/gnome-extension"
cp "${ROOT_DIR}/scripts/install_linux.sh" "${STAGING_DIR}/install.sh"
chmod +x "${STAGING_DIR}/install.sh" "${STAGING_DIR}/qsync"

# Create release README.md
cat << README_EOF > "${STAGING_DIR}/README.md"
# qSync ${TAG_NAME}

Fast, local, and lightweight P2P file transfer and mounting between Android and Linux desktops.

## Package Contents

- \`${PACKAGE_NAME}.apk\`: Android application.
- \`qsync\`: Standalone Linux CLI binary (x86_64).
- \`gnome-extension/\`: GNOME Shell extension (\`qsync@ijin.github.io\`) with Quick Settings toggle and top bar indicator.
- \`install.sh\`: Automated Linux installer and systemd user service setup.

---

## Quick Start

### 1. Android
Transfer \`${PACKAGE_NAME}.apk\` to your phone and install it. Allow storage and Wi-Fi permissions when prompted.
Configure your custom device name in the application settings if desired.

### 2. Linux Desktop
Run the automated installer:

\`\`\`bash
./install.sh install
\`\`\`

This installs:
- CLI binary into \`~/.local/bin/qsync\`
- Systemd user service \`qsync.service\` (\`systemctl --user status qsync.service\`)
- GNOME extension to \`~/.local/share/gnome-shell/extensions/qsync@ijin.github.io/\`

**Note for GNOME Shell:**
Log out and log back in to reload GNOME Shell and activate the extension (or restart GNOME Shell if on X11 with \`Alt+F2\`, \`r\`, \`Enter\`).

### Uninstallation
To remove all Linux components:

\`\`\`bash
./install.sh uninstall
\`\`\`
README_EOF

# 4. Generate ZIP archive
echo "[4/4] Creating ZIP archive: ${ZIP_NAME}..."
rm -f "${ROOT_DIR}/${ZIP_NAME}"
(cd "${ROOT_DIR}/release_staging" && zip -q -r "${ROOT_DIR}/${ZIP_NAME}" "${PACKAGE_NAME}")
rm -rf "${ROOT_DIR}/release_staging"

echo ""
echo "=========================================================="
echo " SUCCESS: Release package created!"
echo " Location: ${ROOT_DIR}/${ZIP_NAME}"
ls -lh "${ROOT_DIR}/${ZIP_NAME}"
echo "=========================================================="
echo "Tip: You can tag this release in git with:"
echo "  git tag -a ${TAG_NAME} -m \"Release ${TAG_NAME}\""
echo "=========================================================="

#!/usr/bin/env bash
#
# install_linux.sh - Installer & uninstaller for Apsu Linux Desktop components
# Installs:
#   1. qSync CLI binary (~/.local/bin/apsu)
#   2. Systemd user service (~/.config/systemd/user/apsu.service)
#   3. GNOME Shell extension (~/.local/share/gnome-shell/extensions/apsu@ijin.github.io)
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Check if running from within scripts/ directory of repo or directly from package root
if [ -d "${SCRIPT_DIR}/../gnome-extension" ]; then
    ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
else
    ROOT_DIR="${SCRIPT_DIR}"
fi

BIN_DIR="${HOME}/.local/bin"
SYSTEMD_DIR="${HOME}/.config/systemd/user"
EXT_UUID="apsu@ijin.github.io"
EXT_DIR="${HOME}/.local/share/gnome-shell/extensions/${EXT_UUID}"

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

install_apsu() {
    print_info "Starting Apsu desktop installation..."

    # 1. Install CLI binary: pre-built binary first, or compile if in repo
    mkdir -p "${BIN_DIR}"
    if [ -f "${ROOT_DIR}/apsu" ]; then
        print_info "Installing pre-built CLI binary from package..."
        install -m 755 "${ROOT_DIR}/apsu" "${BIN_DIR}/apsu"
    elif [ -f "${ROOT_DIR}/cli/apsu" ]; then
        print_info "Installing pre-built CLI binary from cli/apsu..."
        install -m 755 "${ROOT_DIR}/cli/apsu" "${BIN_DIR}/apsu"
    elif command -v go >/dev/null 2>&1 && [ -d "${ROOT_DIR}/cli" ]; then
        print_info "Building CLI binary from source (${ROOT_DIR}/cli)..."
        GIT_VER="$(git -C "${ROOT_DIR}" describe --tags --always 2>/dev/null | sed 's/^v//' || echo "")"
        BUILD_LDFLAGS="-s -w"
        if [ -n "${GIT_VER}" ]; then
            BUILD_LDFLAGS="${BUILD_LDFLAGS} -X main.Version=${GIT_VER}"
        fi
        (cd "${ROOT_DIR}/cli" && go build -ldflags="${BUILD_LDFLAGS}" -o "${BIN_DIR}/apsu" ./cmd/apsu)
    else
        print_error "apsu binary not found and cannot compile. Please ensure 'apsu' exists or Go is installed."
        exit 1
    fi
    print_success "CLI binary installed to ${BIN_DIR}/apsu"

    # Ensure ~/.local/bin is in PATH notice
    if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
        print_warn "${BIN_DIR} is not in your PATH. You may want to add 'export PATH=\"\$HOME/.local/bin:\$PATH\"' to your ~/.bashrc or ~/.profile."
    fi

    # 2. Setup Systemd user service
    print_info "Configuring systemd user service..."
    mkdir -p "${SYSTEMD_DIR}"
    cat <<SYSTEMD_EOF > "${SYSTEMD_DIR}/apsu.service"
[Unit]
Description=Apsu Background D-Bus Daemon
After=network.target

[Service]
Type=simple
ExecStart=%h/.local/bin/apsu daemon
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
SYSTEMD_EOF

    systemctl --user daemon-reload
    systemctl --user enable apsu.service
    systemctl --user restart apsu.service
    print_success "Systemd user service enabled and started (apsu.service)"

    # 3. Install GNOME Shell Extension
    print_info "Installing GNOME Shell extension (${EXT_UUID})..."
    rm -rf "${EXT_DIR}"
    mkdir -p "${EXT_DIR}"
    cp -r "${ROOT_DIR}/gnome-extension/"* "${EXT_DIR}/"
    print_success "GNOME extension files copied to ${EXT_DIR}"

    # Try enabling extension via CLI if available
    if command -v gnome-extensions >/dev/null 2>&1; then
        gnome-extensions enable "${EXT_UUID}" 2>/dev/null || true
    fi

    echo ""
    echo "======================================================================"
    print_success "Installation completed successfully!"
    echo "======================================================================"
    echo ""
    print_warn "IMPORTANT GNOME SHELL NOTICE:"
    echo "  GNOME Shell discovers new extensions during session startup."
    echo "  To activate the top bar icon and Quick Settings toggle:"
    echo "    1. Log out of your desktop session and log back in (Logout / Login)."
    echo "    2. Or on X11, press Alt+F2, type 'r', and press Enter."
    echo ""
    echo "  You can also check daemon status anytime via:"
    echo "    systemctl --user status apsu.service"
    echo "    apsu status"
    echo ""
}

uninstall_apsu() {
    print_info "Starting Apsu uninstallation..."

    # 1. Stop and disable systemd service
    if systemctl --user is-active --quiet apsu.service 2>/dev/null; then
        print_info "Stopping systemd user service..."
        systemctl --user stop apsu.service || true
    fi
    if [ -f "${SYSTEMD_DIR}/apsu.service" ]; then
        print_info "Disabling and removing systemd user service..."
        systemctl --user disable apsu.service 2>/dev/null || true
        rm -f "${SYSTEMD_DIR}/apsu.service"
        systemctl --user daemon-reload
        print_success "Systemd user service removed"
    fi

    # 2. Disable and remove GNOME extension
    if command -v gnome-extensions >/dev/null 2>&1; then
        gnome-extensions disable "${EXT_UUID}" 2>/dev/null || true
    fi
    if [ -d "${EXT_DIR}" ]; then
        print_info "Removing GNOME Shell extension..."
        rm -rf "${EXT_DIR}"
        print_success "GNOME extension directory removed"
    fi

    # 3. Remove CLI binary
    if [ -f "${BIN_DIR}/apsu" ]; then
        print_info "Removing binary ${BIN_DIR}/apsu..."
        rm -f "${BIN_DIR}/apsu"
        print_success "CLI binary removed"
    fi

    echo ""
    echo "======================================================================"
    print_success "Uninstallation completed!"
    echo "======================================================================"
    print_warn "Log out and log back in to fully unload the extension from GNOME Shell."
    echo ""
}

case "$1" in
    install)
        install_apsu
        ;;
    uninstall)
        uninstall_apsu
        ;;
    *)
        echo "Usage: $0 {install|uninstall}"
        echo ""
        echo "Commands:"
        echo "  install    Build and install CLI binary, systemd daemon, and GNOME Shell extension."
        echo "  uninstall  Stop daemon, disable and remove service, CLI, and GNOME extension."
        exit 1
        ;;
esac

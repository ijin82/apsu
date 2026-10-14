#!/usr/bin/env bash
#
# install_linux.sh - Installer & uninstaller for qSync Linux Desktop components
# Installs:
#   1. qSync CLI binary (~/.local/bin/qsync)
#   2. Systemd user service (~/.config/systemd/user/qsync.service)
#   3. GNOME Shell extension (~/.local/share/gnome-shell/extensions/qsync@ijin.github.io)
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${HOME}/.local/bin"
SYSTEMD_DIR="${HOME}/.config/systemd/user"
EXT_UUID="qsync@ijin.github.io"
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

install_qsync() {
    print_info "Starting qSync desktop installation..."

    # 1. Build CLI binary if go is available
    mkdir -p "${BIN_DIR}"
    if command -v go >/dev/null 2>&1; then
        print_info "Building CLI binary from source (${SCRIPT_DIR}/cli)..."
        (cd "${SCRIPT_DIR}/cli" && go build -o "${BIN_DIR}/qsync" ./cmd/qsync)
    elif [ -f "${SCRIPT_DIR}/cli/qsync" ]; then
        print_info "Copying pre-built CLI binary..."
        cp "${SCRIPT_DIR}/cli/qsync" "${BIN_DIR}/qsync"
        chmod +x "${BIN_DIR}/qsync"
    else
        print_error "Go compiler not found and no pre-built binary in cli/qsync. Please install Go (1.22+)."
        exit 1
    fi
    chmod +x "${BIN_DIR}/qsync"
    print_success "CLI binary installed to ${BIN_DIR}/qsync"

    # Ensure ~/.local/bin is in PATH notice
    if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
        print_warn "${BIN_DIR} is not in your PATH. You may want to add 'export PATH=\"\$HOME/.local/bin:\$PATH\"' to your ~/.bashrc or ~/.profile."
    fi

    # 2. Setup Systemd user service
    print_info "Configuring systemd user service..."
    mkdir -p "${SYSTEMD_DIR}"
    cat <<SYSTEMD_EOF > "${SYSTEMD_DIR}/qsync.service"
[Unit]
Description=qSync Background D-Bus Daemon
After=network.target

[Service]
Type=simple
ExecStart=%h/.local/bin/qsync daemon
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
SYSTEMD_EOF

    systemctl --user daemon-reload
    systemctl --user enable qsync.service
    systemctl --user restart qsync.service
    print_success "Systemd user service enabled and started (qsync.service)"

    # 3. Install GNOME Shell Extension
    print_info "Installing GNOME Shell extension (${EXT_UUID})..."
    rm -rf "${EXT_DIR}"
    mkdir -p "${EXT_DIR}"
    cp -r "${SCRIPT_DIR}/gnome-extension/"* "${EXT_DIR}/"
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
    echo "    systemctl --user status qsync.service"
    echo "    qsync status"
    echo ""
}

uninstall_qsync() {
    print_info "Starting qSync uninstallation..."

    # 1. Stop and disable systemd service
    if systemctl --user is-active --quiet qsync.service 2>/dev/null; then
        print_info "Stopping systemd user service..."
        systemctl --user stop qsync.service || true
    fi
    if [ -f "${SYSTEMD_DIR}/qsync.service" ]; then
        print_info "Disabling and removing systemd user service..."
        systemctl --user disable qsync.service 2>/dev/null || true
        rm -f "${SYSTEMD_DIR}/qsync.service"
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
    if [ -f "${BIN_DIR}/qsync" ]; then
        print_info "Removing binary ${BIN_DIR}/qsync..."
        rm -f "${BIN_DIR}/qsync"
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
        install_qsync
        ;;
    uninstall)
        uninstall_qsync
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

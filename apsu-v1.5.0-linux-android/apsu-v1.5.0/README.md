# Apsu v1.5.0

Fast, local, and lightweight P2P file transfer and mounting between Android and Linux desktops.

## Package Contents

- `apsu-v1.5.0.apk`: Android application.
- `apsu`: Standalone Linux CLI binary (x86_64).
- `gnome-extension/`: GNOME Shell extension (`apsu@ijin.github.io`) with Quick Settings toggle and top bar indicator.
- `install.sh`: Automated Linux installer and systemd user service setup.

---

## Quick Start

### 1. Android
Transfer `apsu-v1.5.0.apk` to your phone and install it. Allow storage and Wi-Fi permissions when prompted.
Configure your custom device name in the application settings if desired.

### 2. Linux Desktop
Run the automated installer:

```bash
./install.sh install
```

This installs:
- CLI binary into `~/.local/bin/apsu`
- Systemd user service `apsu.service` (`systemctl --user status apsu.service`)
- GNOME extension to `~/.local/share/gnome-shell/extensions/apsu@ijin.github.io/`

**Note for GNOME Shell:**
Log out and log back in to reload GNOME Shell and activate the extension (or restart GNOME Shell if on X11 with `Alt+F2`, `r`, `Enter`).

### Uninstallation
To remove all Linux components:

```bash
./install.sh uninstall
```

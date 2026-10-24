# Apsu GNOME Shell Extension

Desktop integration and status indicator for **Apsu** (**A**ndroid **P**hone **S**ync **U**tility).

This extension connects GNOME Shell and the Nautilus file manager to your Android device over local Wi-Fi with zero cloud dependencies, PIN pairing, and high-speed local WebDAV transfers.

- **Extension UUID:** `apsu@ijin82.github.com`
- **Supported GNOME Versions:** 45, 46, 47, 48
- **Project Repository:** [https://github.com/ijin82/apsu](https://github.com/ijin82/apsu)

---

## Features

- **Top Bar Indicator & Quick Settings Tile:** Real-time connection status (offline, discovered, mounted, syncing).
- **Zero-Configuration Discovery:** Automatically finds active Apsu Android servers on the local Wi-Fi via mDNS (`_apsu._tcp`).
- **Interactive PIN Pairing:** Enter the 6-digit mutual pairing PIN directly in a native GNOME modal dialog.
- **One-Click Nautilus Mount:** Mounts phone storage via GVfs (`gio mount`) and opens folders directly in Nautilus.
- **Folder Sync Integration:** View synchronization status and trigger one-way folder sync (Desktop &rarr; Phone).
- **Service Management:** Start, stop, or restart the background `apsu` D-Bus daemon directly from GNOME Quick Settings.

---

## Requirements

The GNOME Shell extension serves as the desktop graphical frontend. To function, it requires:

1. **Apsu Android App:** Running on your Android phone (sharing selected folders).
2. **Apsu Linux CLI Daemon:** Running as a systemd user session service (`apsu daemon`, providing the `org.apsu.Daemon` D-Bus interface).

For full installation of the desktop daemon and CLI, see the [main project instructions](https://github.com/ijin82/apsu).

---

## Manual Installation (From Source)

To install this extension locally into your GNOME user profile:

```bash
# Target directory
EXT_DIR="${HOME}/.local/share/gnome-shell/extensions/apsu@ijin82.github.com"
mkdir -p "${EXT_DIR}"

# Copy extension files
cp -r metadata.json extension.js stylesheet.css "${EXT_DIR}/"

# Enable the extension
gnome-extensions enable apsu@ijin82.github.com
```

> **Note:** GNOME Shell discovers newly added extensions during session initialization. If installing for the first time, log out and log back into your desktop session (or restart GNOME Shell with `Alt+F2` &rarr; `r` &rarr; `Enter` on X11).

---

## Packaging for extensions.gnome.org

To create a release ZIP bundle ready for uploading to [extensions.gnome.org](https://extensions.gnome.org/upload/):

Using the repository build script:
```bash
./scripts/package_gnome_extension.sh
```

Or using the official `gnome-extensions` CLI:
```bash
gnome-extensions pack --force --out-dir=.. .
```

---

## License

This extension is free and open-source software distributed under the terms of the **MIT License**.
See the [main repository](https://github.com/ijin82/apsu) for details.

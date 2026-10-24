# apsu — Android Phone Sync Utility (for Android & Linux GNOME)

**Apsu** (**A**ndroid **P**hone **S**ync **U**tility) is a lightweight, zero-friction local Wi-Fi file sharing and folder sync suite. It connects an Android phone to a Linux desktop (GNOME Shell / Nautilus) with zero cloud dependencies, no internet required, end-to-end PIN pairing, and high-speed local WebDAV transfers.

<img width="256" height="256" alt="ic_launcher-playstore" src="https://github.com/user-attachments/assets/a82b812e-c101-47d1-bf90-b71b00dc329a" />

> [!NOTE]
> ### 💡 Why "Apsu"? (A Story on Finding a Free Name in Modern Open Source)
>
> Finding a short, catchy, and unoccupied name on today's internet is a surprisingly hard quest!
>
> The project was originally named *qSync* — a great name, except there are already countless "QSyncs" out there (most notably QNAP's proprietary NAS sync suite). When looking for a replacement, almost every sensible combination was already claimed:
> - *AirPair*? Countless existing software, hardware, and developer consulting brands.
> - *WiMount*? An industrial mast antenna mounting hardware line by QuWireless.
> - *WMAP*? NASA's famous Wilkinson Microwave Anisotropy Probe probe!
> - *APSW*? Another Python SQLite wrapper.
>
> That's how we arrived at **Apsu**: a clean, punchy acronym for **A**ndroid **P**hone **S**ync **U**tility (and an accidental nod to the fresh primordial waters of ancient mythology). It's distinct, easy to type in the terminal (`apsu`), and completely conflict-free.

---

## Architecture

The project is structured as a monorepo containing:

1. **`core/` — Go Core WebDAV Engine & JNI Bridge**
   - High-performance WebDAV server with custom Virtual File System (VFS) routing.
   - Restricts access strictly to user-configured shared folders.
   - mDNS local network announcement (`_apsu._tcp`).
   - PIN-based mutual pairing and bearer token validation.
   - Compiled as a native C-shared JNI library (`libapsuserver.so`) for Android via NDK.

2. **`android/` — Native Android Application (Kotlin + Jetpack Compose)**
   - Built with Material 3 and Jetpack Compose.
   - Android 14+ compatible with `ForegroundService` support to keep the server alive in background.
   - Granular folder selection via Android Storage Access Framework (SAF).
   - Direct JNI bindings (`ApsuBridge`) calling into the Go engine.
   - Multi-language support with on-the-fly switching (English and Russian).

3. **`cli/` — Linux CLI & D-Bus Daemon (Go)**
   - Background D-Bus service (`org.apsu.Daemon`) for system and desktop integration.
   - Automatic mDNS discovery (`apsu scan`).
   - Simple, portable token and device pairing storage in client configuration JSON (`~/.config/apsu/client_config.json`) with automatic migration from older configurations.
   - Direct mounting via GVfs (`gio mount`).

4. **`gnome-extension/` — GNOME Shell Extension (GNOME 45–48)**
   - Status indicator in the GNOME Top Bar and Quick Settings (`apsu@ijin82.github.com`).
   - Dynamic phone detection over local Wi-Fi.
   - In-extension PIN pairing modal dialog.
   - One-click drive mounting and opening in Nautilus.

## Building from Source

### Prerequisites
- Go 1.22+
- Docker (for cross-compiling the Android native core)
- Android Studio / Android SDK (API 34+)

### 1. Build the Android Native Core (`libapsuserver.so`)
The Go engine can be built for multiple Android ABIs:
- **`arm64-v8a`** (default): Modern 64-bit ARM phones (>95% of active devices).
- **`armeabi-v7a`**: Older 32-bit ARM devices and tablets.
- **`x86_64`**: Android emulators, ChromeOS, and Android-x86.

```bash
# Build for standard 64-bit ARM (default):
./scripts/build_android_native.sh arm64

# Or build for all architectures at once:
./scripts/build_android_native.sh all
```
The script compiles `libapsuserver.so` via Docker/NDK and installs it directly into `android/app/src/main/jniLibs/<abi>/`.

### 2. Build the Android App
Open `android/` in Android Studio, or build via Gradle:
```bash
cd android

# Debug APK:
./gradlew assembleDebug

# Release APK (see below for signing):
./gradlew assembleRelease
```
Debug output is generated at `android/app/build/outputs/apk/debug/app-debug.apk`.

### 3. Install Linux Desktop Suite (CLI, Daemon & GNOME Extension)

A single installation script handles compiling the CLI, installing the systemd user service, and deploying the GNOME Shell extension:

```bash
# Install CLI, systemd daemon, and GNOME Shell extension:
./scripts/install_linux.sh install
```

> [!IMPORTANT]
> **GNOME Shell Session Restart Required**
> GNOME Shell only detects newly installed extensions when a session starts.
> After running `./scripts/install_linux.sh install`:
> - **Log out and log back in** to your desktop session (or reboot).
> - On X11, you can also press <kbd>Alt</kbd>+<kbd>F2</kbd>, type `r`, and press <kbd>Enter</kbd>.
> Once logged in, the **Apsu** phone icon will appear in the GNOME Top Bar and the Quick Settings menu.

To uninstall and remove all desktop components:
```bash
./scripts/install_linux.sh uninstall
```

### 4. Managing the Background Service (Daemon)

Apsu runs a background D-Bus daemon that manages device discovery, automatic sync, and GVfs mounting. You can control the service using convenient built-in CLI commands without needing root or `sudo`:

```bash
# Check service status:
apsu service status

# Start, stop, or restart the service:
apsu service start
apsu service stop
apsu service restart

# Check version:
apsu version
```

You can also use standard systemd user commands:
```bash
systemctl --user status apsu.service
systemctl --user restart apsu.service
```

> [!NOTE]
> **Why `systemctl --user` instead of `sudo service`?**
>
> The Apsu daemon operates strictly as a **user session service** (`systemctl --user`), and should **not** be run with `sudo`:
> - **Nautilus & GVfs Mounting**: Network drives are mounted via GVfs (`gio mount`) inside the desktop user's session (`/run/user/$UID/gvfs`). Running under `sudo` would mount files into `root`'s namespace, making them invisible in your Nautilus file manager.
> - **D-Bus Session Bus**: The GNOME Shell extension and CLI communicate with the daemon via the user session bus (`DBUS_SESSION_BUS_ADDRESS`), which system-level root daemons cannot access.
> - **One-Click Quick Settings Toggle**: The GNOME Quick Settings toggle manages the daemon directly without requiring administrative root privileges or Polkit password prompts.
> - **User Credentials & Storage**: Pairing tokens and sync configs are securely scoped to `~/.config/apsu/`.

> [!TIP]
> **Prefer keeping the extension and daemon off by default at boot?**
> If you prefer Apsu to stay turned off when your computer starts, simply disable service autostart:
> ```bash
> systemctl --user disable apsu.service
> ```
> When autostart is disabled, the GNOME Quick Settings toggle stays turned off on boot, and no background polling occurs. You can activate Apsu at any time simply by clicking the toggle in Quick Settings or running `apsu service start`.

## 🔄 Synchronized Folders (Desktop ➔ Phone One-Way Sync)

Apsu features **One-Way Folder Synchronization** from your Linux desktop directly to your Android device.

### How it works:
- **Desktop is the Source of Truth**: The target folder on your phone is maintained as an exact mirror of the mapped local directory on your PC.
- **Fast & Differential**: Only new or modified files are uploaded. Subdirectories are recreated automatically.
- **Atomic Deletions**: Files or folders removed on your desktop will be removed from the phone's sync target folder during synchronization.

> [!CAUTION]
> **Important Warning for Sync Target Folders**
> A folder designated as **"Syncable folder"** on Android is strictly controlled by the desktop.
> **Do not store important or unique files in this folder on your phone!**
> Any files or modifications made on the phone inside a sync target folder may be overwritten or deleted during sync sessions.

### Built-in Safeguards & Protection:
To prevent accidental data loss:
- **Root Storage Protection**: You cannot mark the root of Android internal storage (`/storage/emulated/0`) as a sync target.
- **Desktop System Path Protection**: The sync engine strictly refuses to use `/`, `/home`, your entire user home directory (`~`), `/etc`, `/usr`, or other system directories as a sync source.

### Setup Guide:
1. **On your phone**: In Apsu, tap **Add Folder**, select or create a dedicated target folder (e.g., `Music`, `Podcasts`, or `Coub`), check **"Syncable folder (Desktop ➔ Phone)"**, and tap **Add**.
2. **On your desktop**:
   - In the GNOME Top Bar menu, open your device submenu and click **⚙️ Sync Settings...**.
   - Browse and choose the matching source directory on your PC (or configure mappings via CLI: `apsu sync --map "FolderName=/path/to/folder"`).
   - Click **Save**.
3. **Run Sync**: Click **🔄 Sync Now** from the top bar menu or run `apsu sync` in your terminal. The icon will indicate progress without interrupting you with toast notifications.

## Quick Usage

1. **Android**:
   - Open **Apsu** on your phone.
   - Grant storage permissions when prompted.
   - Select the folders you want to share (e.g. `Download`, `DCIM`, `Music`).
   - Tap **Start Server**.
   - Tap **Pair PC** to reveal a 6-digit PIN.

2. **Desktop (GNOME)**:
   - Click the **Apsu** icon in the GNOME Top Bar (or Quick Settings).
   - Select **Pair Phone...** and enter the 6-digit PIN.
   - Click **Mount** — your phone folders will immediately open in Nautilus as network drives!

## Screenshots
<img width="533" alt="APSU1 2026-10-24 13-24-53" src="https://github.com/user-attachments/assets/26873f09-7341-4dee-b3a4-515fd3858f4e" />
<img width="533" alt="APSU2 2026-10-24 13-24-45" src="https://github.com/user-attachments/assets/a91a2ea9-a913-431c-98e2-dc7e0a9489d6" />
<img width="533" alt="APSU3 2026-10-24 13-27-24" src="https://github.com/user-attachments/assets/0ad80434-aa62-4ced-9b1b-5060f11b4d84" />
<img width="533" alt="APSU4 Screenshot_20261024-133021" src="https://github.com/user-attachments/assets/725effac-2a38-47a8-b21b-af724cd20a3d" />


## 🤝 Call for Contributors: Bring Apsu to More Platforms!

While Apsu currently features first-class desktop integration for **GNOME Shell**, the underlying Go engine, CLI, and WebDAV/HTTP APIs are cross-platform.

We are looking for enthusiastic developers and contributors to help create native management interfaces and tray integrations for:
- **Windows** (System tray utility, Windows Explorer network location / drive mapping)
- **macOS** (Menu bar app, Finder WebDAV integration)
- **Other Linux Desktop Environments**:
  - **KDE Plasma** (Plasma widget, Dolphin / KIO integration)
  - **XFCE** (Panel applet / notification area plugin)
  - **Cinnamon** / **MATE** (Applets)
  - Generic cross-desktop system tray app (e.g., Qt, GTK, Tauri, Fyne, or Wails)

If you're interested in building a UI for your favorite platform or desktop environment, please feel free to open an issue, start a discussion, or submit a pull request!

## License
MIT License

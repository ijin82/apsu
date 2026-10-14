# qSync — Quick Sync for Android & Linux GNOME

**qSync** is a lightweight, zero-friction local Wi-Fi file sharing suite. It connects an Android phone to a Linux desktop (GNOME Shell / Nautilus) with zero cloud dependencies, no internet required, end-to-end PIN pairing, and high-speed local WebDAV transfers.

---

## Architecture

The project is structured as a monorepo containing:

1. **`core/` — Go Core WebDAV Engine & JNI Bridge**
   - High-performance WebDAV server with custom Virtual File System (VFS) routing.
   - Restricts access strictly to user-configured shared folders.
   - mDNS local network announcement (`_qsync._tcp`).
   - PIN-based mutual pairing and bearer token validation.
   - Compiled as a native C-shared JNI library (`libqsyncserver.so`) for Android via NDK.

2. **`android/` — Native Android Application (Kotlin + Jetpack Compose)**
   - Built with Material 3 and Jetpack Compose.
   - Android 14+ compatible with `ForegroundService` support to keep the server alive in background.
   - Granular folder selection via Android Storage Access Framework (SAF).
   - Direct JNI bindings (`QSyncBridge`) calling into the Go engine.

3. **`cli/` — Linux CLI & D-Bus Daemon (Go)**
   - Background D-Bus service (`org.qsync.Daemon`) for system and desktop integration.
   - Automatic mDNS discovery (`qsync scan`).
   - Secure token storage in OS Keyring via `libsecret` / SecretService.
   - Direct mounting via GVfs (`gio mount`).

4. **`gnome-extension/` — GNOME Shell Extension (GNOME 45–48)**
   - Status indicator in the GNOME Top Bar and Quick Settings.
   - Dynamic phone detection over local Wi-Fi.
   - In-extension PIN pairing modal dialog.
   - One-click drive mounting and opening in Nautilus.

---

## Building from Source

### Prerequisites
- Go 1.22+
- Docker (for cross-compiling the Android native core)
- Android Studio / Android SDK (API 34+)

### 1. Build the Android Native Core (`libqsyncserver.so`)
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
The script compiles `libqsyncserver.so` via Docker/NDK and installs it directly into `android/app/src/main/jniLibs/<abi>/`.

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

### 3. Build the Linux CLI & Daemon
```bash
cd cli
go build -o qsync ./cmd/qsync
```

### 4. Install GNOME Shell Extension
```bash
mkdir -p ~/.local/share/gnome-shell/extensions/qsync@ijin.github.io
cp gnome-extension/* ~/.local/share/gnome-shell/extensions/qsync@ijin.github.io/
gnome-extensions enable qsync@ijin.github.io
```

---

## Quick Usage

1. **Android**:
   - Open **qSync** on your phone.
   - Grant storage permissions when prompted.
   - Select the folders you want to share (e.g. `Download`, `DCIM`, `Music`).
   - Tap **Start Server**.
   - Tap **Pair PC** to reveal a 6-digit PIN.

2. **Desktop (GNOME)**:
   - Click the **qSync** icon in the GNOME Top Bar (or Quick Settings).
   - Select **Pair Phone...** and enter the 6-digit PIN.
   - Click **Mount** — your phone folders will immediately open in Nautilus as network drives!

---

## License
MIT License


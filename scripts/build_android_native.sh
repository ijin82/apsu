#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "==> Building libqsyncserver.so using Docker (Android NDK toolchain)..."

docker run --rm \
  -v "$REPO_ROOT/core":/core \
  -v "$HOME/.cache/gomobile-go":/go \
  -w /core \
  -e ANDROID_NDK_HOME=/usr/local/android_sdk/ndk-bundle \
  -e GOTOOLCHAIN=auto \
  -e GOPATH=/go \
  fyneio/fyne-cross-images:android sh -c \
  "export PATH=/go/bin:/usr/local/go/bin:\$PATH; \
   export CC='/usr/local/android_sdk/ndk-bundle/toolchains/llvm/prebuilt/linux-x86_64/bin/clang --target=aarch64-linux-android33 --sysroot=/usr/local/android_sdk/ndk-bundle/toolchains/llvm/prebuilt/linux-x86_64/sysroot'; \
   CGO_ENABLED=1 GOOS=android GOARCH=arm64 go build -buildmode=c-shared -o /core/libqsyncserver.so ./jni_bridge.go"

mkdir -p "$REPO_ROOT/android/app/src/main/jniLibs/arm64-v8a"
cp "$REPO_ROOT/core/libqsyncserver.so" "$REPO_ROOT/android/app/src/main/jniLibs/arm64-v8a/libqsyncserver.so"
rm -f "$REPO_ROOT/core/libqsyncserver.so" "$REPO_ROOT/core/libqsyncserver.h"

echo "==> Successfully installed libqsyncserver.so to android/app/src/main/jniLibs/arm64-v8a/"

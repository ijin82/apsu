#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

TARGET="${1:-arm64}"

build_arch() {
  local arch="$1"
  local goarch="$2"
  local goarm="$3"
  local clang_target="$4"
  local out_dir="$REPO_ROOT/android/app/src/main/jniLibs/$arch"

  echo "==> Building libapsuserver.so for $arch (GOARCH=$goarch)..."
  mkdir -p "$out_dir"

  docker run --rm \
    -v "$REPO_ROOT/core":/core \
    -v "$HOME/.cache/gomobile-go":/go \
    -w /core \
    -e ANDROID_NDK_HOME=/usr/local/android_sdk/ndk-bundle \
    -e GOTOOLCHAIN=auto \
    -e GOPATH=/go \
    fyneio/fyne-cross-images:android sh -c \
    "export PATH=/go/bin:/usr/local/go/bin:\$PATH; \
     printf '#!/bin/bash\n/usr/local/android_sdk/ndk-bundle/toolchains/llvm/prebuilt/linux-x86_64/bin/clang --target=%s \"\$@\"\n' '$clang_target' > /tmp/target-clang; \
     chmod +x /tmp/target-clang; \
     export CC=/tmp/target-clang; \
     CGO_ENABLED=1 GOOS=android GOARCH=$goarch GOARM=$goarm go build -ldflags="-w" -buildmode=c-shared -o /core/libapsuserver.so ./jni_bridge.go"

  cp "$REPO_ROOT/core/libapsuserver.so" "$out_dir/libapsuserver.so"
  rm -f "$REPO_ROOT/core/libapsuserver.so" "$REPO_ROOT/core/libapsuserver.h"
  echo "==> Installed: $out_dir/libapsuserver.so"
}

case "$TARGET" in
  arm64|arm64-v8a)
    build_arch "arm64-v8a" "arm64" "" "aarch64-linux-android33"
    ;;
  arm|armeabi-v7a)
    build_arch "armeabi-v7a" "arm" "7" "armv7a-linux-androideabi33"
    ;;
  x86_64)
    build_arch "x86_64" "amd64" "" "x86_64-linux-android33"
    ;;
  all)
    build_arch "arm64-v8a" "arm64" "" "aarch64-linux-android33"
    build_arch "armeabi-v7a" "arm" "7" "armv7a-linux-androideabi33"
    build_arch "x86_64" "amd64" "" "x86_64-linux-android33"
    ;;
  *)
    echo "Usage: $0 [arm64|arm|x86_64|all]"
    exit 1
    ;;
esac

echo "==> Done!"

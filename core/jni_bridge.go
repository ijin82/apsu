//go:build android

package main

/*
#include <jni.h>
#include <stdlib.h>

static const char* get_string_utf(JNIEnv* env, jstring str) {
    if (!str) return NULL;
    return (*env)->GetStringUTFChars(env, str, NULL);
}

static void release_string_utf(JNIEnv* env, jstring str, const char* chars) {
    if (str && chars) {
        (*env)->ReleaseStringUTFChars(env, str, chars);
    }
}

static jstring new_string_utf(JNIEnv* env, const char* chars) {
    if (!chars) return NULL;
    return (*env)->NewStringUTF(env, chars);
}
*/
import "C"
import (
	"os"
	"sync"
	"unsafe"

	"qsync/core/internal/auth"
	"qsync/core/internal/discovery"
	"qsync/core/internal/server"
	"qsync/core/internal/storage"
)

var (
	engineLock  sync.Mutex
	store       *storage.Manager
	pairingMgr  *auth.PairingManager
	srv         *server.Server
	broadcaster *discovery.Broadcaster
)

//export Java_io_github_ijin_qsync_QSyncBridge_initEngine
func Java_io_github_ijin_qsync_QSyncBridge_initEngine(env *C.JNIEnv, clazz C.jclass, configDir C.jstring) C.jboolean {
	engineLock.Lock()
	defer engineLock.Unlock()

	cStr := C.get_string_utf(env, configDir)
	var cfgPath string
	if cStr != nil {
		cfgPath = C.GoString(cStr)
		C.release_string_utf(env, configDir, cStr)
	}
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = home + "/.config/qsync"
	}

	if broadcaster != nil {
		broadcaster.Stop()
	}
	if srv != nil {
		_ = srv.Stop()
	}

	var err error
	store, err = storage.NewManager(cfgPath)
	if err != nil {
		return C.JNI_FALSE
	}

	pairingMgr = auth.NewPairingManager(store)
	srv = server.NewServer(store, pairingMgr)
	broadcaster = discovery.NewBroadcaster()
	return C.JNI_TRUE
}

//export Java_io_github_ijin_qsync_QSyncBridge_startServer
func Java_io_github_ijin_qsync_QSyncBridge_startServer(env *C.JNIEnv, clazz C.jclass) C.jboolean {
	engineLock.Lock()
	defer engineLock.Unlock()

	if srv == nil || store == nil {
		return C.JNI_FALSE
	}
	if srv.IsRunning() {
		return C.JNI_TRUE
	}

	cfg := store.GetConfig()
	if len(cfg.Folders) == 0 {
		return C.JNI_FALSE
	}

	if err := srv.Start(); err != nil {
		return C.JNI_FALSE
	}
	_ = broadcaster.Start(cfg.ServerName, srv.Port())
	return C.JNI_TRUE
}

//export Java_io_github_ijin_qsync_QSyncBridge_stopServer
func Java_io_github_ijin_qsync_QSyncBridge_stopServer(env *C.JNIEnv, clazz C.jclass) {
	engineLock.Lock()
	defer engineLock.Unlock()

	if broadcaster != nil {
		broadcaster.Stop()
	}
	if srv != nil {
		_ = srv.Stop()
	}
}

//export Java_io_github_ijin_qsync_QSyncBridge_isServerRunning
func Java_io_github_ijin_qsync_QSyncBridge_isServerRunning(env *C.JNIEnv, clazz C.jclass) C.jboolean {
	engineLock.Lock()
	defer engineLock.Unlock()

	if srv == nil {
		return C.JNI_FALSE
	}
	if srv.IsRunning() {
		return C.JNI_TRUE
	}
	return C.JNI_FALSE
}

//export Java_io_github_ijin_qsync_QSyncBridge_generatePIN
func Java_io_github_ijin_qsync_QSyncBridge_generatePIN(env *C.JNIEnv, clazz C.jclass) C.jstring {
	engineLock.Lock()
	defer engineLock.Unlock()

	if pairingMgr == nil {
		return C.new_string_utf(env, nil)
	}
	pin, _ := pairingMgr.GeneratePIN()
	cStr := C.CString(pin)
	defer C.free(unsafe.Pointer(cStr))
	return C.new_string_utf(env, cStr)
}

//export Java_io_github_ijin_qsync_QSyncBridge_revokeDevice
func Java_io_github_ijin_qsync_QSyncBridge_revokeDevice(env *C.JNIEnv, clazz C.jclass, tokenStr C.jstring) C.jboolean {
	engineLock.Lock()
	defer engineLock.Unlock()

	if store == nil {
		return C.JNI_FALSE
	}
	cStr := C.get_string_utf(env, tokenStr)
	if cStr == nil {
		return C.JNI_FALSE
	}
	token := C.GoString(cStr)
	C.release_string_utf(env, tokenStr, cStr)

	if err := store.RevokeDevice(token); err != nil {
		return C.JNI_FALSE
	}
	return C.JNI_TRUE
}

func main() {}

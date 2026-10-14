package io.github.ijin.qsync

object QSyncBridge {
    init {
        System.loadLibrary("qsyncserver")
    }

    external fun initEngine(configDir: String): Boolean
    external fun startServer(): Boolean
    external fun stopServer()
    external fun isServerRunning(): Boolean
    external fun generatePIN(): String?
}

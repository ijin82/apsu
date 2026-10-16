package io.github.ijin.apsu

object ApsuBridge {
    init {
        System.loadLibrary("apsuserver")
    }

    external fun initEngine(configDir: String): Boolean
    external fun startServer(): Boolean
    external fun stopServer()
    external fun isServerRunning(): Boolean
    external fun generatePIN(): String?
    external fun revokeDevice(token: String): Boolean
}

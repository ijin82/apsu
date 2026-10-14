package io.github.ijin.qsync

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat

import android.net.wifi.WifiManager
import android.os.PowerManager

class QSyncService : Service() {

    private var wifiLock: WifiManager.WifiLock? = null
    private var wakeLock: PowerManager.WakeLock? = null

    companion object {
        const val CHANNEL_ID = "qsync_service_channel"
        const val NOTIFICATION_ID = 1001
        const val ACTION_START = "io.github.ijin.qsync.START"
        const val ACTION_STOP = "io.github.ijin.qsync.STOP"

        val isRunning: Boolean
            get() = try {
                QSyncBridge.isServerRunning()
            } catch (e: Exception) {
                false
            }

        fun start(context: Context) {
            val intent = Intent(context, QSyncService::class.java).apply {
                action = ACTION_START
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stop(context: Context) {
            val intent = Intent(context, QSyncService::class.java).apply {
                action = ACTION_STOP
            }
            context.startService(intent)
        }
    }

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                releaseLocks()
                QSyncBridge.stopServer()
                stopForeground(true)
                stopSelf()
            }
            else -> {
                startForeground(NOTIFICATION_ID, buildNotification())
                acquireLocks()
                startServer()
            }
        }
        return START_NOT_STICKY
    }

    private fun acquireLocks() {
        try {
            if (wakeLock == null) {
                val powerManager = getSystemService(Context.POWER_SERVICE) as? PowerManager
                wakeLock = powerManager?.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "qSync:ServerWakeLock")?.apply {
                    setReferenceCounted(false)
                    acquire(24 * 60 * 60 * 1000L) // 24 hours safety timeout
                }
            }
            if (wifiLock == null) {
                val wifiManager = applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
                val mode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    WifiManager.WIFI_MODE_FULL_LOW_LATENCY
                } else {
                    WifiManager.WIFI_MODE_FULL_HIGH_PERF
                }
                wifiLock = wifiManager?.createWifiLock(mode, "qSync:ServerWifiLock")?.apply {
                    setReferenceCounted(false)
                    acquire()
                }
            }
        } catch (e: Exception) {
            android.util.Log.w("qSyncService", "Failed to acquire locks: ${e.message}")
        }
    }

    private fun releaseLocks() {
        try {
            wifiLock?.let {
                if (it.isHeld) it.release()
            }
            wifiLock = null

            wakeLock?.let {
                if (it.isHeld) it.release()
            }
            wakeLock = null
        } catch (e: Exception) {
            android.util.Log.w("qSyncService", "Failed to release locks: ${e.message}")
        }
    }

    private fun startServer() {
        try {
            val configDir = filesDir.absolutePath
            QSyncBridge.initEngine(configDir)
            val success = QSyncBridge.startServer()
            android.util.Log.i("qSyncNative", "startServer returned: $success")
        } catch (e: Exception) {
            android.util.Log.e("qSyncNative", "Failed to start server", e)
        }
    }

    override fun onDestroy() {
        releaseLocks()
        QSyncBridge.stopServer()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "qSync Server",
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = "Background file sharing service"
            }
            val manager = getSystemService(NotificationManager::class.java)
            manager.createNotificationChannel(channel)
        }
    }

    private fun buildNotification(): Notification {
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("qSync is Running")
            .setContentText("Local Wi-Fi file sharing is active")
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setOngoing(true)
            .build()
    }
}

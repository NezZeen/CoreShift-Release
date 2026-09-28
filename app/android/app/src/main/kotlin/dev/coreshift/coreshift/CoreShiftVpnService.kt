package dev.coreshift.coreshift

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import dev.coreshift.mobile.Mobile
import dev.coreshift.mobile.TunConfig
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * The VPN: Android routes every app but CoreShift itself into its TUN, which
 * the engine's TUN layer (sing-box, in this process) reads. CoreShift stays
 * outside, so the cores it runs reach their servers directly.
 */
class CoreShiftVpnService : VpnService() {
    private var tun: ParcelFileDescriptor? = null

    @Volatile
    private var foreground = false

    override fun onCreate() {
        super.onCreate()
        instance = this
        ready.countDown()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_DISCONNECT) {
            // From the notification: the engine closes the TUN itself.
            Thread { Mobile.disconnect() }.start()
            return START_NOT_STICKY
        }
        showNotification()
        return START_NOT_STICKY
    }

    /** Builds the VPN and returns the TUN's descriptor, owned by this service. */
    @Synchronized
    fun establish(cfg: TunConfig): Int {
        tun?.close()
        val b = Builder()
            .setSession("CoreShift")
            .setMtu(cfg.mtu)
            .addDisallowedApplication(packageName)
        val (addr4, len4) = splitPrefix(cfg.address4)
        b.addAddress(addr4, len4).addRoute("0.0.0.0", 0)
        if (cfg.address6.isNotEmpty()) {
            val (addr6, len6) = splitPrefix(cfg.address6)
            b.addAddress(addr6, len6).addRoute("::", 0)
        }
        b.addDnsServer(cfg.dns)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) b.setMetered(false)
        b.setConfigureIntent(openAppIntent())
        val pfd = b.establish() ?: throw IllegalStateException("VPN permission is not granted")
        tun = pfd
        showNotification()
        return pfd.fd
    }

    /** Stops the service if no VPN was built, as after a failed autostart. */
    @Synchronized
    fun stopIfIdle() {
        if (tun == null) shutdown()
    }

    /** Closes the TUN and stops the service. */
    @Synchronized
    fun shutdown() {
        tun?.close()
        tun = null
        foreground = false
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onRevoke() {
        // Another VPN app took over, or the user turned CoreShift off in the
        // system settings.
        Thread { Mobile.revoked() }.start()
    }

    override fun onDestroy() {
        synchronized(this) {
            tun?.close()
            tun = null
        }
        if (instance === this) {
            instance = null
            ready = CountDownLatch(1)
        }
        super.onDestroy()
    }

    private fun splitPrefix(p: String): Pair<String, Int> {
        val i = p.lastIndexOf('/')
        return p.substring(0, i) to p.substring(i + 1).toInt()
    }

    private fun openAppIntent(): PendingIntent = PendingIntent.getActivity(
        this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
    )

    /** Puts the service in the foreground with the connection's notification. */
    private fun showNotification() {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, "VPN", NotificationManager.IMPORTANCE_LOW).apply {
                description = "CoreShift подключён"
                setShowBadge(false)
            },
        )
        val n = buildNotification()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION_ID, n)
        }
        foreground = true
    }

    /** Shows the engine's latest state and speed in the notification. */
    fun refreshNotification() {
        if (!foreground) return
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, buildNotification())
    }

    /**
     * As Happ has it: the server, the time connected and the speed, and a
     * button to disconnect.
     */
    private fun buildNotification(): Notification {
        val s = VpnStatus
        val disconnect = PendingIntent.getService(
            this, 1, Intent(this, CoreShiftVpnService::class.java).setAction(ACTION_DISCONNECT), PendingIntent.FLAG_IMMUTABLE,
        )
        val b = Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_vpn)
            .setContentIntent(openAppIntent())
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(Notification.CATEGORY_SERVICE)
            .addAction(Notification.Action.Builder(null, "Отключить", disconnect).build())
        if (tun != null && s.state == "connected") {
            b.setContentTitle(s.node.ifEmpty { "CoreShift" })
                .setContentText("↓ ${VpnStatus.formatRate(s.down)}    ↑ ${VpnStatus.formatRate(s.up)}")
                .setSubText("Подключено")
            if (s.since > 0) b.setWhen(s.since).setUsesChronometer(true).setShowWhen(true)
        } else {
            b.setContentTitle(if (s.state == "disconnecting") "Отключение…" else "Подключение…")
                .setContentText(s.node.ifEmpty { "CoreShift" })
                .setShowWhen(false)
        }
        return b.build()
    }

    companion object {
        private const val CHANNEL = "vpn"
        private const val NOTIFICATION_ID = 1
        private const val ACTION_DISCONNECT = "dev.coreshift.DISCONNECT"
        /** Started before the engine connects: at boot, from the tile. */
        const val ACTION_CONNECTING = "dev.coreshift.CONNECTING"

        @Volatile
        private var instance: CoreShiftVpnService? = null

        @Volatile
        private var ready = CountDownLatch(1)

        fun current(): CoreShiftVpnService? = instance

        /** Waits for the service started by startForegroundService. */
        fun awaitInstance(timeout: Long, unit: TimeUnit): CoreShiftVpnService? {
            instance?.let { return it }
            ready.await(timeout, unit)
            return instance
        }
    }
}

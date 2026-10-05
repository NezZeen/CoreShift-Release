package dev.coreshift.coreshift

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.net.IpPrefix
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import dev.coreshift.mobile.Mobile
import dev.coreshift.mobile.TunConfig
import java.net.InetAddress
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
        applyAppFilter(b, cfg)
        val (addr4, len4) = splitPrefix(cfg.address4)
        b.addAddress(addr4, len4).addRoute("0.0.0.0", 0)
        if (cfg.address6.isNotEmpty()) {
            val (addr6, len6) = splitPrefix(cfg.address6)
            b.addAddress(addr6, len6).addRoute("::", 0)
        }
        excludeLocalNetwork(b, cfg)
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
        // Waiting for a network keeps the service, and with it the engine's
        // process, until the connection is made or cancelled.
        if (tun == null && VpnStatus.state != "no-network") shutdown()
    }

    /**
     * Which apps use the VPN. CoreShift itself never does: its cores reach
     * their servers directly. Android takes either allowed apps or
     * disallowed ones, not both, and skips none: an app that is no longer
     * installed would throw, so each is added on its own.
     */
    private fun applyAppFilter(b: Builder, cfg: TunConfig) {
        fun lines(s: String) = s.split('\n').map { it.trim() }.filter { it.isNotEmpty() && it != packageName }
        var allowed = 0
        for (app in lines(cfg.allowedApps)) {
            try {
                b.addAllowedApplication(app)
                allowed++
            } catch (_: PackageManager.NameNotFoundException) {
            }
        }
        // None of the chosen apps is installed: with no allowed app every
        // app, CoreShift too, would be in the VPN.
        if (allowed > 0) return
        b.addDisallowedApplication(packageName)
        for (app in lines(cfg.disallowedApps)) {
            try {
                b.addDisallowedApplication(app)
            } catch (_: PackageManager.NameNotFoundException) {
            }
        }
    }

    /**
     * Keeps the local network out of the VPN (Android 13 and later can), so
     * a printer, a NAS or the router's page is reached by the device's own
     * routes. Earlier versions route it into the TUN, which sends it direct.
     * A range Android does not take is skipped on its own.
     */
    private fun excludeLocalNetwork(b: Builder, cfg: TunConfig) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        for (line in cfg.excludeRoutes.split('\n').map { it.trim() }.filter { it.isNotEmpty() }) {
            try {
                val (addr, len) = splitPrefix(line)
                b.excludeRoute(IpPrefix(InetAddress.getByName(addr), len))
            } catch (_: Exception) {
            }
        }
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
        } else if (s.state == "no-network") {
            b.setContentTitle("Нет сети")
                .setContentText(
                    if (tun != null) "VPN продолжит работу, когда сеть вернётся" else "Подключусь, когда появится сеть",
                )
                .setSubText(s.node.ifEmpty { "CoreShift" })
                .setShowWhen(false)
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

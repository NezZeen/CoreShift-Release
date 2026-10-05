package dev.coreshift.coreshift

import android.annotation.SuppressLint
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import dev.coreshift.mobile.Mobile
import dev.coreshift.mobile.Platform
import dev.coreshift.mobile.TunConfig
import java.io.File
import java.net.NetworkInterface
import java.util.concurrent.TimeUnit

/**
 * The Go engine (engine/mobile, bound with gomobile), running in the app's
 * process for as long as the process lives. The Flutter UI talks to it over
 * HTTP on 127.0.0.1, the address and token in [apiFile], as on the desktop.
 */
object Engine {
    private const val TAG = "CoreShift"

    fun dataDir(context: Context) = File(context.filesDir, "engine")

    fun apiFile(context: Context) = File(dataDir(context), "api.json")

    @Volatile
    private var started = false

    @SuppressLint("HardwareIds")
    @Synchronized
    fun start(context: Context) {
        if (started) return
        val app = context.applicationContext
        val dir = dataDir(app).apply { mkdirs() }
        val androidId = Settings.Secure.getString(app.contentResolver, Settings.Secure.ANDROID_ID) ?: ""
        val model = listOf(Build.MANUFACTURER, Build.MODEL).filter { it.isNotBlank() }.distinct().joinToString(" ")
        try {
            Mobile.start(dir.absolutePath, app.applicationInfo.nativeLibraryDir, androidId, Build.VERSION.RELEASE, model, VpnPlatform(app))
            started = true
        } catch (e: Exception) {
            Log.e(TAG, "engine did not start", e)
            return
        }
        Mobile.currentStatus().let { VpnStatus.set(it.state, it.node, it.sinceMillis) }
        watchNetwork(app)
        watchScreen(app)
    }

    /**
     * Tells the engine whether the screen is on: with it off the engine
     * checks the connection and samples the traffic less often, and leaves
     * the notification alone, so the phone sleeps more.
     */
    private fun watchScreen(context: Context) {
        val receiver = object : BroadcastReceiver() {
            override fun onReceive(c: Context, intent: Intent) {
                when (intent.action) {
                    Intent.ACTION_SCREEN_ON -> Mobile.setScreenOn(true)
                    Intent.ACTION_SCREEN_OFF -> Mobile.setScreenOn(false)
                }
            }
        }
        val filter = IntentFilter().apply {
            addAction(Intent.ACTION_SCREEN_ON)
            addAction(Intent.ACTION_SCREEN_OFF)
        }
        // System broadcasts reach a receiver that is not exported too.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            context.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            context.registerReceiver(receiver, filter)
        }
        Mobile.setScreenOn(context.getSystemService(PowerManager::class.java).isInteractive)
    }

    /** Connects the selected server in the background, from the tile. */
    fun connect() {
        Thread {
            try {
                Mobile.connect()
            } catch (e: Exception) {
                Log.w(TAG, "connect from the tile: ${e.message}")
                CoreShiftVpnService.current()?.stopIfIdle()
            }
        }.start()
    }

    /**
     * Connects the selected server if "Автозапуск" is on, in the background.
     * A service started for it that ends up without a VPN goes away.
     */
    fun autoConnect() {
        Thread {
            if (!Mobile.autoConnect()) CoreShiftVpnService.current()?.stopIfIdle()
        }.start()
    }

    /**
     * Tells the engine which network the phone uses. The app is outside its
     * own VPN, so its default network is the real one, also while connected.
     */
    private fun watchNetwork(context: Context) {
        val cm = context.getSystemService(ConnectivityManager::class.java)
        cm.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
            override fun onLinkPropertiesChanged(network: Network, lp: LinkProperties) = report(network, lp)

            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                cm.getLinkProperties(network)?.let { report(network, it) }
            }

            // Only the network reported last: when Wi-Fi hands over to mobile
            // data the old one may be lost after the new one came, and the
            // phone is not offline then.
            override fun onLost(network: Network) {
                if (network != current) return
                current = null
                Mobile.setNetwork("", 0, "", "")
            }
        })
    }

    /** The default network the engine was told of last. */
    @Volatile
    private var current: Network? = null

    private fun report(network: Network, lp: LinkProperties) {
        val name = lp.interfaceName ?: return
        current = network
        val index = try {
            NetworkInterface.getByName(name)?.index ?: 0
        } catch (_: Exception) {
            0
        }
        val addresses = lp.linkAddresses.joinToString(",") { la ->
            "${la.address.hostAddress?.substringBefore('%') ?: ""}/${la.prefixLength}"
        }
        val dns = lp.dnsServers.joinToString(",") { it.hostAddress?.substringBefore('%') ?: "" }
        Mobile.setNetwork(name, index, addresses, dns)
    }

    /** What the engine needs from Android: the VpnService's TUN, the installer. */
    private class VpnPlatform(private val context: Context) : Platform {
        override fun openTun(cfg: TunConfig): Int {
            // Started at boot, the service already runs in the foreground;
            // starting it again from the background Android may refuse.
            if (CoreShiftVpnService.current() == null) {
                context.startForegroundService(Intent(context, CoreShiftVpnService::class.java))
            }
            val service = CoreShiftVpnService.awaitInstance(10, TimeUnit.SECONDS)
                ?: throw IllegalStateException("the VPN service did not start")
            return service.establish(cfg)
        }

        override fun closeTun() {
            CoreShiftVpnService.current()?.shutdown()
        }

        override fun installUpdate(path: String) = Updater.install(context, path)

        override fun stateChanged(state: String, node: String, sinceMillis: Long) {
            VpnStatus.set(state, node, sinceMillis)
            CoreShiftVpnService.current()?.refreshNotification()
            // A wait for the network that ended without a VPN (cancelled,
            // failed) leaves no TUN to close: the service goes by itself.
            if (state == "idle" || state == "failed") CoreShiftVpnService.current()?.stopIfIdle()
            VpnTileService.refresh(context)
        }

        override fun traffic(downRate: Long, upRate: Long) {
            VpnStatus.setTraffic(downRate, upRate)
            CoreShiftVpnService.current()?.refreshNotification()
        }
    }
}

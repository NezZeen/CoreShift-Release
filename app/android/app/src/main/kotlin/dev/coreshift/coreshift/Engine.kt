package dev.coreshift.coreshift

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Build
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
        watchNetwork(app)
    }

    /**
     * Tells the engine which network the phone uses. The app is outside its
     * own VPN, so its default network is the real one, also while connected.
     */
    private fun watchNetwork(context: Context) {
        val cm = context.getSystemService(ConnectivityManager::class.java)
        cm.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
            override fun onLinkPropertiesChanged(network: Network, lp: LinkProperties) = report(lp)

            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                cm.getLinkProperties(network)?.let { report(it) }
            }

            override fun onLost(network: Network) = Mobile.setNetwork("", 0, "", "")
        })
    }

    private fun report(lp: LinkProperties) {
        val name = lp.interfaceName ?: return
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
            val intent = Intent(context, CoreShiftVpnService::class.java)
            context.startForegroundService(intent)
            val service = CoreShiftVpnService.awaitInstance(10, TimeUnit.SECONDS)
                ?: throw IllegalStateException("the VPN service did not start")
            return service.establish(cfg)
        }

        override fun closeTun() {
            CoreShiftVpnService.current()?.shutdown()
        }

        override fun installUpdate(path: String) = Updater.install(context, path)
    }
}

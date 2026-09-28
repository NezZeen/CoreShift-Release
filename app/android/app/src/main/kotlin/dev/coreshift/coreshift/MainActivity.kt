package dev.coreshift.coreshift

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/**
 * The Flutter UI. The channel gives it what only Android has: where the
 * engine's API file is, the user's consent to the VPN and to installing
 * updates, and a way to open links such as the panel's support chat.
 */
class MainActivity : FlutterActivity() {
    private var pendingVpn: MethodChannel.Result? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // "Автозапуск" connects when CoreShift opens, once the VPN is allowed;
        // not when Android only recreates the screen.
        if (savedInstanceState == null && VpnService.prepare(this) == null) Engine.autoConnect()
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        Engine.start(this)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "coreshift/android").setMethodCallHandler { call, result ->
            when (call.method) {
                "apiFile" -> result.success(Engine.apiFile(this).absolutePath)
                "prepareVpn" -> prepareVpn(result)
                "canInstallUpdates" -> result.success(Updater.canInstall(this))
                "allowInstallUpdates" -> {
                    Updater.askPermission(this)
                    result.success(null)
                }
                "openUrl" -> result.success(openUrl(call.arguments as String))
                else -> result.notImplemented()
            }
        }
    }

    /** Opens a link in the app that handles it; false when there is none. */
    private fun openUrl(url: String): Boolean = try {
        startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
        true
    } catch (e: ActivityNotFoundException) {
        false
    }

    /** Asks Android for the VPN once; answers whether it is allowed. */
    private fun prepareVpn(result: MethodChannel.Result) {
        val intent = VpnService.prepare(this)
        if (intent == null) {
            askNotifications()
            result.success(true)
            return
        }
        pendingVpn?.success(false)
        pendingVpn = result
        @Suppress("DEPRECATION")
        startActivityForResult(intent, REQUEST_VPN)
    }

    /** The VPN's notification needs this since Android 13; it is optional. */
    private fun askNotifications() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQUEST_NOTIFICATIONS)
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == REQUEST_VPN) {
            val ok = resultCode == RESULT_OK
            if (ok) askNotifications()
            pendingVpn?.success(ok)
            pendingVpn = null
        }
    }

    companion object {
        private const val REQUEST_VPN = 1
        private const val REQUEST_NOTIFICATIONS = 2
    }
}

package dev.coreshift.coreshift

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Canvas
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.SystemClock
import android.provider.Settings
import android.util.Log
import com.google.mlkit.common.MlKitException
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream

/**
 * The Flutter UI. The channel gives it what only Android has: where the
 * engine's API file is, the user's consent to the VPN and to installing
 * updates, a way to open links such as the panel's support chat, the links
 * panels open CoreShift with, the QR scanner and notifications.
 */
class MainActivity : FlutterActivity() {
    private var pendingVpn: MethodChannel.Result? = null

    /** When the VPN request was started, to tell a refusal without it. */
    private var vpnAskedAt = 0L

    private var channel: MethodChannel? = null

    /** The link CoreShift was opened with, until the UI asks for it. */
    private var startLink: String? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // "Автозапуск" connects when CoreShift opens, once the VPN is allowed;
        // not when Android only recreates the screen.
        if (savedInstanceState == null && VpnService.prepare(this) == null) Engine.autoConnect()
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        Engine.start(this)
        startLink = linkOf(intent)
        channel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "coreshift/android").apply {
            setMethodCallHandler { call, result ->
                when (call.method) {
                    "apiFile" -> result.success(Engine.apiFile(this@MainActivity).absolutePath)
                    "prepareVpn" -> prepareVpn(result)
                    "openVpnSettings" -> result.success(openVpnSettings())
                    "canInstallUpdates" -> result.success(Updater.canInstall(this@MainActivity))
                    "allowInstallUpdates" -> {
                        Updater.askPermission(this@MainActivity)
                        result.success(null)
                    }
                    "openUrl" -> result.success(openUrl(call.arguments as String))
                    "apps" -> Thread { val apps = installedApps(); runOnUiThread { result.success(apps) } }.start()
                    "appIcon" -> Thread { val icon = appIcon(call.arguments as String); runOnUiThread { result.success(icon) } }.start()
                    "initialLink" -> {
                        result.success(startLink)
                        startLink = null
                    }
                    "scanQr" -> scanQr(result)
                    "notify" -> {
                        notify(call.argument<String>("title") ?: "", call.argument<String>("body") ?: "")
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
        }
    }

    /** A panel's "add to app" link opened while CoreShift runs. */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        linkOf(intent)?.let { channel?.invokeMethod("openLink", it) }
    }

    private fun linkOf(intent: Intent?): String? =
        if (intent?.action == Intent.ACTION_VIEW) intent.dataString else null

    /**
     * Google's code scanner: Play services show the camera and hand back the
     * text, so CoreShift needs no camera permission of its own.
     */
    private fun scanQr(result: MethodChannel.Result) {
        val options = GmsBarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build()
        GmsBarcodeScanning.getClient(this, options).startScan()
            .addOnSuccessListener { result.success(it.rawValue) }
            .addOnCanceledListener { result.success(null) }
            .addOnFailureListener {
                Log.w("CoreShift", "QR scanner: ${(it as? MlKitException)?.errorCode} $it")
                // Play services fetch the scanner on first use: until then it is "unavailable".
                val code = if (it is MlKitException && it.errorCode == MlKitException.CODE_SCANNER_UNAVAILABLE) "unavailable" else "scan"
                result.error(code, it.message, null)
            }
    }

    /** A notification about a subscription; a tap opens CoreShift. */
    private fun notify(title: String, body: String) {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(ALERTS, "Подписка", NotificationManager.IMPORTANCE_DEFAULT).apply {
                description = "Окончание срока и трафика подписки"
            },
        )
        val open = PendingIntent.getActivity(this, 2, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val n = Notification.Builder(this, ALERTS)
            .setSmallIcon(R.drawable.ic_stat_vpn)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(Notification.BigTextStyle().bigText(body))
            .setContentIntent(open)
            .setAutoCancel(true)
            .build()
        // One notification per title: the same warning replaces itself.
        nm.notify(title.hashCode(), n)
    }

    /**
     * The apps a user can choose to put into the VPN or leave out of it:
     * those with a launcher icon, but CoreShift.
     */
    private fun installedApps(): List<Map<String, Any>> {
        val pm = packageManager
        val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        return pm.queryIntentActivities(launcher, 0)
            .map { it.activityInfo.applicationInfo }
            .distinctBy { it.packageName }
            .filter { it.packageName != packageName }
            .map {
                mapOf(
                    "package" to it.packageName,
                    "label" to pm.getApplicationLabel(it).toString(),
                    // Preinstalled and never updated. Chrome, YouTube or Gmail
                    // come with the phone too, but the Play Store updates them:
                    // they are the user's apps, not hidden behind «Показывать
                    // системные».
                    "system" to ((it.flags and ApplicationInfo.FLAG_SYSTEM) != 0 &&
                        (it.flags and ApplicationInfo.FLAG_UPDATED_SYSTEM_APP) == 0),
                )
            }
    }

    /** The app's icon as a PNG, 96 pixels square; null when there is none. */
    private fun appIcon(pkg: String): ByteArray? = try {
        val d = packageManager.getApplicationIcon(pkg)
        val size = 96
        val bmp = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(bmp)
        d.setBounds(0, 0, size, size)
        d.draw(canvas)
        val out = ByteArrayOutputStream()
        bmp.compress(Bitmap.CompressFormat.PNG, 100, out)
        out.toByteArray()
    } catch (_: Exception) {
        null
    }

    /** Opens a link in the app that handles it; false when there is none. */
    private fun openUrl(url: String): Boolean = try {
        startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
        true
    } catch (e: ActivityNotFoundException) {
        false
    }

    /**
     * Asks Android for the VPN once. Answers "granted", "denied" (the user
     * declined), or "unasked": refused at once, without the request on the
     * screen, as Android does while another app is the always-on VPN.
     */
    private fun prepareVpn(result: MethodChannel.Result) {
        val intent = VpnService.prepare(this)
        if (intent == null) {
            askNotifications()
            result.success("granted")
            return
        }
        pendingVpn?.success("denied")
        pendingVpn = result
        vpnAskedAt = SystemClock.elapsedRealtime()
        try {
            @Suppress("DEPRECATION")
            startActivityForResult(intent, REQUEST_VPN)
        } catch (e: ActivityNotFoundException) {
            Log.w("CoreShift", "VPN consent: $e")
            pendingVpn = null
            result.success("unasked")
        }
    }

    /**
     * Android's VPN settings, where another app's always-on VPN is turned
     * off; the network settings, or the settings at all, where a phone has
     * no such screen. False when none opened.
     */
    private fun openVpnSettings(): Boolean {
        for (action in listOf(Settings.ACTION_VPN_SETTINGS, Settings.ACTION_WIRELESS_SETTINGS, Settings.ACTION_SETTINGS)) {
            try {
                startActivity(Intent(action))
                return true
            } catch (e: Exception) {
                Log.w("CoreShift", "$action: $e")
            }
        }
        return false
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
            val answer = when {
                resultCode == RESULT_OK -> "granted"
                // Nobody reads and declines a request this fast: it was not shown.
                SystemClock.elapsedRealtime() - vpnAskedAt < UNASKED_WITHIN_MS -> "unasked"
                else -> "denied"
            }
            if (answer == "granted") askNotifications()
            pendingVpn?.success(answer)
            pendingVpn = null
        }
    }

    companion object {
        private const val REQUEST_VPN = 1
        private const val UNASKED_WITHIN_MS = 800L
        private const val REQUEST_NOTIFICATIONS = 2
        private const val ALERTS = "alerts"
    }
}

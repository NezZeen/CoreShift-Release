package dev.coreshift.coreshift

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService
import dev.coreshift.mobile.Mobile

/**
 * "Автозапуск": connects the selected server when the phone starts, if the
 * user allowed CoreShift's VPN before. The engine starts with the process
 * (CoreShiftApp).
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        if (VpnService.prepare(context) != null || !Mobile.autoConnectEnabled()) return
        // In the foreground now, while Android still allows it after boot;
        // the engine then builds the VPN in the running service.
        context.startForegroundService(
            Intent(context, CoreShiftVpnService::class.java).setAction(CoreShiftVpnService.ACTION_AUTOSTART),
        )
        Engine.autoConnect()
    }
}
